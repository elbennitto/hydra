package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/base/buildinfo"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the hydra version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(buildinfo.CLIString())
			return nil
		},
	}
}
