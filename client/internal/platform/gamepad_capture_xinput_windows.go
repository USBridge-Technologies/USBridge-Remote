//go:build windows

package platform

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// The virtual Xbox 360 endpoint on the host advertises a 4 ms interval; polling
	// faster only queues reports, slower adds latency.
	xinputPollInterval = 4 * time.Millisecond
	// The current state is re-sent this often even when nothing changed, so a
	// host that recreated its virtual pad (stream reconnect) is corrected at once
	// instead of waiting for the next input.
	xinputResendInterval = 500 * time.Millisecond
)

// startXInputCapture captures the Xbox-class pad in XInput slot N ("xinput:N").
func startXInputCapture(deviceID string, onState func(GamepadCaptureState)) (*GamepadCapture, error) {
	var slot int
	if _, err := fmt.Sscanf(deviceID, "xinput:%d", &slot); err != nil || slot < 0 || slot >= xinputMaxSlots {
		return nil, fmt.Errorf("invalid XInput device ID %q (expected xinput:0..%d)", deviceID, xinputMaxSlots-1)
	}
	if !xinputConnected(slot) {
		return nil, fmt.Errorf("XInput slot %d has no controller", slot)
	}
	vid, pid, _ := xinputVIDPID(slot)
	logrus.Infof("🎮 [XInput] Starting capture for %s (vid=%04x pid=%04x)", deviceID, vid, pid)

	cap := &GamepadCapture{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(cap.done)
		defer logrus.Infof("🎮 [XInput] Capture stopped for %s", deviceID)

		ticker := time.NewTicker(xinputPollInterval)
		defer ticker.Stop()

		var lastPacket uint32
		var lastSent time.Time
		first := true
		for {
			select {
			case <-cap.stop:
				return
			case <-ticker.C:
			}
			s, ok := xinputGet(slot)
			if !ok {
				logrus.Warnf("🎮 [XInput] %s disconnected during capture", deviceID)
				return
			}
			// dwPacketNumber changes whenever the pad's state does.
			if !first && s.Packet == lastPacket && time.Since(lastSent) < xinputResendInterval {
				continue
			}
			first, lastPacket, lastSent = false, s.Packet, time.Now()
			state := xinputToCapture(s.Pad)
			logrus.Debugf("🎮 [XInput] %s buttons=0x%04x lt=%d rt=%d lx=%d ly=%d rx=%d ry=%d",
				deviceID, state.Buttons, state.LeftTrigger, state.RightTrigger,
				state.LeftX, state.LeftY, state.RightX, state.RightY)
			onState(state)
		}
	}()
	return cap, nil
}
