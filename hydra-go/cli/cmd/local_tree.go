package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func newLocalTreeCommand(localTree func(f action.LocalTreeFlags) error) *cobra.Command {
	flags := action.LocalTreeFlags{}

	cmd := &cobra.Command{
		Use:   "inspect <cluster> [id]",
		Short: "Interactive TUI to browse reference edges for a resource (local templates)",
		Long: `Render all effectively enabled apps on the cluster. With an explicit resource id,
open the reference graph for that id. With only the cluster name, choose an id from
all template-known resources; press / to open the filter popup, type to narrow
the list, use Tab to switch to the field dropdown, then Enter to open the graph.

In the graph: arrow keys move, / opens the filter popup, s changes the sort column, Enter follows an edge, Escape goes back, q quits.`,
		Example: `  hydra local inspect prod --hydra-context /path/to/context
  hydra local inspect prod v1/ConfigMap/my-ns/my-cm --hydra-context /path/to/context
  hydra local inspect prod apps/v1/Deployment/my-ns/my-deploy --helm-network-mode offline`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName, err := types.NewClusterName(args[0])
			if err != nil {
				return err
			}
			flags.Cluster = clusterName
			if len(args) > 1 {
				flags.ResourceId = types.Id(args[1])
			}
			return localTree(flags)
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
