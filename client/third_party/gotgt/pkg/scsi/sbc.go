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

// SCSI block command processing
package scsi

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/util"
	"github.com/gostor/gotgt/pkg/version"
	log "github.com/sirupsen/logrus"
)

const (
	PR_SPECIAL = (1 << 5)
	PR_WE_FA   = (1 << 4)
	PR_EA_FA   = (1 << 3)
	PR_RR_FR   = (1 << 2)
	PR_WE_FN   = (1 << 1)
	PR_EA_FN   = (1 << 0)
)

var (
	EnableORWrite16             = true
	EnablePersistentReservation = true
)

type SBCSCSIDeviceProtocol struct {
	BaseSCSIDeviceProtocol
}

func (sbc SBCSCSIDeviceProtocol) PerformCommand(opcode int) interface{} {
	return sbc.SCSIDeviceOps[opcode]
}

func (sbc SBCSCSIDeviceProtocol) PerformServiceAction(opcode int, action uint8) interface{} {
	var sa *SCSIServiceAction
	for _, sa = range sbc.SCSIDeviceOps[opcode].ServiceAction {
		if sa.ServiceAction == action {
			return sa
		}
	}
	return nil
}

func (sbc SBCSCSIDeviceProtocol) InitLu(lu *api.SCSILu) error {
	// init LU's phy attribute
	lu.Attrs.DeviceType = sbc.DeviceType
	lu.Attrs.Qualifier = false
	lu.Attrs.ThinProvisioning = false
	lu.Attrs.Removable = false
	lu.Attrs.Readonly = false
	lu.Attrs.SWP = false
	lu.Attrs.SenseFormat = false
	lu.Attrs.VendorID = SCSIVendorID
	lu.Attrs.ProductID = SCSIProductID
	lu.Attrs.ProductRev = version.SCSIVersion
	lu.Attrs.SCSIID = fmt.Sprintf("gotgt-scsi-%d%d", 0, lu.UUID)
	lu.Attrs.SCSISN = fmt.Sprintf("gotgt-beaf-%d%d", 0, lu.UUID)

	/*
		SCSIID for PAGE83 T10 VENDOR IDENTIFICATION field
		It is going to be the iSCSI target iqn name
		leave it with a default target name
	*/

	lu.Attrs.SCSIID = SCSIID
	/*
	   The PRODUCT SERIAL NUMBER field contains
	   right-aligned ASCII data (see 4.3.1)
	   that is a vendor specific serial number.
	   If the product serial number is not available,
	   the device server shall return ASCII spaces (20h) in this field.
	   leave it with 4 spaces (20h)
	*/
	lu.Attrs.SCSISN = "    "

	lu.Attrs.VersionDesction = [8]uint16{
		0x0320, // SBC-2 no version claimed
		0x0960, // iSCSI no version claimed
		0x0300, // SPC-3 no version claimed
		0x0060, // SAM-3 no version claimed
	}
	if lu.BlockShift == 0 {
		lu.BlockShift = api.DefaultBlockShift
	}
	pages := []api.ModePage{}
	// Vendor uniq - However most apps seem to call for mode page 0
	//pages = append(pages, api.ModePage{0, 0, []byte{}})
	// Disconnect page
	pages = append(pages, api.ModePage{2, 0, 14, []byte{0x80, 0x80, 0, 0xa, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}})
	// Caching Page
	pages = append(pages, api.ModePage{8, 0, 18, []byte{0x14, 0, 0xff, 0xff, 0, 0, 0xff, 0xff, 0xff, 0xff, 0x80, 0x14, 0, 0, 0, 0, 0, 0}})

	// Control page
	pages = append(pages, api.ModePage{0x0a, 0, 10, []byte{2, 0x10, 0, 0, 0, 0, 0, 0, 2, 0, 0x08, 0, 0, 0, 0, 0, 0, 0}})

	// Control Extensions mode page:  TCMOS:1
	pages = append(pages, api.ModePage{0x0a, 1, 0x1c, []byte{0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}})
	// Informational Exceptions Control page
	pages = append(pages, api.ModePage{0x1c, 0, 10, []byte{8, 0, 0, 0, 0, 0, 0, 0, 0, 0}})
	lu.ModePages = pages
	mbd := util.MarshalUint32(uint32(0xffffffff))
	if size := lu.Size >> lu.BlockShift; size>>32 == 0 {
		mbd = util.MarshalUint32(uint32(size))
	}
	lu.ModeBlockDescriptor = append(mbd, util.MarshalUint32(uint32(1<<lu.BlockShift))...)
	return nil
}

