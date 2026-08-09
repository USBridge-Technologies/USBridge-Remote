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
	"text/tabwriter"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/api/client"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

func newListCommand(cli *client.Client) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "list",
		Short: "List object(s)",
		Long:  `All software has versions. This is Gotgt 's`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(cmd.UsageString())
		},
	}
	cmd.AddCommand(
		newListTargetCmd(cli),
		newListLuCmd(cli),
	)
	return cmd
}

func newListTargetCmd(cli *client.Client) *cobra.Command {
	opts := api.TargetListOptions{}
	var cmd = &cobra.Command{
		Use:   "target",
		Short: "List target(s) of gotgt",
		Long:  `All software has versions. This is Gotgt 's`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf(
					"\"%s\" accepts no argument(s).\n",
					cmd.CommandPath(),
				)
			}
			return listTarget(cli, opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Specify target name")
	flags.BoolVar(&opts.Verbose, "verbose", false, "Show more details")

	return cmd

}

func newListLuCmd(cli *client.Client) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "lu",
		Short: "List Lu(s) of gotgt",
		Long:  `All software has versions. This is Gotgt 's`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := NoArgs(cmd, args); err != nil {
				return err
			}
			return listLu(cli)
		},
	}
	flags := cmd.Flags()
	_ = flags
	return cmd
}

func listTarget(cli *client.Client, opts api.TargetListOptions) error {
	results, err := cli.TargetList(context.Background(), opts)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
	fmt.Fprintln(w, "TARGET NAME\tSTATE\tSESSIONS")
	for _, tgt := range results {
		status := "online"
		if tgt.State == api.TargetReady {
			status = "ready"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\n", tgt.Name, status, len(tgt.ITNexus))
	}
	w.Flush()
	return nil
}

func listLu(cli *client.Client) error {
	return nil
}
