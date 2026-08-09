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

import "errors"

type SCSIError struct {
	errno byte
	err   error
}

var (
	NO_SENSE        byte = 0x00
	RECOVERED_ERROR byte = 0x01
	NOT_READY       byte = 0x02
	MEDIUM_ERROR    byte = 0x03
	HARDWARE_ERROR  byte = 0x04
	ILLEGAL_REQUEST byte = 0x05
	UNIT_ATTENTION  byte = 0x06
	DATA_PROTECT    byte = 0x07
	BLANK_CHECK     byte = 0x08
	COPY_ABORTED    byte = 0x0a
	ABORTED_COMMAND byte = 0x0b
	VOLUME_OVERFLOW byte = 0x0d
	MISCOMPARE      byte = 0x0e
)

var (
	NoSenseError        = SCSIError{NO_SENSE, errors.New("no sense")}
	RecoveredError      = SCSIError{RECOVERED_ERROR, errors.New("recovered error")}
	NotReadyError       = SCSIError{NOT_READY, errors.New("not ready")}
	MediumError         = SCSIError{MEDIUM_ERROR, errors.New("medium error")}
	HardwareError       = SCSIError{HARDWARE_ERROR, errors.New("hardware error")}
	IllegalRequestError = SCSIError{ILLEGAL_REQUEST, errors.New("illegal request")}
	UnitAttentionError  = SCSIError{UNIT_ATTENTION, errors.New("unit attention")}
	DataProtectError    = SCSIError{DATA_PROTECT, errors.New("data protect")}
	BlankCheckError     = SCSIError{BLANK_CHECK, errors.New("blank check")}
	CopyAbortedError    = SCSIError{COPY_ABORTED, errors.New("copy aborted")}
	AbortedCommandError = SCSIError{ABORTED_COMMAND, errors.New("aborted command")}
	VolumeOverflowError = SCSIError{VOLUME_OVERFLOW, errors.New("volume overflow")}
	MiscompareError     = SCSIError{MISCOMPARE, errors.New("miscompare")}
)

type SCSISubError uint16

