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

	"github.com/gostor/gotgt/pkg/util"
)

var (
	iSCSILoginParamTextKV = []util.KeyValue{
		{"HeaderDigest", "None"},
		{"DataDigest", "None"},
		{"ImmediateData", "Yes"},
		{"InitialR2T", "Yes"},
		{"MaxBurstLength", "262144"},
		{"FirstBurstLength", "65536"},
		{"MaxRecvDataSegmentLength", "65536"},
		{"DefaultTime2Wait", "2"},
		{"DefaultTime2Retain", "0"},
		{"MaxOutstandingR2T", "1"},
		{"IFMarker", "No"},
		{"OFMarker", "No"},
		{"DataPDUInOrder", "Yes"},
		{"DataSequenceInOrder", "Yes"}}
)

type iSCSILoginStage int

const (
	SecurityNegotiation         iSCSILoginStage = 0
	LoginOperationalNegotiation                 = 1
	FullFeaturePhase                            = 3
)

func (s iSCSILoginStage) String() string {
	switch s {
	case SecurityNegotiation:
		return "Security Negotiation"
	case LoginOperationalNegotiation:
		return "Login Operational Negotiation"
	case FullFeaturePhase:
		return "Full Feature Phase"
	}
	return "Unknown Stage"
}

func loginKVDeclare(conn *iscsiConnection, negoKV []util.KeyValue) []util.KeyValue {
	negoKV = append(negoKV, util.KeyValue{"TargetPortalGroupTag",
		numberKeyInConv(uint(conn.loginParam.tpgt))})
	negoKV = append(negoKV, util.KeyValue{"MaxRecvDataSegmentLength",
		numberKeyInConv(sessionKeys["MaxRecvDataSegmentLength"].def)})
	return negoKV
}

func stringsContains(s []string, p string) bool {
	for _, q := range s {
		if q == p {
			return true
		}
	}
	return false
}

func (conn *iscsiConnection) processSecurityData() error {
	securityKV := util.ParseKVText(conn.req.RawData)

	for key, val := range securityKV {
		if key == "AuthMethod" {
			// It can be a list.
			vals := strings.Split(val, ",")
			if !stringsContains(vals, "None") {
				// TODO: respond with Reject message, rather
				// than terminating TCP connection.
				return fmt.Errorf("client requesting AuthMethod:%s, only support None", val)
			}
			conn.loginParam.tgtNSG = LoginOperationalNegotiation
			conn.loginParam.tgtTrans = true
			conn.loginParam.authMethod = AuthNone
		} else if key == "TargetName" {
			conn.loginParam.target = val
		} else if key == "InitiatorName" {
			conn.loginParam.initiator = val
		}
	}

	return nil
}

