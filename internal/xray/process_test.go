//go:build !windows

package xray

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessValidateAndLifecycle(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"log":{"loglevel":"warning"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(dir, "fake-xray")
	script := `#!/bin/sh
if [ "$2" = "-test" ]; then
  exit 0
fi
while true; do
  sleep 1
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	p := NewProcess(binary, config)
	if err := p.Validate(context.Background()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !p.Running() {
		t.Fatal("Running() = false after Start")
	}
	if err := p.Start(ctx); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start() error = %v, want ErrAlreadyStarted", err)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestProcessValidationRejectsMissingConfig(t *testing.T) {
	p := NewProcess("xray", filepath.Join(t.TempDir(), "missing.json"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Validate(ctx); err == nil {
		t.Fatal("Validate() error = nil, want missing config error")
	}
}

func TestWaitBeforeStart(t *testing.T) {
	p := NewProcess("xray", filepath.Join(t.TempDir(), "config.json"))
	if err := p.Wait(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Wait() error = %v, want ErrNotStarted", err)
	}
}
