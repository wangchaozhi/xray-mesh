package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/telemetry"
)

type State string

const (
	StateDisabled State = "disabled"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

type ServiceStatus struct {
	Enabled bool   `json:"enabled"`
	State   State  `json:"state"`
	Error   string `json:"error,omitempty"`
}

type Snapshot struct {
	NodeID        string                `json:"node_id"`
	VirtualIP     string                `json:"virtual_ip"`
	NetworkPrefix string                `json:"network_prefix"`
	StartedAt     time.Time             `json:"started_at"`
	Mesh          ServiceStatus         `json:"mesh"`
	Xray          ServiceStatus         `json:"xray"`
	P2P           telemetry.P2PSnapshot `json:"p2p"`
}

type Tracker struct {
	mu   sync.RWMutex
	snap Snapshot
}

func New(nodeID, virtualIP, networkPrefix string, meshEnabled, xrayEnabled bool) *Tracker {
	return &Tracker{snap: Snapshot{
		NodeID:        nodeID,
		VirtualIP:     virtualIP,
		NetworkPrefix: networkPrefix,
		StartedAt:     time.Now().UTC(),
		Mesh:          initialService(meshEnabled),
		Xray:          initialService(xrayEnabled),
	}}
}

func initialService(enabled bool) ServiceStatus {
	if !enabled {
		return ServiceStatus{Enabled: false, State: StateDisabled}
	}
	return ServiceStatus{Enabled: true, State: StateStarting}
}

func (t *Tracker) SetMesh(state State, err error) {
	t.setService(&t.snap.Mesh, state, err)
}

func (t *Tracker) SetXray(state State, err error) {
	t.setService(&t.snap.Xray, state, err)
}

func (t *Tracker) setService(service *ServiceStatus, state State, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	service.State = state
	service.Error = ""
	if err != nil {
		service.Error = err.Error()
	}
}

func (t *Tracker) Snapshot() Snapshot {
	t.mu.RLock()
	snap := t.snap
	t.mu.RUnlock()
	snap.P2P = telemetry.P2P.Snapshot()
	return snap
}

func (t *Tracker) Healthy() bool {
	snap := t.Snapshot()
	return serviceHealthy(snap.Mesh) && serviceHealthy(snap.Xray)
}

func serviceHealthy(service ServiceStatus) bool {
	if !service.Enabled {
		return true
	}
	return service.State == StateRunning
}

func (t *Tracker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, t.Snapshot())
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		status := http.StatusOK
		if !t.Healthy() {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, t.Snapshot())
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
