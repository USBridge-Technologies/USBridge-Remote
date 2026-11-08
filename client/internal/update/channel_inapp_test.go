package update

import "testing"

func TestInAppUpdatesOffOnIOSAndWeb(t *testing.T) {
	for _, goos := range []string{"ios", "js"} {
		if inAppUpdatesOn(goos) {
			t.Errorf("%s has no in-app updates", goos)
		}
	}
	for _, goos := range []string{"android", "linux", "windows", "darwin"} {
		if got := inAppUpdatesOn(goos); got != Enabled {
			t.Errorf("%s: got %v, want %v", goos, got, Enabled)
		}
	}
}
