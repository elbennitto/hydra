package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func newLocalRefsCommand(localRefs func(f action.LocalRefsFlags) error) *cobra.Command {
	flags := action.LocalRefsFlags{}

	cmd := &cobra.Command{
		Use:   "refs <cluster> <id>",
		Short: "List transitive references for a resource id",
		Long: `Render all effectively enabled apps on the cluster and print transitive
incoming and outgoing reachability for the given Hydra resource id.

The id uses Hydra's canonical form: group/version/kind/namespace/name, or for
core resources version/kind/namespace/name (four slash-separated segments).`,
		Example: `  hydra local refs prod v1/ConfigMap/demo/shared-config --hydra-context /path/to/context
  hydra local refs prod apps/v1/Deployment/demo/consumer --helm-network-mode local`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName, err := types.NewClusterName(args[0])
			if err != nil {
				return err
			}
			flags.Cluster = clusterName
			flags.ResourceId = types.Id(args[1])
			return localRefs(flags)
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
