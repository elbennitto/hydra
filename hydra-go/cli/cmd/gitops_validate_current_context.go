package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

// NewClusterValidateCurrentContextCommand creates and returns the validate-current-context subcommand
func NewClusterValidateCurrentContextCommand() *cobra.Command {
	return newClusterValidateCurrentContextCommand(action.ClusterValidateCurrentContext)
}

func newClusterValidateCurrentContextCommand(checkContext func(flags action.ClusterValidateCurrentContextFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ClusterValidateCurrentContextFlags{}

	cmd := &cobra.Command{
		Use:   "validate-current-context <cluster>",
		Short: "Check that the current kubeconfig context matches the expected cluster",
		Long: `Validate that the currently active Kubernetes context (from kubeconfig)
matches the cluster configuration defined in the Hydra context. This is
a safety check to prevent accidental operations on the wrong cluster.

Both the kubeconfig context name and the API server endpoint are verified.`,
		Example: `  # Validate the current context before running cluster operations
  hydra gitops validate-current-context my-cluster --hydra-context /path/to/context`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, result, err := checkContext(flags)
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