func (sbc SBCSCSIDeviceProtocol) ConfigLu(lu *api.SCSILu) error {
	return nil
}

func (sbc SBCSCSIDeviceProtocol) OnlineLu(lu *api.SCSILu) error {
	return nil
}

func (sbc SBCSCSIDeviceProtocol) OfflineLu(lu *api.SCSILu) error {
	return nil
}

func (sbc SBCSCSIDeviceProtocol) ExitLu(lu *api.SCSILu) error {
	return nil
}

func NewSBCDevice(deviceType api.SCSIDeviceType) api.SCSIDeviceProtocol {
	var sbc = SBCSCSIDeviceProtocol{
		BaseSCSIDeviceProtocol{
			DeviceType:    deviceType,
			SCSIDeviceOps: []SCSIDeviceOperation{},
		},
	}
	for i := 0; i < 256; i++ {
		sbc.SCSIDeviceOps = append(sbc.SCSIDeviceOps, NewSCSIDeviceOperation(SPCIllegalOp, nil, 0))
	}
	sbc.SCSIDeviceOps[api.TEST_UNIT_READY] = NewSCSIDeviceOperation(SPCTestUnit, nil, 0)
	sbc.SCSIDeviceOps[api.REQUEST_SENSE] = NewSCSIDeviceOperation(SPCRequestSense, nil, 0)
	sbc.SCSIDeviceOps[api.FORMAT_UNIT] = NewSCSIDeviceOperation(SBCFormatUnit, nil, 0)
	sbc.SCSIDeviceOps[api.READ_6] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.WRITE_6] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN|PR_WE_FA|PR_WE_FN)
	sbc.SCSIDeviceOps[api.INQUIRY] = NewSCSIDeviceOperation(SPCInquiry, nil, 0)
	sbc.SCSIDeviceOps[api.MODE_SELECT] = NewSCSIDeviceOperation(SBCModeSelect, nil, PR_WE_FA|PR_EA_FA|PR_WE_FN|PR_EA_FN)
	sbc.SCSIDeviceOps[api.RESERVE] = NewSCSIDeviceOperation(SBCReserve, nil, 0)
	sbc.SCSIDeviceOps[api.RELEASE] = NewSCSIDeviceOperation(SBCRelease, nil, 0)

	sbc.SCSIDeviceOps[api.MODE_SENSE] = NewSCSIDeviceOperation(SBCModeSense, nil, PR_WE_FA|PR_EA_FA|PR_EA_FN|PR_WE_FN)
	sbc.SCSIDeviceOps[api.START_STOP] = NewSCSIDeviceOperation(SPCStartStop, nil, PR_SPECIAL)
	sbc.SCSIDeviceOps[api.SEND_DIAGNOSTIC] = NewSCSIDeviceOperation(SPCSendDiagnostics, nil, 0)

	sbc.SCSIDeviceOps[api.ALLOW_MEDIUM_REMOVAL] = NewSCSIDeviceOperation(SPCPreventAllowMediaRemoval, nil, 0)
	sbc.SCSIDeviceOps[api.READ_CAPACITY] = NewSCSIDeviceOperation(SBCReadCapacity, nil, 0)
	sbc.SCSIDeviceOps[api.READ_10] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.WRITE_10] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_WE_FA|PR_EA_FA|PR_EA_FN|PR_WE_FN)
	sbc.SCSIDeviceOps[api.WRITE_VERIFY] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.VERIFY_10] = NewSCSIDeviceOperation(SBCVerify, nil, PR_EA_FA|PR_EA_FN)

	sbc.SCSIDeviceOps[api.PRE_FETCH_10] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.SYNCHRONIZE_CACHE] = NewSCSIDeviceOperation(SBCSyncCache, nil, PR_WE_FA|PR_EA_FA|PR_WE_FN|PR_EA_FN)

	sbc.SCSIDeviceOps[api.WRITE_SAME] = NewSCSIDeviceOperation(SBCReadWrite, nil, 0)
	sbc.SCSIDeviceOps[api.UNMAP] = NewSCSIDeviceOperation(SBCUnmap, nil, 0)

	sbc.SCSIDeviceOps[api.MODE_SELECT_10] = NewSCSIDeviceOperation(SBCModeSelect, nil, PR_WE_FA|PR_EA_FA|PR_EA_FN|PR_WE_FN)
	sbc.SCSIDeviceOps[api.MODE_SENSE_10] = NewSCSIDeviceOperation(SBCModeSense, nil, PR_WE_FA|PR_WE_FN|PR_EA_FA|PR_EA_FN)

	if EnablePersistentReservation {
		sbc.SCSIDeviceOps[api.PERSISTENT_RESERVE_IN] = NewSCSIDeviceOperation(SPCServiceAction, []*SCSIServiceAction{
			{ServiceAction: PR_IN_READ_KEYS, CommandPerformFunc: SPCPRReadKeys},
			{ServiceAction: PR_IN_READ_RESERVATION, CommandPerformFunc: SPCPRReadReservation},
			{ServiceAction: PR_IN_REPORT_CAPABILITIES, CommandPerformFunc: SPCPRReportCapabilities},
		}, 0)

		sbc.SCSIDeviceOps[api.PERSISTENT_RESERVE_OUT] = NewSCSIDeviceOperation(SPCServiceAction, []*SCSIServiceAction{
			{ServiceAction: PR_OUT_REGISTER, CommandPerformFunc: SPCPRRegister},
			{ServiceAction: PR_OUT_RESERVE, CommandPerformFunc: SPCPRReserve},
			{ServiceAction: PR_OUT_RELEASE, CommandPerformFunc: SPCPRRelease},
			{ServiceAction: PR_OUT_CLEAR, CommandPerformFunc: SPCPRClear},
			{ServiceAction: PR_OUT_PREEMPT, CommandPerformFunc: SPCPRPreempt},
			//		{ServiceAction: PR_OUT_PREEMPT_AND_ABORT, CommandPerformFunc: SPCPRPreempt},
			{ServiceAction: PR_OUT_REGISTER_AND_IGNORE_EXISTING_KEY, CommandPerformFunc: SPCPRRegister},
			{ServiceAction: PR_OUT_REGISTER_AND_MOVE, CommandPerformFunc: SPCPRRegisterAndMove},
		}, 0)
	}
	sbc.SCSIDeviceOps[api.READ_16] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.WRITE_16] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN|PR_WE_FA|PR_WE_FN)
	if EnableORWrite16 {
		sbc.SCSIDeviceOps[api.ORWRITE_16] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	}
	sbc.SCSIDeviceOps[api.WRITE_VERIFY_16] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.VERIFY_16] = NewSCSIDeviceOperation(SBCVerify, nil, PR_EA_FA|PR_EA_FN)

	sbc.SCSIDeviceOps[api.PRE_FETCH_16] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.SYNCHRONIZE_CACHE_16] = NewSCSIDeviceOperation(SBCSyncCache, nil, PR_EA_FA|PR_EA_FN|PR_WE_FA|PR_WE_FN)
	sbc.SCSIDeviceOps[api.WRITE_SAME_16] = NewSCSIDeviceOperation(SBCReadWrite, nil, 0)
	sbc.SCSIDeviceOps[api.SERVICE_ACTION_IN] = NewSCSIDeviceOperation(SBCServiceAction, nil, 0)

	sbc.SCSIDeviceOps[api.REPORT_LUNS] = NewSCSIDeviceOperation(SPCReportLuns, nil, 0)
	sbc.SCSIDeviceOps[api.MAINT_PROTOCOL_IN] = NewSCSIDeviceOperation(SPCServiceAction, []*SCSIServiceAction{
		{ServiceAction: 0x0C, CommandPerformFunc: SPCReportSupportedOperationCodes},
	}, 0)
	sbc.SCSIDeviceOps[api.EXCHANGE_MEDIUM] = NewSCSIDeviceOperation(SPCIllegalOp, nil, 0)
	sbc.SCSIDeviceOps[api.READ_12] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.WRITE_12] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_WE_FA|PR_EA_FA|PR_WE_FA|PR_WE_FN)
	sbc.SCSIDeviceOps[api.WRITE_VERIFY_12] = NewSCSIDeviceOperation(SBCReadWrite, nil, PR_EA_FA|PR_EA_FN)
	sbc.SCSIDeviceOps[api.VERIFY_12] = NewSCSIDeviceOperation(SBCVerify, nil, PR_EA_FA|PR_EA_FN)

	return sbc
}

