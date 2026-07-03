package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func NewClusterStatusCommand(run func(flags action.ClusterStatusFlags) error) *cobra.Command {
	f := action.ClusterStatusFlags{}

	cmd := &cobra.Command{
		Use:   "status <appId...>",
		Short: "Check whether selected apps are currently in sync",
		Long: `Compute a per-app in sync / out of sync result from rendered desired state
versus tracked live cluster resources.

When multiple apps are selected, repeated --exclude-app patterns are subtracted
from the target set first. All effective app IDs must belong to the same
cluster.`,
		Example: `  # Check one app
  hydra gitops status prod.apps.api

  # Check all prod apps except one excluded app
  hydra gitops status prod.** --exclude-app prod.infra.argocd

  # Check the same cluster with offline Helm dependency handling
  hydra gitops status prod.apps.* --helm-network-mode offline`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return run(f)
		},
	}

	DefineFlags(cmd, &f)
	return cmd
}
