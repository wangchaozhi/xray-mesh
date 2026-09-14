package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/health"
	"github.com/wangchaozhi/xray-mesh/internal/telemetry"
)

var pingRTTRE = regexp.MustCompile(`time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)

type config struct {
	statusURL       string
	peerIP          netip.Addr
	samples         int
	pingTimeout     time.Duration
	pollInterval    time.Duration
	directTimeout   time.Duration
	forceFallback   bool
	relayAddr       string
	fallbackTimeout time.Duration
	fallbackQuiet   time.Duration
	recoveryTimeout time.Duration
	jsonOutput      bool
}

type pingSummary struct {
	Attempts int     `json:"attempts"`
	Success  int     `json:"success"`
	MinMS    float64 `json:"min_ms,omitempty"`
	AvgMS    float64 `json:"avg_ms,omitempty"`
	MaxMS    float64 `json:"max_ms,omitempty"`
}

type phaseResult struct {
	Name           string                `json:"name"`
	Passed         bool                  `json:"passed"`
	DurationMS     int64                 `json:"duration_ms"`
	Ping           pingSummary           `json:"ping"`
	CountersBefore telemetry.P2PSnapshot `json:"counters_before"`
	CountersAfter  telemetry.P2PSnapshot `json:"counters_after"`
	Note           string                `json:"note,omitempty"`
}

type report struct {
	NodeID     string                `json:"node_id"`
	VirtualIP  string                `json:"virtual_ip"`
	PeerIP     string                `json:"peer_ip"`
	StatusURL  string                `json:"status_url"`
	Relay      string                `json:"relay,omitempty"`
	StartedAt  time.Time             `json:"started_at"`
	FinishedAt time.Time             `json:"finished_at"`
	Passed     bool                  `json:"passed"`
	Phases     []phaseResult         `json:"phases"`
	FinalP2P   telemetry.P2PSnapshot `json:"final_p2p"`
}

type checker struct {
	cfg    config
	client *http.Client
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	c := &checker{cfg: cfg, client: &http.Client{Timeout: 3 * time.Second}}
	rep, runErr := c.run(ctx)
	if cfg.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		printReport(rep)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "meshcheck:", runErr)
		os.Exit(1)
	}
}

func parseFlags() (config, error) {
	statusURL := flag.String("status", "http://127.0.0.1:8670/v1/status", "local client status endpoint")
	peer := flag.String("peer", "", "peer mesh IPv4 address, for example 10.66.0.3")
	samples := flag.Int("samples", 5, "successful ping samples per measurement phase")
	pingTimeout := flag.Duration("ping-timeout", 2*time.Second, "timeout for one ping")
	pollInterval := flag.Duration("poll-interval", time.Second, "interval while waiting for path transitions")
	directTimeout := flag.Duration("direct-timeout", 25*time.Second, "maximum time to observe encrypted direct TX")
	forceFallback := flag.Bool("force-fallback", false, "temporarily block non-relay outbound UDP to verify relay fallback (Linux root only)")
	relay := flag.String("relay", "", "relay UDP endpoint host:port; required with -force-fallback")
	fallbackTimeout := flag.Duration("fallback-timeout", 55*time.Second, "maximum time to observe relay fallback after direct UDP is blocked")
	fallbackQuiet := flag.Duration("fallback-quiet", 3*time.Second, "required period with no direct_tx increase before fallback is accepted")
	recoveryTimeout := flag.Duration("recovery-timeout", 25*time.Second, "maximum time for direct path to recover after firewall cleanup")
	jsonOutput := flag.Bool("json", false, "emit a JSON report")
	flag.Parse()

	peerIP, err := netip.ParseAddr(strings.TrimSpace(*peer))
	if err != nil || !peerIP.Is4() {
		return config{}, errors.New("-peer must be a valid IPv4 mesh address")
	}
	if strings.TrimSpace(*statusURL) == "" {
		return config{}, errors.New("-status must not be empty")
	}
	if *samples <= 0 {
		return config{}, errors.New("-samples must be greater than zero")
	}
	for name, d := range map[string]time.Duration{
		"-ping-timeout":     *pingTimeout,
		"-poll-interval":    *pollInterval,
		"-direct-timeout":   *directTimeout,
		"-fallback-timeout": *fallbackTimeout,
		"-fallback-quiet":   *fallbackQuiet,
		"-recovery-timeout": *recoveryTimeout,
	} {
		if d <= 0 {
			return config{}, fmt.Errorf("%s must be greater than zero", name)
		}
	}
	if *forceFallback && strings.TrimSpace(*relay) == "" {
		return config{}, errors.New("-relay is required with -force-fallback")
	}
	return config{
		statusURL:       strings.TrimSpace(*statusURL),
		peerIP:          peerIP,
		samples:         *samples,
		pingTimeout:     *pingTimeout,
		pollInterval:    *pollInterval,
		directTimeout:   *directTimeout,
		forceFallback:   *forceFallback,
		relayAddr:       strings.TrimSpace(*relay),
		fallbackTimeout: *fallbackTimeout,
		fallbackQuiet:   *fallbackQuiet,
		recoveryTimeout: *recoveryTimeout,
		jsonOutput:      *jsonOutput,
	}, nil
}

func (c *checker) run(ctx context.Context) (report, error) {
	rep := report{PeerIP: c.cfg.peerIP.String(), StatusURL: c.cfg.statusURL, Relay: c.cfg.relayAddr, StartedAt: time.Now().UTC()}
	if _, err := exec.LookPath("ping"); err != nil {
		return c.finish(rep, errors.New("ping command not found"))
	}

	status, err := c.readStatus(ctx)
	if err != nil {
		return c.finish(rep, fmt.Errorf("read local status: %w", err))
	}
	rep.NodeID = status.NodeID
	rep.VirtualIP = status.VirtualIP
	if !status.Mesh.Enabled || status.Mesh.State != health.StateRunning {
		return c.finish(rep, fmt.Errorf("mesh service is not running: enabled=%t state=%s", status.Mesh.Enabled, status.Mesh.State))
	}

	baseline, err := c.measurePhase(ctx, "baseline-connectivity", c.cfg.samples, "mesh peer must be reachable before path validation")
	rep.Phases = append(rep.Phases, baseline)
	if err != nil {
		return c.finish(rep, err)
	}

	direct, err := c.waitForDirect(ctx, "direct-path", c.cfg.directTimeout)
	rep.Phases = append(rep.Phases, direct)
	if err != nil {
		return c.finish(rep, err)
	}

	if c.cfg.forceFallback {
		fallback, fallbackErr := c.verifyFallback(ctx)
		rep.Phases = append(rep.Phases, fallback)
		if fallbackErr != nil {
			return c.finish(rep, fallbackErr)
		}
		recovery, recoveryErr := c.waitForDirect(ctx, "direct-recovery", c.cfg.recoveryTimeout)
		rep.Phases = append(rep.Phases, recovery)
		if recoveryErr != nil {
			return c.finish(rep, recoveryErr)
		}
	}

	return c.finish(rep, nil)
}

func (c *checker) finish(rep report, err error) (report, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if status, statusErr := c.readStatus(ctx); statusErr == nil {
		rep.FinalP2P = status.P2P
	}
	rep.FinishedAt = time.Now().UTC()
	rep.Passed = err == nil
	return rep, err
}

func (c *checker) measurePhase(ctx context.Context, name string, count int, note string) (phaseResult, error) {
	start := time.Now()
	before, err := c.readStatus(ctx)
	if err != nil {
		return phaseResult{Name: name, Note: note}, err
	}
	rtts, attempts := c.collectPings(ctx, count, count)
	after, statusErr := c.readStatus(ctx)
	phase := phaseResult{Name: name, DurationMS: time.Since(start).Milliseconds(), Ping: summarizePings(attempts, rtts), CountersBefore: before.P2P, Note: note}
	if statusErr == nil {
		phase.CountersAfter = after.P2P
	}
	phase.Passed = statusErr == nil && len(rtts) == count
	if statusErr != nil {
		return phase, statusErr
	}
	if len(rtts) != count {
		return phase, fmt.Errorf("%s: %d/%d ping samples succeeded", name, len(rtts), count)
	}
	return phase, nil
}

func (c *checker) waitForDirect(ctx context.Context, name string, timeout time.Duration) (phaseResult, error) {
	start := time.Now()
	before, err := c.readStatus(ctx)
	if err != nil {
		return phaseResult{Name: name}, err
	}
	phaseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rtts := make([]time.Duration, 0, c.cfg.samples)
	attempts := 0

	for phaseCtx.Err() == nil {
		attempts++
		rtt, pingErr := pingOnce(phaseCtx, c.cfg.peerIP.String(), c.cfg.pingTimeout)
		if pingErr == nil {
			rtts = append(rtts, rtt)
		}
		status, statusErr := c.readStatus(phaseCtx)
		if statusErr != nil {
			if phaseCtx.Err() != nil {
				break
			}
			return phaseResult{Name: name}, statusErr
		}
		if status.P2P.DirectTX > before.P2P.DirectTX && len(rtts) > 0 {
			need := c.cfg.samples - len(rtts)
			if need > 0 {
				more, moreAttempts := c.collectPings(phaseCtx, need, need*2)
				attempts += moreAttempts
				rtts = append(rtts, more...)
			}
			after, _ := c.readStatus(context.Background())
			phase := phaseResult{Name: name, DurationMS: time.Since(start).Milliseconds(), Ping: summarizePings(attempts, rtts), CountersBefore: before.P2P, CountersAfter: after.P2P, Note: "direct_tx increased while peer traffic remained reachable"}
			phase.Passed = len(rtts) >= c.cfg.samples
			if !phase.Passed {
				return phase, fmt.Errorf("%s: direct path observed but only %d/%d successful ping samples were collected", name, len(rtts), c.cfg.samples)
			}
			return phase, nil
		}
		if err := sleepContext(phaseCtx, c.cfg.pollInterval); err != nil {
			break
		}
	}
	after, _ := c.readStatus(context.Background())
	phase := phaseResult{Name: name, Passed: false, DurationMS: time.Since(start).Milliseconds(), Ping: summarizePings(attempts, rtts), CountersBefore: before.P2P, CountersAfter: after.P2P, Note: "timed out waiting for direct_tx to increase"}
	return phase, fmt.Errorf("%s: direct path not observed within %s", name, timeout)
}

func (c *checker) verifyFallback(ctx context.Context) (phaseResult, error) {
	start := time.Now()
	before, err := c.readStatus(ctx)
	if err != nil {
		return phaseResult{Name: "relay-fallback"}, err
	}
	fw, err := installRelayOnlyFirewall(ctx, c.cfg.relayAddr)
	if err != nil {
		return phaseResult{Name: "relay-fallback", CountersBefore: before.P2P}, err
	}
	closed := false
	defer func() {
		if closed {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = fw.Close(cleanupCtx)
	}()

	phaseCtx, cancel := context.WithTimeout(ctx, c.cfg.fallbackTimeout)
	defer cancel()
	lastDirectTX := before.P2P.DirectTX
	lastDirectChange := time.Now()
	attempts := 0
	rtts := make([]time.Duration, 0, c.cfg.samples+1)

	for phaseCtx.Err() == nil {
		attempts++
		rtt, pingErr := pingOnce(phaseCtx, c.cfg.peerIP.String(), c.cfg.pingTimeout)
		status, statusErr := c.readStatus(phaseCtx)
		if statusErr != nil {
			if phaseCtx.Err() != nil {
				break
			}
			return phaseResult{Name: "relay-fallback"}, statusErr
		}
		if status.P2P.DirectTX != lastDirectTX {
			lastDirectTX = status.P2P.DirectTX
			lastDirectChange = time.Now()
		}
		if pingErr == nil && time.Since(lastDirectChange) >= c.cfg.fallbackQuiet {
			confirmBefore := status.P2P.DirectTX
			confirmRTTs, confirmAttempts := c.collectPings(phaseCtx, c.cfg.samples, c.cfg.samples)
			attempts += confirmAttempts
			rtts = append(rtts, rtt)
			rtts = append(rtts, confirmRTTs...)
			confirmStatus, confirmStatusErr := c.readStatus(phaseCtx)
			if confirmStatusErr != nil {
				return phaseResult{Name: "relay-fallback"}, confirmStatusErr
			}
			if len(confirmRTTs) == c.cfg.samples && confirmStatus.P2P.DirectTX == confirmBefore {
				phase := phaseResult{Name: "relay-fallback", Passed: true, DurationMS: time.Since(start).Milliseconds(), Ping: summarizePings(attempts, rtts), CountersBefore: before.P2P, CountersAfter: confirmStatus.P2P, Note: "peer stayed reachable while direct_tx remained unchanged with non-relay UDP blocked"}
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				cleanupErr := fw.Close(cleanupCtx)
				cleanupCancel()
				closed = true
				if cleanupErr != nil {
					phase.Passed = false
					return phase, fmt.Errorf("cleanup fallback firewall: %w", cleanupErr)
				}
				return phase, nil
			}
		}
		if err := sleepContext(phaseCtx, c.cfg.pollInterval); err != nil {
			break
		}
	}
	after, _ := c.readStatus(context.Background())
	phase := phaseResult{Name: "relay-fallback", Passed: false, DurationMS: time.Since(start).Milliseconds(), Ping: summarizePings(attempts, rtts), CountersBefore: before.P2P, CountersAfter: after.P2P, Note: "timed out before relay-only reachability was confirmed"}
	return phase, fmt.Errorf("relay fallback not confirmed within %s", c.cfg.fallbackTimeout)
}

func (c *checker) collectPings(ctx context.Context, successes, maxAttempts int) ([]time.Duration, int) {
	if successes <= 0 || maxAttempts <= 0 {
		return nil, 0
	}
	rtts := make([]time.Duration, 0, successes)
	attempts := 0
	for attempts < maxAttempts && len(rtts) < successes && ctx.Err() == nil {
		attempts++
		rtt, err := pingOnce(ctx, c.cfg.peerIP.String(), c.cfg.pingTimeout)
		if err == nil {
			rtts = append(rtts, rtt)
		}
	}
	return rtts, attempts
}

func (c *checker) readStatus(ctx context.Context) (health.Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.cfg.statusURL, nil)
	if err != nil {
		return health.Snapshot{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return health.Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return health.Snapshot{}, fmt.Errorf("status endpoint returned %s", resp.Status)
	}
	var snap health.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return health.Snapshot{}, err
	}
	return snap, nil
}

func pingOnce(ctx context.Context, peer string, timeout time.Duration) (time.Duration, error) {
	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout+time.Second)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(pingCtx, "ping", "-n", "-c", "1", "-W", strconv.Itoa(seconds), peer)
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return 0, errors.New(msg)
	}
	if rtt, ok := parsePingRTT(string(out)); ok {
		return rtt, nil
	}
	return elapsed, nil
}

func parsePingRTT(output string) (time.Duration, bool) {
	match := pingRTTRE.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0, false
	}
	ms, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(ms * float64(time.Millisecond)), true
}

func summarizePings(attempts int, rtts []time.Duration) pingSummary {
	summary := pingSummary{Attempts: attempts, Success: len(rtts)}
	if len(rtts) == 0 {
		return summary
	}
	values := append([]time.Duration(nil), rtts...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total time.Duration
	for _, rtt := range values {
		total += rtt
	}
	summary.MinMS = float64(values[0]) / float64(time.Millisecond)
	summary.AvgMS = float64(total) / float64(len(values)) / float64(time.Millisecond)
	summary.MaxMS = float64(values[len(values)-1]) / float64(time.Millisecond)
	return summary
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func printReport(rep report) {
	fmt.Printf("meshcheck node=%s local=%s peer=%s passed=%t\n", rep.NodeID, rep.VirtualIP, rep.PeerIP, rep.Passed)
	for _, phase := range rep.Phases {
		fmt.Printf("%-22s passed=%-5t ping=%d/%d rtt[min/avg/max]=%.2f/%.2f/%.2fms direct_tx=%d->%d healthy=%d->%d fallback=%d->%d\n", phase.Name, phase.Passed, phase.Ping.Success, phase.Ping.Attempts, phase.Ping.MinMS, phase.Ping.AvgMS, phase.Ping.MaxMS, phase.CountersBefore.DirectTX, phase.CountersAfter.DirectTX, phase.CountersBefore.DirectHealthyMarks, phase.CountersAfter.DirectHealthyMarks, phase.CountersBefore.DirectFallbacks, phase.CountersAfter.DirectFallbacks)
		if phase.Note != "" {
			fmt.Printf("  %s\n", phase.Note)
		}
	}
	fmt.Printf("final p2p: healthy_marks=%d direct_tx=%d direct_rx=%d fallbacks=%d replay_drops=%d\n", rep.FinalP2P.DirectHealthyMarks, rep.FinalP2P.DirectTX, rep.FinalP2P.DirectRX, rep.FinalP2P.DirectFallbacks, rep.FinalP2P.ReplayDrops)
}
