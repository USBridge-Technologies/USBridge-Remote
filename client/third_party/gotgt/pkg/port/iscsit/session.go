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

package iscsit

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/scsi"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
)

var (
	SESSION_NORMAL    int = 0
	SESSION_DISCOVERY int = 1
)

var DIGEST_CRC32C uint = 1 << 1
var DIGEST_NONE uint = 1 << 0
var DIGEST_ALL uint = DIGEST_NONE | DIGEST_CRC32C
var BHS_SIZE = 48

const (
	MAX_QUEUE_CMD_MIN = 1
	MAX_QUEUE_CMD_DEF = 128
	MAX_QUEUE_CMD_MAX = 512
)

const (
	ISCSI_PARAM_MAX_RECV_DLENGTH = iota
	ISCSI_PARAM_HDRDGST_EN
	ISCSI_PARAM_DATADGST_EN
	ISCSI_PARAM_INITIAL_R2T_EN
	ISCSI_PARAM_MAX_R2T
	ISCSI_PARAM_IMM_DATA_EN
	ISCSI_PARAM_FIRST_BURST
	ISCSI_PARAM_MAX_BURST
	ISCSI_PARAM_PDU_INORDER_EN
	ISCSI_PARAM_DATASEQ_INORDER_EN
	ISCSI_PARAM_ERL
	ISCSI_PARAM_IFMARKER_EN
	ISCSI_PARAM_OFMARKER_EN
	ISCSI_PARAM_DEFAULTTIME2WAIT
	ISCSI_PARAM_DEFAULTTIME2RETAIN
	ISCSI_PARAM_OFMARKINT
	ISCSI_PARAM_IFMARKINT
	ISCSI_PARAM_MAXCONNECTIONS
	/* iSCSI Extensions for RDMA (RFC5046) */
	ISCSI_PARAM_RDMA_EXTENSIONS
	ISCSI_PARAM_TARGET_RDSL
	ISCSI_PARAM_INITIATOR_RDSL
	ISCSI_PARAM_MAX_OUTST_PDU
	/* "local" parmas, never sent to the initiator */
	ISCSI_PARAM_FIRST_LOCAL
	ISCSI_PARAM_MAX_XMIT_DLENGTH = ISCSI_PARAM_FIRST_LOCAL
	ISCSI_PARAM_MAX_QUEUE_CMD
	/* must always be last */
	ISCSI_PARAM_MAX
)

type ISCSISessionParam struct {
	idx   uint
	State int
	Value uint
}
type ISCSISessionParamList []ISCSISessionParam

func (list ISCSISessionParamList) Len() int {
	return len(list)
}

func (list ISCSISessionParamList) Less(i, j int) bool {
	if list[i].idx <= list[j].idx {
		return true
	} else {
		return false
	}
}

func (list ISCSISessionParamList) Swap(i, j int) {
	list[i], list[j] = list[j], list[i]
}

/*
 * The defaults here are according to the spec and must not be changed,
 * otherwise the initiator may make the wrong assumption.  If you want
 * to change a value, edit the value in iscsi_target_create.
 *
 * The param MaxXmitDataSegmentLength doesn't really exist.  It's a way
 * to remember the RDSL of the initiator, which defaults to 8k if he has
 * not told us otherwise.
 */
type KeyConvFunc func(value string) (uint, bool)
type KeyInConvFunc func(value uint) string

type iscsiSessionKeys struct {
	idx        uint
	constValue bool
	def        uint
	min        uint
	max        uint
	conv       KeyConvFunc
	inConv     KeyInConvFunc
}

func digestKeyConv(value string) (uint, bool) {
	var crc uint
	valueArray := strings.Split(value, ",")
	if len(valueArray) == 0 {
		return crc, false
	}
	for _, tmpV := range valueArray {
		if strings.EqualFold(tmpV, "crc32c") {
			crc |= DIGEST_CRC32C
		} else if strings.EqualFold(tmpV, "none") {
			crc |= DIGEST_NONE
		} else {
			return crc, false
		}
	}

	return crc, true
}

func digestKeyInConv(value uint) string {
	str := ""
	switch value {
	case DIGEST_NONE:
		str = "None"
	case DIGEST_CRC32C:
		str = "CRC32C"
	case DIGEST_ALL:
		str = "None,CRC32C"
	}
	return str
}

func numberKeyConv(value string) (uint, bool) {
	v, err := strconv.Atoi(value)
	if err == nil {
		return uint(v), true
	}
	return uint(v), false
}

func numberKeyInConv(value uint) string {
	s := strconv.Itoa(int(value))
	return s
}

func boolKeyConv(value string) (uint, bool) {
	if strings.EqualFold(value, "yes") {
		return 1, true
	} else if strings.EqualFold(value, "no") {
		return 0, true
	}
	return 0, false
}

