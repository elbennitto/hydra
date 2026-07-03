package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type ClusterBackupCommandParams struct {
	ClusterBackupCreate  func(flags action.ClusterBackupCreateFlags) error
	ClusterBackupRestore func(flags action.ClusterBackupRestoreFlags) error
	ClusterBackupList    func(flags action.ClusterBackupListFlags) error
	ClusterBackupDiff    func(flags action.ClusterBackupDiffFlags) error
}

// NewClusterBackupCommand creates and returns the backup command with subcommands.
func NewClusterBackupCommand(params ClusterBackupCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup and restore Kubernetes secrets managed by Hydra apps",
		Long: `Create and restore per-app backups of Kubernetes secrets. Secrets are
stored as SOPS-encrypted SopsSecret CRDs in the GitOps repository.

Each app can define which secrets to backup using ref-parsers with
tag: [backup] in its Helm values (global.hydra.refs).`,
	}

	cmd.AddCommand(newClusterBackupCreateCommand(params.ClusterBackupCreate))
	cmd.AddCommand(newClusterBackupRestoreCommand(params.ClusterBackupRestore))
	cmd.AddCommand(newClusterBackupListCommand(params.ClusterBackupList))
	cmd.AddCommand(newClusterBackupDiffCommand(params.ClusterBackupDiff))

	return cmd
}

func newClusterBackupCreateCommand(create func(flags action.ClusterBackupCreateFlags) error) *cobra.Command {
	f := action.ClusterBackupCreateFlags{}

	cmd := &cobra.Command{
		Use:   "create <appId> [appId...]",
		Short: "Backup secrets from the cluster for the selected apps",
		Long: `Fetch secrets matching backup predicates from the cluster and store them
as SOPS-encrypted SopsSecret CRDs in the app's static manifests directory.

For each secret, the command shows whether it is up-to-date or was newly
backed up. If an existing backup differs from the cluster state, a
hash-diff is displayed showing which fields changed.

Use --include / --exclude to further narrow the selected backup secrets
after the app-defined backup predicates have matched. If a matched secret
targets a namespace that does not belong to the selected app, the command
fails instead of writing a misplaced backup.`,
		Example: `  # Backup secrets for cert-manager
  hydra gitops backup create prod.cluster-infra.cert-manager

  # Backup secrets for multiple apps
  hydra gitops backup create prod.cluster-infra.cert-manager prod.cluster-infra.dex

  # Backup all child apps
  hydra gitops backup create prod.*.*

  # Dry-run to preview what would be backed up
  hydra gitops backup create prod.*.* --dry-run

  # Only back up a specific secret from the matched backup scope
  hydra gitops backup create prod.*.* --include 'id == "v1/Secret/cert-manager/wildcard-tls"'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return create(f)
		},
	}

	DefineFlags(cmd, &f)
	return cmd
}

func newClusterBackupRestoreCommand(restore func(flags action.ClusterBackupRestoreFlags) error) *cobra.Command {
	f := action.ClusterBackupRestoreFlags{}

	cmd := &cobra.Command{
		Use:   "restore <appId> [appId...]",
		Short: "Restore secrets from backup files to the cluster",
		Long: `Restore secrets from SOPS-encrypted SopsSecret backup files. For each
backup, the command checks the current cluster state:

  - If the secret does not exist: restores it
  - If the secret exists and is identical: reports "up-to-date"
  - If the secret exists but differs: reports "would overwrite" and
    shows a hash-diff. Use --force-backup-restore to proceed anyway.

Restore discovers backup inputs only from the selected app IDs.
Use --include / --exclude to further narrow the selected backup secrets.
Use --create-namespaces when selected backups target namespaces that do not
exist in the cluster yet. Backups whose target namespace does not belong to
the selected app are warned about and reported as skipped.`,
		Example: `  # Restore secrets for cert-manager
  hydra gitops backup restore prod.cluster-infra.cert-manager

  # Force-restore even if cluster secrets differ
  hydra gitops backup restore prod.*.* --force-backup-restore

  # Create missing target namespaces for the selected backups first
  hydra gitops backup restore prod.*.* --create-namespaces

  # Dry-run to preview what would be restored
  hydra gitops backup restore prod.*.* --dry-run

  # Restore only a specific secret from the available backups
  hydra gitops backup restore prod.*.* --include 'id == "v1/Secret/cert-manager/wildcard-tls"'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return restore(f)
		},
	}

	DefineFlags(cmd, &f)
	return cmd
}

func newClusterBackupListCommand(list func(flags action.ClusterBackupListFlags) error) *cobra.Command {
	f := action.ClusterBackupListFlags{}

	cmd := &cobra.Command{
		Use:   "list <appId> [appId...]",
		Short: "List backup SopsSecrets found in rendered manifests",
		Long: `Show which backup SopsSecrets (annotated with hydra-gitops.org/hydra-backup: "true")
exist in the rendered manifests. This command does not connect to the
cluster. Use "hydra gitops backup diff" to compare backups with the
cluster state.`,
		Example: `  # List backups for all apps
  hydra gitops backup list prod.*.*

  # List backups for cert-manager
  hydra gitops backup list prod.cluster-infra.cert-manager`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			return list(f)
		},
	}

	DefineFlags(cmd, &f)
	return cmd
}

func newClusterBackupDiffCommand(diff func(flags action.ClusterBackupDiffFlags) error) *cobra.Command {
	f := action.ClusterBackupDiffFlags{}

	cmd := &cobra.Command{
		Use:   "diff <appId> [appId...]",
		Short: "Compare backup secrets with the current cluster state",
		Long: `Decrypt backup SopsSecrets, extract the v1/Secret data, and compare it
with the live cluster secrets. For each backup, the command reports
whether it is up-to-date, differs from the cluster (with a hash-diff),
or is missing from the cluster.`,
		Example: `  # Diff backups for all apps
  hydra gitops backup diff prod.*.*

  # Diff backups for cert-manager
  hydra gitops backup diff prod.cluster-infra.cert-manager`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return diff(f)
		},
	}

	DefineFlags(cmd, &f)
	return cmd
}
