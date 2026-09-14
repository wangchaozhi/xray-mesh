package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8666", "coordinator base URL")
	nodeID := flag.String("node", "", "unique node ID")
	flag.Parse()

	if strings.TrimSpace(*nodeID) == "" {
		log.Fatal("-node is required")
	}

	body, _ := json.Marshal(map[string]string{"node_id": *nodeID})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(strings.TrimRight(*server, "/")+"/v1/peers", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("register peer: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Fatalf("coordinator returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var peer mesh.PeerView
	if err := json.NewDecoder(resp.Body).Decode(&peer); err != nil {
		log.Fatalf("decode response: %v", err)
	}
	fmt.Printf("registered node=%s virtual_ip=%s\n", peer.NodeID, peer.VirtualIP)
}
