// Package guestauth is guest access to this agent: people other than the
// owner who connect with their own identity, a role and an end date instead
// of the master key.
//
// The owner creates a share (an e-mail, a role, an expiry) with the master
// key; the agent keeps its own record of it (Store) -- that record, not the
// cloud, is what decides who may connect. The backend e-mails the invite and,
// once the guest signs in with the invited address, signs a short-lived
// ticket binding the share to a key the guest generated. Every guest request
// then carries that ticket plus a signature made with the guest's private
// key (Authenticator), so a stolen ticket is useless on its own. The master
// key is never given to a guest.
package guestauth

// Role is what a guest may do.
type Role string

const (
	RoleViewer     Role = "viewer"     // see and hear the screen
	RoleOperator   Role = "operator"   // + mouse, keyboard, clipboard
	RoleTechnician Role = "technician" // + USB devices, disks, display and video settings
)

// Cap is one capability a route can require. A route that names none is
// owner-only: guests are denied by default, never allowed by default.
type Cap uint16

const (
	CapView Cap = 1 << iota
	CapAudio
	CapInput
	CapClipboard
	CapUSB
	CapDisks
	CapDisplay
	CapVideoSettings
)

var roleCaps = map[Role]Cap{
	RoleViewer:     CapView | CapAudio,
	RoleOperator:   CapView | CapAudio | CapInput | CapClipboard,
	RoleTechnician: CapView | CapAudio | CapInput | CapClipboard | CapUSB | CapDisks | CapDisplay | CapVideoSettings,
}

// ValidRole reports whether r is one of the known roles.
func ValidRole(r Role) bool { _, ok := roleCaps[r]; return ok }

// Caps returns what role may do; an unknown role may do nothing.
func (r Role) Caps() Cap { return roleCaps[r] }

// Has reports whether every capability in need is granted by role.
func (r Role) Has(need Cap) bool { return need != 0 && r.Caps()&need == need }
