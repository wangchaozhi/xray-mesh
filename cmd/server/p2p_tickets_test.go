package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/p2p"
)

func TestProbeTicketAPIRequiresAuthentication(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	a := &api{registry: registry, tickets: p2p.NewTicketManager(30 * time.Second)}

	req := httptest.NewRequest(http.MethodPost, "/v1/p2p/tickets", bytes.NewBufferString(`{"target_node":"beta"}`))
	rec := httptest.NewRecorder()
	a.issueProbeTicket(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestProbeTicketIsVisibleOnlyToPair(t *testing.T) {
	registry, err := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	alpha, _ := registry.Register("alpha")
	beta, _ := registry.Register("beta")
	gamma, _ := registry.Register("gamma")
	manager := p2p.NewTicketManager(30 * time.Second)
	a := &api{registry: registry, tickets: manager}

	issueReq := httptest.NewRequest(http.MethodPost, "/v1/p2p/tickets", bytes.NewBufferString(`{"target_node":"beta"}`))
	issueReq.Header.Set("Authorization", "Bearer "+alpha.SessionToken)
	issueRec := httptest.NewRecorder()
	a.issueProbeTicket(issueRec, issueReq)
	if issueRec.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", issueRec.Code, issueRec.Body.String())
	}
	var issued p2p.ProbeTicket
	if err := json.NewDecoder(issueRec.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	if issued.SourceNode != "alpha" || issued.TargetNode != "beta" || issued.Ticket == "" {
		t.Fatalf("issued=%#v", issued)
	}

	betaTickets := fetchProbeTicketsForTest(t, a, beta.SessionToken)
	if len(betaTickets) != 1 || betaTickets[0].Ticket != issued.Ticket {
		t.Fatalf("beta tickets=%#v", betaTickets)
	}
	alphaTickets := fetchProbeTicketsForTest(t, a, alpha.SessionToken)
	if len(alphaTickets) != 1 || alphaTickets[0].Ticket != issued.Ticket {
		t.Fatalf("alpha tickets=%#v", alphaTickets)
	}
	gammaTickets := fetchProbeTicketsForTest(t, a, gamma.SessionToken)
	if len(gammaTickets) != 0 {
		t.Fatalf("gamma saw unrelated tickets=%#v", gammaTickets)
	}
}

func TestProbeTicketRejectsSelfAndMissingTarget(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	alpha, _ := registry.Register("alpha")
	a := &api{registry: registry, tickets: p2p.NewTicketManager(30 * time.Second)}

	selfReq := httptest.NewRequest(http.MethodPost, "/v1/p2p/tickets", bytes.NewBufferString(`{"target_node":"alpha"}`))
	selfReq.Header.Set("Authorization", "Bearer "+alpha.SessionToken)
	selfRec := httptest.NewRecorder()
	a.issueProbeTicket(selfRec, selfReq)
	if selfRec.Code != http.StatusBadRequest {
		t.Fatalf("self status=%d", selfRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodPost, "/v1/p2p/tickets", bytes.NewBufferString(`{"target_node":"missing"}`))
	missingReq.Header.Set("Authorization", "Bearer "+alpha.SessionToken)
	missingRec := httptest.NewRecorder()
	a.issueProbeTicket(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missingRec.Code)
	}
}

func fetchProbeTicketsForTest(t *testing.T, a *api, token string) []p2p.ProbeTicket {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/p2p/tickets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.listProbeTickets(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tickets []p2p.ProbeTicket
	if err := json.NewDecoder(rec.Body).Decode(&tickets); err != nil {
		t.Fatal(err)
	}
	return tickets
}
