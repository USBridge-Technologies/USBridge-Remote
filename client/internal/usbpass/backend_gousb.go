//go:build usbpass_gousb

package usbpass

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/gousb"
	"github.com/sirupsen/logrus"
)

// TryClaimGousb opens the device by VID/PID (preferring Busnum/Devnum when
// set), detaches the kernel driver, claims the first interface, and replaces
// the descriptor backend with live libusb control/bulk. Build with
// -tags usbpass_gousb and link against libusb-1.0.
func TryClaimGousb(dev *ExportedDevice) error {
	ctx := gousb.NewContext()
	devices, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		if uint16(desc.Vendor) != dev.VID || uint16(desc.Product) != dev.PID {
			return false
		}
		// When BusID is a real Linux busid, Busnum/Devnum are parsed from it
		// and uniquely identify the stick (multiple same VID:PID otherwise).
		if dev.Busnum != 0 && dev.Devnum != 0 {
			return uint8(desc.Bus) == uint8(dev.Busnum) && uint8(desc.Address) == uint8(dev.Devnum)
		}
		return true
	})
	if err != nil {
		_ = ctx.Close()
		return err
	}
	if len(devices) == 0 {
		_ = ctx.Close()
		return fmt.Errorf("gousb: no device %04x:%04x bus=%d addr=%d", dev.VID, dev.PID, dev.Busnum, dev.Devnum)
	}
	d := devices[0]
	for _, extra := range devices[1:] {
		_ = extra.Close()
	}
	if err := d.SetAutoDetach(true); err != nil {
		logrus.Warnf("usbpass: SetAutoDetach: %v", err)
	}

	cfgNum, err := d.ActiveConfigNum()
	if err != nil || cfgNum == 0 {
		cfgNum = 0
		for _, c := range d.Desc.Configs {
			if cfgNum == 0 || c.Number < cfgNum {
				cfgNum = c.Number
			}
		}
		if cfgNum == 0 {
			cfgNum = 1
		}
	}
	cfg, err := d.Config(cfgNum)
	if err != nil {
		_ = d.Close()
		_ = ctx.Close()
		return fmt.Errorf("gousb config %d: %w", cfgNum, err)
	}

	ifaceNum, alt := 0, 0
	if cdesc, ok := d.Desc.Configs[cfgNum]; ok && len(cdesc.Interfaces) > 0 {
		ifaceNum = cdesc.Interfaces[0].Number
		if len(cdesc.Interfaces[0].AltSettings) > 0 {
			alt = cdesc.Interfaces[0].AltSettings[0].Alternate
		}
	}
	intf, err := cfg.Interface(ifaceNum, alt)
	if err != nil {
		_ = cfg.Close()
		_ = d.Close()
		_ = ctx.Close()
		return fmt.Errorf("gousb interface %d/%d: %w", ifaceNum, alt, err)
	}

	deviceDesc, err := readUSBDescriptor(d, 0x01, 0, 18)
	if err != nil {
		logrus.Warnf("usbpass: live device descriptor: %v (keeping synthetic)", err)
		deviceDesc = dev.DeviceDesc
	}
	configDesc, err := readConfigDescriptor(d, cfgNum)
	if err != nil {
		logrus.Warnf("usbpass: live config descriptor: %v (keeping synthetic)", err)
		configDesc = dev.ConfigDesc
	} else {
		dev.ConfigDesc = configDesc
		dev.DeviceDesc = deviceDesc
		dev.ConfigVal = uint8(cfgNum)
		dev.Interfaces = interfacesFromConfigDesc(configDesc)
	}
	if len(deviceDesc) >= 14 {
		dev.DeviceDesc = deviceDesc
		dev.Class = deviceDesc[4]
		dev.SubClass = deviceDesc[5]
		dev.Protocol = deviceDesc[6]
		dev.BCDDevice = binary.LittleEndian.Uint16(deviceDesc[12:14])
		if len(deviceDesc) >= 18 {
			dev.NumConfigs = deviceDesc[17]
		}
	}
	// Prefer sysfs / gousb speed over the HIGH default — required for USB3 sticks.
	if sp := resolveUSBSpeed(dev.BusID); sp != 0 {
		dev.Speed = sp
	}

	logrus.Infof("usbpass: gousb claimed %04x:%04x bus=%d addr=%d cfg=%d iface=%d speed=%d",
		dev.VID, dev.PID, d.Desc.Bus, d.Desc.Address, cfgNum, ifaceNum, dev.Speed)
	backend := &gousbBackend{
		ctx:        ctx,
		dev:        d,
		cfg:        cfg,
		intf:       intf,
		deviceDesc: deviceDesc,
		configDesc: configDesc,
		configVal:  uint8(cfgNum),
		ifaceNum:   uint8(ifaceNum),
		altNum:     uint8(alt),
		busnum:     uint32(d.Desc.Bus),
		devnum:     uint32(d.Desc.Address),
		bulkSem:    make(chan struct{}, 1),
	}
	// A stick can be left with a stale host-side endpoint-halt state from a
	// previous session (crash, unclean client exit, or a prior STALL whose
	// device-side recovery didn't reach the host controller's own endpoint
	// tracking — see clearEndpointHalt). Clear every bulk endpoint up front
	// so the very first CBW/CSW exchange doesn't inherit a leftover halt.
	for _, ep := range bulkEndpointsFromConfigDesc(configDesc, uint8(ifaceNum), uint8(alt)) {
		backend.clearEndpointHalt(ep)
	}
	dev.Backend = backend
	return nil
}

