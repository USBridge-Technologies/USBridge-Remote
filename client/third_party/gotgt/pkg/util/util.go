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

// Package util provides some basic util functions.
package util

import (
	"encoding/binary"
)

type KeyValue struct {
	Key   string
	Value string
}

func GetUnalignedUint16(u8 []uint8) uint16 {
	return binary.BigEndian.Uint16(u8)
}

func GetUnalignedUint32(u8 []uint8) uint32 {
	return binary.BigEndian.Uint32(u8)
}

func GetUnalignedUint64(u8 []uint8) uint64 {
	return binary.BigEndian.Uint64(u8)
}

// ParseKVText parses iSCSI key value data.
func ParseKVText(txt []byte) map[string]string {
	m := make(map[string]string)
	var kv, sep int
	var key string
	for i := 0; i < len(txt); i++ {
		if txt[i] == '=' {
			if key == "" {
				sep = i
				key = string(txt[kv:sep])
			}
			continue
		}
		if txt[i] == 0 && key != "" {
			m[key] = string(txt[sep+1 : i])
			key = ""
			kv = i + 1
		}
	}
	return m
}

func MarshalKVText(kv []KeyValue) []byte {
	var data []byte
	for _, v := range kv {
		data = append(data, []byte(v.Key)...)
		data = append(data, '=')
		data = append(data, []byte(v.Value)...)
		data = append(data, 0)
	}
	return data
}

func MarshalUint16(i uint16) []byte {
	var data []byte
	for j := 8; j >= 0; j -= 8 {
		b := byte(i >> uint16(j))
		data = append(data, b)
	}
	return data
}

func MarshalUint32(i uint32) []byte {
	var data []byte
	for j := 24; j >= 0; j -= 8 {
		b := byte(i >> uint32(j))
		data = append(data, b)
	}
	return data
}

func MarshalUint64(v uint64) []byte {
	var data = [8]byte{}
	var i = 0
	for j := 56; j >= 0; j -= 8 {
		data[i] = byte(v >> uint32(j))
		i++
	}
	return data[0:8]
}

func StringToByte(str string, align int, maxlength int) []byte {
	var (
		data   []byte
		data2  []byte
		length int
		d      int
	)

	data = []byte(str)
	length = len(data)
	d = align - (length % align)

	if (length + d) > maxlength {
		data = ([]byte(str))[0:maxlength]
		return data
	} else {
		data2 = make([]byte, length+d)
		copy(data2, data)
		return data2
	}
}

const (
	POSIX_FADV_NORMAL = iota
	POSIX_FADV_RANDOM
	POSIX_FADV_SEQUENTIAL
	POSIX_FADV_WILLNEED
	POSIX_FADV_DONTNEED
	POSIX_FADV_NOREUSE
)