func boolKeyInConv(value uint) string {
	if value == 0 {
		return "No"
	}
	return "Yes"
}

var sessionKeys map[string]*iscsiSessionKeys = map[string]*iscsiSessionKeys{
	// ISCSI_PARAM_MAX_RECV_DLENGTH
	"MaxRecvDataSegmentLength": {ISCSI_PARAM_MAX_RECV_DLENGTH, true, 65536, 512, 16777215, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_HDRDGST_EN
	"HeaderDigest": {ISCSI_PARAM_HDRDGST_EN, false, DIGEST_NONE, DIGEST_NONE, DIGEST_ALL, digestKeyConv, digestKeyInConv},
	// ISCSI_PARAM_DATADGST_EN
	"DataDigest": {ISCSI_PARAM_DATADGST_EN, false, DIGEST_NONE, DIGEST_NONE, DIGEST_ALL, digestKeyConv, digestKeyInConv},
	// ISCSI_PARAM_INITIAL_R2T_EN
	"InitialR2T": {ISCSI_PARAM_INITIAL_R2T_EN, true, 1, 0, 1, boolKeyConv, boolKeyInConv},
	// ISCSI_PARAM_MAX_R2T
	"MaxOutstandingR2T": {ISCSI_PARAM_MAX_R2T, true, 1, 1, 65535, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_IMM_DATA_EN
	"ImmediateData": {ISCSI_PARAM_IMM_DATA_EN, true, 1, 0, 1, boolKeyConv, boolKeyInConv},
	// ISCSI_PARAM_FIRST_BURST
	"FirstBurstLength": {ISCSI_PARAM_FIRST_BURST, true, 65536, 512, 16777215, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_MAX_BURST
	"MaxBurstLength": {ISCSI_PARAM_MAX_BURST, true, 262144, 512, 16777215, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_PDU_INORDER_EN
	"DataPDUInOrder": {ISCSI_PARAM_PDU_INORDER_EN, true, 1, 0, 1, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_DATASEQ_INORDER_EN
	"DataSequenceInOrder": {ISCSI_PARAM_DATASEQ_INORDER_EN, true, 1, 0, 1, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_ERL
	"ErrorRecoveryLevel": {ISCSI_PARAM_ERL, true, 0, 0, 2, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_IFMARKER_EN
	"IFMarker": {ISCSI_PARAM_IFMARKER_EN, true, 0, 0, 1, boolKeyConv, boolKeyInConv},
	// ISCSI_PARAM_OFMARKER_EN
	"OFMarker": {ISCSI_PARAM_OFMARKER_EN, true, 0, 0, 1, boolKeyConv, boolKeyInConv},
	// ISCSI_PARAM_DEFAULTTIME2WAIT
	"DefaultTime2Wait": {ISCSI_PARAM_DEFAULTTIME2WAIT, true, 2, 0, 3600, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_DEFAULTTIME2RETAIN
	"DefaultTime2Retain": {ISCSI_PARAM_DEFAULTTIME2RETAIN, false, 20, 0, 3600, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_OFMARKINT
	"OFMarkInt": {ISCSI_PARAM_OFMARKINT, true, 2048, 1, 65535, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_IFMARKINT
	"IFMarkInt": {ISCSI_PARAM_IFMARKINT, true, 2048, 1, 65535, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_MAXCONNECTIONS
	"MaxConnections": {ISCSI_PARAM_MAXCONNECTIONS, true, 1, 1, 65535, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_RDMA_EXTENSIONS
	"RDMAExtensions": {ISCSI_PARAM_RDMA_EXTENSIONS, true, 0, 0, 1, boolKeyConv, boolKeyInConv},
	// ISCSI_PARAM_TARGET_RDSL
	"TargetRecvDataSegmentLength": {ISCSI_PARAM_TARGET_RDSL, true, 8192, 512, 16777215, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_INITIATOR_RDSL
	"InitiatorRecvDataSegmentLength": {ISCSI_PARAM_INITIATOR_RDSL, true, 8192, 512, 16777215, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_MAX_OUTST_PDU
	"MaxOutstandingUnexpectedPDUs": {ISCSI_PARAM_MAX_OUTST_PDU, true, 0, 2, 4294967295, numberKeyConv, numberKeyInConv},
	// "local" parmas, never sent to the initiator
	// ISCSI_PARAM_MAX_XMIT_DLENGTH
	"MaxXmitDataSegmentLength": {ISCSI_PARAM_MAX_XMIT_DLENGTH, true, 8192, 512, 16777215, numberKeyConv, numberKeyInConv},
	// ISCSI_PARAM_MAX_QUEUE_CMD
	"MaxQueueCmd": {ISCSI_PARAM_MAX_QUEUE_CMD, true, MAX_QUEUE_CMD_DEF, MAX_QUEUE_CMD_MIN, MAX_QUEUE_CMD_MAX, numberKeyConv, numberKeyInConv},
}

// Session is an iSCSI session.
type ISCSISession struct {
	Refcount       int
	Initiator      string
	InitiatorAlias string
	Target         *ISCSITarget
	ISID           uint64
	TSIH           uint16
	TPGT           uint16
	SessionType    int
	ITNexus        *api.ITNexus

	ExpCmdSN uint32
	MaxCmdSN uint32
	// currently, this is only one connection per session
	Connections        map[uint16]*iscsiConnection
	ConnectionsRWMutex sync.RWMutex
	Commands           []*ISCSICommand
	PendingTasks       taskQueue
	PendingTasksMutex  sync.RWMutex
	MaxQueueCommand    uint32
	SessionParam       ISCSISessionParamList
	Info               string
	Rdma               int
}

type taskQueue []*iscsiTask

func (tq taskQueue) Len() int { return len(tq) }

func (tq taskQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, priority so we use greater than here.
	return tq[i].cmd.CmdSN > tq[j].cmd.CmdSN
}

func (tq taskQueue) Swap(i, j int) {
	tq[i], tq[j] = tq[j], tq[i]
}

func (tq *taskQueue) Push(x *iscsiTask) {
	item := x
	*tq = append(*tq, item)
}

func (tq *taskQueue) Pop() *iscsiTask {
	old := *tq
	n := len(old)
	item := old[n-1]
	*tq = old[0 : n-1]
	return item
}

func (tq taskQueue) GetByTag(tag uint32) *iscsiTask {
	for _, t := range tq {
		if t.tag == tag {
			return t
		}
	}
	return nil
}

func (tq *taskQueue) RemoveByTag(tag uint32) *iscsiTask {
	old := *tq
	for i, t := range old {
		if t.tag == tag {
			*tq = append(old[:i], old[i+1:]...)
			return t
		}
	}
	return nil
}

func (s *ISCSITargetDriver) LookupISCSISession(tgtName string, iniName string, isid uint64, tsih uint16, tpgt uint16) *ISCSISession {
	var (
		tgt  *ISCSITarget
		sess *ISCSISession
		ok   bool
	)
	tgt, ok = s.iSCSITargets[tgtName]
	if !ok {
		return nil
	}
	tgt.SessionsRWMutex.RLock()
	defer tgt.SessionsRWMutex.RUnlock()
	sess, ok = tgt.Sessions[tsih]
	if !ok {
		return nil
	}
	if (sess.ISID == isid) && (sess.TPGT == tpgt) {
		return sess
	}
	return nil
}

func (s *ISCSITargetDriver) UnBindISCSISession(sess *ISCSISession) {
	target := sess.Target
	target.SessionsRWMutex.Lock()
	defer target.SessionsRWMutex.Unlock()
	delete(target.Sessions, sess.TSIH)
	scsi.RemoveITNexus(&sess.Target.SCSITarget, sess.ITNexus)
}

func (s *ISCSITargetDriver) BindISCSISession(conn *iscsiConnection) error {
	var (
		target    *ISCSITarget
		existSess *ISCSISession
		existConn *iscsiConnection
		newSess   *ISCSISession
		tpgt      uint16
		err       error
	)

	//Find TPGT and Target ID
	if conn.loginParam.sessionType == SESSION_DISCOVERY {
		conn.tid = 0xffff
	} else {
		for _, t := range s.iSCSITargets {
			if t.SCSITarget.Name == conn.loginParam.target {
				target = t
				break
			}
		}
		if target == nil {
			return fmt.Errorf("No target found with name(%s)", conn.loginParam.target)
		}

		tpgt, err = target.FindTPG(conn.conn.LocalAddr().String())
		if err != nil {
			return err
		}
		conn.loginParam.tpgt = tpgt
		conn.tid = target.TID
	}

	existSess = s.LookupISCSISession(conn.loginParam.target, conn.loginParam.initiator,
		conn.loginParam.isid, conn.loginParam.tsih, conn.loginParam.tpgt)
	if existSess != nil {
		existConn = existSess.LookupConnection(conn.cid)
	}

	if conn.loginParam.sessionType == SESSION_DISCOVERY &&
		conn.loginParam.tsih != ISCSI_UNSPEC_TSIH &&
		existSess != nil {
		return fmt.Errorf("initiator err, invalid request")
	}

	if existSess == nil && conn.loginParam.tsih != 0 &&
		existSess.TSIH != conn.loginParam.tsih {
		return fmt.Errorf("initiator err, no session")
	}

	if existSess == nil {
		newSess, err = s.NewISCSISession(conn)
		if err != nil {
			return err
		}

		if newSess.SessionType == SESSION_NORMAL {
			log.Infof("Login request received from initiator: %v, Session type: %s, Target name:%v, ISID: 0x%x",
				conn.loginParam.initiator, "Normal", conn.loginParam.target, conn.loginParam.isid)
			//register normal session
			itnexus := &api.ITNexus{uuid.NewV1(), GeniSCSIITNexusID(newSess)}
			scsi.AddITNexus(&newSess.Target.SCSITarget, itnexus)
			newSess.ITNexus = itnexus
			conn.session = newSess

			newSess.Target.SessionsRWMutex.Lock()
			newSess.Target.Sessions[newSess.TSIH] = newSess
			newSess.Target.SessionsRWMutex.Unlock()
		} else {
			log.Infof("Discovery request received from initiator: %v, Session type: %s, ISID: 0x%x",
				conn.loginParam.initiator, "Discovery", conn.loginParam.isid)
			conn.session = newSess
		}
	} else {
		if conn.loginParam.tsih == ISCSI_UNSPEC_TSIH {
			log.Infof("Session Reinstatement initiator name:%v,target name:%v,ISID:0x%x",
				conn.loginParam.initiator, conn.loginParam.target, conn.loginParam.isid)
			newSess, err = s.ReInstatement(existConn.session, conn)
			if err != nil {
				return err
			}

			itnexus := &api.ITNexus{uuid.NewV1(), GeniSCSIITNexusID(newSess)}
			scsi.AddITNexus(&newSess.Target.SCSITarget, itnexus)
			newSess.ITNexus = itnexus
			conn.session = newSess

			newSess.Target.SessionsRWMutex.Lock()
			newSess.Target.Sessions[newSess.TSIH] = newSess
			newSess.Target.SessionsRWMutex.Unlock()
		} else {
			if existConn != nil {
				log.Infof("Connection Reinstatement initiator name:%v,target name:%v,ISID:0x%x",
					conn.loginParam.initiator, conn.loginParam.target, conn.loginParam.isid)
				existConn.ReInstatement(conn)
			}
		}
	}

	return nil
}

// New creates a new session.
func (s *ISCSITargetDriver) NewISCSISession(conn *iscsiConnection) (*ISCSISession, error) {
	var (
		target *ISCSITarget
		tsih   uint16
	)

	for _, t := range s.iSCSITargets {
		if t.TID == conn.tid {
			target = t
			break
		}
	}
	if target == nil && conn.tid != 0xffff {
		return nil, fmt.Errorf("No target found with tid(%d)", conn.tid)
	}

	tsih = s.AllocTSIH()
	if tsih == ISCSI_UNSPEC_TSIH {
		return nil, fmt.Errorf("TSIH Pool exhausted tid(%d)", conn.tid)
	}

	sess := &ISCSISession{
		TSIH:            tsih,
		ISID:            conn.loginParam.isid,
		TPGT:            conn.loginParam.tpgt,
		Initiator:       conn.loginParam.initiator,
		InitiatorAlias:  conn.loginParam.initiatorAlias,
		SessionType:     conn.loginParam.sessionType,
		Target:          target,
		Connections:     map[uint16]*iscsiConnection{conn.cid: conn},
		SessionParam:    conn.loginParam.sessionParam,
		MaxQueueCommand: uint32(conn.loginParam.sessionParam[ISCSI_PARAM_MAX_QUEUE_CMD].Value),
		Rdma:            0,
		ExpCmdSN:        conn.expCmdSN,
	}
	return sess, nil
}

func (sess *ISCSISession) LookupConnection(cid uint16) *iscsiConnection {
	sess.ConnectionsRWMutex.RLock()
	defer sess.ConnectionsRWMutex.RUnlock()
	conn := sess.Connections[cid]
	return conn
}

func (s *ISCSITargetDriver) ReInstatement(existSess *ISCSISession, conn *iscsiConnection) (*ISCSISession, error) {
	newSess, err := s.NewISCSISession(conn)
	if err != nil {
		return nil, err
	}
	newSess.ExpCmdSN = existSess.ExpCmdSN
	newSess.MaxCmdSN = existSess.MaxCmdSN + 1
	s.UnBindISCSISession(existSess)
	for _, tmpConn := range existSess.Connections {
		tmpConn.close()
	}
	existSess.Connections = map[uint16]*iscsiConnection{}
	return newSess, nil
}

/*
 * iSCSI I_T nexus identifer = (iSCSI Initiator Name + 'i' + ISID, iSCSI Target Name + 't' + Portal Group Tag)
 */
func GeniSCSIITNexusID(sess *ISCSISession) string {
	strID := fmt.Sprintf("%si0x%12x,%st%d",
		sess.Initiator, sess.ISID,
		sess.Target.SCSITarget.Name,
		sess.TPGT)
	return strID
}
