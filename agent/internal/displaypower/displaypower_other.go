//go:build !windows

package displaypower

// List reports ErrUnsupported off Windows.
func List() ([]Monitor, error) { return nil, ErrUnsupported }

// SetEnabled reports ErrUnsupported off Windows.
func SetEnabled(id string, on bool) error { return ErrUnsupported }

// AttachVirtual reports ErrUnsupported off Windows.
func AttachVirtual(Mode, bool, bool) (*VirtualState, error) { return nil, ErrUnsupported }

// SetPrimary reports ErrUnsupported off Windows.
func (st *VirtualState) SetPrimary(bool) error { return ErrUnsupported }

// RestoreVirtual reports ErrUnsupported off Windows.
func RestoreVirtual(*VirtualState) error { return ErrUnsupported }

// OthersOff reports ErrUnsupported off Windows.
func OthersOff(st *VirtualState) (*VirtualState, error) { return st, ErrUnsupported }

// OthersOn reports ErrUnsupported off Windows.
func OthersOn(*VirtualState) error { return ErrUnsupported }

func prepareHelperThread() {}

// PlannedVirtual reports ErrUnsupported off Windows.
func PlannedVirtual() (gdi, friendly string, err error) { return "", "", ErrUnsupported }