var (
	// Key 0: No Sense Errors
	NO_ADDITIONAL_SENSE         SCSISubError = 0x0000
	ASC_MARK                    SCSISubError = 0x0001
	ASC_EOM                     SCSISubError = 0x0002
	ASC_BOM                     SCSISubError = 0x0004
	ASC_END_OF_DATA             SCSISubError = 0x0005
	ASC_OP_IN_PROGRESS          SCSISubError = 0x0016
	ASC_DRIVE_REQUIRES_CLEANING SCSISubError = 0x8282

	// Key 1: Recovered Errors
	ASC_WRITE_ERROR            SCSISubError = 0x0c00
	ASC_READ_ERROR             SCSISubError = 0x1100
	ASC_RECOVERED_WITH_RETRYS  SCSISubError = 0x1701
	ASC_MEDIA_LOAD_EJECT_ERROR SCSISubError = 0x5300
	ASC_FAILURE_PREDICTION     SCSISubError = 0x5d00

	// Key 2: Not ready
	ASC_CAUSE_NOT_REPORTABLE    SCSISubError = 0x0400
	ASC_BECOMING_READY          SCSISubError = 0x0401
	ASC_INITIALIZING_REQUIRED   SCSISubError = 0x0402
	ASC_CLEANING_CART_INSTALLED SCSISubError = 0x3003
	ASC_CLEANING_FAILURE        SCSISubError = 0x3007
	ASC_MEDIUM_NOT_PRESENT      SCSISubError = 0x3a00
	ASC_LOGICAL_UNIT_NOT_CONFIG SCSISubError = 0x3e00

	// Key 3: Medium Errors
	ASC_UNRECOVERED_READ           SCSISubError = 0x1100
	ASC_RECORDED_ENTITY_NOT_FOUND  SCSISubError = 0x1400
	ASC_UNKNOWN_FORMAT             SCSISubError = 0x3001
	ASC_IMCOMPATIBLE_FORMAT        SCSISubError = 0x3002
	ASC_MEDIUM_FORMAT_CORRUPT      SCSISubError = 0x3100
	ASC_SEQUENTIAL_POSITION_ERR    SCSISubError = 0x3b00
	ASC_WRITE_APPEND_ERR           SCSISubError = 0x5000
	ASC_CARTRIDGE_FAULT            SCSISubError = 0x5200
	ASC_MEDIA_LOAD_OR_EJECT_FAILED SCSISubError = 0x5300

	// Key 4: Hardware Failure
	ASC_COMPRESSION_CHECK            SCSISubError = 0x0c04
	ASC_DECOMPRESSION_CRC            SCSISubError = 0x110d
	ASC_MECHANICAL_POSITIONING_ERROR SCSISubError = 0x1501
	ASC_MANUAL_INTERVENTION_REQ      SCSISubError = 0x0403
	ASC_HARDWARE_FAILURE             SCSISubError = 0x4000
	ASC_INTERNAL_TGT_FAILURE         SCSISubError = 0x4400
	ASC_ERASE_FAILURE                SCSISubError = 0x5100

	// Key 5: Illegal Request
	ASC_PARAMETER_LIST_LENGTH_ERR                 SCSISubError = 0x1a00
	ASC_INVALID_OP_CODE                           SCSISubError = 0x2000
	ASC_LBA_OUT_OF_RANGE                          SCSISubError = 0x2100
	ASC_INVALID_FIELD_IN_CDB                      SCSISubError = 0x2400
	ASC_LUN_NOT_SUPPORTED                         SCSISubError = 0x2500
	ASC_INVALID_FIELD_IN_PARMS                    SCSISubError = 0x2600
	ASC_INVALID_RELEASE_OF_PERSISTENT_RESERVATION SCSISubError = 0x2604
	ASC_INCOMPATIBLE_FORMAT                       SCSISubError = 0x3005
	ASC_SAVING_PARMS_UNSUP                        SCSISubError = 0x3900
	ASC_MEDIUM_DEST_FULL                          SCSISubError = 0x3b0d
	ASC_MEDIUM_SRC_EMPTY                          SCSISubError = 0x3b0e
	ASC_POSITION_PAST_BOM                         SCSISubError = 0x3b0c
	ASC_MEDIUM_REMOVAL_PREVENTED                  SCSISubError = 0x5302
	ASC_INSUFFICIENT_REGISTRATION_RESOURCES       SCSISubError = 0x5504
	ASC_BAD_MICROCODE_DETECTED                    SCSISubError = 0x8283

	// Key 6: Unit Attention
	ASC_NOT_READY_TO_TRANSITION         SCSISubError = 0x2800
	ASC_POWERON_RESET                   SCSISubError = 0x2900
	ASC_I_T_NEXUS_LOSS_OCCURRED         SCSISubError = 0x2907
	ASC_MODE_PARAMETERS_CHANGED         SCSISubError = 0x2a01
	ASC_RESERVATIONS_PREEMPTED          SCSISubError = 0x2a03
	ASC_RESERVATIONS_RELEASED           SCSISubError = 0x2a04
	ASC_INSUFFICIENT_TIME_FOR_OPERATION SCSISubError = 0x2e00
	ASC_CMDS_CLEARED_BY_ANOTHER_INI     SCSISubError = 0x2f00
	ASC_MICROCODE_DOWNLOADED            SCSISubError = 0x3f01
	ASC_INQUIRY_DATA_HAS_CHANGED        SCSISubError = 0x3f03
	ASC_REPORTED_LUNS_DATA_HAS_CHANGED  SCSISubError = 0x3f0e
	ASC_FAILURE_PREDICTION_FALSE        SCSISubError = 0x5dff

	// Data Protect
	ASC_WRITE_PROTECT              SCSISubError = 0x2700
	ASC_MEDIUM_OVERWRITE_ATTEMPTED SCSISubError = 0x300c

	// Miscompare
	ASC_MISCOMPARE_DURING_VERIFY_OPERATION SCSISubError = 0x1d00
)
