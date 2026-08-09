/*
Copyright 2016 The GoStor Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.  */

package scsi

import (
	"errors"
	"strings"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/config"
)

// NewSCSILu: create a new SCSI LU
// path format <protocol>:/absolute/file/path
func NewSCSILu(bs *config.BackendStorage) (*api.SCSILu, error) {
	pathinfo := strings.SplitN(bs.Path, ":", 2)
	if len(pathinfo) < 2 {
		return nil, errors.New("invalid device path string")
	}
	backendType := pathinfo[0]
	backendPath := pathinfo[1]

	sbc := NewSBCDevice(api.TYPE_DISK)
	backing, err := NewBackingStore(backendType)
	if err != nil {
		return nil, err
	}

	var lu = &api.SCSILu{
		PerformCommand: luPerformCommand,
		DeviceProtocol: sbc,
		Storage:        backing,
		BlockShift:     bs.BlockShift,
		UUID:           bs.DeviceID,
	}

	err = backing.Open(lu, backendPath)
	if err != nil {
		return nil, err
	}
	lu.Size = backing.Size(lu)
	lu.DeviceProtocol.InitLu(lu)
	lu.Attrs.ThinProvisioning = bs.ThinProvisioning
	lu.Attrs.Online = bs.Online
	lu.Attrs.Lbppbe = 3
	return lu, nil
}

// NewLUN0: create a new fake SCSI LU
func NewLUN0() *api.SCSILu {

	sbc := NewSBCDevice(api.TYPE_UNKNOWN)
	backing, _ := NewBackingStore("null")
	var lu = &api.SCSILu{
		PerformCommand: luPerformCommand,
		DeviceProtocol: sbc,
		Storage:        backing,
		BlockShift:     api.DefaultBlockShift,
		UUID:           0,
	}

	lu.Size = backing.Size(lu)
	lu.DeviceProtocol.InitLu(lu)
	lu.Attrs.Online = false
	lu.Attrs.Lbppbe = 3
	return lu
}

func GetReservation(dev *api.SCSILu, nexusID uint64) *api.SCSIReservation {
	return nil
}

func luPerformCommand(tid int, cmd *api.SCSICommand) api.SAMStat {
	op := int(cmd.SCB[0])
	fn := cmd.Device.DeviceProtocol.PerformCommand(op)
	if fn != nil {
		fnop := fn.(SCSIDeviceOperation)
		// TODO host := cmd.ITNexus.Host
		host := 0
		cmd.State = api.SCSICommandProcessed
		return fnop.CommandPerformFunc(host, cmd)
	}
	return api.SAMStatGood
}

func luPreventRemoval(lu *api.SCSILu) bool {
	// TODO
	return false
}
