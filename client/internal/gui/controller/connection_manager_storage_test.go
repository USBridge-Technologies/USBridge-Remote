package controller

import "testing"

func TestApplyConnectionAgentInfo_UpdatesProtocolOnSameHost(t *testing.T) {
	conns := []SavedConnection{
		{Name: "Office", Host: "192.168.1.10", InternalHost: "192.168.1.10", RemoteOS: "Windows", RemoteProtocol: "opensource"},
		{Name: "Other", Host: "10.0.0.2", RemoteOS: "linux", RemoteProtocol: "pro"},
	}

	idx, changed := applyConnectionAgentInfo(conns, "192.168.1.10", "", "pro")
	if idx != 0 || !changed {
		t.Fatalf("idx=%d changed=%v, want 0 true", idx, changed)
	}
	if conns[0].RemoteProtocol != "pro" {
		t.Fatalf("protocol=%q, want pro", conns[0].RemoteProtocol)
	}
	if conns[0].RemoteOS != "Windows" {
		t.Fatalf("OS should stay Windows, got %q", conns[0].RemoteOS)
	}
	if conns[1].RemoteProtocol != "pro" {
		t.Fatalf("other row must be unchanged, got %q", conns[1].RemoteProtocol)
	}

	idx, changed = applyConnectionAgentInfo(conns, "192.168.1.10", "", "pro")
	if idx != 0 || changed {
		t.Fatalf("same tariff should be a no-op, idx=%d changed=%v", idx, changed)
	}
}

func TestApplyConnectionAgentInfo_MatchesTailscaleHost(t *testing.T) {
	conns := []SavedConnection{
		{Name: "Office", Host: "192.168.1.10", InternalHost: "192.168.1.10", TailscaleHost: "100.64.1.2", RemoteOS: "linux", RemoteProtocol: "opensource"},
	}
	idx, changed := applyConnectionAgentInfo(conns, "100.64.1.2", "linux", "free")
	if idx != 0 || !changed || conns[0].RemoteProtocol != "free" {
		t.Fatalf("tailscale host should update tariff, idx=%d changed=%v proto=%q", idx, changed, conns[0].RemoteProtocol)
	}
}

func TestApplyConnectionAgentInfo_UnknownHost(t *testing.T) {
	conns := []SavedConnection{{Name: "Office", Host: "192.168.1.10", RemoteProtocol: "opensource"}}
	idx, changed := applyConnectionAgentInfo(conns, "10.9.9.9", "", "pro")
	if idx != -1 || changed {
		t.Fatalf("unknown host idx=%d changed=%v", idx, changed)
	}
	if conns[0].RemoteProtocol != "opensource" {
		t.Fatalf("must not rewrite other hosts, got %q", conns[0].RemoteProtocol)
	}
}

func TestLookupConnectionAgentInfo(t *testing.T) {
	conns := []SavedConnection{
		{Name: "Office", Host: "192.168.1.10", InternalHost: "192.168.1.10", RemoteOS: "Windows", RemoteProtocol: "opensource"},
	}
	osName, protocol := lookupConnectionAgentInfo(conns, "192.168.1.10")
	if osName != "Windows" || protocol != "opensource" {
		t.Fatalf("got os=%q protocol=%q", osName, protocol)
	}
	osName, protocol = lookupConnectionAgentInfo(conns, "10.9.9.9")
	if osName != "" || protocol != "" {
		t.Fatalf("unknown host got os=%q protocol=%q", osName, protocol)
	}
}
