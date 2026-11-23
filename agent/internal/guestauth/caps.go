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
	RoleCustom     Role = "custom"     // granular checkbox policy matrix
)

// Cap is one capability a route or channel can require. A route that names
// none is owner-only: guests and scoped local tokens are denied by default.
type Cap uint32

const (
	CapView          Cap = 1 << iota // 0x0001: see screen
	CapAudio                         // 0x0002: hear audio
	CapInput                         // 0x0004: mouse and keyboard
	CapPenTablet                     // 0x0008: Wacom / digitizer / RawHID pen tablet
	CapGamepad                       // 0x0010: gamepads
	CapClipToHost                    // 0x0020: clipboard Client -> Host (paste into workstation)
	CapClipFromHost                  // 0x0040: clipboard Host -> Client (copy out of workstation)
	CapFileTransfer                  // 0x0080: clipboard files, drag&drop, upload/download
	CapUSB                           // 0x0100: general USB passthrough (filtered by USBPolicy)
	CapDisks                         // 0x0200: mass storage / flash drives / MTP / NBD / ISO
	CapDisplay                       // 0x0400: virtual displays, monitor enable/primary
	CapVideoSettings                 // 0x0800: codec, FEC, video source settings
)

// CapClipboard matches bidirectional clipboard access (legacy convenience mask).
const CapClipboard = CapClipToHost | CapClipFromHost

// USBPolicy controls which real USB device classes (verified by usb-broker
// via live OP_REQ_DEVLIST + HID usage page probing) may be attached.
// Default-deny: any class not explicitly allowed is rejected.
type USBPolicy struct {
	// AllowHID permits USB HID (class 0x03) input devices other than pen
	// tablets, including keyboards, mice, and 3D manipulators such as
	// 3Dconnexion SpaceMouse (Generic Desktop 0x01 / Multi-axis Controller 0x08).
	AllowHID bool `json:"allow_hid"`
	// AllowPenTablet permits HID digitizers (usage page 0x0D) and Wacom
	// vendor-specific interfaces (usage page 0xFF0D).
	AllowPenTablet bool `json:"allow_pen_tablet"`
	// AllowAudio permits USB Audio class (0x01) devices.
	AllowAudio bool `json:"allow_audio"`
	// AllowMassStorage permits USB Mass Storage (class 0x08). Must be false
	// for TPN (Disney/Netflix) compliance.
	AllowMassStorage bool `json:"allow_mass_storage"`
	// AllowOther permits any other USB interface class (CDC, vendor, etc.).
	AllowOther bool `json:"allow_other"`
	// AllowedVIDPIDs is an optional list of "vvvv:pppp" lowercase hex IDs
	// explicitly permitted (e.g. hardware license dongles). Mass Storage
	// interfaces are still blocked unless AllowMassStorage is true.
	AllowedVIDPIDs []string `json:"allowed_vid_pids,omitempty"`
}

// Policy is the complete granular permission matrix for a share or local token.
type Policy struct {
	Caps Cap       `json:"caps"`
	USB  USBPolicy `json:"usb"`
}

// Has reports whether every capability bit in need is granted by p.
func (p Policy) Has(need Cap) bool { return need != 0 && p.Caps&need == need }

// HasAny reports whether at least one capability bit in mask is granted by p.
func (p Policy) HasAny(mask Cap) bool { return mask != 0 && p.Caps&mask != 0 }

// PresetVFXTPN returns the strict TPN (Disney/Netflix) VFX artist preset:
// screen, audio, mouse/keyboard, Wacom pen tablet, and display layout are
// allowed; clipboard, file transfer, USB flash drives, and arbitrary USB are
// strictly blocked.
func PresetVFXTPN() Policy {
	return Policy{
		Caps: CapView | CapAudio | CapInput | CapPenTablet | CapDisplay,
		USB: USBPolicy{
			AllowHID:       true,
			AllowPenTablet: true,
		},
	}
}

// PresetViewer returns the view-only preset (dailies / review).
func PresetViewer() Policy {
	return Policy{
		Caps: CapView | CapAudio,
	}
}

// PresetOperator returns the operator preset (screen, audio, input, clipboard).
func PresetOperator() Policy {
	return Policy{
		Caps: CapView | CapAudio | CapInput | CapClipToHost | CapClipFromHost,
		USB: USBPolicy{
			AllowHID: true,
		},
	}
}

// PresetTechnician returns full non-owner capabilities (all devices & settings).
func PresetTechnician() Policy {
	return Policy{
		Caps: CapView | CapAudio | CapInput | CapPenTablet | CapGamepad |
			CapClipToHost | CapClipFromHost | CapFileTransfer |
			CapUSB | CapDisks | CapDisplay | CapVideoSettings,
		USB: USBPolicy{
			AllowHID:         true,
			AllowPenTablet:   true,
			AllowAudio:       true,
			AllowMassStorage: true,
			AllowOther:       true,
		},
	}
}

// PolicyForRole translates a legacy role into its equivalent Policy.
func PolicyForRole(r Role) Policy {
	switch r {
	case RoleViewer:
		return PresetViewer()
	case RoleOperator:
		return PresetOperator()
	case RoleTechnician:
		return PresetTechnician()
	default:
		return Policy{}
	}
}

var roleCaps = map[Role]Cap{
	RoleViewer:     PresetViewer().Caps,
	RoleOperator:   PresetOperator().Caps,
	RoleTechnician: PresetTechnician().Caps,
}

// ValidRole reports whether r is one of the known roles.
func ValidRole(r Role) bool {
	if r == RoleCustom {
		return true
	}
	_, ok := roleCaps[r]
	return ok
}

// Caps returns what role may do; an unknown role may do nothing.
func (r Role) Caps() Cap { return roleCaps[r] }

// Has reports whether every capability in need is granted by role.
func (r Role) Has(need Cap) bool { return need != 0 && r.Caps()&need == need }
