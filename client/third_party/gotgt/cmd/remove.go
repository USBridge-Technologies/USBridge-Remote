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

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/api/client"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

func newRemoveCommand(cli *client.Client) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "rm",
		Short: "remove a new object",
		Long:  `All software has versions. This is Gotgt 's`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(cmd.UsageString())
		},
	}
	cmd.AddCommand(
		newRemoveTargetCmd(cli),
		newRemoveLuCmd(cli),
	)
	return cmd
}

func newRemoveTargetCmd(cli *client.Client) *cobra.Command {
	opts := api.TargetRemoveOptions{}
	var cmd = &cobra.Command{
		Use:   "target",
		Short: "Remove a new target into gotgt",
		Long:  `All software has versions. This is Gotgt 's`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return removeTarget(cli, opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Specify target name")
	flags.BoolVar(&opts.Force, "force", false, "Specify target name")

	return cmd

}

func newRemoveLuCmd(cli *client.Client) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "lu",
		Short: "Remove a new Lu into gotgt",
		Long:  `All software has versions. This is Gotgt 's`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return removeLu(cli)
		},
	}
	flags := cmd.Flags()
	_ = flags
	return cmd
}

func removeTarget(cli *client.Client, opts api.TargetRemoveOptions) error {
	err := cli.TargetRemove(context.Background(), opts)
	if err != nil {
		return err
	}
	fmt.Printf("Target %s successfully removed\n", opts.Name)
	return nil
}

func removeLu(cli *client.Client) error {
	return nil
}
