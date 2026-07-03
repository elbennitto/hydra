package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// NewClusterTemplateCommand creates hydra gitops template.
func NewClusterTemplateCommand(clusterTemplate func(flags action.ClusterTemplateFlags) error) *cobra.Command {
	f := action.ClusterTemplateFlags{}

	cmd := &cobra.Command{
		Use:   "template <appId> [appId...]",
		Short: "Render Helm templates with cluster-normalized API versions and full-cluster templatePatches",
		Long: `Render the Helm templates for the specified app(s) and print Kubernetes manifests to stdout,
similar to hydra local template, but this command contacts the Kubernetes API server to discover
preferred API versions (so apiVersion fields match what the apiserver prefers) and merges
global.hydra.templatePatches from Helm values plus Hydra ConfigMap data.hydra fragments from every
app on the cluster before applying them to each selected app's rendered set.

App IDs support glob-style wildcard matching:
  *            matches any characters except '.' (stays within one segment)
  **           matches any characters including '.' (crosses segments)

Use --exclude-app to remove specific apps from the selection.

Use --include and --exclude (CEL) to print only matching rendered resources, same as hydra local template.`,
		Example: `  hydra gitops template prod.infra.monitoring --hydra-context /path/to/context

  hydra gitops template prod.infra.monitoring prod.infra.logging

  hydra gitops template prod.*.* --exclude-app prod.cluster-infra.cert-manager

  hydra gitops template prod.apps.my-service --include 'kind == "Deployment"'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return clusterTemplate(f)
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}
