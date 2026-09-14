package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/packet"
	"github.com/wangchaozhi/xray-mesh/internal/router"
	"github.com/wangchaozhi/xray-mesh/internal/transport"
	"github.com/wangchaozhi/xray-mesh/internal/tun"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8666", "coordinator base URL")
	relayAddr := flag.String("relay", "127.0.0.1:8667", "UDP relay address")
	nodeID := flag.String("node", "", "unique node ID")
	enableTUN := flag.Bool("tun", false, "enable the Linux TUN peer data plane")
	tunName := flag.String("tun-name", "xrmesh0", "TUN interface name")
	flag.Parse()

	if strings.TrimSpace(*nodeID) == "" {
		log.Fatal("-node is required")
	}

	registration, err := register(*server, *nodeID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("registered node=%s virtual_ip=%s prefix=%s\n", registration.NodeID, registration.VirtualIP, registration.NetworkPrefix)
	if !*enableTUN {
		return
	}

	if err := runTUN(registration, *relayAddr, *tunName); err != nil {
		log.Fatal(err)
	}
}

func register(server, nodeID string) (mesh.RegistrationView, error) {
	body, err := json.Marshal(map[string]string{"node_id": nodeID})
	if err != nil {
		return mesh.RegistrationView{}, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(strings.TrimRight(server, "/")+"/v1/peers", "application/json", bytes.NewReader(body))
	if err != nil {
		return mesh.RegistrationView{}, fmt.Errorf("register peer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return mesh.RegistrationView{}, fmt.Errorf("coordinator returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var registration mesh.RegistrationView
	if err := json.NewDecoder(resp.Body).Decode(&registration); err != nil {
		return mesh.RegistrationView{}, fmt.Errorf("decode response: %w", err)
	}
	return registration, nil
}

func runTUN(registration mesh.RegistrationView, relayAddr, tunName string) error {
	virtualIP, err := netip.ParseAddr(registration.VirtualIP)
	if err != nil {
		return fmt.Errorf("invalid virtual IP from coordinator: %w", err)
	}
	networkPrefix, err := netip.ParsePrefix(registration.NetworkPrefix)
	if err != nil {
		return fmt.Errorf("invalid network prefix from coordinator: %w", err)
	}
	if !networkPrefix.Contains(virtualIP) {
		return fmt.Errorf("virtual IP %s is outside prefix %s", virtualIP, networkPrefix)
	}

	dev, err := tun.Open(tunName)
	if err != nil {
		return err
	}
	defer dev.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := dev.Configure(ctx, netip.PrefixFrom(virtualIP, networkPrefix.Bits())); err != nil {
		return err
	}

	remote, err := net.ResolveUDPAddr("udp", relayAddr)
	if err != nil {
		return fmt.Errorf("resolve relay: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}
	defer conn.Close()

	hello, err := transport.MarshalHello(registration.SessionToken)
	if err != nil {
		return err
	}
	if _, err := conn.Write(hello); err != nil {
		return fmt.Errorf("relay hello: %w", err)
	}

	log.Printf("mesh TUN=%s addr=%s relay=%s; Internet egress is not enabled yet", dev.Name(), netip.PrefixFrom(virtualIP, networkPrefix.Bits()), remote)
	return pump(ctx, dev, conn, registration.SessionToken, networkPrefix)
}

func pump(ctx context.Context, dev tun.Device, conn *net.UDPConn, token string, prefix netip.Prefix) error {
	errCh := make(chan error, 2)
	classifier := router.PrefixClassifier{MeshPrefix: prefix}

	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := dev.ReadPacket(ctx, buf)
			if err != nil {
				errCh <- err
				return
			}
			src, dst, err := packet.IPv4Endpoints(buf[:n])
			if err != nil {
				continue
			}
			class := classifier.Classify(router.Packet{Source: src, Destination: dst, Payload: buf[:n]})
			if class != router.ClassPeer && class != router.ClassDiscovery {
				continue
			}
			frame, err := transport.MarshalPacket(token, buf[:n])
			if err != nil {
				errCh <- err
				return
			}
			if _, err := conn.Write(frame); err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				errCh <- err
				return
			}
			frame, err := transport.ParseFrame(buf[:n])
			if err != nil || frame.Type != transport.FrameDeliver {
				continue
			}
			if _, err := dev.WritePacket(ctx, frame.Payload); err != nil {
				errCh <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close()
		_ = dev.Close()
		return nil
	case err := <-errCh:
		if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}
