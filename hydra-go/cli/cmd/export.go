package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
)

func newExportCommand(exportContext func(flags action.ClusterViewContextFlags) error) *cobra.Command {
	flags := action.ClusterViewContextFlags{}

	cmd := &cobra.Command{
		Use:   "export <output-dir>",
		Short: "Export dependency model, manifests, and charts for all clusters in the context",
		Long: `Render templates for every cluster in the resolved context and export the
complete resource dependency graph, rendered manifests, and Helm chart archives
to the given output directory. Each cluster gets its own subdirectory.`,
		Example: `  hydra local export ./output --hydra-context /path/to/context`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.OutputDir = args[0]
			return exportContext(flags)
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
