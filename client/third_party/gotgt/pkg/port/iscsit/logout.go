package iscsit

import (
	"bytes"

	"github.com/gostor/gotgt/pkg/util"
)

func (m *ISCSICommand) logoutRespBytes() []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(byte(OpLogoutResp))
	buf.WriteByte(0x80)
	buf.WriteByte(0x00) // response
	buf.WriteByte(0x00)
	for i := 4; i < 16; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.TaskTag))[4:])
	for i := 20; i < 24; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(util.MarshalUint64(uint64(m.StatSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.ExpCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.MaxCmdSN))[4:])
	for i := 36; i < 48; i++ {
		buf.WriteByte(0x00)
	}
	return buf.Bytes()
}
