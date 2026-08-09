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
limitations under the License.
*/

package scsi

import (
	"fmt"
	"unsafe"

	"github.com/gostor/gotgt/pkg/api"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
)

func (s *SCSITargetService) NewSCSITarget(tid int, driverName, name string) (*api.SCSITarget, error) {
	// verify the target ID

	// verify the target's Name

	// verify the low level driver
	var target = &api.SCSITarget{
		Name:             name,
		TID:              tid,
		TargetPortGroups: []*api.TargetPortGroup{},
		ITNexus:          make(map[uuid.UUID]*api.ITNexus),
	}
	tpg := &api.TargetPortGroup{0, []*api.SCSITargetPort{}}
	s.Targets = append(s.Targets, target)
	target.Devices = GetTargetLUNMap(target.Name)
	target.LUN0 = NewLUN0()
	target.TargetPortGroups = append(target.TargetPortGroups, tpg)
	return target, nil
}

func (s *SCSITargetService) RereadTargetLUNMap() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, tgt := range s.Targets {
		tgt.Devices = GetTargetLUNMap(tgt.Name)
	}
}

func FindTargetGroup(target *api.SCSITarget, relPortID uint16) uint16 {
	for _, tpg := range target.TargetPortGroups {
		for _, port := range tpg.TargetPortGroup {
			if port.RelativeTargetPortID == relPortID {
				return tpg.GroupID
			}
		}
	}
	return 0
}

func FindTargetPort(target *api.SCSITarget, relPortID uint16) *api.SCSITargetPort {
	for _, tpg := range target.TargetPortGroups {
		for _, port := range tpg.TargetPortGroup {
			if port.RelativeTargetPortID == relPortID {
				return port
			}
		}
	}
	return nil
}

func AddITNexus(target *api.SCSITarget, itnexus *api.ITNexus) bool {
	var ret bool = true
	target.ITNexusMutex.Lock()
	defer target.ITNexusMutex.Unlock()
	if _, ok := target.ITNexus[itnexus.ID]; !ok {
		target.ITNexus[itnexus.ID] = itnexus
		ret = true
	} else {
		ret = false
	}
	return ret
}

func RemoveITNexus(target *api.SCSITarget, itnexus *api.ITNexus) {
	target.ITNexusMutex.Lock()
	defer target.ITNexusMutex.Unlock()
	delete(target.ITNexus, itnexus.ID)
}

func deviceReserve(cmd *api.SCSICommand) error {
	var lu *api.SCSILu
	lun := *(*uint64)(unsafe.Pointer(&cmd.Lun))

	for tgtLUN, lunDev := range cmd.Target.Devices {
		if tgtLUN == lun {
			lu = lunDev
			break
		}
	}
	if lu == nil {
		log.Errorf("invalid target and lun %d %d", cmd.Target.TID, lun)
		return nil
	}

	if !uuid.Equal(lu.ReserveID, uuid.Nil) && uuid.Equal(lu.ReserveID, cmd.ITNexusID) {
		log.Errorf("already reserved %d, %d", lu.ReserveID, cmd.ITNexusID)
		return fmt.Errorf("already reserved")
	}
	lu.ReserveID = cmd.ITNexusID
	return nil
}

func deviceRelease(tid int, itn uuid.UUID, lun uint64, force bool) error {
	// TODO
	return nil
}
