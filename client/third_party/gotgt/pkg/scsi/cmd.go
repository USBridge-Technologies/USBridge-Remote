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
	"github.com/gostor/gotgt/pkg/util"
)

const (
	// PERSISTENT_RESERVE_IN service action codes
	PR_IN_READ_KEYS           byte = 0x00
	PR_IN_READ_RESERVATION    byte = 0x01
	PR_IN_REPORT_CAPABILITIES byte = 0x02
	PR_IN_READ_FULL_STATUS    byte = 0x03

	// PERSISTENT_RESERVE_OUT service action codes
	PR_OUT_REGISTER                         byte = 0x00
	PR_OUT_RESERVE                          byte = 0x01
	PR_OUT_RELEASE                          byte = 0x02
	PR_OUT_CLEAR                            byte = 0x03
	PR_OUT_PREEMPT                          byte = 0x04
	PR_OUT_PREEMPT_AND_ABORT                byte = 0x05
	PR_OUT_REGISTER_AND_IGNORE_EXISTING_KEY byte = 0x06
	PR_OUT_REGISTER_AND_MOVE                byte = 0x07

	// Persistent Reservation scope
	PR_LU_SCOPE byte = 0x00

	// Persistent Reservation Type Mask format
	PR_TYPE_WRITE_EXCLUSIVE          byte = 0x01
	PR_TYPE_EXCLUSIVE_ACCESS         byte = 0x03
	PR_TYPE_WRITE_EXCLUSIVE_REGONLY  byte = 0x05
	PR_TYPE_EXCLUSIVE_ACCESS_REGONLY byte = 0x06
	PR_TYPE_WRITE_EXCLUSIVE_ALLREG   byte = 0x07
	PR_TYPE_EXCLUSIVE_ACCESS_ALLREG  byte = 0x08
)

const (
	//  6-byte commands
	CDB_GROUPID_0 = 6
	// 10-byte commands
	CDB_GROUPID_1 = 10
	// 10-byte commands
	CDB_GROUPID_2 = 10
	// reserved
	CDB_GROUPID_3 = 0
	// 16-byte commands
	CDB_GROUPID_4 = 16
	// 12-byte commands
	CDB_GROUPID_5 = 12
	// vendor specific
	CDB_GROUPID_6 = 0
	// vendor specific
	CDB_GROUPID_7 = 0
)

func SCSICDBGroupID(opcode byte) byte {
	return ((opcode >> 5) & 0x7)
}

/*
 * Protocol Identifier Values
 *
 * 0 Fibre Channel (FCP-2)
 * 1 Parallel SCSI (SPI-5)
 * 2 SSA (SSA-S3P)
 * 3 IEEE 1394 (SBP-3)
 * 4 SCSI Remote Direct Memory Access (SRP)
 * 5 iSCSI
 * 6 SAS Serial SCSI Protocol (SAS)
 * 7 Automation/Drive Interface (ADT)
 * 8 AT Attachment Interface (ATA/ATAPI-7)
 */

const (
	PIV_FCP   = byte(0x00)
	PIV_SPI   = byte(0x01)
	PIV_S3P   = byte(0x02)
	PIV_SBP   = byte(0x03)
	PIV_SRP   = byte(0x04)
	PIV_ISCSI = byte(0x05)
	PIV_SAS   = byte(0x06)
	PIV_ADT   = byte(0x07)
	PIV_ATA   = byte(0x08)
	PIV_USB   = byte(0x09)
	PIV_SOP   = byte(0x0A)
)

const (
	VERSION_NOT_CLAIM         = byte(0x00)
	VERSION_WITHDRAW_STANDARD = byte(0x03)
	VERSION_WITHDRAW_SPC2     = byte(0x04)
	VERSION_WITHDRAW_SPC3     = byte(0x05)
)

/*
 * Code Set
 *
 *  1 - Designator fild contains binary values
 *  2 - Designator field contains ASCII printable chars
 *  3 - Designaotor field contains UTF-8
 */
const (
	INQ_CODE_BIN   = byte(1)
	INQ_CODE_ASCII = byte(2)
	INQ_CODE_UTF8  = byte(3)
)

/*
 * Association field
 *
 * 00b - Associated with Logical Unit
 * 01b - Associated with target port
 * 10b - Associated with SCSI Target device
 * 11b - Reserved
 */
const (
	ASS_LU       = byte(0x00)
	ASS_TGT_PORT = byte(0x01)
	ASS_TGT_DEV  = byte(0x02)
)

