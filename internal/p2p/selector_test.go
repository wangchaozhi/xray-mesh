package p2p

import (
	"testing"
	"time"
)

func TestSelectorDefaultsToRelay(t *testing.T) {
	s := NewSelector(10 * time.Second)
	if got := s.Select("beta"); got.Kind != PathRelay {
		t.Fatalf("selection=%#v", got)
	}
}

func TestSelectorUsesFreshDirectPathThenFallsBack(t *testing.T) {
	s := NewSelector(10 * time.Second)
	now := time.Unix(100, 0).UTC()
	s.now = func() time.Time { return now }
	if err := s.MarkDirectHealthy("beta", "203.0.113.8:40000", now); err != nil {
		t.Fatal(err)
	}
	if got := s.Select("beta"); got.Kind != PathDirect || got.Endpoint != "203.0.113.8:40000" {
		t.Fatalf("selection=%#v", got)
	}

	now = now.Add(11 * time.Second)
	if got := s.Select("beta"); got.Kind != PathRelay {
		t.Fatalf("expired selection=%#v", got)
	}
}

func TestSelectorFailureForcesRelay(t *testing.T) {
	s := NewSelector(time.Minute)
	now := time.Unix(200, 0).UTC()
	s.now = func() time.Time { return now }
	if err := s.MarkDirectHealthy("beta", "203.0.113.8:40000", now); err != nil {
		t.Fatal(err)
	}
	s.MarkDirectFailed("beta")
	if got := s.Select("beta"); got.Kind != PathRelay {
		t.Fatalf("selection=%#v", got)
	}
}

func TestSelectorRejectsBadEndpoint(t *testing.T) {
	s := NewSelector(time.Minute)
	if err := s.MarkDirectHealthy("beta", "not-an-endpoint", time.Now()); err == nil {
		t.Fatal("expected invalid endpoint error")
	}
}
