package controller

import "testing"

func TestMergeAgentIdentityPrefersLiveThenSaved(t *testing.T) {
	osName, protocol := MergeAgentIdentity("Windows", "opensource", "Linux", "pro")
	if osName != "Windows" || protocol != "opensource" {
		t.Fatalf("live must win, got os=%q protocol=%q", osName, protocol)
	}

	osName, protocol = MergeAgentIdentity("", "", "Windows", "opensource")
	if osName != "Windows" || protocol != "opensource" {
		t.Fatalf("saved fills blanks, got os=%q protocol=%q", osName, protocol)
	}

	osName, protocol = MergeAgentIdentity("macOS", "", "Windows", "pro")
	if osName != "macOS" || protocol != "pro" {
		t.Fatalf("mixed live OS + saved protocol, got os=%q protocol=%q", osName, protocol)
	}
}

func TestIsSoftwareAgentOSEmptyIsHardware(t *testing.T) {
	if IsSoftwareAgentOS("") {
		t.Fatal("empty OS must stay hardware until identity is known")
	}
	if !IsSoftwareAgentOS("Windows") {
		t.Fatal("Windows is a software agent")
	}
	if IsSoftwareAgentOS("USBridge KVM") {
		t.Fatal("usbridge OS is hardware")
	}
}
