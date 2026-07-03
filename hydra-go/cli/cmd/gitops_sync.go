package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
)

type ClusterSyncCommandParams struct {
	ClusterSyncStatus  func(flags action.ClusterSyncStatusFlags) error
	ClusterSyncAuto    func(flags action.ClusterSyncSetFlags) error
	ClusterSyncManual  func(flags action.ClusterSyncSetFlags) error
	ClusterSyncPrevent func(flags action.ClusterSyncSetFlags) error
}

func NewClusterSyncCommand(params ClusterSyncCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manage ArgoCD AppProject sync",
		Long: `View and control ArgoCD sync for applications managed by Hydra.

Sync is configured on AppProject resources and controls whether
ArgoCD is allowed to automatically reconcile applications.

Three states are supported:
  auto     Automatic sync enabled (green in ArgoCD UI)
  manual   Only manual sync allowed (yellow in ArgoCD UI)
  prevent  All sync blocked (red in ArgoCD UI)`,
	}

	cmd.AddCommand(newClusterSyncStatusCommand(params.ClusterSyncStatus))
	cmd.AddCommand(newClusterSyncAutoCommand(params.ClusterSyncAuto))
	cmd.AddCommand(newClusterSyncManualCommand(params.ClusterSyncManual))
	cmd.AddCommand(newClusterSyncPreventCommand(params.ClusterSyncPrevent))

	return cmd
}

func newClusterSyncStatusCommand(syncStatus func(flags action.ClusterSyncStatusFlags) error) *cobra.Command {
	flags := action.ClusterSyncStatusFlags{}

	cmd := &cobra.Command{
		Use:   "status [appId...]",
		Short: "Show AppProject sync and ArgoCD status",
		Long: `List AppProject sync configuration in the argocd
namespace, and show whether the ArgoCD controller is running.

Optional app ID arguments filter which applications are displayed.
Wildcard patterns ending with * match all apps with the given prefix.
The command always connects to the in-cluster ArgoCD instance.`,
		Example: `  # Show sync for all apps
  hydra gitops sync status

  # Show sync for a specific app
  hydra gitops sync status in-cluster.cluster-infra.kyverno

  # Show sync for all apps of a cluster
  hydra gitops sync status in-cluster.*

  # Show sync for multiple apps using wildcards
  hydra gitops sync status in-cluster.cluster-infra.* in-cluster.monitoring.*`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.AppIds = args
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			return syncStatus(flags)
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}

func newClusterSyncAutoCommand(syncAuto func(flags action.ClusterSyncSetFlags) error) *cobra.Command {
	flags := action.ClusterSyncSetFlags{}

	cmd := &cobra.Command{
		Use:   "auto <appId> [appId...]",
		Short: "Enable automatic sync (green)",
		Long: `Enable automatic sync for the specified applications.
ArgoCD will automatically sync the applications when changes are detected.`,
		Example: `  # Enable automatic sync for one app
  hydra gitops sync auto in-cluster.cluster-infra.kyverno

  # Enable automatic sync for multiple apps
  hydra gitops sync auto in-cluster.cluster-infra.kyverno in-cluster.cluster-infra.cert-manager

  # Enable automatic sync for all apps of a cluster using wildcard
  hydra gitops sync auto in-cluster.*`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.AppIds = args
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			return syncAuto(flags)
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}

func newClusterSyncManualCommand(syncManual func(flags action.ClusterSyncSetFlags) error) *cobra.Command {
	flags := action.ClusterSyncSetFlags{}

	cmd := &cobra.Command{
		Use:   "manual <appId> [appId...]",
		Short: "Allow only manual sync (yellow)",
		Long: `Disable automatic sync but allow manual sync for the specified applications.
ArgoCD will not automatically sync, but manual sync remains possible.`,
		Example: `  # Switch to manual sync for one app
  hydra gitops sync manual in-cluster.cluster-infra.kyverno

  # Switch to manual sync for multiple apps
  hydra gitops sync manual in-cluster.cluster-infra.kyverno in-cluster.cluster-infra.cert-manager

  # Switch to manual sync for all apps of a cluster using wildcard
  hydra gitops sync manual in-cluster.*`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.AppIds = args
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			return syncManual(flags)
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}

func newClusterSyncPreventCommand(syncPrevent func(flags action.ClusterSyncSetFlags) error) *cobra.Command {
	flags := action.ClusterSyncSetFlags{}

	cmd := &cobra.Command{
		Use:   "prevent <appId> [appId...]",
		Short: "Block all sync (red)",
		Long: `Block all sync activity for the specified applications.
Neither automatic nor manual sync will be possible.`,
		Example: `  # Block all sync for one app
  hydra gitops sync prevent in-cluster.cluster-infra.kyverno

  # Block all sync for multiple apps
  hydra gitops sync prevent in-cluster.cluster-infra.kyverno in-cluster.cluster-infra.cert-manager

  # Block all sync for all apps of a cluster using wildcard
  hydra gitops sync prevent in-cluster.*`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.AppIds = args
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			return syncPrevent(flags)
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
