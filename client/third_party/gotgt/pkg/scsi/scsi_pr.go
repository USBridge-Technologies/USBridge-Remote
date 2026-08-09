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
	"github.com/gostor/gotgt/pkg/api"
	"github.com/satori/go.uuid"
)

type SCSIReservationOperator interface {
	GetCurrentReservation(tgtName string, devUUID uint64) *api.SCSIReservation
	SetCurrentReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) bool
	IsCurrentReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) bool

	GetPRGeneration(tgtName string, devUUID uint64) (uint32, bool)
	IncPRGeneration(tgtName string, devUUID uint64) bool

	AddReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) bool
	GetReservation(tgtName string, devUUID uint64, ITNexusID uuid.UUID) *api.SCSIReservation
	GetReservationList(tgtName string, devUUID uint64) []*api.SCSIReservation
	DeleteAndRemoveReservation(tgtName string, devUUID uint64, res *api.SCSIReservation)
	RemoveReservation(tgtName string, devUUID uint64, res *api.SCSIReservation)
	RemoveAllReservation(tgtName string, devUUID uint64)

	IsKeyExists(tgtName string, devUUID uint64, key uint64) bool

	Save(tgtName string, devUUID uint64) bool
}

var onePROperator SCSIReservationOperator

func GetSCSIReservationOperator() SCSIReservationOperator {
	if onePROperator == nil {
		onePROperator = &SCSISimpleReservationOperator{
			targetReservations: make(map[string]SCSILUReservationMap),
		}
	}
	return onePROperator
}

type SCSILUReservation struct {
	TargetName         string
	DeviceUUID         uint64
	PRGeneration       uint32
	Reservations       []*api.SCSIReservation
	CurrentReservation *api.SCSIReservation
}

type SCSILUReservationMap map[uint64]*SCSILUReservation /* device uuid as the key */

type SCSISimpleReservationOperator struct {
	SCSIReservationOperator
	targetReservations map[string]SCSILUReservationMap /* target name as the key*/
}

func (op *SCSISimpleReservationOperator) InitLUReservation(tgtName string, devUUID uint64) {
	var (
		targetRes SCSILUReservationMap
		ok        bool
	)

	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		op.targetReservations[tgtName] = make(SCSILUReservationMap)
		targetRes = op.targetReservations[tgtName]
	}
	if _, ok = targetRes[devUUID]; !ok {
		targetRes[devUUID] = &SCSILUReservation{TargetName: tgtName,
			DeviceUUID:         devUUID,
			PRGeneration:       0,
			Reservations:       []*api.SCSIReservation{},
			CurrentReservation: nil,
		}
	}
}

func (op *SCSISimpleReservationOperator) GetReservation(tgtName string, devUUID uint64, ITNexusID uuid.UUID) *api.SCSIReservation {
	var (
		LURes     *SCSILUReservation
		SCSIRes   *api.SCSIReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return nil
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return nil
	}
	for _, SCSIRes = range LURes.Reservations {
		if uuid.Equal(SCSIRes.ITNexusID, ITNexusID) {
			return SCSIRes
		}
	}

	return nil
}

func (op *SCSISimpleReservationOperator) GetPRGeneration(tgtName string, devUUID uint64) (uint32, bool) {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return 0, false
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return 0, false
	}

	return LURes.PRGeneration, true
}

func (op *SCSISimpleReservationOperator) IncPRGeneration(tgtName string, devUUID uint64) bool {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return false
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return false
	}
	LURes.PRGeneration++
	return true
}

func (op *SCSISimpleReservationOperator) GetCurrentReservation(tgtName string, devUUID uint64) *api.SCSIReservation {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return nil
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return nil
	}
	return LURes.CurrentReservation
}

func (op *SCSISimpleReservationOperator) SetCurrentReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) bool {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return false
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return false
	}
	LURes.CurrentReservation = res
	return true
}

func (op *SCSISimpleReservationOperator) GetReservationList(tgtName string, devUUID uint64) []*api.SCSIReservation {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return nil
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return nil
	}
	return LURes.Reservations
}

func (op *SCSISimpleReservationOperator) DeleteAndRemoveReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) {
	var (
		i         int = -1
		ok        bool
		tmpRes    *api.SCSIReservation
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return
	}

	resArray := LURes.Reservations
	curRes := LURes.CurrentReservation

	for i, tmpRes = range resArray {
		if tmpRes == res {
			break
		}
	}

	if i >= 0 {
		resArray[i] = resArray[len(resArray)-1]
		resArray[len(resArray)-1] = nil
		resArray = resArray[:len(resArray)-1]
		LURes.Reservations = resArray
	}

	if curRes == nil {
		return
	}

	if !op.IsCurrentReservation(tgtName, devUUID, res) {
		return
	}

	if (curRes.Type != PR_TYPE_WRITE_EXCLUSIVE_ALLREG &&
		curRes.Type != PR_TYPE_EXCLUSIVE_ACCESS_ALLREG) ||
		len(resArray) == 0 {
		curRes.Scope = 0
		curRes.Type = 0
		LURes.CurrentReservation = nil
		for i, tmpRes = range resArray {
			if tmpRes == res {
				continue
			}
			//TODO send sense code
		}
		LURes.PRGeneration++
	} else {
		for i, tmpRes = range resArray {
			if tmpRes != res {
				//kep scope and type
				LURes.CurrentReservation = tmpRes
				tmpRes.Scope = curRes.Scope
				tmpRes.Type = curRes.Type
				break
			}
		}
		LURes.PRGeneration++
	}

}

func (op *SCSISimpleReservationOperator) RemoveReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		tmpRes    *api.SCSIReservation
		i         int = -1
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return
	}
	resArray := LURes.Reservations

	for i, tmpRes = range resArray {
		if tmpRes == res {
			break
		}
	}

	if i >= 0 {
		resArray[i] = resArray[len(resArray)-1]
		resArray[len(resArray)-1] = nil
		resArray = resArray[:len(resArray)-1]
		LURes.Reservations = resArray
	}
}

func (op *SCSISimpleReservationOperator) AddReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) bool {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return false
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return false
	}
	LURes.Reservations = append(LURes.Reservations, res)
	return true
}

func (op *SCSISimpleReservationOperator) IsKeyExists(tgtName string, devUUID uint64, key uint64) bool {

	resList := op.GetReservationList(tgtName, devUUID)
	for _, tmpRes := range resList {
		if tmpRes.Key == key {
			return true
		}
	}

	return false
}

func (op *SCSISimpleReservationOperator) IsCurrentReservation(tgtName string, devUUID uint64, res *api.SCSIReservation) bool {
	curRes := op.GetCurrentReservation(tgtName, devUUID)
	if curRes == nil {
		return false
	}
	if curRes.Type == PR_TYPE_WRITE_EXCLUSIVE_ALLREG ||
		curRes.Type == PR_TYPE_EXCLUSIVE_ACCESS_ALLREG {
		return true
	}

	if curRes == res {
		return true
	}

	return false
}

func (op *SCSISimpleReservationOperator) RemoveAllReservation(tgtName string, devUUID uint64) {
	var (
		LURes     *SCSILUReservation
		targetRes SCSILUReservationMap
		ok        bool
	)
	if targetRes, ok = op.targetReservations[tgtName]; !ok {
		return
	}
	if LURes, ok = targetRes[devUUID]; !ok {
		return
	}
	LURes.Reservations = []*api.SCSIReservation{}

}

func (op *SCSISimpleReservationOperator) Save(tgtName string, devUUID uint64) bool {
	return true
}