func (conn *iscsiConnection) processLoginData() ([]util.KeyValue, error) {
	var (
		uintVal    uint
		ok         bool
		defSessKey *iscsiSessionKeys
		negoKV     []util.KeyValue
		kvChanges  int
	)
	loginKV := util.ParseKVText(conn.req.RawData)

	for key, val := range loginKV {
		// The MaxRecvDataSegmentLength of initiator
		// is the MaxXmitDataSegmentLength of target
		if key == "MaxRecvDataSegmentLength" {
			defSessKey, ok = sessionKeys["MaxXmitDataSegmentLength"]
			uintVal, ok = defSessKey.conv(val)
			conn.loginParam.sessionParam[defSessKey.idx].Value = uintVal
			continue
		}

		if key == "InitiatorName" {
			conn.loginParam.initiator = val
			continue
		} else if key == "InitiatorAlias" {
			conn.loginParam.initiatorAlias = val
			continue
		} else if key == "TargetName" {
			conn.loginParam.target = val
			continue
		} else if key == "SessionType" {
			if val == "Normal" {
				conn.loginParam.sessionType = SESSION_NORMAL
			} else {
				conn.loginParam.sessionType = SESSION_DISCOVERY
			}
			continue
		}

		defSessKey, ok = sessionKeys[key]
		if ok {
			uintVal, ok = defSessKey.conv(val)

			//hack here
			if key == "HeaderDigest" || key == "DataDigest" {
				if uintVal == DIGEST_ALL {
					uintVal = DIGEST_NONE
				}
			}
			if ok {
				if defSessKey.constValue {
					//the Negotiation Key cannot be changed! Uses Target default key
					if uintVal != defSessKey.def {
						kvChanges++
					}
					negoKV = append(negoKV, util.KeyValue{key, defSessKey.inConv(defSessKey.def)})
				} else {
					if (uintVal >= defSessKey.min) && (uintVal <= defSessKey.max) {
						conn.loginParam.sessionParam[defSessKey.idx].Value = uintVal
						negoKV = append(negoKV, util.KeyValue{key, defSessKey.inConv(uintVal)})
					} else {
						// the value out of the acceptable range, Uses target default key
						negoKV = append(negoKV, util.KeyValue{key, defSessKey.inConv(defSessKey.def)})
						kvChanges++
					}
				}
			}
		} else {
			//Unknown Key, reject it
			return negoKV, fmt.Errorf("Unknowen Nego KV [%s:%s]", key, val)
		}
	}

	if kvChanges == 0 {
		if (conn.loginParam.iniNSG == FullFeaturePhase) && conn.loginParam.iniTrans {
			conn.loginParam.tgtNSG = FullFeaturePhase
			conn.loginParam.tgtTrans = true
		} else {
			//Currently, we just reject these kind of cases
			return negoKV, fmt.Errorf("reject CSG=%s,NSG=%s,trans=%t",
				conn.loginParam.iniCSG, conn.loginParam.iniNSG, conn.loginParam.iniTrans)
		}
	} else {
		conn.loginParam.tgtNSG = FullFeaturePhase
		conn.loginParam.tgtTrans = true
	}
	return negoKV, nil
}

type iscsiLoginParam struct {
	paramInit bool

	iniCSG   iSCSILoginStage
	iniNSG   iSCSILoginStage
	iniTrans bool
	iniCont  bool

	tgtCSG   iSCSILoginStage
	tgtNSG   iSCSILoginStage
	tgtTrans bool
	tgtCont  bool

	sessionType  int
	sessionParam ISCSISessionParamList
	keyDeclared  bool

	initiator      string
	initiatorAlias string
	target         string
	targetAlias    string

	tpgt uint16
	isid uint64
	tsih uint16

	authMethod AuthMethod
}

func (m *ISCSICommand) loginRespBytes() []byte {
	// rfc7143 11.13
	buf := &bytes.Buffer{}
	// byte 0
	buf.WriteByte(byte(OpLoginResp))
	var b byte
	if m.Transit {
		b |= 0x80
	}
	if m.Cont {
		b |= 0x40
	}
	b |= byte(m.CSG&0xff) << 2
	b |= byte(m.NSG & 0xff)
	// byte 1
	buf.WriteByte(b)

	b = 0
	buf.WriteByte(b)                                          // version-max
	buf.WriteByte(b)                                          // version-active
	buf.WriteByte(b)                                          // ahsLen
	buf.Write(util.MarshalUint64(uint64(len(m.RawData)))[5:]) // data segment length, no padding
	buf.Write(util.MarshalUint64(m.ISID)[2:])
	buf.Write(util.MarshalUint64(uint64(m.TSIH))[6:])
	buf.Write(util.MarshalUint64(uint64(m.TaskTag))[4:])
	buf.WriteByte(b)
	buf.WriteByte(b)
	buf.WriteByte(b)
	buf.WriteByte(b) // "reserved"
	buf.Write(util.MarshalUint64(uint64(m.StatSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.ExpCmdSN))[4:])
	buf.Write(util.MarshalUint64(uint64(m.MaxCmdSN))[4:])
	buf.WriteByte(byte(m.StatusClass))
	buf.WriteByte(byte(m.StatusDetail))
	buf.WriteByte(b)
	buf.WriteByte(b) // "reserved"
	var bs [8]byte
	buf.Write(bs[:])
	rd := m.RawData
	for len(rd)%4 != 0 {
		rd = append(rd, 0)
	}
	buf.Write(rd)
	return buf.Bytes()
}
