package streamhost

import (
	"reflect"
	"testing"
)

// TestCodecsFromFlags_PyroWave pins the USBridge PyroWave bit (rust-shine and
// the Punktfunk fork advertise it in ServerCodecModeSupport) alongside the
// standard ones, and that a failed query never claims more than h264.
func TestCodecsFromFlags_PyroWave(t *testing.T) {
	cases := []struct {
		flags int
		ok    bool
		want  []string
	}{
		{0x01010101, true, []string{"h264", "h265", "av1", "pyrowave"}},
		{scmH264 | scmHEVC, true, []string{"h264", "h265"}},
		{scmH264 | scmUSBridgePyroWave, true, []string{"h264", "pyrowave"}},
		{scmH264 | scmPyroWave, true, []string{"h264", "pyrowave"}},
		{0x01010101, false, []string{"h264"}},
	}
	for _, c := range cases {
		if got := codecsFromFlags(c.flags, c.ok); !reflect.DeepEqual(got, c.want) {
			t.Errorf("codecsFromFlags(0x%08X, %v) = %v, want %v", c.flags, c.ok, got, c.want)
		}
	}
}
