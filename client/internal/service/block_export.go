package service

// BlockExportRunner is the interface a disk data-plane transport must
// implement to be usable by the disk widget's mount/unmount flow. iSCSI
// (IscsiTargetRunner, in iscsi_target.go) is the only implementation today.
type BlockExportRunner interface {
	Start(port int) error
	Stop() error
	IsRunning() bool
	GetServerStatus() map[string]interface{}
	WaitReady() <-chan struct{}
	SignalReady()
	// ExportNameForConnection returns the name the initiator uses to
	// address this export (for iSCSI: the target IQN).
	ExportNameForConnection() string
	// ExportNameForAPI returns the name reported to the agent via
	// DeviceStartRequest (for iSCSI: the target IQN, also mirrored into
	// TargetIQN).
	ExportNameForAPI() string
}
