package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

// sunshineTCPPorts returns Sunshine's fixed TCP ports (HTTPS, HTTP, web-UI
// HTTPS, RTSP) relative to its NvHTTP base port (Sunshine's "port" config
// value, i.e. a.cfg.SunshinePort-1), per Sunshine's documented port-offset
// scheme.
func sunshineTCPPorts(basePort int) []int {
	return []int{basePort - 5, basePort, basePort + 1, basePort + 21}
}

// sunshineUDPPorts returns Sunshine's fixed UDP ports (video, control,
// audio, mic) relative to its NvHTTP base port.
func sunshineUDPPorts(basePort int) []int {
	return []int{basePort + 9, basePort + 10, basePort + 11, basePort + 13}
}

// startSunshineTSNetForwarding exposes Sunshine's GameStream ports over the
// embedded tsnet node.
//
// tsnet.Server installs its own TCP/UDP flow handler
// (getTCPHandlerForFlow/getUDPHandlerForFlow in the tailscale.com/tsnet
// package) on every node. Unlike the raw wgengine/netstack used by a real
// system tailscaled — which auto-forwards any unclaimed inbound port to
// 127.0.0.1 on the host — tsnet.Server's handler immediately RSTs (TCP) or
// silently drops (UDP) any inbound connection to a port that wasn't
// explicitly registered via Listen()/ListenPacket(), with no forwarding
// fallback of its own for UDP. Sunshine's ports were never registered here,
// so every Moonlight connection attempt over Tailscale was refused before
// ever reaching Sunshine — indistinguishable from "Sunshine is down," on
// every OS, though it only surfaced as a real-world bug on Mac/Windows
// because Linux test machines happened to also run a real system tailscaled
// whose kernel TUN route silently carried the traffic instead.
func (a *App) startSunshineTSNetForwarding() {
	if a.ts == nil {
		return
	}
	tsSrv, err := a.ts.Server()
	if err != nil {
		log.Printf("[app] tsnet server unavailable for Sunshine port forwarding: %v", err)
		return
	}

	basePort := a.cfg.SunshinePort - 1

	tcpPorts := sunshineTCPPorts(basePort)
	tcpSet := make(map[uint16]bool, len(tcpPorts))
	for _, p := range tcpPorts {
		tcpSet[uint16(p)] = true
	}
	tsSrv.RegisterFallbackTCPHandler(func(_, dst netip.AddrPort) (func(net.Conn), bool) {
		port := dst.Port()
		if !tcpSet[port] {
			return nil, false
		}
		return func(c net.Conn) { forwardTCPToLocalhost(c, port) }, true
	})
	log.Printf("[app] tsnet forwarding Sunshine TCP ports %v to 127.0.0.1", tcpPorts)

	go a.startSunshineUDPForwarding(tsSrv, sunshineUDPPorts(basePort))
}

// startSunshineUDPForwarding sets up a ListenPacket-based relay for each
// Sunshine UDP port. Unlike Listen (TCP), tsnet's ListenPacket rejects an
// address with no host part, so the node's own Tailscale IPv4 must be known
// first — it isn't necessarily available the instant tsnet starts, so this
// polls briefly for login to complete.
func (a *App) startSunshineUDPForwarding(tsSrv *tsnet.Server, udpPorts []int) {
	var selfIP string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := a.ts.Status(context.Background()); err == nil && status != nil && status.LoggedIn && status.Self.IP4 != "" {
			selfIP = status.Self.IP4
			break
		}
		time.Sleep(time.Second)
	}
	if selfIP == "" {
		log.Printf("[app] tsnet UDP forwarding: gave up waiting for Tailscale login")
		return
	}

	for _, port := range udpPorts {
		go forwardSunshineUDPPort(tsSrv, selfIP, port)
	}
	log.Printf("[app] tsnet forwarding Sunshine UDP ports %v to 127.0.0.1", udpPorts)
}

func forwardTCPToLocalhost(c net.Conn, port uint16) {
	defer c.Close()
	backend, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		log.Printf("[app] sunshine tsnet forward: could not reach 127.0.0.1:%d: %v", port, err)
		return
	}
	defer backend.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(backend, c)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(c, backend)
		done <- struct{}{}
	}()
	<-done
}

// forwardSunshineUDPPort relays UDP packets between Tailscale peers and
// Sunshine's local backend port, keyed per remote peer since UDP has no
// built-in session concept.
func forwardSunshineUDPPort(tsSrv *tsnet.Server, selfIP string, port int) {
	pc, err := tsSrv.ListenPacket("udp", fmt.Sprintf("%s:%d", selfIP, port))
	if err != nil {
		log.Printf("[app] sunshine tsnet udp listen %s:%d failed: %v", selfIP, port, err)
		return
	}
	defer pc.Close()

	backendAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Printf("[app] sunshine tsnet udp resolve :%d failed: %v", port, err)
		return
	}

	var mu sync.Mutex
	backends := make(map[string]*net.UDPConn)

	buf := make([]byte, 65535)
	for {
		n, clientAddr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		key := clientAddr.String()

		mu.Lock()
		backend, ok := backends[key]
		mu.Unlock()
		if !ok {
			backend, err = net.DialUDP("udp", nil, backendAddr)
			if err != nil {
				log.Printf("[app] sunshine tsnet udp :%d dial backend failed: %v", port, err)
				continue
			}
			mu.Lock()
			backends[key] = backend
			mu.Unlock()
			go relaySunshineUDPReplies(pc, backend, clientAddr, key, &mu, backends)
		}

		if _, err := backend.Write(buf[:n]); err != nil {
			log.Printf("[app] sunshine tsnet udp :%d write to backend failed: %v", port, err)
		}
	}
}

// relaySunshineUDPReplies copies Sunshine's replies on a per-peer backend
// socket back out to that peer over tsnet, until the backend goes idle.
func relaySunshineUDPReplies(pc net.PacketConn, backend *net.UDPConn, clientAddr net.Addr, key string, mu *sync.Mutex, backends map[string]*net.UDPConn) {
	defer func() {
		mu.Lock()
		delete(backends, key)
		mu.Unlock()
		backend.Close()
	}()

	buf := make([]byte, 65535)
	for {
		_ = backend.SetReadDeadline(time.Now().Add(2 * time.Minute))
		n, err := backend.Read(buf)
		if err != nil {
			return
		}
		if _, err := pc.WriteTo(buf[:n], clientAddr); err != nil {
			return
		}
	}
}
