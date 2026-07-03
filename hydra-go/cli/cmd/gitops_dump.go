package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

func NewClusterDumpCommand(dump func(flags action.ClusterDumpFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ClusterDumpFlags{}

	cmd := &cobra.Command{
		Use:   "dump <cluster>",
		Short: "Fetch and display live Kubernetes resources from a cluster",
		Long: `Fetch all Kubernetes resources managed by Hydra from the specified cluster
and print them to stdout as YAML. This connects to the live cluster and shows the
actual state of resources (not the rendered templates). Output is a multi-document
YAML stream (documents separated by ---), each with the Hydra resource id as a comment.

Resources can be filtered using CEL expressions via --include and --exclude.`,
		Example: `  # Dump all resources from a cluster
  hydra gitops dump my-cluster --hydra-context /path/to/context

  # Dump only Secrets
  hydra gitops dump my-cluster --include 'entity.kind=="Secret"'

  # Dump everything except ConfigMaps
  hydra gitops dump my-cluster --exclude 'entity.kind=="ConfigMap"'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			_, _, err := dump(flags)
			return err
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
