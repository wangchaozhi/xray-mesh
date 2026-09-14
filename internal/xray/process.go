package xray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var (
	ErrAlreadyStarted = errors.New("xray process already started")
	ErrNotStarted     = errors.New("xray process not started")
)

// Process supervises an external Xray Core process. xray-mesh deliberately
// delegates VLESS and transport handling to the existing Xray binary instead
// of reimplementing those protocols.
type Process struct {
	Binary string
	Config string
	Stdout io.Writer
	Stderr io.Writer

	mu      sync.Mutex
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	waitErr error
}

func NewProcess(binary, config string) *Process {
	return &Process{
		Binary: binary,
		Config: config,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Validate asks Xray Core to parse and validate the configured file without
// starting the long-running proxy process.
func (p *Process) Validate(ctx context.Context) error {
	if err := p.validateFields(); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, p.Binary, "run", "-test", "-config", p.Config)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			return fmt.Errorf("validate xray config: %w", err)
		}
		return fmt.Errorf("validate xray config: %w: %s", err, msg)
	}
	return nil
}

// Start launches Xray Core and returns once the child process has started.
// Call Wait to observe an unexpected exit. Cancelling ctx stops the child.
func (p *Process) Start(ctx context.Context) error {
	if err := p.validateFields(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil {
		return ErrAlreadyStarted
	}

	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, p.Binary, "run", "-config", p.Config)
	cmd.Stdout = p.Stdout
	cmd.Stderr = p.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start xray: %w", err)
	}

	done := make(chan struct{})
	p.cmd = cmd
	p.cancel = cancel
	p.done = done
	p.waitErr = nil

	go func() {
		err := cmd.Wait()
		if childCtx.Err() != nil {
			err = nil
		}
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(done)
	}()
	return nil
}

// Wait blocks until the supervised Xray process exits. Multiple callers can
// safely wait for the same process and receive the same result.
func (p *Process) Wait() error {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done == nil {
		return ErrNotStarted
	}
	<-done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *Process) Running() bool {
	p.mu.Lock()
	done := p.done
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// Close is idempotent. It cancels the command context; os/exec then terminates
// the child process. The closed done channel broadcasts completion to all
// concurrent Wait callers instead of making them compete for one result.
func (p *Process) Close() error {
	p.mu.Lock()
	cancel := p.cancel
	done := p.done
	p.cancel = nil
	p.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		<-done
	}
	return nil
}

func (p *Process) validateFields() error {
	if strings.TrimSpace(p.Binary) == "" {
		return errors.New("xray binary is required")
	}
	if strings.TrimSpace(p.Config) == "" {
		return errors.New("xray config path is required")
	}
	info, err := os.Stat(p.Config)
	if err != nil {
		return fmt.Errorf("xray config: %w", err)
	}
	if info.IsDir() {
		return errors.New("xray config path is a directory")
	}
	return nil
}
