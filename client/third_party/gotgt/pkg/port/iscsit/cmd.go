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

package iscsit

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/util"
	log "github.com/sirupsen/logrus"
)

type OpCode int

const (
	// Defined on the initiator.
	OpNoopOut     OpCode = 0x00
	OpSCSICmd            = 0x01
	OpSCSITaskReq        = 0x02
	OpLoginReq           = 0x03
	OpTextReq            = 0x04
	OpSCSIOut            = 0x05
	OpLogoutReq          = 0x06
	OpSNACKReq           = 0x10
	// Defined on the target.
	OpNoopIn       OpCode = 0x20
	OpSCSIResp            = 0x21
	OpSCSITaskResp        = 0x22
	OpLoginResp           = 0x23
	OpTextResp            = 0x24
	OpSCSIIn              = 0x25
	OpLogoutResp          = 0x26
	OpReady               = 0x31
	OpAsync               = 0x32
	OpReject              = 0x3f
)

const (
	MaxBurstLength           uint32 = 262144
	MaxRecvDataSegmentLength uint32 = 65536
)

var opCodeMap = map[OpCode]string{
	OpNoopOut:      "NOP-Out",
	OpSCSICmd:      "SCSI Command",
	OpSCSITaskReq:  "SCSI Task Management FunctionRequest",
	OpLoginReq:     "Login Request",
	OpTextReq:      "Text Request",
	OpSCSIOut:      "SCSI Data-Out (write)",
	OpLogoutReq:    "Logout Request",
	OpSNACKReq:     "SNACK Request",
	OpNoopIn:       "NOP-In",
	OpSCSIResp:     "SCSI Response",
	OpSCSITaskResp: "SCSI Task Management Function Response",
	OpLoginResp:    "Login Response",
	OpTextResp:     "Text Response",
	OpSCSIIn:       "SCSI Data-In (read)",
	OpLogoutResp:   "Logout Response",
	OpReady:        "Ready To Transfer (R2T)",
	OpAsync:        "Asynchronous Message",
	OpReject:       "Reject",
}

const DataPadding = 4

type ISCSITaskManagementFunc struct {
	Result            byte
	TaskFunc          uint32
	ReferencedTaskTag uint32
}

type ISCSICommand struct {
	OpCode             OpCode
	RawHeader          []byte
	DataLen            int
	RawData            []byte
	Final              bool
	FinalInSeq         bool
	Immediate          bool
	TaskTag            uint32
	StartTime          time.Time
	ExpCmdSN, MaxCmdSN uint32
	AHSLen             int
	Resid              uint32

	// Connection ID.
	ConnID uint16
	// Command serial number.
	CmdSN uint32
	// Expected status serial.
	ExpStatSN uint32

	Read, Write bool
	LUN         [8]uint8
	// Transit bit.
	Transit bool
	// Continue bit.
	Cont bool
	// Current Stage, Next Stage.
	CSG, NSG iSCSILoginStage
	// Initiator part of the SSID.
	ISID uint64
	// Target-assigned Session Identifying Handle.
	TSIH uint16
	// Status serial number.
	StatSN uint32

	// For login response.
	StatusClass  uint8
	StatusDetail uint8

	// SCSI commands
	SCSIOpCode      byte
	ExpectedDataLen uint32
	CDB             []byte
	Status          byte
	SCSIResponse    byte

	// Task request
	ISCSITaskManagementFunc

	// R2T
	R2TSN         uint32
	DesiredLength uint32

	// Data-In/Out
	HasStatus    bool
	DataSN       uint32
	BufferOffset uint32
}

func (cmd *ISCSICommand) Bytes() []byte {
	switch cmd.OpCode {
	case OpLoginResp:
		return cmd.loginRespBytes()
	case OpLogoutResp:
		return cmd.logoutRespBytes()
	case OpSCSIResp:
		return cmd.scsiCmdRespBytes()
	case OpSCSIIn:
		return cmd.dataInBytes()
	case OpTextResp:
		return cmd.textRespBytes()
	case OpNoopIn:
		return cmd.noopInBytes()
	case OpSCSITaskResp:
		return cmd.scsiTMFRespBytes()
	case OpReady:
		return cmd.r2tRespBytes()
	}
	return nil
}

