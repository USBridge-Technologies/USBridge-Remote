/*
Copyright 2017 The GoStor Authors All rights reserved.

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
package api

import (
	"errors"
	"io"
	"sync"

	uuid "github.com/satori/go.uuid"
)

type SCSICommandType byte

var (
	TEST_UNIT_READY           SCSICommandType = 0x00
	REZERO_UNIT               SCSICommandType = 0x01
	REQUEST_SENSE             SCSICommandType = 0x03
	FORMAT_UNIT               SCSICommandType = 0x04
	READ_BLOCK_LIMITS         SCSICommandType = 0x05
	REASSIGN_BLOCKS           SCSICommandType = 0x07
	INITIALIZE_ELEMENT_STATUS SCSICommandType = 0x07
	READ_6                    SCSICommandType = 0x08
	WRITE_6                   SCSICommandType = 0x0a
	SEEK_6                    SCSICommandType = 0x0b
	READ_REVERSE              SCSICommandType = 0x0f
	WRITE_FILEMARKS           SCSICommandType = 0x10
	SPACE                     SCSICommandType = 0x11
	INQUIRY                   SCSICommandType = 0x12
	RECOVER_BUFFERED_DATA     SCSICommandType = 0x14
	MODE_SELECT               SCSICommandType = 0x15
	RESERVE                   SCSICommandType = 0x16
	RELEASE                   SCSICommandType = 0x17
	COPY                      SCSICommandType = 0x18
	ERASE                     SCSICommandType = 0x19
	MODE_SENSE                SCSICommandType = 0x1a
	START_STOP                SCSICommandType = 0x1b
	RECEIVE_DIAGNOSTIC        SCSICommandType = 0x1c
	SEND_DIAGNOSTIC           SCSICommandType = 0x1d
	ALLOW_MEDIUM_REMOVAL      SCSICommandType = 0x1e

	SET_WINDOW                           SCSICommandType = 0x24
	READ_CAPACITY                        SCSICommandType = 0x25
	READ_10                              SCSICommandType = 0x28
	WRITE_10                             SCSICommandType = 0x2a
	SEEK_10                              SCSICommandType = 0x2b
	POSITION_TO_ELEMENT                  SCSICommandType = 0x2b
	WRITE_VERIFY                         SCSICommandType = 0x2e
	VERIFY_10                            SCSICommandType = 0x2f
	SEARCH_HIGH                          SCSICommandType = 0x30
	SEARCH_EQUAL                         SCSICommandType = 0x31
	SEARCH_LOW                           SCSICommandType = 0x32
	SET_LIMITS                           SCSICommandType = 0x33
	PRE_FETCH_10                         SCSICommandType = 0x34
	READ_POSITION                        SCSICommandType = 0x34
	SYNCHRONIZE_CACHE                    SCSICommandType = 0x35
	LOCK_UNLOCK_CACHE                    SCSICommandType = 0x36
	READ_DEFECT_DATA                     SCSICommandType = 0x37
	INITIALIZE_ELEMENT_STATUS_WITH_RANGE SCSICommandType = 0x37
	MEDIUM_SCAN                          SCSICommandType = 0x38
	COMPARE                              SCSICommandType = 0x39
	COPY_VERIFY                          SCSICommandType = 0x3a
	WRITE_BUFFER                         SCSICommandType = 0x3b
	READ_BUFFER                          SCSICommandType = 0x3c
	UPDATE_BLOCK                         SCSICommandType = 0x3d
	READ_LONG                            SCSICommandType = 0x3e
	WRITE_LONG                           SCSICommandType = 0x3f
	CHANGE_DEFINITION                    SCSICommandType = 0x40
	WRITE_SAME                           SCSICommandType = 0x41
	UNMAP                                SCSICommandType = 0x42
	READ_TOC                             SCSICommandType = 0x43
	GET_CONFIGURATION                    SCSICommandType = 0x46
	LOG_SELECT                           SCSICommandType = 0x4c
	LOG_SENSE                            SCSICommandType = 0x4d
	READ_DISK_INFO                       SCSICommandType = 0x51
	READ_TRACK_INFO                      SCSICommandType = 0x52
	MODE_SELECT_10                       SCSICommandType = 0x55
	RESERVE_10                           SCSICommandType = 0x56
	RELEASE_10                           SCSICommandType = 0x57
	MODE_SENSE_10                        SCSICommandType = 0x5a
	CLOSE_TRACK                          SCSICommandType = 0x5b
	READ_BUFFER_CAP                      SCSICommandType = 0x5c
	PERSISTENT_RESERVE_IN                SCSICommandType = 0x5e
	PERSISTENT_RESERVE_OUT               SCSICommandType = 0x5f
	VARLEN_CDB                           SCSICommandType = 0x7f
	READ_16                              SCSICommandType = 0x88
	COMPARE_AND_WRITE                    SCSICommandType = 0x89
	WRITE_16                             SCSICommandType = 0x8a
	ORWRITE_16                           SCSICommandType = 0x8b
	WRITE_VERIFY_16                      SCSICommandType = 0x8e
	VERIFY_16                            SCSICommandType = 0x8f
	PRE_FETCH_16                         SCSICommandType = 0x90
	SYNCHRONIZE_CACHE_16                 SCSICommandType = 0x91
	WRITE_SAME_16                        SCSICommandType = 0x93
	SERVICE_ACTION_IN                    SCSICommandType = 0x9e
	SAI_READ_CAPACITY_16                 SCSICommandType = 0x10
	SAI_GET_LBA_STATUS                   SCSICommandType = 0x12
	REPORT_LUNS                          SCSICommandType = 0xa0
	MAINT_PROTOCOL_IN                    SCSICommandType = 0xa3
	MOVE_MEDIUM                          SCSICommandType = 0xa5
	EXCHANGE_MEDIUM                      SCSICommandType = 0xa6
	READ_12                              SCSICommandType = 0xa8
	WRITE_12                             SCSICommandType = 0xaa
	GET_PERFORMANCE                      SCSICommandType = 0xac
	READ_DVD_STRUCTURE                   SCSICommandType = 0xad
	WRITE_VERIFY_12                      SCSICommandType = 0xae
	VERIFY_12                            SCSICommandType = 0xaf
	SEARCH_HIGH_12                       SCSICommandType = 0xb0
	SEARCH_EQUAL_12                      SCSICommandType = 0xb1
	SEARCH_LOW_12                        SCSICommandType = 0xb2
	READ_ELEMENT_STATUS                  SCSICommandType = 0xb8
	SEND_VOLUME_TAG                      SCSICommandType = 0xb6
	SET_STREAMING                        SCSICommandType = 0xb6
	SET_CD_SPEED                         SCSICommandType = 0xbb
	WRITE_LONG_2                         SCSICommandType = 0xea
)

type SCSITargetState int

var (
	TargetOnline SCSITargetState = 1
	TargetReady  SCSITargetState = 2
)

type SCSIDataDirection int

const (
	SCSIDataNone = iota
	SCSIDataWrite
	SCSIDataRead
	SCSIDataBidirection
)

type SenseBuffer struct {
	Buffer []byte
	Length uint32
}

type SCSIDataBuffer struct {
	Buffer         []byte
	Length         uint32
	TransferLength uint32
	Resid          uint32
}

type SCSICommandState uint64

var (
	SCSICommandQueued    SCSICommandState = 1
	SCSICommandProcessed SCSICommandState = 2
	SCSICommandAsync     SCSICommandState = 3
	SCSICommandNotLast   SCSICommandState = 4
)

type SCSICommand struct {
	OpCode          byte
	Target          *SCSITarget
	DeviceID        uint64
	Device          *SCSILu
	State           SCSICommandState
	Direction       SCSIDataDirection
	InSDBBuffer     *SCSIDataBuffer
	OutSDBBuffer    *SCSIDataBuffer
	RelTargetPortID uint16
	// Command ITN ID
	ITNexusID     uuid.UUID
	Offset        uint64
	TL            uint32
	SCB           []byte
	SCBLength     int
	Lun           [8]uint8
	Attribute     int
	Tag           uint64
	Result        byte
	SenseBuffer   *SenseBuffer
	ITNexus       *ITNexus
	ITNexusLuInfo *ITNexusLuInfo
}

type ITNexus struct {
	// UUID v1
	ID uuid.UUID `json:"id"`
	// For protocol spec identifer
	Tag string `json:"Tag"`
}

type ITNexusLuInfo struct {
	Lu      *SCSILu
	ID      uint64
	Prevent int
}

type SCSITargetPort struct {
	RelativeTargetPortID uint16
	TargetPortName       string
}

type TargetPortGroup struct {
	GroupID         uint16
	TargetPortGroup []*SCSITargetPort `json:"targetportgroup"`
}

type SCSITarget struct {
	Name    string          `json:"name"`
	TID     int             `json:"tid"`
	LID     int             `json:"lid"`
	State   SCSITargetState `json:"state"`
	Devices LUNMap          `json:"-"`
	LUN0    *SCSILu         `json:"-"`

	TargetPortGroups []*TargetPortGroup `json:"tpg"`
	SCSITargetDriver interface{}        `json:"-"`

	ITNexusMutex sync.Mutex
	ITNexus      map[uuid.UUID]*ITNexus `json:"itnexus"`
}

type SCSITargetDriverState int

const (
	// just registered
	SCSI_DRIVER_REGD = iota
	// initialized ok
	SCSI_DRIVER_INIT
	// failed to initialize
	SCSI_DRIVER_ERR
	// exited
	SCSI_DRIVER_EXIT
)

type SCSITargetDriverCommon struct {
	Name       string
	State      SCSITargetDriverState
	DefaultBST string
}

type SCSILuPhyAttribute struct {
	SCSIID          string
	SCSISN          string
	NumID           uint64
	VendorID        string
	ProductID       string
	ProductRev      string
	VersionDesction [8]uint16
	// Peripheral device type
	DeviceType SCSIDeviceType
	// Peripheral Qualifier
	Qualifier bool
	// Removable media
	Removable bool
	// Read Only media
	Readonly bool
	// Software Write Protect
	SWP bool
	// Use thin-provisioning for this LUN
	ThinProvisioning bool
	// Logical Unit online
	Online bool
	// Descrptor format sense data supported
	SenseFormat bool
	// Logical blocks per physical block exponent
	Lbppbe int
	// Do not update it automatically when the backing file changes
	NoLbppbe int
	// Lowest aligned LBA
	LowestAlignedLBA int
}

var (
	DefaultBlockShift      uint = 9
	DefaultSenseBufferSize int  = 252
)

var (
	SAM_STAT_GOOD                       byte = 0x00
	SAM_STAT_CHECK_CONDITION            byte = 0x02
	SAM_STAT_CONDITION_MET              byte = 0x04
	SAM_STAT_BUSY                       byte = 0x08
	SAM_STAT_INTERMEDIATE               byte = 0x10
	SAM_STAT_INTERMEDIATE_CONDITION_MET byte = 0x14
	SAM_STAT_RESERVATION_CONFLICT       byte = 0x18
	SAM_STAT_COMMAND_TERMINATED         byte = 0x22
	SAM_STAT_TASK_SET_FULL              byte = 0x28
	SAM_STAT_ACA_ACTIVE                 byte = 0x30
	SAM_STAT_TASK_ABORTED               byte = 0x40
)

type SAMStat struct {
	Stat byte
	Err  error
}

var (
	SAMStatGood                     = SAMStat{SAM_STAT_GOOD, nil}
	SAMStatCheckCondition           = SAMStat{SAM_STAT_CHECK_CONDITION, errors.New("check condition")}
	SAMStatConditionMet             = SAMStat{SAM_STAT_CONDITION_MET, errors.New("condition met")}
	SAMStatBusy                     = SAMStat{SAM_STAT_BUSY, errors.New("busy")}
	SAMStatIntermediate             = SAMStat{SAM_STAT_INTERMEDIATE, errors.New("intermediate")}
	SAMStatIntermediateConditionMet = SAMStat{SAM_STAT_INTERMEDIATE_CONDITION_MET, errors.New("intermediate condition met")}
	SAMStatReservationConflict      = SAMStat{SAM_STAT_RESERVATION_CONFLICT, errors.New("reservation conflict")}
	SAMStatCommandTerminated        = SAMStat{SAM_STAT_COMMAND_TERMINATED, errors.New("command terminated")}
	SAMStatTaskSetFull              = SAMStat{SAM_STAT_TASK_SET_FULL, errors.New("task set full")}
	SAMStatAcaActive                = SAMStat{SAM_STAT_ACA_ACTIVE, errors.New("aca active")}
	SAMStatTaskAborted              = SAMStat{SAM_STAT_TASK_ABORTED, errors.New("task aborted")}
)

type SCSIDeviceType byte

var (
	TYPE_DISK      SCSIDeviceType = 0x00
	TYPE_TAPE      SCSIDeviceType = 0x01
	TYPE_PRINTER   SCSIDeviceType = 0x02
	TYPE_PROCESSOR SCSIDeviceType = 0x03
	TYPE_WORM      SCSIDeviceType = 0x04
	TYPE_MMC       SCSIDeviceType = 0x05
	TYPE_SCANNER   SCSIDeviceType = 0x06
	TYPE_MOD       SCSIDeviceType = 0x07

	TYPE_MEDIUM_CHANGER SCSIDeviceType = 0x08
	TYPE_COMM           SCSIDeviceType = 0x09
	TYPE_RAID           SCSIDeviceType = 0x0c
	TYPE_ENCLOSURE      SCSIDeviceType = 0x0d
	TYPE_RBC            SCSIDeviceType = 0x0e
	TYPE_OSD            SCSIDeviceType = 0x11
	TYPE_NO_LUN         SCSIDeviceType = 0x7f
	TYPE_UNKNOWN        SCSIDeviceType = 0x1f

	TYPE_PT SCSIDeviceType = 0xff
)

type CommandFunc func(host int, cmd *SCSICommand) SAMStat

type BackingStore interface {
	Open(dev *SCSILu, path string) error
	Close(dev *SCSILu) error
	Init(dev *SCSILu, Opts string) error
	Exit(dev *SCSILu) error
	Size(dev *SCSILu) uint64
	Read(offset, tl int64) ([]byte, error)
	Write([]byte, int64) error
	DataSync(offset, tl int64) error
	DataAdvise(int64, int64, uint32) error
	Unmap([]UnmapBlockDescriptor) error
}

type SCSIDeviceProtocol interface {
	PerformCommand(opcode int) interface{}
	PerformServiceAction(opcode int, action uint8) interface{}
	InitLu(lu *SCSILu) error
	ConfigLu(lu *SCSILu) error
	OnlineLu(lu *SCSILu) error
	OfflineLu(lu *SCSILu) error
	ExitLu(lu *SCSILu) error
}
type ModePage struct {
	// Page code
	PageCode uint8
	// Sub page code
	SubPageCode uint8
	Size        uint8
	// Rest of mode page info
	Data []byte
}

type SCSIReservation struct {
	// Internal reservation ID
	ID        uuid.UUID
	Key       uint64
	ITNexusID uuid.UUID
	Scope     uint8
	Type      uint8
}

type SCSILu struct {
	Address             uint64
	Size                uint64
	UUID                uint64
	Path                string
	BsoFlags            int
	BlockShift          uint
	ReserveID           uuid.UUID
	Attrs               SCSILuPhyAttribute
	ModePages           []ModePage
	Storage             BackingStore
	DeviceProtocol      SCSIDeviceProtocol
	ModeBlockDescriptor []byte
	SCSIVendorID        string
	SCSIProductID       string
	SCSIID              string

	PerformCommand CommandFunc
	FinishCommand  func(*SCSITarget, *SCSICommand)
}

type LUNMap map[uint64]*SCSILu

type UnmapBlockDescriptor struct {
	Offset uint64
	TL     uint32
}

type ReaderWriterAt interface {
	io.ReaderAt
	io.WriterAt
}

type RemoteBackingStore interface {
	ReaderWriterAt
	Sync() (int, error)
	Unmap(int64, int64) (int, error)
}