// bulkEndpointsFromConfigDesc returns the bulk endpoint addresses (with the
// IN/OUT direction bit) of one interface/altsetting, parsed straight out of
// the raw config descriptor so it works identically to interfacesFromConfigDesc.
func bulkEndpointsFromConfigDesc(cfg []byte, ifaceNum, alt uint8) []uint8 {
	var eps []uint8
	i := 0
	inTarget := false
	for i+2 <= len(cfg) {
		length := int(cfg[i])
		typ := cfg[i+1]
		if length < 2 || i+length > len(cfg) {
			break
		}
		switch {
		case typ == 0x04 && length >= 9: // INTERFACE
			inTarget = cfg[i+2] == ifaceNum && cfg[i+3] == alt
		case typ == 0x05 && length >= 7 && inTarget: // ENDPOINT
			attrs := cfg[i+3]
			if attrs&0x03 == 0x02 { // bulk
				eps = append(eps, cfg[i+2])
			}
		}
		i += length
	}
	return eps
}

func readUSBDescriptor(d *gousb.Device, descType, index uint16, length int) ([]byte, error) {
	buf := make([]byte, length)
	n, err := d.Control(0x80, 0x06, descType<<8|index, 0, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func readConfigDescriptor(d *gousb.Device, cfgNum int) ([]byte, error) {
	hdr := make([]byte, 9)
	n, err := d.Control(0x80, 0x06, 0x0200|uint16(cfgNum-1), 0, hdr)
	if err != nil || n < 4 {
		// Some stacks want cfg index 0 for the first/active config.
		n, err = d.Control(0x80, 0x06, 0x0200, 0, hdr)
		if err != nil {
			return nil, err
		}
	}
	total := int(binary.LittleEndian.Uint16(hdr[2:4]))
	if total < 9 {
		total = 9
	}
	buf := make([]byte, total)
	n, err = d.Control(0x80, 0x06, 0x0200, 0, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func interfacesFromConfigDesc(cfg []byte) [][3]uint8 {
	var out [][3]uint8
	i := 0
	for i+2 <= len(cfg) {
		length := int(cfg[i])
		if length < 2 || i+length > len(cfg) {
			break
		}
		if cfg[i+1] == 0x04 && length >= 9 { // INTERFACE
			out = append(out, [3]uint8{cfg[i+5], cfg[i+6], cfg[i+7]})
		}
		i += length
	}
	return out
}

type gousbBackend struct {
	ctx        *gousb.Context
	dev        *gousb.Device
	cfg        *gousb.Config
	intf       *gousb.Interface
	deviceDesc  []byte
	configDesc  []byte

	// What libusb actually claimed, used to answer configuration requests
	// locally instead of forwarding them to the device.
	configVal uint8
	ifaceNum  uint8
	altNum    uint8

	// Linux busnum/devnum, used to reach the usbfs node directly for
	// USBDEVFS_CLEAR_HALT — see clearEndpointHalt.
	busnum uint32
	devnum uint32

	// Set from the CBW opcode sniffed in HandleBulk's bulk-OUT path when it
	// is one we answer ourselves instead of forwarding — see
	// shortCircuitCBW's doc comment for why.
	shortCircuit     bool
	shortCircuitTag  [4]byte
	shortCircuitXfer uint32

	lastCBWDatalen  uint32
	lastCBWTransfer uint32

	lastCBWDatalen  uint32
	lastCBWTransfer uint32

	// bulkSem is a size-1 semaphore serializing the CBW/data/CSW steps of
	// Bulk-Only Transport across the whole device: BOT is strictly one
	// command in flight at a time on a given bulk pipe (the device has one
	// pending-CBW slot, not a queue), but server.go's serveURBs dispatches
	// each CMD_SUBMIT in its own goroutine to keep CMD_UNLINK responsive,
	// and a real client (confirmed live against Windows' usbip-win2 VHCI)
	// pipelines URBs — the next CBW can arrive before the previous one's
	// CSW came back. Without this, two BOT cycles' phases interleave on the
	// wire (confirmed live: a WRITE(10) CBW and a READ(10) CBW logged
	// within the same millisecond, no CSW between them), which desyncs the
	// device's own CBW/CSW state machine and was the actual cause of the
	// STALL storms this file's other recovery logic was fighting — not
	// hardware wear. A channel-based semaphore (not sync.Mutex) so a queued
	// call can still abandon waiting the moment CMD_UNLINK cancels its ctx.
	// Guarded by bulkSemMu because acquireBulk can replace it outright (see
	// its own doc comment) — a plain channel var would race between a
	// reader draining the old one and a writer swapping it.
	bulkSemMu sync.Mutex
	bulkSem   chan struct{}

	// cycleHeld reports whether bulkSem is currently held on behalf of an
	// in-progress BOT cycle (CBW-out, its optional data phase(s), and its
	// CSW-in) rather than just the one URB HandleBulk is answering right
	// now. Each of those steps is a *separate* URB — and therefore a
	// separate HandleBulk call, frequently on a different goroutine per
	// server.go's async dispatch — so acquiring and releasing bulkSem
	// within a single HandleBulk call only serialized individual URBs, not
	// whole BOT cycles: the gap between "CBW-out URB done" and "CSW-in URB
	// starts" was wide enough for a pipelined next command's CBW to slip in
	// and get answered first. Confirmed live: two different CBW opcodes
	// logged within the same millisecond, with the first one's CSW not yet
	// read — which desyncs the device's BOT state machine and produced the
	// repeated ~15s "transfer was cancelled" stalls (and eventual I/O
	// error on the Windows side) that motivated this field, not device
	// wear. See beginCycle/endCycle.
	cycleMu   sync.Mutex
	cycleHeld bool
}

// acquireBulk serializes entry into the BOT command cycle; see bulkSem.
// Returns false if ctx was cancelled before a turn was granted (the URB was
// unlinked while still queued — nothing was sent to the device for it).
//
// Deadlock backstop: gousb's cancellation path (transfer.go's wait()) asks
// libusb to cancel, then bounds its own wait on libusb's completion
// callback (see wait()'s own 5s bound) rather than blocking forever, but
// this is still a second, coarser line of defense — nothing here can prove
// every device/kernel/driver combination in the field always honors that.
// Waiting past holdTimeout discards the old channel and hands out a fresh
// one instead of waiting on it forever; the abandoned goroutine, if it ever
// does return, releases into a channel nobody is reading from anymore,
// which is a harmless no-op.
//
// holdTimeout has to cover a whole BOT cycle now, not just one HandleBulk
// call: bulkSem is held from a CBW's first URB through its CSW (see
// cycleHeld), and each step in between can itself retry once after its own
// 15s cap (HandleBulk's freshCtx) — CBW-out, one or more data phases, and
// CSW-in each worth up to ~30s in the worst case. 120s gives real headroom
// over that worst-case chain (confirmed live: legitimate single-step stalls
// on a loaded flash controller reaching into the tens of seconds) so this
// backstop only ever fires for an actual stuck cycle, not a slow-but-alive
// one — firing on the latter would reset the semaphore mid-cycle and
// reopen exactly the interleaving race cycleHeld exists to close.
func (b *gousbBackend) acquireBulk(ctx context.Context) bool {
	const holdTimeout = 120 * time.Second
	for {
		b.bulkSemMu.Lock()
		sem := b.bulkSem
		b.bulkSemMu.Unlock()

		timer := time.NewTimer(holdTimeout)
		select {
		case sem <- struct{}{}:
			timer.Stop()
			return true
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
			b.bulkSemMu.Lock()
			if b.bulkSem == sem { // nobody else already recovered it
				logrus.Warnf("usbpass: bulk semaphore held past %s (a prior transfer never returned) — resetting", holdTimeout)
				b.bulkSem = make(chan struct{}, 1)
			}
			b.bulkSemMu.Unlock()
			// Loop: try again against whichever channel is now current.
		}
	}
}

func (b *gousbBackend) releaseBulk() {
	b.bulkSemMu.Lock()
	sem := b.bulkSem
	b.bulkSemMu.Unlock()
	select {
	case <-sem:
	default:
		// sem was already reset out from under us by the holdTimeout path
		// above; nothing to release.
	}
}

// beginCycle acquires bulkSem for the URB HandleBulk is about to answer.
// newCBW must be true only for a fresh CBW-out submission (31 bytes,
// "USBC" signature) — see cycleHeld's doc comment for why this can't just
// be "acquire on every call": a fresh CBW must actually contend for the
// semaphore (a previous cycle's CSW may still be unread), but every other
// step of an already-open cycle — a data phase or the CSW-in read — must
// reuse the same hold instead of racing a concurrent CBW for it, or the
// same interleaving this exists to prevent happens one level up.
func (b *gousbBackend) beginCycle(ctx context.Context, newCBW bool) bool {
	if !newCBW {
		b.cycleMu.Lock()
		held := b.cycleHeld
		b.cycleMu.Unlock()
		if held {
			return true
		}
		// Arrived with no cycle open — not a spec-conformant BOT sequence,
		// but fall back to a transient acquire so it's still serialized
		// against any real concurrent CBW rather than racing the wire.
	}
	if !b.acquireBulk(ctx) {
		return false
	}
	b.cycleMu.Lock()
	b.cycleHeld = true
	b.cycleMu.Unlock()
	return true
}

// endCycle releases bulkSem, closing out the BOT cycle beginCycle opened.
// Called once per cycle: on the CSW actually being delivered (success), or
// on any error/timeout/cancellation that abandons the cycle — never on a
// step that expects more URBs to follow (CBW submitted, data phase done).
func (b *gousbBackend) endCycle() {
	b.cycleMu.Lock()
	b.cycleHeld = false
	b.cycleMu.Unlock()
	b.releaseBulk()
}

// clearEndpointHalt clears both the device-side STALL and the host
// controller's own endpoint-halt bookkeeping for ep (full address, with the
// IN/OUT direction bit set, e.g. 0x81). Forwarding a raw CLEAR_FEATURE
// control transfer through libusb only reaches the device; on xHCI the
// endpoint context itself latches into "Halted" on a STALL and needs an
// explicit Reset Endpoint, which is exactly what libusb_clear_halt() does
// that a raw control transfer does not.
//
// Goes through gousb's own Device.ClearHalt (vendored in third_party/gousb —
// upstream doesn't expose libusb_clear_halt at all), which calls it on the
// SAME libusb handle that already claimed the interface. Two earlier
// approaches were both confirmed live to fail: an independent second
// usbfs open() gets EBUSY (usbfs only lets the fd that actually claimed an
// interface clear a halt on its endpoints — journalctl showed "did not
// claim interface 0 before use" for every such call), and reaching into
// gousb's private handle field via reflection produced an unusable pointer
// (same kernel message, from libusb_clear_halt itself this time — the
// reflected value did not resolve to the real handle). A method added
// inside the gousb package itself has direct access to the real *Device
// and needs neither trick.
func (b *gousbBackend) clearEndpointHalt(ep uint8) {
	if err := b.dev.ClearHalt(ep); err == nil {
		logrus.Debugf("usbpass: cleared halt on ep=%#02x bus=%d addr=%d", ep, b.busnum, b.devnum)
		return
	} else {
		logrus.Debugf("usbpass: libusb clear-halt ep=%#02x bus=%d addr=%d: %v", ep, b.busnum, b.devnum, err)
	}
	if err := usbfsClearHalt(b.busnum, b.devnum, ep); err != nil {
		logrus.Debugf("usbpass: usbfs clear-halt ep=%#02x bus=%d addr=%d: %v", ep, b.busnum, b.devnum, err)
	} else {
		logrus.Debugf("usbpass: cleared halt on ep=%#02x bus=%d addr=%d (usbfs fallback)", ep, b.busnum, b.devnum)
	}
}

func (b *gousbBackend) HandleControl(ctx context.Context, setup [8]byte, wLength int) (int32, []byte) {
	_ = ctx // control transfers use gousb's own fixed ControlTimeout; only bulk is UNLINK-cancellable (see HandleBulk)
	bm := setup[0]
	req := setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])
	wIndex := binary.LittleEndian.Uint16(setup[4:6])

	// Configuration/alt-setting requests must never reach the wire: libusb
	// already put the device into this exact configuration when we claimed
	// it, and forwarding them makes the host controller tear down and rebuild
	// the endpoint contexts of the claimed interface. After that every bulk
	// transfer fails with "transfer failed" — Windows shows the stick as
	// enumerated but no SCSI command ever completes, so no drive appears.
	switch {
	case bm == 0x00 && req == 0x09: // SET_CONFIGURATION
		if uint8(wValue) != b.configVal {
			logrus.Warnf("usbpass: ignoring SET_CONFIGURATION(%d), claimed config is %d", uint8(wValue), b.configVal)
		}
		return 0, nil
	case bm == 0x01 && req == 0x0b: // SET_INTERFACE
		if uint8(wIndex) != b.ifaceNum || uint8(wValue) != b.altNum {
			logrus.Warnf("usbpass: ignoring SET_INTERFACE(iface=%d alt=%d), claimed %d/%d",
				uint8(wIndex), uint8(wValue), b.ifaceNum, b.altNum)
		}
		return 0, nil
	case bm == 0x80 && req == 0x08: // GET_CONFIGURATION
		return 0, []byte{b.configVal}
	case bm == 0x81 && req == 0x0a: // GET_INTERFACE
		return 0, []byte{b.altNum}
	case bm == 0x02 && req == 0x01 && wValue == 0x0000: // CLEAR_FEATURE(ENDPOINT_HALT)
		// See clearEndpointHalt: a raw forwarded control transfer only
		// clears the device side, not the host controller's own endpoint
		// state, so recovery after a STALL needs the real usbfs ioctl.
		b.clearEndpointHalt(uint8(wIndex))
		return 0, nil
	}

	if bm == 0x80 && req == 0x06 {
		descType := uint8(wValue >> 8)
		var src []byte
		switch descType {
		case 0x01:
			src = b.deviceDesc
		case 0x02:
			src = b.configDesc
		case 0x03:
			if uint8(wValue&0xff) == 0 {
				src = []byte{4, 0x03, 0x09, 0x04}
			} else {
				// Fall through to live control for string descriptors.
				goto live
			}
		}
		if src != nil {
			if wLength < len(src) {
				src = src[:wLength]
			}
			return 0, append([]byte(nil), src...)
		}
	}
live:
	data := make([]byte, wLength)
	n, err := b.dev.Control(bm, req, wValue, wIndex, data)
	logrus.Debugf("usbpass: control bm=%#02x req=%#02x wValue=%#04x wIndex=%#04x wLength=%d -> n=%d err=%v",
		bm, req, wValue, wIndex, wLength, n, err)
	if err != nil {
		return errnoEPIPE, nil
	}
	return 0, data[:n]
}

// shortCircuitCBW reports whether opcode should be answered directly (CSW
// CHECK CONDITION, no data) instead of forwarded to the real device.
//
// 0xA2 (SECURITY PROTOCOL IN) is Windows' IEEE 1667 / TCG Opal probe that
// runs on every new USB disk arrival to check for hardware encryption
// support. A stick that doesn't implement it correctly rejects it by
// STALLing the bulk-IN endpoint — valid per the BOT spec, and we do recover
// from that STALL correctly (see clearEndpointHalt) — but confirmed live
// that Windows' own port error-recovery counts repeated endpoint STALLs as
// a sign of a malfunctioning port and full-resets it, which restarts
// enumeration from GET_DESCRIPTOR. Since the stick stalls this exact probe
// every single time, that reset loops forever and the disk never finishes
// mounting. Answering it ourselves — CHECK CONDITION, zero data, no
// STALL — is what a device that rejects the command "cleanly" (without
// stalling) looks like from the host's side, and breaks the loop.
func shortCircuitCBW(opcode byte) bool {
	return opcode == 0xa2 // SECURITY PROTOCOL IN
}

// isStall reports whether err is (or wraps) a libusb STALL/pipe condition —
// the two error shapes gousb actually returns for it (a TransferStatus from
// the transfer path, or the raw ErrorPipe from a synchronous libusb call).
func isStall(err error) bool {
	if err == nil {
		return false
	}
	if ts, ok := err.(gousb.TransferStatus); ok {
		return ts == gousb.TransferStall
	}
	if e, ok := err.(gousb.Error); ok {
		return e == gousb.ErrorPipe
	}
	return errors.Is(err, gousb.TransferStall) || errors.Is(err, gousb.ErrorPipe)
}

// isCancelled reports whether err is gousb's TransferCancelled — what
// ReadContext/WriteContext return whenever their ctx is Done, regardless of
// *why* (our own local WithTimeout expiring, or the caller's ctx being
// cancelled because CMD_UNLINK arrived). ctx.Err() is what distinguishes
// those two cases (see the two call sites below): DeadlineExceeded is us,
// worth a recovery retry; Canceled is the caller's, meaning Windows already
// gave up on this exact URB and a retry would just be racing a RET_UNLINK
// that already went out.
func isCancelled(err error) bool {
	if err == nil {
		return false
	}
	if ts, ok := err.(gousb.TransferStatus); ok {
		return ts == gousb.TransferCancelled
	}
	return errors.Is(err, gousb.TransferCancelled)
}

func (b *gousbBackend) HandleBulk(reqCtx context.Context, ep uint8, dirIn bool, length int, outData []byte) (int32, []byte) {
	// Only one CBW/data/CSW cycle may be in flight on the device at a time,
	// no matter how many URBs server.go's serveURBs has dispatched
	// concurrently — see cycleHeld's doc comment for why the hold has to
	// span the whole cycle (multiple URBs/HandleBulk calls), not just this
	// one call.
	isNewCBW := !dirIn && len(outData) == 31 && outData[0] == 'U' && outData[1] == 'S' && outData[2] == 'B' && outData[3] == 'C'
	if !b.beginCycle(reqCtx, isNewCBW) {
		return errnoEPIPE, nil
	}
	// cycleDone defaults to true (this call also closes out the cycle) and
	// is only cleared at the specific points below where more URBs for the
	// same cycle are still expected (CBW submitted, a non-CSW data phase
	// completed) — every error/timeout return and the actual CSW delivery
	// leave it true, releasing the hold for the next command.
	cycleDone := true
	defer func() {
		if cycleDone {
			b.endCycle()
		}
	}()

	num := int(ep & 0x7f)
	fullAddr := num
	if dirIn {
		fullAddr |= 0x80
	}
	// reqCtx is cancelled the instant a CMD_UNLINK for this exact URB
	// arrives (see server.go's serveURBs) — deriving from it, not
	// context.Background(), is what makes that cancellation actually reach
	// libusb's blocking ReadContext/WriteContext below instead of leaving
	// them running for the full 15s regardless. freshCtx hands out a new
	// 15s-capped context each call so a recovery retry (below) gets its own
	// full timeout window rather than reusing one that just expired.
	freshCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(reqCtx, 15*time.Second)
	}
	ctx, cancel := freshCtx()
	defer cancel()
	// recoverable reports whether err is worth one clear-halt-and-retry
	// attempt: a real STALL always is; a cancelled transfer only is when
	// *our own* 15s timeout is what fired — if reqCtx itself is already
	// Done, Windows sent CMD_UNLINK for this exact URB and retrying would
	// just race the RET_UNLINK serveURBs already sent for it.
	recoverable := func(err error) bool {
		if isStall(err) {
			return true
		}
		return isCancelled(err) && reqCtx.Err() == nil
	}
	if dirIn {
		if length <= 0 {
			cycleDone = false // nothing concluded; the real CSW read is still to come
			return 0, nil
		}
		if b.shortCircuit {
			// See shortCircuitCBW: this command's data/CSW phases are
			// synthesized entirely in software, without ever touching the
			// real device, so it can't STALL. A CSW is always exactly 13
			// bytes; anything else here is the (empty) data phase.
			if length != 13 {
				cycleDone = false // synthesized data phase; the synthesized CSW is still to come
				return 0, nil
			}
			csw := make([]byte, 13)
			copy(csw[0:4], "USBS")
			copy(csw[4:8], b.shortCircuitTag[:])
			binary.LittleEndian.PutUint32(csw[8:12], b.shortCircuitXfer)
			csw[12] = 1 // CHECK CONDITION — command not supported
			b.shortCircuit = false
			logrus.Debugf("usbpass: CSW status=1 residue=%d (short-circuited)", b.shortCircuitXfer)
			return 0, csw
		}
		inep, err := b.intf.InEndpoint(num)
		if err != nil {
			logrus.Debugf("usbpass: bulk IN ep=%d: %v", num, err)
			return errnoEPIPE, nil
		}
		// libusb rejects a transfer with "device sent more data than
		// requested" (babble) if the device's reply doesn't fit the buffer
		// exactly — confirmed live against a real SanDisk stick, whose SCSI
		// responses don't always match byte-for-byte what Windows asked
		// this particular URB to carry. A device only ever sends whole
		// wMaxPacketSize chunks, so over-allocating to the next multiple of
		// it (libusb's own documented recommendation for this exact error)
		// absorbs that without changing what we hand back upstream — still
		// truncated to `length`, so Windows never sees the difference.
		bufLen := length
		if mps := inep.Desc.MaxPacketSize; mps > 0 && bufLen%mps != 0 {
			bufLen += mps - bufLen%mps
		}
		buf := make([]byte, bufLen)
		n, err := inep.ReadContext(ctx, buf)
		if n > length {
			n = length
		}
		if err != nil {
			if n > 0 && isStall(err) {
				logrus.Debugf("usbpass: bulk IN ep=%d len=%d short read (%d bytes) with STALL, clearing halt immediately", num, length, n)
				b.clearEndpointHalt(uint8(fullAddr))
				err = nil
			} else if n == 0 {
				logrus.Debugf("usbpass: bulk IN ep=%d len=%d: %v", num, length, err)
				if recoverable(err) {
					// STALL, or our own timeout with no UNLINK yet - clear halt
					// (see clearEndpointHalt) and retry once, with a fresh 15s
					// window, instead of bubbling an error up to Windows, which
					// would otherwise abort the whole SCSI command over what a
					// real device recovers from routinely.
					b.clearEndpointHalt(uint8(fullAddr))
					retryCtx, retryCancel := freshCtx()
					n, err = inep.ReadContext(retryCtx, buf)
					retryCancel()
					if n > length {
						n = length
					}
					if err != nil && n == 0 {
						logrus.Debugf("usbpass: bulk IN ep=%d len=%d after clear-halt retry: %v", num, length, err)
						return errnoEPIPE, nil
					}
				} else {
					return errnoEPIPE, nil
				}
			}
		}
		// A CSW is exactly 13 bytes, signature "USBS" at offset 0, status
		// byte at offset 12 (0=pass, 1=fail, 2=phase error) — log it even on
		// success, it's the one thing that tells a completed SCSI command
		// from a silently-wrong one.
		isCSW := n == 13 && buf[0] == 'U' && buf[1] == 'S' && buf[2] == 'B' && buf[3] == 'S'
		if isCSW {
			logrus.Debugf("usbpass: CSW status=%d residue=%d", buf[12], binary.LittleEndian.Uint32(buf[8:12]))
		} else {
			// A data-in phase, not the CSW — the CSW read is still to come
			// as its own URB; keep the cycle open for it.
			cycleDone = false
		}
		// Short reads are valid (ZLP / short packet); return what we got.
		return 0, buf[:n]
	}
	// A CBW is exactly 31 bytes, signature "USBC" at offset 0, opcode at
	// offset 15 — log it so a repeating failure can be tied to a specific
	// SCSI command instead of just an endpoint/length pair.
	if len(outData) == 31 && outData[0] == 'U' && outData[1] == 'S' && outData[2] == 'B' && outData[3] == 'C' {
		opcode := outData[15]
		logrus.Debugf("usbpass: CBW opcode=%#02x cdblen=%d datalen=%d dir=%s",
			opcode, outData[14], binary.LittleEndian.Uint32(outData[8:12]), map[bool]string{true: "in", false: "out"}[outData[12]&0x80 != 0])
		// A new CBW conclusively ends the previous command's cycle, whether
		// or not the host actually read back a short-circuited command's
		// data/CSW phases (confirmed live: it doesn't always) — leaving
		// b.shortCircuit set would otherwise wrongly intercept this new
		// command's own data phase with an empty read.
		b.shortCircuit = false
		if shortCircuitCBW(opcode) {
			copy(b.shortCircuitTag[:], outData[4:8])
			b.shortCircuitXfer = binary.LittleEndian.Uint32(outData[8:12])
			b.shortCircuit = true
			logrus.Debugf("usbpass: short-circuiting opcode=%#02x (not forwarded to device)", opcode)
			cycleDone = false // synthesized data/CSW phases for this command are still to come
			return 0, nil
		}

		b.lastCBWDatalen = binary.LittleEndian.Uint32(outData[8:12])
		b.lastCBWTransfer = 0
	} else {
		// Bulk OUT data phase (not a CBW).
		cycleDone = false
		b.lastCBWTransfer += uint32(len(outData))
	}

	outep, err := b.intf.OutEndpoint(num)
	if err != nil {
		logrus.Debugf("usbpass: bulk OUT ep=%d: %v", num, err)
		return errnoEPIPE, nil
	}
	// WriteContext may short-write; loop until all CBW/data bytes are out.
	off := 0
	retried := false
	for off < len(outData) {
		n, err := outep.WriteContext(ctx, outData[off:])
		if n > 0 {
			off += n
		}
		if err != nil {
			// A STALL is the case ClearHalt exists for, but a timeout with
			// no UNLINK yet (see recoverable) is worth the exact same
			// recovery attempt: ClearHalt drives a Reset Endpoint at the
			// host controller, which can un-stick an endpoint the scheduler
			// otherwise considers permanently blocked even though the
			// device itself never signalled a STALL. Confirmed live that
			// plain STALLs recover this way; worth trying once for a hard
			// timeout too, with a fresh 15s window, before giving up.
			if !retried && recoverable(err) {
				retried = true
				logrus.Debugf("usbpass: bulk OUT ep=%d len=%d: %v; clearing halt and retrying once", num, len(outData), err)
				b.clearEndpointHalt(uint8(fullAddr))
				var retryCancel context.CancelFunc
				ctx, retryCancel = freshCtx()
				defer retryCancel()
				continue
			}
			if off == 0 {
				logrus.Debugf("usbpass: bulk OUT ep=%d len=%d: %v", num, len(outData), err)
				return errnoEPIPE, nil
			}
			break
		}
		if n == 0 {
			break
		}
	}
	if off < len(outData) {
		logrus.Debugf("usbpass: bulk OUT short %d/%d", off, len(outData))
		return errnoEPIPE, nil
	}
	// A successful OUT never carries the CSW (that's always read via IN) —
	// whether this was the CBW submission or a data-out phase, at least one
	// more URB (the CSW-in, possibly preceded by more data) is still coming
	// for this cycle.
	cycleDone = false
	return 0, nil
}

func (b *gousbBackend) Close() error {
	if b.intf != nil {
		b.intf.Close()
	}
	if b.cfg != nil {
		_ = b.cfg.Close()
	}
	if b.dev != nil {
		_ = b.dev.Close()
	}
	if b.ctx != nil {
		return b.ctx.Close()
	}
	return nil
}

