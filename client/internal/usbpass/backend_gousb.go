//go:build usbpass_gousb

package usbpass

import (
	"encoding/binary"
	"fmt"

	"github.com/google/gousb"
	"github.com/sirupsen/logrus"
)

// TryClaimGousb opens the device by VID/PID and replaces the descriptor
// backend with a claim-capable one. Build with -tags usbpass_gousb and
// link against libusb-1.0.
func TryClaimGousb(dev *ExportedDevice) error {
	ctx := gousb.NewContext()
	devices, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return uint16(desc.Vendor) == dev.VID && uint16(desc.Product) == dev.PID
	})
	if err != nil {
		_ = ctx.Close()
		return err
	}
	if len(devices) == 0 {
		_ = ctx.Close()
		return fmt.Errorf("gousb: no device %04x:%04x", dev.VID, dev.PID)
	}
	d := devices[0]
	for _, extra := range devices[1:] {
		_ = extra.Close()
	}
	cfg, err := d.Config(1)
	if err != nil {
		_ = d.Close()
		_ = ctx.Close()
		return err
	}
	intf, err := cfg.Interface(0, 0)
	if err != nil {
		_ = cfg.Close()
		_ = d.Close()
		_ = ctx.Close()
		return err
	}
	logrus.Infof("usbpass: gousb claimed %04x:%04x", dev.VID, dev.PID)
	dev.Backend = &gousbBackend{
		ctx:        ctx,
		dev:        d,
		cfg:        cfg,
		intf:       intf,
		deviceDesc:  dev.DeviceDesc,
		configDesc:  dev.ConfigDesc,
	}
	return nil
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
	// Prefer live control when possible; fall back to cached descriptors
	// for standard GET_DESCRIPTOR during early enum.
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
				src = []byte{2, 0x03}
			}
		}
		if wLength < len(src) {
			src = src[:wLength]
		}
		return 0, append([]byte(nil), src...)
	}
	data := make([]byte, wLength)
	n, err := b.dev.Control(bm, req, wValue, wIndex, data)
	if err != nil {
		return errnoEPIPE, nil
	}
	return 0, data[:n]
}

func (b *gousbBackend) HandleBulk(ep uint8, dirIn bool, length int, outData []byte) (int32, []byte) {
	addr := gousb.EndpointAddress(ep)
	if dirIn {
		addr |= 0x80
	}
	inep, err := b.intf.InEndpoint(int(addr & 0x7f))
	if dirIn {
		if err != nil {
			return errnoEPIPE, nil
		}
		buf := make([]byte, length)
		n, err := inep.Read(buf)
		if err != nil {
			return errnoEPIPE, nil
		}
		return 0, buf[:n]
	}
	outep, err := b.intf.OutEndpoint(int(addr & 0x7f))
	if err != nil {
		return errnoEPIPE, nil
	}
	n, err := outep.Write(outData)
	if err != nil {
		return errnoEPIPE, nil
	}
	_ = n
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
