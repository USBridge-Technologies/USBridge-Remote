package usbpass

// Exported facade over wacom_model.go's model resolution + hidGenBackend,
// for two callers outside this file that only know a tablet by vendor/
// product id (and, for the browser path, its productName string) rather
// than by a real device handle to read descriptors from:
//
//   - usbaes_attach_wasm.go (this same package, wasm-only): resolves the
//     same model to fill its own outgoing attachPayload's VID/PID/
//     DeviceDesc/ConfigDesc, mirroring x360DeviceDesc()/x360ConfigDesc()'s
//     role in the gamepad path.
//   - agent/internal/browserusb, via the usbpasscore alias package (a
//     different module -- see pkg/usbpasscore/export.go's own doc comment
//     for why that indirection exists at all): builds the real backend that
//     answers the exported tablet's USB traffic and receives the browser's
//     raw HID input reports.
//
// No build tag: wacom_model.go itself has none either (pure Go, already
// shared by the Windows and macOS native passthrough paths), so this
// compiles for wasm the same as everywhere else.

import "fmt"

// WacomVendorID is Wacom's USB vendor id.
const WacomVendorID = wacomVendorID

// WacomKnown reports whether vid:pid can be exported from a captured or
// database model (see wacom_model.go's doc comment on the two sources).
func WacomKnown(vid, pid uint16) bool { return wacomKnown(vid, pid) }

// WacomSession pushes raw input reports from an external source (WebHID's
// oninputreport today) into a synthetic Wacom tablet's USB export.
type WacomSession struct {
	model   *wacomModel
	backend *hidGenBackend
}

// PushReport forwards one raw input report -- report id first byte, exactly
// as the source read it (WebHID's oninputreport, hidraw and IOHID all
// already hand back this same "wire report" shape, no reassembly needed) --
// to the interface the model's descriptor declares it on. Returns false if
// the report doesn't match any known report id for this model (dropped, not
// an error: some tablets multiplex vendor-specific reports this model does
// not describe).
func (s *WacomSession) PushReport(rep []byte) bool {
	return s.model.pushReport(s.backend, rep, -1)
}

// NewWacomExportedDevice resolves vid:pid to a model (the captured
// database first, then the community descriptor database, see
// wacom_model.go) and builds the ExportedDevice + WacomSession for it.
// productName fills in the model's own USB string descriptors when the
// database doesn't already carry a captured one. Returns nil, nil if the
// tablet is unknown -- the caller (both call sites above) should surface
// that as "unsupported tablet" rather than falling back to anything else,
// same as every other platform's Wacom path already does for an
// unrecognized vid:pid.
func NewWacomExportedDevice(busID string, vid, pid uint16, productName string) (*ExportedDevice, *WacomSession, error) {
	m := wacomModelFor(vid, pid)
	if m == nil {
		m = wacomModelFromDB(vid, pid, 0, "", productName, "", nil, nil)
	}
	if m == nil {
		return nil, nil, fmt.Errorf("wacom: unknown tablet %04X:%04X", vid, pid)
	}
	dev := &ExportedDevice{BusID: busID, Path: "/sys/devices/usbridge/" + busID}
	backend := applyWacomModel(dev, m)
	return dev, &WacomSession{model: m, backend: backend}, nil
}