func (m *ISCSICommand) String() string {
	var s []string
	s = append(s, fmt.Sprintf("Op: %v", opCodeMap[m.OpCode]))
	s = append(s, fmt.Sprintf("Final = %v", m.Final))
	s = append(s, fmt.Sprintf("Immediate = %v", m.Immediate))
	s = append(s, fmt.Sprintf("Data Segment Length = %d", m.DataLen))
	s = append(s, fmt.Sprintf("Task Tag = %x", m.TaskTag))
	s = append(s, fmt.Sprintf("AHS Length = %d", m.AHSLen))
	switch m.OpCode {
	case OpLoginReq:
		s = append(s, fmt.Sprintf("ISID = %x", m.ISID))
		s = append(s, fmt.Sprintf("Transit = %v", m.Transit))
		s = append(s, fmt.Sprintf("Continue = %v", m.Cont))
		s = append(s, fmt.Sprintf("Current Stage = %v", m.CSG))
		s = append(s, fmt.Sprintf("Next Stage = %v", m.NSG))
	case OpLoginResp:
		s = append(s, fmt.Sprintf("ISID = %x", m.ISID))
		s = append(s, fmt.Sprintf("Transit = %v", m.Transit))
		s = append(s, fmt.Sprintf("Continue = %v", m.Cont))
		s = append(s, fmt.Sprintf("Current Stage = %v", m.CSG))
		s = append(s, fmt.Sprintf("Next Stage = %v", m.NSG))
		s = append(s, fmt.Sprintf("Status Class = %d", m.StatusClass))
		s = append(s, fmt.Sprintf("Status Detail = %d", m.StatusDetail))
	case OpSCSICmd, OpSCSIOut, OpSCSIIn:
		s = append(s, fmt.Sprintf("LUN = %d", m.LUN))
		s = append(s, fmt.Sprintf("ExpectedDataLen = %d", m.ExpectedDataLen))
		s = append(s, fmt.Sprintf("CmdSN = %d", m.CmdSN))
		s = append(s, fmt.Sprintf("ExpStatSN = %d", m.ExpStatSN))
		s = append(s, fmt.Sprintf("Read = %v", m.Read))
		s = append(s, fmt.Sprintf("Write = %v", m.Write))
		s = append(s, fmt.Sprintf("CDB = %x", m.CDB))
	case OpSCSIResp:
		s = append(s, fmt.Sprintf("StatSN = %d", m.StatSN))
		s = append(s, fmt.Sprintf("ExpCmdSN = %d", m.ExpCmdSN))
		s = append(s, fmt.Sprintf("MaxCmdSN = %d", m.MaxCmdSN))
	}
	return strings.Join(s, "\n")
}

// parseUint parses the given slice as a network-byte-ordered integer.  If
// there are more than 8 bytes in data, it overflows.
func ParseUint(data []byte) uint64 {
	var out uint64
	for i := 0; i < len(data); i++ {
		out += uint64(data[len(data)-i-1]) << uint(8*i)
	}
	return out
}