func SBCModeSelect(host int, cmd *api.SCSICommand) api.SAMStat {
	return api.SAMStatGood
}

func SBCModeSense(host int, cmd *api.SCSICommand) api.SAMStat {
	// DPOFUA = 0x10
	var deviceSpecific uint8 = 0x10

	if err := SPCModeSense(host, cmd); err.Err != nil {
		return err
	}

	// If this is a read-only lun, we must set the write protect bit
	if cmd.Device.Attrs.Readonly || cmd.Device.Attrs.SWP {
		deviceSpecific |= 0x80
	}

	if cmd.SCB[0] == 0x1a {
		cmd.InSDBBuffer.Buffer[2] = deviceSpecific
	} else {
		cmd.InSDBBuffer.Buffer[3] = deviceSpecific
	}

	return api.SAMStatGood
}

/*
 * SBCFormatUnit Implements SCSI FORMAT UNIT command
 * The FORMAT UNIT command requests that the device server format the medium into application client
 * accessible logical blocks as specified in the number of blocks and block length values received
 * in the last mode parameter block descriptor in a MODE SELECT command (see SPC-3).  In addition,
 * the device server may certify the medium and create control structures for the management of the medium and defects.
 * The degree that the medium is altered by this command is vendor-specific.
 *
 * Reference : SBC2r16
 * 5.2 - FORMAT UNIT
 */
