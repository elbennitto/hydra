package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type ClusterCertManagerCommandParams struct {
	ClusterCertManagerRestore func(flags action.ClusterCertManagerFlags) error
}

// NewClusterCertManagerCommand creates and returns the cert-manager command with subcommands
func NewClusterCertManagerCommand(params ClusterCertManagerCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert-manager",
		Short: "Backup and restore cert-manager resources (Certificates, Issuers, etc.)",
		Long: `Backup and restore cert-manager custom resources (Certificates, Issuers,
ClusterIssuers, etc.) for a cluster. This is useful before uninstalling
cert-manager to preserve TLS certificates and their configuration.

The backup is stored as part of the Hydra context and can be restored
after reinstalling cert-manager.`,
	}

	// Add subcommands
	cmd.AddCommand(newClusterCertManagerRestoreCommand(params.ClusterCertManagerRestore))

	return cmd
}

// newClusterCertManagerRestoreCommand restores cert-manager resources to cluster
func newClusterCertManagerRestoreCommand(restore func(flags action.ClusterCertManagerFlags) error) *cobra.Command {
	flags := action.ClusterCertManagerFlags{}

	cmd := &cobra.Command{
		Use:   "restore <cluster>",
		Short: "Restore previously backed-up cert-manager resources to the cluster",
		Long: `Restore cert-manager custom resources from a previous backup in the Hydra
context directory back into the specified cluster.`,
		Example: `  # Restore cert-manager resources
  hydra gitops cert-manager restore my-cluster --hydra-context /path/to/context

  # Restore with dry-run
  hydra gitops cert-manager restore my-cluster --dry-run

  # Overwrite differing cluster secrets from backup
  hydra gitops cert-manager restore my-cluster --force-backup-restore`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.Cluster = types.ClusterName(args[0])
			err := restore(flags)
			if err != nil {
				return err
			}
			return nil
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
