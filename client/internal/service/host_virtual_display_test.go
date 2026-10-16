package service

import (
	"testing"

	"usbridge-client/internal/api/moonlight"
)

func TestStockHostLaunchParams(t *testing.T) {
	defer SetHostVirtualDisplayEnabled(true)
	m := &MoonlightService{}

	// With the usbridge agent nothing extra is sent: the agent runs the
	// virtual display (MttVDD) itself.
	m.noteHostVirtualDisplay(&moonlight.ServerInfo{VirtualDisplayCapable: true, VirtualDisplayDriverReady: true})
	m.hdr = true
	if p := m.stockHostLaunchParams(); p != nil {
		t.Fatalf("agent host got %v", p)
	}

	m.SetGameStreamOnly(true)
	m.noteHostVirtualDisplay(&moonlight.ServerInfo{VirtualDisplayCapable: true, VirtualDisplayDriverReady: true})
	if !m.HostVirtualDisplayCapable() {
		t.Fatal("capable host not reported")
	}
	if p := m.stockHostLaunchParams(); p["virtualDisplay"] != "1" || p["hdrMode"] != "1" {
		t.Fatalf("on by default: got %v", p)
	}
	SetHostVirtualDisplayEnabled(false)
	if p := m.stockHostLaunchParams(); p["virtualDisplay"] != "0" {
		t.Fatalf("switched off: got %v", p)
	}

	// Driver not up: asking for it would fail the launch, so ask nothing.
	SetHostVirtualDisplayEnabled(true)
	m.noteHostVirtualDisplay(&moonlight.ServerInfo{VirtualDisplayCapable: true})
	if p := m.stockHostLaunchParams(); p["virtualDisplay"] != "" || m.HostVirtualDisplayCapable() {
		t.Fatalf("driver not ready: got %v, capable=%v", p, m.HostVirtualDisplayCapable())
	}

	// A plain Sunshine: no virtual display parameter at all.
	m.noteHostVirtualDisplay(&moonlight.ServerInfo{})
	m.hdr = false
	if p := m.stockHostLaunchParams(); len(p) != 0 {
		t.Fatalf("plain Sunshine got %v", p)
	}
	if m.HostVirtualDisplayCapable() {
		t.Fatal("plain Sunshine reported capable")
	}
}
