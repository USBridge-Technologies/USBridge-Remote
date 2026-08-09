// Throwaway Phase-0 spike: prove gostor/gotgt can serve a flat-file LUN
// over iSCSI on loopback. Not part of the product build — delete after
// the spike is validated. Run with:
//
//	go run ./tools/iscsi_spike <path-to-test-image> [iqn] [host:port]
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gostor/gotgt/pkg/config"
	"github.com/gostor/gotgt/pkg/scsi"

	_ "github.com/gostor/gotgt/pkg/port/iscsit"
	_ "github.com/gostor/gotgt/pkg/scsi/backingstore"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: iscsi_spike <path-to-test-image> [iqn] [host:port]")
		os.Exit(1)
	}
	imgPath := os.Args[1]
	iqn := "iqn.2026-01.com.usbridge.spike:target0"
	if len(os.Args) > 2 {
		iqn = os.Args[2]
	}
	portal := "127.0.0.1:3260"
	if len(os.Args) > 3 {
		portal = os.Args[3]
	}
	port := 3260
	if idx := strings.LastIndex(portal, ":"); idx >= 0 {
		if p, err := strconv.Atoi(portal[idx+1:]); err == nil {
			port = p
		}
	}

	cfg := &config.Config{
		Storages: []config.BackendStorage{
			{
				DeviceID: 1000,
				Path:     "file:" + imgPath,
				Online:   true,
			},
		},
		ISCSIPortals: []config.ISCSIPortalInfo{
			{ID: 0, Portal: portal},
		},
		ISCSITargets: map[string]config.ISCSITarget{
			iqn: {
				TPGTs: map[string][]uint64{"1": {0}},
				LUNs:  map[string]uint64{"0": 1000},
			},
		},
	}

	if err := scsi.InitSCSILUMap(cfg); err != nil {
		fmt.Println("InitSCSILUMap error:", err)
		os.Exit(1)
	}

	target := scsi.NewSCSITargetService()
	driver, err := scsi.NewTargetDriver("iscsi", target)
	if err != nil {
		fmt.Println("NewTargetDriver error:", err)
		os.Exit(1)
	}

	for tgtname := range cfg.ISCSITargets {
		if err := driver.NewTarget(tgtname, cfg); err != nil {
			fmt.Println("NewTarget error:", err)
			os.Exit(1)
		}
	}

	fmt.Printf("gotgt target %q listening on %s, serving %s\n", iqn, portal, imgPath)
	driver.Run(port)
}
