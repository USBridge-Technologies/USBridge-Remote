//go:build windows || (js && wasm)

package platform

import (
	_ "embed"
	"strconv"
	"strings"
)

// sdlDB is the Windows DirectInput part of SDL_GameControllerDB (see the header
// of the file for its license and origin).
//
//go:embed gamecontrollerdb_windows.txt
var sdlDB string

// sdlMappingFor returns the SDL mapping of a DirectInput pad by USB vendor and
// product id, or nil when the database does not know it.
func sdlMappingFor(vid, pid uint16) *sdlMapping {
	m := lookupSDLMapping(sdlDB, vid, pid)
	if m != nil && isDS4Family(vid, pid) {
		m.withDS4Defaults()
	}
	return m
}

// genericWinMMName is what WinMM reports for practically every DirectInput pad
// (it is the name of the inbox driver, not of the pad).
const genericWinMMName = "PC-joystick driver"

// friendlyPadName replaces WinMM's generic driver name with the pad's real
// model from the SDL database ("Razer Raiju TE"), so the device list does not
// show every DirectInput pad as "Microsoft PC-joystick driver". Any other name
// is kept.
func friendlyPadName(name, vendorID, productID string) string {
	if !strings.Contains(name, genericWinMMName) {
		return name
	}
	parse := func(s string) (uint16, bool) {
		v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 16)
		return uint16(v), err == nil
	}
	vid, ok1 := parse(vendorID)
	pid, ok2 := parse(productID)
	if !ok1 || !ok2 {
		return name
	}
	if m := sdlMappingFor(vid, pid); m != nil && m.name != "" {
		return m.name
	}
	return name
}
