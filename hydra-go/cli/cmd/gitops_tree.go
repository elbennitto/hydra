package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func NewClusterTreeCommand(run func(f action.ClusterTreeFlags) error) *cobra.Command {
	f := action.ClusterTreeFlags{}

	cmd := &cobra.Command{
		Use:   "inspect <cluster> [id]",
		Short: "Interactive TUI to browse reference edges (templates + live cluster)",
		Long: `Connect to the Kubernetes API, merge Hydra ref-parsers from live ConfigMaps,
materialize clone predictions for CEL options, then open a full-screen UI. With an
explicit id, show that resource's reference graph. With only the cluster name, pick
an id from templates plus live objects; press / to open the filter popup and Tab to switch
from the query input to the field dropdown.

Validate your kubeconfig context first with hydra gitops validate-current-context.`,
		Example: `  hydra gitops validate-current-context prod
  hydra gitops inspect prod --hydra-context /path/to/context
  hydra gitops inspect prod apps/v1/Deployment/my-ns/my-deploy --hydra-context /path/to/context`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				f.ResourceId = types.Id(args[1])
			}
			return run(f)
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}