func SBCFormatUnit(host int, cmd *api.SCSICommand) api.SAMStat {
	var (
		key = ILLEGAL_REQUEST
		asc = ASC_INVALID_FIELD_IN_CDB
	)

	if err := deviceReserve(cmd); err != nil {
		return api.SAMStatReservationConflict
	}

	if !cmd.Device.Attrs.Online {
		key = NOT_READY
		asc = ASC_MEDIUM_NOT_PRESENT
		goto sense
	}

	if cmd.Device.Attrs.Readonly || cmd.Device.Attrs.SWP {
		key = DATA_PROTECT
		asc = ASC_WRITE_PROTECT
		goto sense
	}

	if cmd.SCB[1]&0x80 != 0 {
		// we dont support format protection information
		goto sense
	}
	if cmd.SCB[1]&0x10 != 0 {
		// we dont support format data
		goto sense
	}
	if cmd.SCB[1]&0x07 != 0 {
		// defect list format must be 0
		goto sense
	}

	return api.SAMStatGood
sense:
	BuildSenseData(cmd, key, asc)
	return api.SAMStatCheckCondition
}

func SBCUnmap(host int, cmd *api.SCSICommand) api.SAMStat {
	// check ANCHOR
	if cmd.SCB[1]&0x01 != 0 {
		BuildSenseData(cmd, ILLEGAL_REQUEST, ASC_INVALID_FIELD_IN_CDB)
		return api.SAMStatCheckCondition
	}

	const blockDescLen = 16

	var blockDescs []api.UnmapBlockDescriptor
	for off := 8; uint32(off+blockDescLen) <= cmd.OutSDBBuffer.Length; off += blockDescLen {
		lba := binary.BigEndian.Uint64(cmd.OutSDBBuffer.Buffer[off : off+8])
		num := binary.BigEndian.Uint32(cmd.OutSDBBuffer.Buffer[off+8 : off+12])
		blockDescs = append(blockDescs, api.UnmapBlockDescriptor{
			Offset: lba << cmd.Device.BlockShift,
			TL:     num << cmd.Device.BlockShift,
		})
	}

	if len(blockDescs) == 0 {
		return api.SAMStatGood
	}

	if err := cmd.Device.Storage.Unmap(blockDescs); err != nil {
		BuildSenseData(cmd, MEDIUM_ERROR, NO_ADDITIONAL_SENSE)
		return api.SAMStatCheckCondition
	}

	return api.SAMStatGood
}

