package app

import (
	"testing"

	"usbridge_agent/internal/guestauth"
	"usbridge_agent/internal/streamhost"
)

func TestGuestMoonlightPerm(t *testing.T) {
	viewer, _ := guestMoonlightPerm(guestauth.RoleViewer)
	if viewer&streamhost.PermInputKbd != 0 || viewer&streamhost.PermInputMouse != 0 || viewer&streamhost.PermClipboardSet != 0 {
		t.Errorf("viewer may not have input or clipboard: %#x", viewer)
	}
	if viewer&streamhost.PermView == 0 || viewer&streamhost.PermLaunch == 0 {
		t.Errorf("viewer must be able to start the stream: %#x", viewer)
	}
	for _, role := range []guestauth.Role{guestauth.RoleOperator, guestauth.RoleTechnician} {
		p, _ := guestMoonlightPerm(role)
		if p&streamhost.PermInputKbd == 0 || p&streamhost.PermServerCmd != 0 {
			t.Errorf("%s: input yes, server commands no: %#x", role, p)
		}
	}
	if p, _ := guestMoonlightPerm("nobody"); p != 0 {
		t.Errorf("unknown role got %#x", p)
	}
}