func parseHeader(data []byte) (*ISCSICommand, error) {
	if len(data) != BHS_SIZE {
		return nil, fmt.Errorf("garbled header")
	}
	// TODO: sync.Pool
	m := &ISCSICommand{}
	m.Immediate = 0x40&data[0] == 0x40
	m.OpCode = OpCode(data[0] & ISCSI_OPCODE_MASK)
	m.Final = 0x80&data[1] == 0x80
	m.AHSLen = int(data[4]) * 4
	m.DataLen = int(ParseUint(data[5:8]))
	m.TaskTag = uint32(ParseUint(data[16:20]))
	m.StartTime = time.Now()
	switch m.OpCode {
	case OpSCSICmd:
		m.LUN = [8]byte{data[9]}
		m.ExpectedDataLen = uint32(ParseUint(data[20:24]))
		m.CmdSN = uint32(ParseUint(data[24:28]))
		m.Read = data[1]&0x40 == 0x40
		m.Write = data[1]&0x20 == 0x20
		m.CDB = data[32:48]
		m.ExpStatSN = uint32(ParseUint(data[28:32]))
		m.SCSIOpCode = m.CDB[0]
		SCSIOpcode := api.SCSICommandType(m.SCSIOpCode)
		switch SCSIOpcode {
		case api.READ_6, api.READ_10, api.READ_12, api.READ_16:
			m.Read = true
		case api.WRITE_6, api.WRITE_10, api.WRITE_12, api.WRITE_16, api.WRITE_VERIFY, api.WRITE_VERIFY_12, api.WRITE_VERIFY_16:
			m.Write = true
		}
		fallthrough
	case OpSCSITaskReq:
		m.ReferencedTaskTag = uint32(ParseUint(data[20:24]))
		m.TaskFunc = uint32(data[1] & ISCSI_FLAG_TM_FUNC_MASK)
	case OpSCSIResp:
	case OpSCSIOut:
		m.LUN = [8]byte{data[9]}
		m.ExpStatSN = uint32(ParseUint(data[28:32]))
		m.DataSN = uint32(ParseUint(data[36:40]))
		m.BufferOffset = uint32(ParseUint(data[40:44]))
	case OpLoginReq, OpTextReq, OpNoopOut, OpLogoutReq:
		m.Transit = m.Final
		m.Cont = data[1]&0x40 == 0x40
		if m.Cont && m.Transit {
			// rfc7143 11.12.2
			return nil, fmt.Errorf("transit and continue bits set in same login request")
		}
		m.CSG = iSCSILoginStage(data[1]&0xc) >> 2
		m.NSG = iSCSILoginStage(data[1] & 0x3)
		m.ISID = uint64(ParseUint(data[8:14]))
		m.TSIH = uint16(ParseUint(data[14:16]))
		m.ConnID = uint16(ParseUint(data[20:22]))
		m.CmdSN = uint32(ParseUint(data[24:28]))
		m.ExpStatSN = uint32(ParseUint(data[28:32]))
	case OpLoginResp:
		m.Transit = m.Final
		m.Cont = data[1]&0x40 == 0x40
		if m.Cont && m.Transit {
			// rfc7143 11.12.2
			return nil, fmt.Errorf("transit and continue bits set in same login request")
		}
		m.CSG = iSCSILoginStage(data[1]&0xc) >> 2
		m.NSG = iSCSILoginStage(data[1] & 0x3)
		m.StatSN = uint32(ParseUint(data[24:28]))
		m.ExpCmdSN = uint32(ParseUint(data[28:32]))
		m.MaxCmdSN = uint32(ParseUint(data[32:36]))
		m.StatusClass = uint8(data[36])
		m.StatusDetail = uint8(data[37])
	}
	return m, nil
}

func (m *ISCSICommand) scsiCmdRespBytes() []byte {
	// rfc7143 11.4
	buf := bytes.Buffer{}
	buf.WriteByte(byte(OpSCSIResp))
	var flag byte = 0x80
	if m.Resid > 0 {
		if m.Resid > m.ExpectedDataLen {
			flag |= 0x04
		} else {
			flag |= 0x02
		}
	}
	buf.WriteByte(flag)
	buf.WriteByte(byte(m.SCSIResponse))
	buf.WriteByte(byte(m.Status))

	buf.WriteByte(0x00)
	buf.Write(util.MarshalUint64(uint64(len(m.RawData)))[5:]) // 5-8
	// Skip through to byte 16
	for i := 0; i < 8; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.TaskTag))[4:])
	for i := 0; i < 4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.StatSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.ExpCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.MaxCmdSN))[4:])
	for i := 0; i < 2*4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.Resid))[4:])
	buf.Write(m.RawData)
	dl := len(m.RawData)
	for dl%4 > 0 {
		dl++
		buf.WriteByte(0x00)
	}

	return buf.Bytes()
}

