package controller

import (
	"testing"

	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"
)

// fakePenSender records every SendMoonlightPenEvent call for assertions and
// implements the rest of service.MoonlightInputSender as no-ops.
type fakePenSender struct {
	active bool
	calls  []penCall
}

type penCall struct {
	eventType, toolType, penButtons uint8
	x, y, pressure                  float32
	rotation                        uint16
	tilt                            uint8
}

func (f *fakePenSender) IsInputActive() bool { return f.active }
func (f *fakePenSender) SendMoonlightPenEvent(eventType, toolType, penButtons uint8, x, y, pressureOrDistance float32, rotation uint16, tilt uint8) {
	f.calls = append(f.calls, penCall{eventType, toolType, penButtons, x, y, pressureOrDistance, rotation, tilt})
}
func (f *fakePenSender) SendMoonlightKey(int16, int8, int8)                                 {}
func (f *fakePenSender) SendMoonlightMouseMove(int16, int16)                                {}
func (f *fakePenSender) SendMoonlightMousePosition(int16, int16, int16, int16)              {}
func (f *fakePenSender) SendMoonlightMouseButton(int8, int)                                 {}
func (f *fakePenSender) SendMoonlightScroll(int8)                                           {}
func (f *fakePenSender) SendMoonlightControllerEvent(uint16, uint16, uint16, uint8, uint8, int16, int16, int16, int16) {
}
func (f *fakePenSender) SendMoonlightUtf8Text(string) {}

var _ service.MoonlightInputSender = (*fakePenSender)(nil)

func newTestDiskWidgetWithSender(sender *fakePenSender) *DiskWidget {
	dw := &DiskWidget{}
	dw.moonlightProvider = func() service.MoonlightInputSender { return sender }
	return dw
}

func TestForwardPenStateDropsInputWhenSessionInactive(t *testing.T) {
	sender := &fakePenSender{active: false}
	dw := newTestDiskWidgetWithSender(sender)
	tracker := &penCaptureTracker{}

	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TipSwitch: true})

	if len(sender.calls) != 0 {
		t.Fatalf("expected no calls while session inactive, got %d", len(sender.calls))
	}
}

func TestForwardPenStateEventTypeTransitions(t *testing.T) {
	sender := &fakePenSender{active: true}
	dw := newTestDiskWidgetWithSender(sender)
	tracker := &penCaptureTracker{}

	// 1. Hovering in range, tip up -> HOVER.
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TipSwitch: false})
	// 2. Tip touches down -> DOWN.
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TipSwitch: true})
	// 3. Still touching -> MOVE.
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TipSwitch: true})
	// 4. Tip lifts, still hovering -> UP.
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TipSwitch: false})
	// 5. Pen leaves the tablet entirely -> CANCEL_ALL.
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: false, TipSwitch: false})

	want := []uint8{liTouchEventHover, liTouchEventDown, liTouchEventMove, liTouchEventUp, liTouchEventCancelAll}
	if len(sender.calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(sender.calls), len(want), sender.calls)
	}
	for i, w := range want {
		if sender.calls[i].eventType != w {
			t.Errorf("call %d: eventType = 0x%02x, want 0x%02x", i, sender.calls[i].eventType, w)
		}
	}
}

// TestForwardPenStateNoSpuriousCancelAfterLift guards the exact regression
// found live against a real Wacom Intuos S: a touch-then-lift sequence must
// not emit CANCEL_ALL merely because the tip came up -- only a genuine
// InRange transition to false should (see decodePenReport's doc comment on
// why bit6, not bit5, is InRange).
func TestForwardPenStateNoSpuriousCancelAfterLift(t *testing.T) {
	sender := &fakePenSender{active: true}
	dw := newTestDiskWidgetWithSender(sender)
	tracker := &penCaptureTracker{}

	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TipSwitch: true})  // DOWN
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TipSwitch: false}) // UP, still in range

	for i, c := range sender.calls {
		if c.eventType == liTouchEventCancelAll {
			t.Fatalf("call %d: unexpected CANCEL_ALL while still in range: %+v", i, c)
		}
	}
}

func TestForwardPenStateToolTypeAndButtons(t *testing.T) {
	sender := &fakePenSender{active: true}
	dw := newTestDiskWidgetWithSender(sender)
	tracker := &penCaptureTracker{}

	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, Eraser: true})
	if got := sender.calls[0].toolType; got != liToolTypeEraser {
		t.Errorf("toolType = %d, want liToolTypeEraser (%d)", got, liToolTypeEraser)
	}

	sender.calls = nil
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, Button1: true, Button2: true})
	got := sender.calls[0].penButtons
	if got&liPenButtonSecondary == 0 || got&liPenButtonTertiary == 0 {
		t.Errorf("penButtons = 0x%02x, want both secondary (0x%02x) and tertiary (0x%02x) set", got, liPenButtonSecondary, liPenButtonTertiary)
	}
}

func TestForwardPenStateNormalizesCoordinatesAndPressure(t *testing.T) {
	sender := &fakePenSender{active: true}
	dw := newTestDiskWidgetWithSender(sender)
	tracker := &penCaptureTracker{}

	dw.forwardPenState("id", tracker, platform.PenCaptureState{
		InRange: true, TipSwitch: true,
		X: platform.PenMaxX, Y: platform.PenMaxY / 2, Pressure: platform.PenMaxPressure,
	})

	c := sender.calls[0]
	if c.x != 1.0 {
		t.Errorf("x = %v, want 1.0 (X at MaxX)", c.x)
	}
	if diff := c.y - 0.5; diff > 0.001 || diff < -0.001 {
		t.Errorf("y = %v, want ~0.5 (Y at MaxY/2)", c.y)
	}
	if c.pressure != 1.0 {
		t.Errorf("pressure = %v, want 1.0 (Pressure at MaxPressure)", c.pressure)
	}
}

func TestForwardPenStateTiltAndRotationUnknownSentinels(t *testing.T) {
	sender := &fakePenSender{active: true}
	dw := newTestDiskWidgetWithSender(sender)
	tracker := &penCaptureTracker{}

	// No tilt/rotation reported (a standard pen with no Art Pen features) ->
	// both sentinels.
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true})
	c := sender.calls[0]
	if c.tilt != liTiltUnknown {
		t.Errorf("tilt = %d, want liTiltUnknown (%d)", c.tilt, liTiltUnknown)
	}
	if c.rotation != liRotUnknown {
		t.Errorf("rotation = %d, want liRotUnknown (%d)", c.rotation, liRotUnknown)
	}

	sender.calls = nil
	dw.forwardPenState("id", tracker, platform.PenCaptureState{InRange: true, TiltX: 30, TiltY: 40, Rotation: 100})
	c = sender.calls[0]
	if c.tilt == liTiltUnknown || c.tilt == 0 {
		t.Errorf("tilt = %d, want a real combined magnitude", c.tilt)
	}
	if c.rotation != 100 {
		t.Errorf("rotation = %d, want 100 passed through", c.rotation)
	}
}

func TestCombinedTiltDegreesClampsAndHandlesZero(t *testing.T) {
	if got := combinedTiltDegrees(0, 0); got != 0 {
		t.Errorf("combinedTiltDegrees(0,0) = %d, want 0", got)
	}
	if got := combinedTiltDegrees(90, 90); got != 90 {
		t.Errorf("combinedTiltDegrees(90,90) = %d, want clamped to 90", got)
	}
	if got := combinedTiltDegrees(-30, 40); got != 50 {
		t.Errorf("combinedTiltDegrees(-30,40) = %d, want 50 (3-4-5 triangle)", got)
	}
}