/*
 * SBCReadWrite Implements SCSI READ(10/12/16), WRITE(10/12/16), WRITE AND VERIFY(10/12/16), WRITE SAME(10/12/16)
 * The READ command requests that the device server read the specified logical block(s) and transfer them to the data-in buffer.
 * The WRITE command requests that the device server transfer the specified logical block(s) from the data-out buffer and write them.
 * The WRITE AND VERIFY command requests that the device server transfer the specified logical block(s) from the data-out buffer,
 * write them to the medium, and then verify that they are correctly written.
 *
 * Reference : SBC2r16
 * 5.6 - READ (10)
 * 5.7 - READ (12)
 * 5.8 - READ (16)
 * 5.25 - WRITE (10)
 * 5.26 - WRITE (12)
 * 5.27 - WRITE (16)
 * 5.29 - WRITE AND VERIFY (10)
 * 5.30 - WRITE AND VERIFY (12)
 * 5.31 - WRITE AND VERIFY (16)
 */
func SBCReadWrite(host int, cmd *api.SCSICommand) api.SAMStat {
	var (
		key    = ILLEGAL_REQUEST
		asc    = ASC_INVALID_FIELD_IN_CDB
		dev    = cmd.Device
		scb    = cmd.SCB
		opcode = api.SCSICommandType(scb[0])
		lba    uint64
		tl     uint32
		err    error
	)
	if dev.Attrs.Removable && !dev.Attrs.Online {
		key = NOT_READY
		asc = ASC_MEDIUM_NOT_PRESENT
		log.Warnf("sense")
		goto sense
	}

	switch opcode {
	case api.READ_10, api.READ_12, api.READ_16, api.WRITE_10, api.WRITE_12, api.WRITE_16, api.ORWRITE_16,
		api.WRITE_VERIFY, api.WRITE_VERIFY_12, api.WRITE_VERIFY_16, api.COMPARE_AND_WRITE:
		// We only support protection information type 0
		if scb[1]&0xe0 != 0 {
			key = ILLEGAL_REQUEST
			asc = ASC_INVALID_FIELD_IN_CDB
			log.Warnf("sense data(ILLEGAL_REQUEST,ASC_INVALID_FIELD_IN_CDB) encounter")
			goto sense
		}
	case api.WRITE_SAME, api.WRITE_SAME_16:
		// We dont support resource-provisioning so ANCHOR bit == 1 is an error.
		if scb[1]&0x10 != 0 {
			key = ILLEGAL_REQUEST
			asc = ASC_INVALID_FIELD_IN_CDB
			goto sense
		}
		// We only support unmap for thin provisioned LUNS
		if (scb[1]&0x08 != 0) && !dev.Attrs.ThinProvisioning {
			key = ILLEGAL_REQUEST
			asc = ASC_INVALID_FIELD_IN_CDB
			goto sense
		}
		// We only support protection information type 0
		if scb[1]&0xe0 != 0 {
			key = ILLEGAL_REQUEST
			asc = ASC_INVALID_FIELD_IN_CDB
			goto sense
		}
		// LBDATA and PBDATA can not both be set
		if (scb[1] & 0x06) == 0x06 {
			key = ILLEGAL_REQUEST
			asc = ASC_INVALID_FIELD_IN_CDB
			goto sense
		}
	}

	if dev.Attrs.Readonly || dev.Attrs.SWP {
		switch opcode {
		case api.WRITE_6, api.WRITE_10, api.WRITE_12, api.WRITE_16, api.ORWRITE_16,
			api.WRITE_VERIFY, api.WRITE_VERIFY_12, api.WRITE_VERIFY_16, api.WRITE_SAME, api.WRITE_SAME_16,
			api.PRE_FETCH_10, api.PRE_FETCH_16, api.COMPARE_AND_WRITE:
			key = DATA_PROTECT
			asc = ASC_WRITE_PROTECT
			log.Warnf("sense data(data protect) and asc(ASC_WRITE_PROTECT) encounter")
			goto sense
		}
	}

	lba = getSCSIReadWriteOffset(scb)
	tl = getSCSIReadWriteCount(scb)

	// Verify that we are not doing i/o beyond the end-of-lun
	if tl != 0 {
		if lba+uint64(tl) < lba || lba+uint64(tl) > dev.Size>>dev.BlockShift {
			key = ILLEGAL_REQUEST
			asc = ASC_LBA_OUT_OF_RANGE
			log.Warnf("sense data(ILLEGAL_REQUEST,ASC_LBA_OUT_OF_RANGE) encounter: lba: %d, tl: %d, size: %d", lba, tl, dev.Size>>dev.BlockShift)
			goto sense
		}
	} else {
		if lba >= dev.Size>>dev.BlockShift {
			key = ILLEGAL_REQUEST
			asc = ASC_LBA_OUT_OF_RANGE
			log.Warnf("sense data(ILLEGAL_REQUEST,ASC_LBA_OUT_OF_RANGE) encounter: lba: %d, size: %d", lba, dev.Size>>dev.BlockShift)
			goto sense
		}
	}

	cmd.Offset = lba << dev.BlockShift
	cmd.TL = tl << dev.BlockShift

	// Handle residuals
	switch opcode {
	case api.READ_6, api.READ_10, api.READ_12, api.READ_16:
		/*
			if (cmd->tl != scsi_get_in_length(cmd))
				scsi_set_in_resid_by_actual(cmd, cmd->tl);
		*/
	case api.WRITE_6, api.WRITE_10, api.WRITE_12, api.WRITE_16, api.WRITE_VERIFY, api.WRITE_VERIFY_12, api.WRITE_VERIFY_16:
		/*
			if (cmd->tl != scsi_get_out_length(cmd)) {
				scsi_set_out_resid_by_actual(cmd, cmd->tl);

				/* We need to clamp the size of the in-buffer
				 * so that we dont try to write > cmd->tl in the
				 * backend store.
				 *
				if (cmd->tl < scsi_get_out_length(cmd)) {
					scsi_set_out_length(cmd, cmd->tl);
				}
			}
		*/
	}

	err, key, asc = bsPerformCommand(dev.Storage, cmd)
	if err != nil {
		log.Errorf("Error from backend: %v", err)
		BuildSenseData(cmd, key, asc)
		return api.SAMStatBusy
	} else {
		return api.SAMStatGood
	}

sense:
	BuildSenseData(cmd, key, asc)
	return api.SAMStatCheckCondition
}

