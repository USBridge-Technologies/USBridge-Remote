package cmd

import (
	"github.com/spf13/cobra"

	"github.com/gostor/gotgt/pkg/api/client"
	"github.com/gostor/gotgt/pkg/version"
)

func newVersionCommand(cli *client.Client) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number of gotgt",
		Long:  `All software has versions. This is Gotgt 's`,
		Run: func(cmd *cobra.Command, args []string) {
			version.PrintVersionAndExit()
		},
	}
	return cmd
}
