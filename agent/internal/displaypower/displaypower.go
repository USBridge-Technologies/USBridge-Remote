// Package displaypower turns the host's monitors on and off -- physical ones and the
// MttVDD virtual monitor alike -- the way Windows' own display settings "Disconnect this
// display" does: by adding or dropping the monitor's display path. No elevation is
// needed. Windows only; elsewhere every call reports ErrUnsupported.
package displaypower

import "errors"

// Monitor is one display target Windows knows about, active or not.
type Monitor struct {
	// ID is the monitor's device path (`\\?\DISPLAY#...`): stable while it is off,
	// unlike the GDI name, which only an active monitor has.
	ID string `json:"id"`
	// Name is the monitor's friendly (EDID) name, e.g. "ARZOPA" or "VDD by MTT".
	Name string `json:"name"`
	// Active: part of the desktop now.
	Active bool `json:"active"`
	// GDIName is `\\.\DISPLAYn` while active, "" otherwise.
	GDIName string `json:"gdi_name,omitempty"`
	// Primary: the active monitor at the desktop origin.
	Primary bool `json:"primary,omitempty"`
	// Virtual: the MttVDD virtual monitor.
	Virtual bool `json:"virtual,omitempty"`
}

var (
	// ErrUnsupported is returned off Windows.
	ErrUnsupported = errors.New("turning monitors on and off is supported on Windows only")
	// ErrNotFound: no monitor with that ID.
	ErrNotFound = errors.New("no such monitor")
	// ErrLastDisplay: refusing to switch off the only active monitor.
	ErrLastDisplay = errors.New("this is the only active display -- switching it off would leave the desktop with no screen")
	// ErrNoVirtualDriver: the MttVDD virtual display driver is not installed (or its
	// monitor has no free display output).
	ErrNoVirtualDriver = errors.New("the MttVDD virtual display is not available -- is its driver installed?")
)

// sessionRunner runs a helper command (RunHelper) in the user's session; set by a
// service (SetSessionRunner), nil when this process already is in that session.
var sessionRunner func(args ...string) (HelperResult, error)

// SetSessionRunner routes ListInSession and SetEnabledInSession through run: the
// service lives in session 0, whose display configuration is not the user's
// desktop -- listing from there named and marked the monitors wrongly.
func SetSessionRunner(run func(args ...string) (HelperResult, error)) { sessionRunner = run }

// ListInSession is List for the user's desktop, from wherever this process runs.
func ListInSession() ([]Monitor, error) {
	if sessionRunner == nil {
		return List()
	}
	res, err := sessionRunner("list", "-")
	if err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, helperError(res.Error)
	}
	return res.Monitors, nil
}

// SetEnabledInSession is SetEnabled on the user's desktop.
func SetEnabledInSession(id string, on bool) error {
	if sessionRunner == nil {
		return SetEnabled(id, on)
	}
	arg := "0"
	if on {
		arg = "1"
	}
	res, err := sessionRunner("enable", "-", id, arg)
	if err != nil {
		return err
	}
	if res.Error != "" {
		return helperError(res.Error)
	}
	return nil
}

// helperError maps a helper's error text back onto this package's errors.
func helperError(msg string) error {
	for _, e := range []error{ErrUnsupported, ErrNotFound, ErrLastDisplay, ErrNoVirtualDriver} {
		if msg == e.Error() {
			return e
		}
	}
	return errors.New(msg)
}