func SBCReserve(host int, cmd *api.SCSICommand) api.SAMStat {
	if err := deviceReserve(cmd); err != nil {
		return api.SAMStatReservationConflict
	}
	return api.SAMStatGood
}

func SBCRelease(host int, cmd *api.SCSICommand) api.SAMStat {
	lun := *(*uint64)(unsafe.Pointer(&cmd.Lun))
	if err := deviceRelease(cmd.Target.TID, cmd.ITNexusID, lun, false); err != nil {
		return api.SAMStatReservationConflict
	}

	return api.SAMStatGood
}

/*
 * SBCReadCapacity Implements SCSI READ CAPACITY(10) command
 * The READ CAPACITY (10) command requests that the device server transfer 8 bytes of parameter data
 * describing the capacity and medium format of the direct-access block device to the data-in buffer.
 * This command may be processed as if it has a HEAD OF QUEUE task attribute.  If the logical unit supports
 * protection information, the application client should use the READ CAPACITY (16) command instead of
 * the READ CAPACITY (10) command.
 *
 * Reference : SBC2r16
 * 5.10 - READ CAPACITY(10)
 */
func SBCReadCapacity(host int, cmd *api.SCSICommand) api.SAMStat {
	var (
		scb    = cmd.SCB
		key    = ILLEGAL_REQUEST
		asc    = ASC_LUN_NOT_SUPPORTED
		bshift = cmd.Device.BlockShift
		size   = cmd.Device.Size >> bshift
	)

	if cmd.Device.Attrs.Removable && !cmd.Device.Attrs.Online {
		key = NOT_READY
		asc = ASC_MEDIUM_NOT_PRESENT
		goto sense
	}

	if (scb[8]&0x1 == 0) && (scb[2]|scb[3]|scb[4]|scb[5]) != 0 {
		asc = ASC_INVALID_FIELD_IN_CDB
		goto sense
	}

	if cmd.InSDBBuffer.Length < 8 {
		goto overflow
	}

	// data[0] = (size >> 32) ? __cpu_to_be32(0xffffffff) : __cpu_to_be32(size - 1);
	if size>>32 != 0 {
		copy(cmd.InSDBBuffer.Buffer, util.MarshalUint32(uint32(0xffffffff)))
	} else {
		copy(cmd.InSDBBuffer.Buffer, util.MarshalUint32(uint32(size-1)))
	}

	// data[1] = __cpu_to_be32(1U << bshift);
	copy(cmd.InSDBBuffer.Buffer[4:], util.MarshalUint32(uint32(1<<bshift)))
overflow:
	cmd.InSDBBuffer.Resid = 8
	return api.SAMStatGood
sense:
	if cmd.InSDBBuffer != nil {
		cmd.InSDBBuffer.Resid = 0
	}
	BuildSenseData(cmd, key, asc)
	return api.SAMStatCheckCondition
}

