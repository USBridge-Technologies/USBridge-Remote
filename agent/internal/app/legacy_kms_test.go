package app

import (
	"errors"
	"testing"
)

// Regression: the legacy setcap on the streamer was dropped as soon as
// the launcher was installed, before a signed bundle existed, and
// RustShine lost KMS capture (no video).
func TestShouldDropLegacySetcap(t *testing.T) {
	cases := []struct {
		installed bool
		verifyErr error
		want      bool
	}{
		{false, nil, false},
		{true, errors.New("no bundle"), false},
		{true, nil, true},
	}
	for _, c := range cases {
		if got := shouldDropLegacySetcap(c.installed, c.verifyErr); got != c.want {
			t.Errorf("installed=%v verifyErr=%v: got %v want %v", c.installed, c.verifyErr, got, c.want)
		}
	}
}
