package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

// NewClusterValuesCommand creates hydra gitops values.
func NewClusterValuesCommand(clusterValues func(flags action.ClusterValuesFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ClusterValuesFlags{}

	cmd := &cobra.Command{
		Use:   "values <appId>",
		Short: "Show merged Helm values including Hydra ConfigMaps from all cluster apps",
		Long: `Compute and display the merged Helm values for a single application, then replace
global.hydra with the effective configuration produced by the same merge as other cluster-aware
commands: Helm values.yaml global.hydra deep-merged with every data.hydra document from Hydra
ConfigMaps discovered in a full-cluster Helm render (chart-owned ConfigMaps from other apps
included). Inferred ownerNamespaces are applied the same way as hydra local config.

This command does not connect to the Kubernetes API; it only renders Helm templates locally to
extract ConfigMap payloads.`,
		Example: `  hydra gitops values prod.infra.cert-manager --hydra-context /path/to/context

  hydra gitops values prod.apps.my-service --helm-network-mode offline`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, result, err := clusterValues(flags)
			if err != nil {
				return err
			}
			if err := writeCommandResult(result); err != nil {
				return err
			}
			return nil
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