/* SBCVerify Implements SCSI VERIFY(10) command
 * The VERIFY (10) command requests that the device server verify the specified logical block(s) on the medium.
 *
 * Reference : SBC2r16
 * 5.20 - VERIFY(10)
 */
func SBCVerify(host int, cmd *api.SCSICommand) api.SAMStat {
	var (
		key = ILLEGAL_REQUEST
		asc = ASC_INVALID_FIELD_IN_CDB
		dev = cmd.Device
		scb = cmd.SCB
		lba uint64
		tl  uint32
		err error
	)
	if dev.Attrs.Removable && !dev.Attrs.Online {
		key = NOT_READY
		asc = ASC_MEDIUM_NOT_PRESENT
		goto sense
	}

	if scb[1]&0xe0 != 0 {
		// We only support protection information type 0
		key = ILLEGAL_REQUEST
		asc = ASC_INVALID_FIELD_IN_CDB
		goto sense
	}

	if scb[1]&0x02 == 0 {
		// no data compare with the media
		return api.SAMStatGood
	}
	lba = getSCSIReadWriteOffset(scb)
	tl = getSCSIReadWriteCount(scb)

	// Verify that we are not doing i/o beyond the end-of-lun
	if tl != 0 {
		if lba+uint64(tl) < lba || lba+uint64(tl) > dev.Size>>dev.BlockShift {
			key = ILLEGAL_REQUEST
			asc = ASC_LBA_OUT_OF_RANGE
			log.Warnf("sense: lba: %d, tl: %d, size: %d", lba, tl, dev.Size>>dev.BlockShift)
			goto sense
		}
	} else {
		if lba >= dev.Size>>dev.BlockShift {
			key = ILLEGAL_REQUEST
			asc = ASC_LBA_OUT_OF_RANGE
			log.Warnf("sense")
			goto sense
		}
	}

	cmd.Offset = lba << dev.BlockShift
	cmd.TL = tl << dev.BlockShift
	err, key, asc = bsPerformCommand(dev.Storage, cmd)
	if err != nil {
		goto sense
	}
	return api.SAMStatGood
sense:
	BuildSenseData(cmd, key, asc)
	return api.SAMStatCheckCondition
}

/*
 * SBCReadCapacity16 Implements SCSI READ CAPACITY(16) command
 * The READ CAPACITY (16) command requests that the device server transfer parameter data
 * describing the capacity and medium format of the direct-access block device to the data-in buffer.
 *
 * Reference : SBC2r16
 * 5.11 - READ CAPACITY(16)
 */
