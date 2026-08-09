/*
Copyright 2016 openebs authors All rights reserved.

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

package remote

import (
	"fmt"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/scsi"
	log "github.com/sirupsen/logrus"
)

var (
	Size uint64
)

func init() {
	scsi.RegisterBackingStore("RemBs", NewRemoteBackingStore)
}

// RemBackingStore
type RemBackingStore struct {
	scsi.BaseBackingStore
	// Remote backing store, remote server exposing
	// read and write methods.
	RemBs api.RemoteBackingStore
}

func NewRemoteBackingStore() (api.BackingStore, error) {
	return &RemBackingStore{
		BaseBackingStore: scsi.BaseBackingStore{
			Name:            "RemBs",
			OflagsSupported: 0,
		},
	}, nil
}

func (bs *RemBackingStore) Open(dev *api.SCSILu, path string) error {
	if Size == 0 {
		return fmt.Errorf("Size is not initialized")
	}
	var err error
	bs.DataSize = Size
	bs.RemBs, err = scsi.GetTargetBSMap(path)
	if err != nil {
		return err
	}
	return nil
}

func (bs *RemBackingStore) Close(dev *api.SCSILu) error {
	/* TODO return bs.File.Close()*/
	return nil
}

func (bs *RemBackingStore) Init(dev *api.SCSILu, Opts string) error {
	return nil
}

func (bs *RemBackingStore) Exit(dev *api.SCSILu) error {
	return nil
}

func (bs *RemBackingStore) Size(dev *api.SCSILu) uint64 {
	return bs.DataSize
}

func (bs *RemBackingStore) Read(offset, tl int64) ([]byte, error) {
	tmpbuf := make([]byte, tl)
	length, err := bs.RemBs.ReadAt(tmpbuf, offset)
	if err != nil {
		return nil, err
	}
	if length != len(tmpbuf) {
		return nil, fmt.Errorf("Incomplete read expected:%d actual:%d", tl, length)
	}
	return tmpbuf, nil
}

func (bs *RemBackingStore) Write(wbuf []byte, offset int64) error {
	length, err := bs.RemBs.WriteAt(wbuf, offset)
	if err != nil {
		log.Error(err)
		return err
	}
	if length != len(wbuf) {
		return fmt.Errorf("Incomplete write expected:%d actual:%d", len(wbuf), length)
	}
	return nil
}

func (bs *RemBackingStore) DataAdvise(offset, length int64, advise uint32) error {
	return nil
}

func (bs *RemBackingStore) DataSync(offset, length int64) (err error) {
	_, err = bs.RemBs.Sync()
	return
}

func (bs *RemBackingStore) Unmap(bd []api.UnmapBlockDescriptor) (err error) {
	//_, err = bs.RemBs.Unmap(int64(bd[0].Offset), int64(bd[0].TL))
	return
}
