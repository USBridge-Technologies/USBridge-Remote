//go:build linux && !android

package platform

import (
	"os"
	"syscall"
	"testing"
)

func TestInvertAxisClampsInsteadOfOverflowing(t *testing.T) {
	cases := []struct{ in, want int16 }{
		{-32768, 32767}, // negating int16 would stay -32768 and flip the stick
		{32767, -32767},
		{0, 0},
		{1, -1},
		{-1, 1},
	}
	for _, c := range cases {
		if got := invertAxis(c.in); got != c.want {
			t.Errorf("invertAxis(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestLinuxKeyDown(t *testing.T) {
	bitmap := make([]byte, linuxKeyMax/8+1)
	bitmap[linuxBtnA/8] |= 1 << (linuxBtnA % 8)
	bitmap[linuxBtnThumbR/8] |= 1 << (linuxBtnThumbR % 8)
	if !linuxKeyDown(bitmap, linuxBtnA) || !linuxKeyDown(bitmap, linuxBtnThumbR) {
		t.Fatal("set keys not reported as down")
	}
	if linuxKeyDown(bitmap, linuxBtnB) || linuxKeyDown(bitmap, linuxBtnMode) {
		t.Fatal("unset keys reported as down")
	}
	if linuxKeyDown(bitmap[:4], linuxBtnA) {
		t.Fatal("a code beyond the bitmap must be up, not a panic")
	}
}

// xboxState is a pad as xpad reports a Wolverine V2 / Xbox One controller:
// 16-bit sticks, 10-bit triggers.
func xboxState() *linuxState {
	return &linuxState{axes: map[uint16]linuxAxisRange{
		linuxAbsX:  {-32768, 32767},
		linuxAbsY:  {-32768, 32767},
		linuxAbsRX: {-32768, 32767},
		linuxAbsRY: {-32768, 32767},
		linuxAbsZ:  {0, 1023},
		linuxAbsRZ: {0, 1023},
	}}
}

func TestToCaptureStickExtremesAndCentre(t *testing.T) {
	s := xboxState()
	got := s.toCapture()
	if got.LeftX != 0 || got.LeftY != 0 || got.RightX != 0 || got.RightY != 0 {
		// raw 0 is the middle of -32768..32767 (within one count)
		if abs16(got.LeftX) > 1 || abs16(got.LeftY) > 1 || abs16(got.RightX) > 1 || abs16(got.RightY) > 1 {
			t.Fatalf("centre = %+v", got)
		}
	}

	// evdev: Y grows downward. Full up (raw min) must become +32767, full down -32767.
	s.leftXRaw, s.leftYRaw = 32767, -32768
	s.rightXRaw, s.rightYRaw = -32768, 32767
	got = s.toCapture()
	if got.LeftX != 32767 || got.LeftY != 32767 {
		t.Errorf("left stick right+up = (%d,%d), want (32767,32767)", got.LeftX, got.LeftY)
	}
	if got.RightX != -32768 || got.RightY != -32767 {
		t.Errorf("right stick left+down = (%d,%d), want (-32768,-32767)", got.RightX, got.RightY)
	}
}

func TestTriggerNormalizationCoversFullRange(t *testing.T) {
	s := xboxState()
	if got := s.normalizeTrigger(linuxAbsZ, 0); got != 0 {
		t.Errorf("released = %d, want 0", got)
	}
	if got := s.normalizeTrigger(linuxAbsZ, 1023); got != 255 {
		t.Errorf("fully pulled = %d, want 255", got)
	}
	if got := s.normalizeTrigger(linuxAbsRZ, 512); got < 126 || got > 128 {
		t.Errorf("half pulled = %d, want ~127", got)
	}
}

func TestDpadHatBitsAndButtons(t *testing.T) {
	s := xboxState()
	s.hatX, s.hatY = 1, -1 // right + up
	s.buttons = 0x1000     // A
	got := s.toCapture().Buttons
	if got != 0x1000|0x0008|0x0001 {
		t.Errorf("buttons = %#x, want A|right|up", got)
	}
}

func abs16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}

// TestLiveSeed reads a real pad's current state through the same ioctls the
// capture uses. Opt in with USBRIDGE_CAPTURE_EVDEV=/dev/input/eventN; hold a
// button or a stick to see it reflected.
func TestLiveSeed(t *testing.T) {
	path := os.Getenv("USBRIDGE_CAPTURE_EVDEV")
	if path == "" {
		t.Skip("set USBRIDGE_CAPTURE_EVDEV to a gamepad event node to run")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer syscall.Close(fd)

	st := &linuxState{axes: map[uint16]linuxAxisRange{}}
	for _, axis := range []uint16{linuxAbsX, linuxAbsY, linuxAbsZ, linuxAbsRX, linuxAbsRY, linuxAbsRZ} {
		info, ok := linuxEviocgabs(fd, axis)
		if !ok {
			t.Fatalf("EVIOCGABS %d failed", axis)
		}
		t.Logf("axis %d range [%d..%d] value %d", axis, info.Minimum, info.Maximum, info.Value)
		st.axes[axis] = linuxAxisRange{info.Minimum, info.Maximum}
	}
	if _, ok := linuxEviocgkey(fd); !ok {
		t.Fatal("EVIOCGKEY failed")
	}
	st.seed(fd)
	t.Logf("seeded state: %+v", st.toCapture())
}
