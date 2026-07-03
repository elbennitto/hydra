package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// NewClusterApplyCommand creates and returns the apply command
func NewClusterApplyCommand(clusterApply func(flags action.ClusterApplyFlags) error) *cobra.Command {
	f := action.ClusterApplyFlags{}

	cmd := &cobra.Command{
		Use:   "apply <appId> [appId...]",
		Short: "Apply Hydra-managed resources to a Kubernetes cluster",
		Long: `Render Helm templates and apply the resulting Kubernetes resources to the
cluster. Resources that differ from the live cluster state are updated
using server-side apply.

Resources that are managed by the selected Hydra apps but are no longer
present in the rendered templates are automatically deleted (orphan cleanup).

App IDs support glob-style wildcard matching:
  *            matches any characters except '.' (stays within one segment)
  **           matches any characters including '.' (crosses segments)

Examples:
  prod.*       all root apps in cluster prod
  prod.*.*     all child apps in cluster prod
  prod.**      all apps (root + child) in cluster prod

Use --exclude-app to remove specific apps from the selection.

Use --include and --exclude (CEL, repeatable) to apply only matching rendered resources,
same as hydra gitops diff. Orphan cleanup (deleting cluster objects no longer in templates)
is skipped when resource filters are set. You cannot combine resource filters with
--replace or --orphan-scale-down (including when --orphan-scale-down is implied by --bootstrap;
pass --no-orphan-scale-down or --orphan-scale-down=false to keep --bootstrap while disabling orphan scale-down).`,
		Example: `  # Apply a single app
  hydra gitops apply prod.infra.monitoring --hydra-context /path/to/context

  # Apply multiple apps at once
  hydra gitops apply prod.infra.monitoring prod.infra.logging

  # Apply all apps in a cluster
  hydra gitops apply prod.**

  # Apply all child apps except cert-manager
  hydra gitops apply prod.*.* --exclude-app prod.cluster-infra.cert-manager

  # Dry-run to preview what would change
  hydra gitops apply prod.infra.monitoring --dry-run

  # Apply only Deployments (orphan deletion skipped; do not use with --replace or --orphan-scale-down)
  hydra gitops apply prod.apps.* --include 'kind == "Deployment"'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			resolveClusterApplyImpliedFlags(cmd, &f)
			if err := resolveClusterApplySyncWindow(cmd, &f); err != nil {
				return err
			}
			if err := validateClusterApplyFlags(cmd, &f); err != nil {
				return err
			}
			return clusterApply(f)
		},
	}

	DefineFlags(cmd, &f)
	configureClusterApplyUsage(cmd)
	cmd.MarkFlagsMutuallyExclusive("bootstrap", "skip-bootstrap-guard")
	cmd.MarkFlagsMutuallyExclusive("bootstrap", "skip-ref-checks")
	cmd.MarkFlagsMutuallyExclusive("bootstrap-guard", "skip-bootstrap-guard")
	cmd.MarkFlagsMutuallyExclusive("backup-restore", "skip-backup-restore", "no-backup-restore")
	cmd.MarkFlagsMutuallyExclusive("include", "replace")
	cmd.MarkFlagsMutuallyExclusive("exclude", "replace")
	cmd.MarkFlagsMutuallyExclusive("include", "orphan-scale-down")
	cmd.MarkFlagsMutuallyExclusive("exclude", "orphan-scale-down")

	return cmd
}
