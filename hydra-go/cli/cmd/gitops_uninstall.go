package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// NewClusterUninstallCommand creates and returns the uninstall command
func NewClusterUninstallCommand(clusterUninstall func(flags action.ClusterUninstallFlags) error) *cobra.Command {
	f := action.ClusterUninstallFlags{}

	cmd := &cobra.Command{
		Use:   "uninstall <appId> [appId...]",
		Short: "Remove Hydra-managed resources from a Kubernetes cluster",
		Long: `Remove Hydra-managed Kubernetes resources from a cluster. Before deleting,
the command compares rendered templates against the live cluster state to
determine which resources to remove.

Safety checks are performed to ensure that only resources managed by the
selected Hydra apps are deleted. The operation aborts if unmanaged
resources are found in the target namespaces.

If the app includes cert-manager resources, a backup is automatically
created before uninstallation.

App IDs support glob-style wildcard matching:
  *            matches any characters except '.' (stays within one segment)
  **           matches any characters including '.' (crosses segments)

Examples:
  prod.*       all root apps in cluster prod
  prod.*.*     all child apps in cluster prod
  prod.**      all apps (root + child) in cluster prod
  *.*.*        all child apps across all clusters

Use --exclude-app to remove specific apps from the selection.`,
		Example: `  # Uninstall a single app
  hydra gitops uninstall prod.infra.monitoring --hydra-context /path/to/context

  # Uninstall multiple apps at once
  hydra gitops uninstall prod.infra.monitoring prod.infra.logging

  # Uninstall all child apps in a cluster
  hydra gitops uninstall prod.*.*

  # Uninstall all child apps except one
  hydra gitops uninstall *.*.* --exclude-app prod.cluster-infra.ingress-nginx

  # Dry-run to preview what would be deleted
  hydra gitops uninstall prod.infra.monitoring --dry-run

  # Faster cluster inventory + filter-namespaces phase on large clusters
  hydra gitops uninstall prod.*.* --parallel 8`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return clusterUninstall(f)
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}
