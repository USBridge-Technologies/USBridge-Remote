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

// Target Driver Interface
package scsi

import (
	"fmt"

	"github.com/gostor/gotgt/pkg/config"
)

type SCSITargetDriver interface {
	Run(port int) error
	NewTarget(string, *config.Config) error
	RereadTargetLUNMap()
	Close() error
	Resize(uint64) error
	Stats() Stats
	SetClusterIP(string)
	EnableBlockMultipleHostLogin()
}

type Stats struct {
	IsClientConnected bool
	RevisionCounter   int64
	ReplicaCounter    int64
	SCSIIOCount       map[int]int64

	ReadIOPS            int64
	TotalReadTime       int64
	TotalReadBlockCount int64

	WriteIOPS            int64
	TotalWriteTime       int64
	TotalWriteBlockCount int64

	UsedLogicalBlocks int64
	UsedBlocks        int64
	SectorSize        int64
}

type TargetDriverFunc func(*SCSITargetService) (SCSITargetDriver, error)

var registeredPlugins = map[string](TargetDriverFunc){}

func RegisterTargetDriver(name string, f TargetDriverFunc) {
	registeredPlugins[name] = f
}

func NewTargetDriver(tgtDriver string, s *SCSITargetService) (SCSITargetDriver, error) {
	if tgtDriver == "" {
		return nil, nil
	}
	targetInitFunc, ok := registeredPlugins[tgtDriver]
	if !ok {
		return nil, fmt.Errorf("SCSI target driver %s is not found.", tgtDriver)
	}
	return targetInitFunc(s)
}