func (m *ISCSICommand) dataInBytes() []byte {
	// rfc7143 11.7
	dl := m.DataLen
	for dl%4 > 0 {
		dl++
	}
	var buf = make([]byte, (48 + dl))
	buf[0] = byte(OpSCSIIn)
	var flag byte
	if m.FinalInSeq || m.Final == true {
		flag |= 0x80
	}
	if m.HasStatus && m.Final == true {
		flag |= 0x01
	}
	log.Debugf("resid: %v, ExpectedDataLen: %v", m.Resid, m.ExpectedDataLen)
	if m.Resid > 0 {
		if m.Resid > m.ExpectedDataLen {
			flag |= 0x04
		} else if m.Resid < m.ExpectedDataLen {
			flag |= 0x02
		}
	}
	buf[1] = flag
	//buf.WriteByte(0x00)
	if m.HasStatus && m.Final == true {
		flag = byte(m.Status)
	}
	//buf.WriteByte(flag)
	buf[3] = flag
	copy(buf[5:], util.MarshalUint64(uint64(m.DataLen))[5:])
	// Skip through to byte 16 Since A bit is not set 11.7.4
	copy(buf[16:], util.MarshalUint32(m.TaskTag))
	copy(buf[24:], util.MarshalUint32(m.StatSN))
	copy(buf[28:], util.MarshalUint32(m.ExpCmdSN))
	copy(buf[32:], util.MarshalUint32(m.MaxCmdSN))
	copy(buf[36:], util.MarshalUint32(m.DataSN))
	copy(buf[40:], util.MarshalUint32(m.BufferOffset))
	copy(buf[44:], util.MarshalUint32(m.Resid))
	if m.ExpectedDataLen != 0 {
		copy(buf[48:], m.RawData[m.BufferOffset:m.BufferOffset+uint32(m.DataLen)])
	}

	return buf
}

func (m *ISCSICommand) textRespBytes() []byte {
	buf := bytes.Buffer{}
	buf.WriteByte(byte(OpTextResp))
	var b byte
	if m.Final {
		b |= 0x80
	}
	if m.Cont {
		b |= 0x40
	}
	// byte 1
	buf.WriteByte(b)

	b = 0
	buf.WriteByte(b)
	buf.WriteByte(b)
	buf.WriteByte(b)
	buf.Write(util.MarshalUint64(uint64(len(m.RawData)))[5:]) // 5-8
	// Skip through to byte 12
	for i := 0; i < 2*4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.TaskTag))[4:])
	for i := 0; i < 4; i++ {
		buf.WriteByte(0xff)
	}
	buf.Write(util.MarshalUint64(uint64(m.StatSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.ExpCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.MaxCmdSN))[4:])
	for i := 0; i < 3*4; i++ {
		buf.WriteByte(0x00)
	}
	rd := m.RawData
	for len(rd)%4 != 0 {
		rd = append(rd, 0)
	}
	buf.Write(rd)
	return buf.Bytes()
}

func (m *ISCSICommand) noopInBytes() []byte {
	buf := bytes.Buffer{}
	buf.WriteByte(byte(OpNoopIn))
	var b byte
	b |= 0x80
	// byte 1
	buf.WriteByte(b)

	b = 0
	buf.WriteByte(b)
	buf.WriteByte(b)
	buf.WriteByte(b)
	buf.Write(util.MarshalUint64(uint64(len(m.RawData)))[5:]) // 5-8
	// Skip through to byte 12
	for i := 0; i < 2*4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.TaskTag))[4:])
	for i := 0; i < 4; i++ {
		buf.WriteByte(0xff)
	}
	buf.Write(util.MarshalUint64(uint64(m.StatSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.ExpCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.MaxCmdSN))[4:])
	for i := 0; i < 3*4; i++ {
		buf.WriteByte(0x00)
	}
	rd := m.RawData
	for len(rd)%4 != 0 {
		rd = append(rd, 0)
	}
	buf.Write(rd)
	return buf.Bytes()
}

func (m *ISCSICommand) scsiTMFRespBytes() []byte {
	// rfc7143 11.6
	buf := bytes.Buffer{}
	buf.WriteByte(byte(OpSCSITaskResp))
	buf.WriteByte(0x80)
	buf.WriteByte(m.Result)
	buf.WriteByte(0x00)

	// Skip through to byte 16
	for i := 0; i < 3*4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.TaskTag))[4:])
	for i := 0; i < 4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.StatSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.ExpCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.MaxCmdSN))[4:])
	for i := 0; i < 3*4; i++ {
		buf.WriteByte(0x00)
	}

	return buf.Bytes()
}

func (m *ISCSICommand) r2tRespBytes() []byte {
	// rfc7143 11.8
	buf := bytes.Buffer{}
	buf.WriteByte(byte(OpReady))
	var b byte
	if m.Final {
		b |= 0x80
	}
	buf.WriteByte(b)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)

	// Skip through to byte 16
	for i := 0; i < 3*4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.TaskTag))[4:])
	for i := 0; i < 4; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.StatSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.ExpCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.MaxCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.R2TSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.BufferOffset))[4:])
	buf.Write(util.MarshalUint64(uint64(m.DesiredLength))[4:])

	return buf.Bytes()
}
