package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
)

var errHeartbeatUnauthorized = errors.New("coordinator rejected peer heartbeat")

const maxHeartbeatFailures = 3

func startHeartbeat(ctx context.Context, server string, registration mesh.RegistrationView) <-chan error {
	if registration.HeartbeatIntervalSeconds <= 0 || strings.TrimSpace(registration.SessionToken) == "" {
		return nil
	}
	endpoint := strings.TrimRight(server, "/") + "/v1/heartbeat"
	client := &http.Client{Timeout: 5 * time.Second}
	return heartbeatLoop(ctx, client, endpoint, registration.SessionToken, time.Duration(registration.HeartbeatIntervalSeconds)*time.Second)
}

func heartbeatLoop(ctx context.Context, client *http.Client, endpoint, token string, interval time.Duration) <-chan error {
	waitCh := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		failures := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := sendHeartbeat(ctx, client, endpoint, token)
				if err == nil {
					failures = 0
					continue
				}
				if errors.Is(err, errHeartbeatUnauthorized) {
					waitCh <- err
					return
				}
				failures++
				log.Printf("peer heartbeat failed attempt=%d/%d: %v", failures, maxHeartbeatFailures, err)
				if failures >= maxHeartbeatFailures {
					waitCh <- fmt.Errorf("peer heartbeat failed %d consecutive times: %w", failures, err)
					return
				}
			}
		}
	}()
	return waitCh
}

func sendHeartbeat(ctx context.Context, client *http.Client, endpoint, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	message := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if message == "" {
			return errHeartbeatUnauthorized
		}
		return fmt.Errorf("%w: %s", errHeartbeatUnauthorized, message)
	}
	if message == "" {
		return fmt.Errorf("heartbeat returned %s", resp.Status)
	}
	return fmt.Errorf("heartbeat returned %s: %s", resp.Status, message)
}
