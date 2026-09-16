package ui

import (
	"testing"

	"usbridge_agent/internal/account"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/ui/design"
)

func TestProtocolNeedsPurchase_AccountProUnlocksChange(t *testing.T) {
	st := entitlement.Status{Tier: "free"}
	acc := account.Status{
		LoggedIn: true,
		Licenses: []account.License{{Identifier: "lic-1", Status: "licensed", Tier: "pro"}},
	}
	if protocolNeedsPurchase(protocolPro, st, acc) {
		t.Fatal("logged-in Pro account should not need to buy Pro")
	}
	if !protocolNeedsPurchase(protocolEnterprise, st, acc) {
		t.Fatal("Pro account should still need to buy Enterprise")
	}
	if protocolNeedsPurchase(protocolFree, st, acc) {
		t.Fatal("Free protocol never needs purchase")
	}
}

func TestProtocolNeedsPurchase_FreeAccountNeedsBuy(t *testing.T) {
	st := entitlement.Status{Tier: "free"}
	acc := account.Status{}
	if !protocolNeedsPurchase(protocolPro, st, acc) {
		t.Fatal("no paid tier or account license should need to buy Pro")
	}
}

func TestProtocolNeedsPurchase_MachineProNoAccount(t *testing.T) {
	st := entitlement.Status{Tier: "pro"}
	acc := account.Status{}
	if protocolNeedsPurchase(protocolPro, st, acc) {
		t.Fatal("hardware-bound Pro should not need to buy Pro")
	}
}

func TestAccountLicenseIdentifier(t *testing.T) {
	acc := account.Status{Licenses: []account.License{
		{Identifier: "trial", Status: "trial", Tier: "pro"},
		{Identifier: "pro-1", Status: "licensed", Tier: "pro"},
	}}
	if got := accountLicenseIdentifier(acc, protocolPro); got != "pro-1" {
		t.Fatalf("accountLicenseIdentifier(pro) = %q, want pro-1", got)
	}
	if got := accountLicenseIdentifier(acc, protocolEnterprise); got != "" {
		t.Fatalf("accountLicenseIdentifier(enterprise) = %q, want empty", got)
	}
}

func TestProtocolIncludes(t *testing.T) {
	if !protocolIncludes(protocolPro, protocolFree) {
		t.Fatal("Pro should include Free")
	}
	if protocolIncludes(protocolPro, protocolPro) {
		t.Fatal("Pro should not mark itself as included")
	}
	if !protocolIncludes(protocolEnterprise, protocolPro) {
		t.Fatal("Enterprise should include Pro")
	}
	if protocolIncludes(protocolOpensource, protocolFree) {
		t.Fatal("Opensource should not include Free")
	}
}

func TestProtocolNormalizePick(t *testing.T) {
	if got := protocolNormalizePick(protocolFree, protocolPro, protocolPro); got != protocolPro {
		t.Fatalf("Pro applied + Free click = %q, want pro", got)
	}
	if got := protocolNormalizePick(protocolFree, protocolOpensource, protocolPro); got != protocolPro {
		t.Fatalf("Opensource + Pro plan + Free click = %q, want pro", got)
	}
	if got := protocolNormalizePick(protocolFree, protocolOpensource, ""); got != protocolFree {
		t.Fatalf("no plan + Free click = %q, want free", got)
	}
}

func TestProtocolRowIncluded(t *testing.T) {
	if !protocolRowIncluded(protocolOpensource, protocolPro, protocolFree) {
		t.Fatal("picking Pro before Change should gray-check Free")
	}
	if !protocolRowIncluded(protocolPro, protocolPro, protocolFree) {
		t.Fatal("applied Pro should gray-check Free")
	}
	if protocolRowIncluded(protocolOpensource, protocolPro, protocolPro) {
		t.Fatal("selected Pro should not be the included gray check")
	}
}

func TestProtocolHoverHighlights(t *testing.T) {
	if !protocolHoverHighlights(protocolFree, protocolPro, protocolFree) {
		t.Fatal("hover Free with Pro plan should light Free")
	}
	if !protocolHoverHighlights(protocolFree, protocolPro, protocolPro) {
		t.Fatal("hover Free with Pro plan should light Pro")
	}
	if !protocolHoverHighlights(protocolPro, protocolPro, protocolFree) {
		t.Fatal("hover Pro with Pro plan should light Free")
	}
	if protocolHoverHighlights(protocolFree, "", protocolPro) {
		t.Fatal("no Pro plan: hover Free should not light Pro")
	}
}

func TestChromePinFromPref(t *testing.T) {
	if chromePinFromPref("white") != protocolOpensource {
		t.Fatal("white pin should be opensource chrome")
	}
	if chromePinFromPref("blue") != protocolFree {
		t.Fatal("blue pin should be free chrome")
	}
	if chromePinFromPref("pro") != protocolPro {
		t.Fatal("pro pin should be pro chrome")
	}
	if chromePinFromPref("default") != "" {
		t.Fatal("default pin should follow protocol")
	}
}

func TestEffectiveChromeKind(t *testing.T) {
	if got := effectiveChromeKind(protocolOpensource, protocolPro); got != protocolOpensource {
		t.Fatalf("pinned white should stay white, got %q", got)
	}
	if got := effectiveChromeKind("", protocolPro); got != protocolPro {
		t.Fatalf("default should follow protocol, got %q", got)
	}
}

func TestProtocolBadgeColorsFollowsProChrome(t *testing.T) {
	chromeMu.Lock()
	oldNow, oldPin, oldProto := chromeNow, chromePin, chromeProtocol
	chromeMu.Unlock()
	t.Cleanup(func() {
		chromeMu.Lock()
		chromeNow, chromePin, chromeProtocol = oldNow, oldPin, oldProto
		chromeMu.Unlock()
	})
	setChromePin("")
	setChromeKind(protocolPro)
	fg, _ := protocolBadgeColors(protocolFree)
	if fg != design.ColorMutedOlive {
		t.Fatal("Free pill should match Opensource when Pro chrome is on")
	}
	setChromeKind(protocolFree)
	fg, _ = protocolBadgeColors(protocolFree)
	if fg != design.ColorTeal {
		t.Fatal("Free pill should stay teal on Free chrome")
	}
}
