package mock

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/config" /* init lib */
	"github.com/gostor/gotgt/pkg/port/iscsit"
	"github.com/gostor/gotgt/pkg/scsi"
	_ "github.com/gostor/gotgt/pkg/scsi/backingstore" /* init lib */
	"github.com/gostor/gotgt/pkg/scsi/backingstore/remote"
)

type remoteBs struct {
	Volume     string
	Size       int64
	SectorSize int

	isUp bool
	rw   api.RemoteBackingStore

	tgtName      string
	lhbsName     string
	clusterIP    string
	cfg          *config.Config
	targetDriver scsi.SCSITargetDriver
	stats        scsi.Stats
}

func (r *remoteBs) ReadAt(data []byte, size int64) (int, error) {
	return 0, nil
}

func (r *remoteBs) WriteAt(data []byte, size int64) (int, error) {
	return 0, nil
}

func (r *remoteBs) Sync() (int, error) {
	return 0, nil
}

func (r *remoteBs) Unmap(bs int64, size int64) (int, error) {
	return 0, nil
}

func initializeSCSITarget(size int64) {
	iscsit.EnableStats = true
	scsi.EnableORWrite16 = false
	scsi.EnablePersistentReservation = false
	scsi.EnableMultipath = false
	remote.Size = uint64(size)
}

// Startup starts iscsi target server
func (r *remoteBs) Startup(name string, frontendIP string, clusterIP string, size, sectorSize int64) error {
	initializeSCSITarget(size)

	if frontendIP == "" {
		host, _ := os.Hostname()
		addrs, _ := net.LookupIP(host)
		for _, addr := range addrs {
			if ipv4 := addr.To4(); ipv4 != nil {
				frontendIP = ipv4.String()
				if frontendIP == "127.0.0.1" {
					continue
				}
				break
			}
		}
	}

	r.tgtName = "iqn.2016-09.com.gotgt.gostor:" + name
	r.lhbsName = "RemBs:" + name
	r.cfg = &config.Config{
		Storages: []config.BackendStorage{
			{
				DeviceID: 1000,
				Path:     r.lhbsName,
				Online:   true,
			},
		},
		ISCSIPortals: []config.ISCSIPortalInfo{
			{
				ID:     0,
				Portal: frontendIP + ":3260",
			},
		},
		ISCSITargets: map[string]config.ISCSITarget{
			r.tgtName: {
				TPGTs: map[string][]uint64{
					"1": {0},
				},
				LUNs: map[string]uint64{
					"1": uint64(1000),
				},
			},
		},
	}

	r.Volume = name
	r.Size = size
	r.SectorSize = int(sectorSize)
	r.rw = r
	r.clusterIP = clusterIP
	logrus.Info("Start SCSI target")
	if err := r.startScsiTarget(r.cfg); err != nil {
		return err
	}

	r.isUp = true

	return nil
}

// Shutdown stop scsi target
func (r *remoteBs) Shutdown() error {
	if r.Volume != "" {
		r.Volume = ""
	}

	if err := r.stopScsiTarget(); err != nil {
		return fmt.Errorf("Failed to stop scsi target, err: %v", err)
	}
	r.isUp = false

	return nil
}

// State provides info whether scsi target is up or down
func (r *remoteBs) State() string {
	if r.isUp {
		return "Up"
	}
	return "Down"
}

// Stats get target stats from the scsi target
func (r *remoteBs) Stats() scsi.Stats {
	if !r.isUp {
		return scsi.Stats{}
	}
	return r.targetDriver.Stats()
}

//  Resize is called to resize the volume
func (r *remoteBs) Resize(size uint64) error {
	if !r.isUp {
		return fmt.Errorf("Volume is not up")
	}
	return r.targetDriver.Resize(size)
}

func (r *remoteBs) startScsiTarget(cfg *config.Config) error {
	var err error
	id := uuid.NewV4()
	uid := binary.BigEndian.Uint64(id[:8])
	err = scsi.InitSCSILUMapEx(&config.BackendStorage{
		DeviceID:         uid,
		Path:             "RemBs:" + r.tgtName,
		Online:           true,
		BlockShift:       9,
		ThinProvisioning: true,
	},
		r.tgtName, uint64(0), r.rw)
	if err != nil {
		return err
	}
	scsiTarget := scsi.NewSCSITargetService()
	r.targetDriver, err = scsi.NewTargetDriver("iscsi", scsiTarget)
	if err != nil {
		logrus.Errorf("iscsi target driver error")
		return err
	}
	r.targetDriver.NewTarget(r.tgtName, cfg)
	//r.targetDriver.SetClusterIP(r.clusterIP)
	go r.targetDriver.Run(3260)
	// Wait here so that listener get started
	time.Sleep(1 * time.Second)

	logrus.Infof("SCSI device created")
	return nil
}

func (r *remoteBs) stopScsiTarget() error {
	if r.targetDriver == nil {
		return nil
	}
	logrus.Infof("Stopping target %v ...", r.tgtName)
	if err := r.targetDriver.Close(); err != nil {
		return err
	}
	logrus.Infof("Target %v stopped", r.tgtName)
	return nil
}
