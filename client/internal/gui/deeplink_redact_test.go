package gui

import (
	"strings"
	"testing"
)

func TestRedactDeepLinkHidesMasterKey(t *testing.T) {
	const secret = "0123456789abcdefSECRETVALUE"
	for _, k := range []string{"master_key", "token"} {
		got := redactDeepLink("usbridge://connect?internal_host=10.0.0.2&" + k + "=" + secret + "&protocol=sunshine")
		if strings.Contains(got, secret) || !strings.Contains(got, "internal_host=10.0.0.2") {
			t.Fatalf("redactDeepLink(%s) = %q", k, got)
		}
	}
}

func TestParseDeepLinkErrorOmitsURI(t *testing.T) {
	const secret = "0123456789abcdefSECRETVALUE"
	_, _, _, _, _, _, _, err := (&DeepLinkHandler{}).parseDeepLink("usbridge://connect?master_key=" + secret + "&x=%zz\x7f")
	if err != nil && strings.Contains(err.Error(), secret) {
		t.Fatalf("parse error leaks the key: %v", err)
	}
}
