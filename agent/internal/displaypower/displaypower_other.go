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
