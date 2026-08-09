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

package backingstore

import (
	"fmt"
	"io"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/scsi"
	"github.com/gostor/gotgt/pkg/util"
)

const (
	FileBackingStorage = "file"
)

func init() {
	scsi.RegisterBackingStore(FileBackingStorage, new)
}

type FileBackingStore struct {
	scsi.BaseBackingStore
	file *os.File
}

func new() (api.BackingStore, error) {
	return &FileBackingStore{
		BaseBackingStore: scsi.BaseBackingStore{
			Name:            FileBackingStorage,
			DataSize:        0,
			OflagsSupported: 0,
		},
	}, nil
}

func (bs *FileBackingStore) Open(dev *api.SCSILu, path string) error {
	var mode os.FileMode

	finfo, err := os.Stat(path)
	if err != nil {
		return err
	} else {
		// determine file type
		mode = finfo.Mode()
	}

	f, err := os.OpenFile(path, os.O_RDWR, os.ModePerm)

	if err == nil {
		// block device filesize needs to be treated differently
		if (mode & os.ModeDevice) != 0 {
			pos, err := f.Seek(0, io.SeekEnd)
			if err != nil {
				return err
			}
			bs.DataSize = uint64(pos)
		} else {
			if finfo == nil {
				log.Infof("finfo is nil")
			}
			bs.DataSize = uint64(finfo.Size())
		}
	}

	bs.file = f
	return err
}

func (bs *FileBackingStore) Close(dev *api.SCSILu) error {
	return bs.file.Close()
}

func (bs *FileBackingStore) Init(dev *api.SCSILu, Opts string) error {
	return nil
}

func (bs *FileBackingStore) Exit(dev *api.SCSILu) error {
	return nil
}

func (bs *FileBackingStore) Size(dev *api.SCSILu) uint64 {
	return bs.DataSize
}

func (bs *FileBackingStore) Read(offset, tl int64) ([]byte, error) {
	if bs.file == nil {
		return nil, fmt.Errorf("Backend store is nil")
	}
	tmpbuf := make([]byte, tl)
	length, err := bs.file.ReadAt(tmpbuf, offset)
	if err != nil {
		return nil, err
	}
	if length != len(tmpbuf) {
		return nil, fmt.Errorf("read is not same length of length")
	}
	return tmpbuf, nil
}

func (bs *FileBackingStore) Write(wbuf []byte, offset int64) error {
	length, err := bs.file.WriteAt(wbuf, offset)
	if err != nil {
		log.Error(err)
		return err
	}
	if length != len(wbuf) {
		return fmt.Errorf("write is not same length of length")
	}
	return nil
}

func (bs *FileBackingStore) DataSync(offset, tl int64) error {
	return util.Fdatasync(bs.file)
}

func (bs *FileBackingStore) DataAdvise(offset, length int64, advise uint32) error {
	return util.Fadvise(bs.file, offset, length, advise)
}

func (bs *FileBackingStore) Unmap([]api.UnmapBlockDescriptor) error {
	return nil
}
