//go:build ceph
// +build ceph

/*
Copyright 2018 The GoStor Authors All rights reserved.

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
package cephstore

import (
	"fmt"
	"strings"

	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
	log "github.com/sirupsen/logrus"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/scsi"
)

// This ceph-rbd plugin is only for linux
// path format ceph-rbd:poolname/imagename
const (
	CephBackingStorage = "ceph-rbd"
)

func init() {
	scsi.RegisterBackingStore(CephBackingStorage, newCeph)
}

type CephBackingStore struct {
	scsi.BaseBackingStore
	poolName  string
	imageName string
	conn      *rados.Conn
	ioctx     *rados.IOContext
	image     *rbd.Image
}

func newCeph() (api.BackingStore, error) {
	return &CephBackingStore{
		BaseBackingStore: scsi.BaseBackingStore{
			Name:            CephBackingStorage,
			DataSize:        0,
			OflagsSupported: 0,
		},
	}, nil
}

func (bs *CephBackingStore) Open(dev *api.SCSILu, path string) error {

	pathinfo := strings.SplitN(path, "/", 2)
	if len(pathinfo) != 2 {
		return fmt.Errorf("invalid device path string:%s", path)
	}
	poolName := pathinfo[0]
	imageName := pathinfo[1]
	log.Debugf("ceph path = %s", path)
	if conn, err := rados.NewConn(); err != nil {
		log.Error(err)
		return err
	} else {
		bs.conn = conn
	}
	if err := bs.conn.ReadDefaultConfigFile(); err != nil {
		log.Error(err)
		return err
	}

	if err := bs.conn.Connect(); err != nil {
		log.Error(err)
		return err
	}

	if ioctx, err := bs.conn.OpenIOContext(poolName); err != nil {
		bs.conn.Shutdown()
		log.Error(err)
		return err
	} else {
		bs.ioctx = ioctx
	}

	if image := rbd.GetImage(bs.ioctx, imageName); image == nil {
		err := fmt.Errorf("rbdGetImage failed:poolName:%s,imageName:%s",
			poolName, imageName)
		log.Error(err)
	} else {
		bs.image = image
	}

	if err := bs.image.Open(); err != nil {
		log.Error(err)
		return err
	}

	if dataSize, err := bs.image.GetSize(); err != nil {
		log.Error(err)
		return err
	} else {
		bs.DataSize = dataSize
	}
	return nil
}

func (bs *CephBackingStore) Close(dev *api.SCSILu) error {
	err := bs.image.Close()
	bs.ioctx.Destroy()
	bs.conn.Shutdown()
	return err
}

func (bs *CephBackingStore) Init(dev *api.SCSILu, Opts string) error {
	return nil
}

func (bs *CephBackingStore) Exit(dev *api.SCSILu) error {
	return nil
}

func (bs *CephBackingStore) Size(dev *api.SCSILu) uint64 {
	return bs.DataSize
}

func (bs *CephBackingStore) Read(offset, tl int64) ([]byte, error) {
	tmpbuf := make([]byte, tl)
	_, err := bs.image.ReadAt(tmpbuf, offset)
	return tmpbuf, err
}

func (bs *CephBackingStore) Write(wbuf []byte, offset int64) error {
	_, err := bs.image.WriteAt(wbuf, offset)
	return err
}

func (bs *CephBackingStore) DataSync(offset, tl int64) error {
	err := bs.image.Flush()
	return err
}

func (bs *CephBackingStore) DataAdvise(offset, length int64, advise uint32) error {
	return nil
}

func (bs *CephBackingStore) Unmap([]api.UnmapBlockDescriptor) error {
	return nil
}
