//go:build windows

package platform

import _ "embed"

// sdlDB is the Windows DirectInput part of SDL_GameControllerDB (see the header
// of the file for its license and origin).
//
//go:embed gamecontrollerdb_windows.txt
var sdlDB string

// sdlMappingFor returns the SDL mapping of a DirectInput pad by USB vendor and
// product id, or nil when the database does not know it.
func sdlMappingFor(vid, pid uint16) *sdlMapping {
	return lookupSDLMapping(sdlDB, vid, pid)
}
