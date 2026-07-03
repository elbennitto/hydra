package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func NewClusterRefsCommand(run func(f action.ClusterRefsFlags) error) *cobra.Command {
	f := action.ClusterRefsFlags{}

	cmd := &cobra.Command{
		Use:   "refs <cluster> <id>",
		Short: "List transitive references for a resource id",
		Long: `Connect to the Kubernetes API, merge rendered template refs with live cluster refs,
then print transitive incoming and outgoing reachability for the given Hydra resource id.

The id uses Hydra's canonical form: group/version/kind/namespace/name, or for
core resources version/kind/namespace/name (four slash-separated segments).`,
		Example: `  hydra gitops validate-current-context prod
  hydra gitops refs prod apps/v1/Deployment/demo/consumer --hydra-context /path/to/context
  hydra gitops refs prod v1/ConfigMap/demo/shared-config --helm-network-mode local`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			f.ResourceId = types.Id(args[1])
			return run(f)
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}
