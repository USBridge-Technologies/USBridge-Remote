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
	"net/http"
	"os"
	"strings"

	"github.com/docker/go-connections/sockets"
	"github.com/gostor/gotgt/pkg/api/client"
	"github.com/spf13/cobra"

	"github.com/gostor/gotgt/pkg/version"
)

func NewCommand() *cobra.Command {
	var cli *client.Client
	var host string = "tcp://127.0.0.1:23457"
	var cmd = &cobra.Command{
		Use:   "gotgt",
		Short: "Gotgt is a very fast and stable SCSI target framework",
		Long:  ``,
		RunE: func(cmd *cobra.Command, args []string) error {
			httpClient, err := newHTTPClient(host)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v", err)
				return err
			}

			cli, err = client.NewClient(host, version.Version, httpClient, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v", err)
				return err
			}
			// Do Stuff Here
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&host, "host", host, "Endpoint for SCSI target daemon")
	cmd.AddCommand(
		newDaemonCommand(),
		newCreateCommand(cli),
		newRemoveCommand(cli),
		newListCommand(cli),
		newVersionCommand(cli),
	)
	return cmd
}

func newHTTPClient(host string) (*http.Client, error) {
	tr := &http.Transport{
		TLSClientConfig: nil,
	}
	proto, addr, _, err := client.ParseHost(host)
	if err != nil {
		return nil, err
	}

	sockets.ConfigureTransport(tr, proto, addr)

	return &http.Client{
		Transport: tr,
	}, nil
}

// NoArgs validate args and returns an error if there are any args
func NoArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}

	if cmd.HasSubCommands() {
		return fmt.Errorf("\n" + strings.TrimRight(cmd.UsageString(), "\n"))
	}

	return fmt.Errorf(
		"\"%s\" accepts no argument(s).\n",
		cmd.CommandPath(),
	)
}
