//go:build usbpass_gousb

package usbpass

import (
	"context"
	"encoding/binary"
	"fmt"
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
	dev.Backend = &gousbBackend{
		ctx:        ctx,
		dev:        d,
		cfg:        cfg,
		intf:       intf,
		deviceDesc:  deviceDesc,
		configDesc:  configDesc,
	}
	return nil
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
}

func (b *gousbBackend) HandleControl(setup [8]byte, wLength int) (int32, []byte) {
	bm := setup[0]
	req := setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])
	wIndex := binary.LittleEndian.Uint16(setup[4:6])
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
	if err != nil {
		return errnoEPIPE, nil
	}
	return 0, data[:n]
}

func (b *gousbBackend) HandleBulk(ep uint8, dirIn bool, length int, outData []byte) (int32, []byte) {
	num := int(ep & 0x7f)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if dirIn {
		if length <= 0 {
			return 0, nil
		}
		inep, err := b.intf.InEndpoint(num)
		if err != nil {
			logrus.Debugf("usbpass: bulk IN ep=%d: %v", num, err)
			return errnoEPIPE, nil
		}
		buf := make([]byte, length)
		n, err := inep.ReadContext(ctx, buf)
		if err != nil && n == 0 {
			logrus.Debugf("usbpass: bulk IN ep=%d len=%d: %v", num, length, err)
			return errnoEPIPE, nil
		}
		// Short reads are valid (ZLP / short packet); return what we got.
		return 0, buf[:n]
	}
	outep, err := b.intf.OutEndpoint(num)
	if err != nil {
		logrus.Debugf("usbpass: bulk OUT ep=%d: %v", num, err)
		return errnoEPIPE, nil
	}
	// WriteContext may short-write; loop until all CBW/data bytes are out.
	off := 0
	for off < len(outData) {
		n, err := outep.WriteContext(ctx, outData[off:])
		if n > 0 {
			off += n
		}
		if err != nil {
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
