//go:build !linux

package capture

func InitPortalSession() error    { return nil }
func GetPortalSession() string    { return "" }
func GetPortalPipeWireNodeID() uint32 { return 0 }
func GetPortalPipeWireFD() int    { return 0 }
