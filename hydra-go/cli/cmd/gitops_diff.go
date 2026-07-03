package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// NewClusterDiffCommand creates and returns the diff command
func NewClusterDiffCommand(clusterDiff func(flags action.ClusterDiffFlags) error) *cobra.Command {
	f := action.ClusterDiffFlags{}

	cmd := &cobra.Command{
		Use:   "diff <appId> [appId...]",
		Short: "Show differences between rendered templates and the live cluster state",
		Long: `Compare rendered Helm templates against the resources currently deployed
in the Kubernetes cluster. The output is a unified diff highlighting
additions, modifications, and deletions.

Resources managed by the selected Hydra apps (identified via ArgoCD
tracking IDs) that are no longer present in the rendered templates are
shown as deletions. Controller-managed resources (with ownerReferences)
are excluded from orphan detection.

App IDs support glob-style wildcard matching:
  *            matches any characters except '.' (stays within one segment)
  **           matches any characters including '.' (crosses segments)

Examples:
  prod.*       all root apps in cluster prod
  prod.*.*     all child apps in cluster prod
  prod.**      all apps (root + child) in cluster prod

Use --exclude-app to remove specific apps from the selection.

Use --include and --exclude (CEL expressions, repeatable) to limit which
Kubernetes resources appear in the diff, after merge and orphan detection.

Two diff strategies are available (--diff-mode):

  server (default)  Templates are sent through a server-side apply
                    dry-run before comparison. The API server fills in
                    defaults, producing clean diffs.

  raw               Templates are compared 1:1 against the live cluster
                    state. Faster but may show false positives from
                    server-side defaults.`,
		Example: `  # Show diff for a single app
  hydra gitops diff prod.infra.monitoring --hydra-context /path/to/context

  # Show diff for multiple apps
  hydra gitops diff prod.infra.monitoring prod.infra.logging

  # Show diff for all apps in a cluster
  hydra gitops diff prod.**

  # Show diff excluding specific apps
  hydra gitops diff prod.*.* --exclude-app prod.cluster-infra.cert-manager

  # Diff only Deployments
  hydra gitops diff prod.apps.* --include 'kind == "Deployment"'

  # Raw 1:1 comparison without server-side dry-run
  hydra gitops diff prod.infra.monitoring --diff-mode=raw`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return clusterDiff(f)
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}