func SBCReadCapacity16(host int, cmd *api.SCSICommand) api.SAMStat {
	var (
		bshift           = cmd.Device.BlockShift
		size             = cmd.Device.Size >> bshift
		allocationLength uint32
	)
	allocationLength = util.GetUnalignedUint32(cmd.SCB[10:14])
	copy(cmd.InSDBBuffer.Buffer, util.MarshalUint64(uint64(size-1)))
	if allocationLength > 12 {
		copy(cmd.InSDBBuffer.Buffer[8:], util.MarshalUint32(uint32(1<<bshift)))
		if allocationLength > 16 {
			var lbpme int
			if cmd.Device.Attrs.ThinProvisioning {
				lbpme = 1
			}
			val := (cmd.Device.Attrs.Lbppbe << 16) | (lbpme << 15) | cmd.Device.Attrs.LowestAlignedLBA
			copy(cmd.InSDBBuffer.Buffer[12:], util.MarshalUint32(uint32(val)))
		}
	}
	return api.SAMStatGood
}

func SBCGetLbaStatus(host int, cmd *api.SCSICommand) api.SAMStat {
	var (
		key = ILLEGAL_REQUEST
		asc = ASC_INVALID_FIELD_IN_CDB
		dev = cmd.Device
		scb = cmd.SCB
		lba uint64
		tl  uint32
	)
	if dev.Attrs.Removable && !dev.Attrs.Online {
		key = NOT_READY
		asc = ASC_MEDIUM_NOT_PRESENT
		goto sense
	}

	if scb[1]&0xe0 != 0 {
		// We only support protection information type 0
		key = ILLEGAL_REQUEST
		asc = ASC_INVALID_FIELD_IN_CDB
		goto sense
	}

	if scb[1]&0x02 == 0 {
		// no data compare with the media
		return api.SAMStatGood
	}
	lba = getSCSIReadWriteOffset(scb)
	tl = getSCSIReadWriteCount(scb)
	// Verify that we are not doing i/o beyond the end-of-lun
	if tl != 0 {
		if lba+uint64(tl) < lba || lba+uint64(tl) > dev.Size>>dev.BlockShift {
			key = ILLEGAL_REQUEST
			asc = ASC_LBA_OUT_OF_RANGE
			log.Warnf("sense: lba: %d, tl: %d, size: %d", lba, tl, dev.Size>>dev.BlockShift)
			goto sense
		}
	} else {
		if lba >= dev.Size>>dev.BlockShift {
			key = ILLEGAL_REQUEST
			asc = ASC_LBA_OUT_OF_RANGE
			log.Warnf("sense")
			goto sense
		}
	}
	return api.SAMStatGood
sense:
	if cmd.InSDBBuffer != nil {
		cmd.InSDBBuffer.Resid = 0
	}
	BuildSenseData(cmd, key, asc)
	return api.SAMStatCheckCondition
}

func SBCServiceAction(host int, cmd *api.SCSICommand) api.SAMStat {
	opcode := api.SCSICommandType(cmd.SCB[1] & 0x1f)
	switch opcode {
	case api.READ_CAPACITY:
		return SBCReadCapacity(host, cmd)
	case api.SAI_READ_CAPACITY_16:
		return SBCReadCapacity16(host, cmd)
	case api.SAI_GET_LBA_STATUS:
		return SBCGetLbaStatus(host, cmd)
	}
	return api.SAMStatGood
}

/*
 * SBCSyncCache Implements SCSI SYNCHRONIZE CACHE(10) and SYNCHRONIZE CACHE(16) command
 * The SYNCHRONIZE CACHE command requests that the device server ensure that
 * the specified logical blocks have their most recent data values recorded in
 * non-volatile cache and/or on the medium, based on the SYNC_NV bit.
 *
 * Reference : SBC2r16
 * 5.18 - SYNCHRONIZE CACHE (10)
 * 5.19 - SYNCHRONIZE CACHE (16)
 */
func SBCSyncCache(host int, cmd *api.SCSICommand) api.SAMStat {
	scb := cmd.SCB
	lba := getSCSIReadWriteOffset(scb)
	tl := getSCSIReadWriteCount(scb)
	dev := cmd.Device

	cmd.Offset = lba << dev.BlockShift
	cmd.TL = tl << dev.BlockShift

	err, key, asc := bsPerformCommand(dev.Storage, cmd)
	if err != nil {
		BuildSenseData(cmd, key, asc)
		return api.SAMStatCheckCondition
	}

	return api.SAMStatGood
}
