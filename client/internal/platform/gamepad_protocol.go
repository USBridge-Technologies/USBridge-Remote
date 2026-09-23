package platform

// MoonlightButton* are the XInput-convention wButtons bit flags Moonlight's
// wire protocol (and this project's synthetic Xbox 360 USB/IP export,
// usbpass/x360_backend.go) both use. Every other place in this codebase
// that needs these values today hardcodes its own copy of the same numbers
// (gamepad_sdlmap.go's sdlButtonFlags, gamepad_capture_windows.go's
// winmmButtonToMoonlight, x360_backend.go's X360State doc comment) --
// exported here so a new consumer (gamepad_capture_wasm.go's browser
// Gamepad API poller) has one to import instead of adding a fourth copy.
const (
	MoonlightButtonDPadUp        uint16 = 0x0001
	MoonlightButtonDPadDown      uint16 = 0x0002
	MoonlightButtonDPadLeft      uint16 = 0x0004
	MoonlightButtonDPadRight     uint16 = 0x0008
	MoonlightButtonStart         uint16 = 0x0010
	MoonlightButtonBack          uint16 = 0x0020
	MoonlightButtonLeftStick     uint16 = 0x0040
	MoonlightButtonRightStick    uint16 = 0x0080
	MoonlightButtonLeftShoulder  uint16 = 0x0100
	MoonlightButtonRightShoulder uint16 = 0x0200
	MoonlightButtonGuide         uint16 = 0x0400
	MoonlightButtonA             uint16 = 0x1000
	MoonlightButtonB             uint16 = 0x2000
	MoonlightButtonX             uint16 = 0x4000
	MoonlightButtonY             uint16 = 0x8000
)
