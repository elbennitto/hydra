package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func NewClusterReviewCommand(clusterReviewRefs func(flags action.ReviewRefsFlags) error) *cobra.Command {
	parent := &cobra.Command{
		Use:   "review",
		Short: "Review rendered resource references against live cluster resources",
		Long: `Review commands compare rendered Hydra manifests to live cluster state.

Use "hydra gitops review app" with app id patterns to limit findings to those applications.

Use "hydra gitops review cluster" with a cluster name (no dots) to review every application
defined for that cluster in the repository (after --exclude-app), including ref ownership
findings for live objects that have no Hydra app assignment.`,
	}

	parent.AddCommand(newClusterReviewAppCommand(clusterReviewRefs))
	parent.AddCommand(newClusterReviewClusterCommand(clusterReviewRefs))

	return parent
}

func newClusterReviewAppCommand(clusterReviewRefs func(flags action.ReviewRefsFlags) error) *cobra.Command {
	flags := action.ReviewRefsFlags{}

	cmd := &cobra.Command{
		Use:   "app <appId...>",
		Short: "Review only resources belonging to the selected app ids",
		Long: `Render the selected app(s) locally and review their resource references against live
cluster resources from the same cluster.

Ref ownership findings for live objects that belong to no Hydra app are not reported; use
"hydra gitops review cluster" for that audit.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.AppIdPatterns = types.ToAppIdPatterns(args)
			flags.ClusterReviewClusterName = ""
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			err := clusterReviewRefs(flags)
			cmd.SilenceUsage = !exitcode.IsShowUsage(err)
			return err
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}

func newClusterReviewClusterCommand(clusterReviewRefs func(flags action.ReviewRefsFlags) error) *cobra.Command {
	flags := action.ReviewRefsFlags{}

	cmd := &cobra.Command{
		Use:   "cluster <cluster>",
		Short: "Review all apps in a cluster (optional excludes), including unassigned live resources",
		Long: `Review reference integrity and ref ownership for every application defined for the given
cluster in the Hydra repository, after applying --exclude-app patterns.

The cluster argument must be a single segment (no '.'), matching a directory name under the
Hydra context — not an app id.

This mode reports ref ownership findings for live cluster objects that have no Hydra app
assignment (within the same namespace scope as cluster uninstall), in addition to the usual
reference checks.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.ClusterReviewClusterName = args[0]
			flags.AppIdPatterns = nil
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			err := clusterReviewRefs(flags)
			cmd.SilenceUsage = !exitcode.IsShowUsage(err)
			return err
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
