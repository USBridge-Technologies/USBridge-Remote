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

package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/gostor/gotgt/pkg/apiserver"
	"github.com/gostor/gotgt/pkg/config"
	_ "github.com/gostor/gotgt/pkg/port/iscsit"
	"github.com/gostor/gotgt/pkg/scsi"
	_ "github.com/gostor/gotgt/pkg/scsi/backingstore"
)

func newDaemonCommand() *cobra.Command {
	var (
		host               string
		driver             string
		logLevel           string
		blockMultipleHosts bool
		port               int
	)
	var cmd = &cobra.Command{
		Use:   "daemon",
		Short: "Setup a daemon",
		Long:  `Setup the Gotgt's daemon`,
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			host = viper.GetString("host")
			return createDaemon(host, driver, logLevel, blockMultipleHosts, port)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&logLevel, "log", "info", "Log level of SCSI target daemon")
	flags.StringVar(&driver, "driver", "iscsi", "SCSI low level driver")
	flags.IntVar(&port, "port", 3260, "iSCSI default target port")
	flags.BoolVar(&blockMultipleHosts, "block-multiple-hosts", false, "Disable login from multiple hosts")
	return cmd
}

func createDaemon(host, driver, level string, blockMultipleHosts bool, port int) error {
	switch level {
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "panic", "fatal", "error":
		log.SetLevel(log.ErrorLevel)
	default:
		return fmt.Errorf("unknown log level: %v", level)
	}
	config, err := config.Load(config.ConfigDir())
	if err != nil {
		log.Error(err)
		return err
	}

	err = scsi.InitSCSILUMap(config)
	if err != nil {
		log.Error(err)
		return err
	}

	scsiTarget := scsi.NewSCSITargetService()
	targetDriver, err := scsi.NewTargetDriver(driver, scsiTarget)
	if err != nil {
		log.Error(err)
		return err
	}

	for tgtname := range config.ISCSITargets {
		targetDriver.NewTarget(tgtname, config)
	}

	if blockMultipleHosts {
		targetDriver.EnableBlockMultipleHostLogin()
	}

	// comment this to avoid concurrent issue
	// runtime.GOMAXPROCS(runtime.NumCPU())
	// run a service
	go targetDriver.Run(port)

	serverConfig := &apiserver.Config{
		Addrs: []apiserver.Addr{},
	}

	hosts := []string{}
	if host != "" {
		hosts = append(hosts, host)
	}
	for _, protoAddr := range hosts {
		protoAddrParts := strings.SplitN(protoAddr, "://", 2)
		if len(protoAddrParts) != 2 {
			err = fmt.Errorf("bad format %s, expected PROTO://ADDR", protoAddr)
			log.Error(err)
			return err
		}
		serverConfig.Addrs = append(serverConfig.Addrs, apiserver.Addr{Proto: protoAddrParts[0], Addr: protoAddrParts[1]})
	}

	s, err := apiserver.New(serverConfig)
	if err != nil {
		log.Error(err)
		return err
	}
	s.InitRouters()
	// The serve API routine never exits unless an error occurs
	// We need to start it as a goroutine and wait on it so
	// daemon doesn't exit
	serveAPIWait := make(chan error)
	go s.Wait(serveAPIWait)

	stopAll := make(chan os.Signal, 1)
	signal.Notify(stopAll, syscall.SIGINT, syscall.SIGTERM)

	// Daemon is fully initialized and handling API traffic
	// Wait for serve API job to complete
	select {
	case errAPI := <-serveAPIWait:
		// If we have an error here it is unique to API (as daemonErr would have
		// exited the daemon process above)
		if errAPI != nil {
			log.Warnf("Shutting down due to ServeAPI error: %v", errAPI)
		}
	case <-stopAll:
		break
	}
	s.Close()
	return nil
}