/*
 * Table 177 — PERIPHERAL QUALIFIER field
 * Qualifier Description
 * 000b - A peripheral device having the indicated peripheral
 * 	device type is connected to this logical unit. If the device server is
 * 	unable to determine whether or not a peripheral device is connected,
 * 	then the device server also shall use this peripheral qualifier.
 * 	This peripheral qualifier does not indicate that the peripheral
 * 	device connected to the logical unit is ready for access.
 * 001b - A peripheral device having the indicated peripheral device type
 * 	is not connected to this logical unit. However, the device server is capable of
 *	supporting the indicated peripheral device type on this logical unit.
 * 010b - Reserved
 * 011b - The device server is not capable of supporting a
 * 	peripheral device on this logical unit. For this peripheral
 *	qualifier the peripheral device type shall be set to 1Fh. All other peripheral
 * device type values are reserved for this peripheral qualifier.
 * 100b to 111b Vendor specific
 */
const (
	PQ_DEVICE_CONNECTED   = byte(0x00 << 5)
	PQ_DEVICE_NOT_CONNECT = byte(0x01 << 5)
	PQ_RESERVED           = byte(0x02 << 5)
	PQ_NOT_SUPPORT        = byte(0x03 << 5)
)

const (
	INQUIRY_SCCS          = byte(0x80)
	INQUIRY_AAC           = byte(0x40)
	INQUIRY_TPGS_NO       = byte(0x00)
	INQUIRY_TPGS_IMPLICIT = byte(0x10)
	INQUIRY_TPGS_EXPLICIT = byte(0x20)
	INQUIRY_TPGS_BOTH     = byte(0x30)
	INQUIRY_3PC           = byte(0x08)
	INQUIRY_Reserved      = byte(0x06)
	INQUIRY_PROTECT       = byte(0x01)

	INQUIRY_NORM_ACA        = byte(0x20)
	INQUIRY_HISUP           = byte(0x10)
	INQUIRY_STANDARD_FORMAT = byte(0x02)

	INQUIRY_ENCSERV = byte(0x40)
	INQUIRY_VS0     = byte(0x20)
	INQUIRY_MULTIP  = byte(0x10)
	INQUIRY_ADDR16  = byte(0x01)

	INQUIRY_WBUS16 = byte(0x20)
	INQUIRY_SYNC   = byte(0x10)
	INQUIRY_CMDQUE = byte(0x02)
	INQUIRY_VS1    = byte(0x01)

	INQUIRY_QAS = byte(0x02)
	INQUIRY_IUS = byte(0x01)
)

const (
	ADDRESS_METHOD_PERIPHERAL_DEVICE     = byte(0x00)
	ADDRESS_METHOD_FLAT_SPACE            = byte(0x01)
	ADDRESS_METHOD_LOGICAL_UNIT          = byte(0x02)
	ADDRESS_METHOD_EXTENDED_LOGICAL_UNIT = byte(0x03)
)

/*
 * Designator type - SPC-4 Reference
 *
 * 0 - Vendor specific - 7.6.3.3
 * 1 - T10 vendor ID - 7.6.3.4
 * 2 - EUI-64 - 7.6.3.5
 * 3 - NAA - 7.6.3.6
 * 4 - Relative Target port identifier - 7.6.3.7
 * 5 - Target Port group - 7.6.3.8
 * 6 - Logical Unit group - 7.6.3.9
 * 7 - MD5 logical unit identifier - 7.6.3.10
 * 8 - SCSI name string - 7.6.3.11
 */
const (
	DESG_VENDOR = iota
	DESG_T10
	DESG_EUI64
	DESG_NAA
	DESG_REL_TGT_PORT
	DESG_TGT_PORT_GRP
	DESG_LU_GRP
	DESG_MD5
	DESG_SCSI
)

const (
	NAA_IEEE_EXTD      = byte(0x2)
	NAA_LOCAL          = byte(0x3)
	NAA_IEEE_REGD      = byte(0x5)
	NAA_IEEE_REGD_EXTD = byte(0x6)
)

var (
	SCSIVendorID  = "GOSTOR"
	SCSIProductID = "GOTGT"
	SCSIID        = "iqn.2016-09.com.gotgt.gostor:iscsi-tgt"
)

/*
 * Transfer Length (if any)
 * Parameter List Length (if any)
 * Allocation Length (if any)
 */
func SCSICDBBufXLength(scb []byte) (int64, bool) {
	var (
		opcode byte
		length int64
		group  byte
		ok     bool = true
	)
	opcode = scb[0]
	group = SCSICDBGroupID(opcode)

	switch group {
	case CDB_GROUPID_0:
		length = int64(scb[4])
	case CDB_GROUPID_2:
		length = int64(util.GetUnalignedUint16(scb[7:9]))
	case CDB_GROUPID_3:
		if opcode == 0x7F {
			length = int64(scb[7])
		} else {
			ok = false
		}
	case CDB_GROUPID_4:
		length = int64(util.GetUnalignedUint32(scb[6:10]))
	case CDB_GROUPID_5:
		length = int64(util.GetUnalignedUint32(scb[10:14]))
	default:
		ok = false
	}
	return length, ok
}
