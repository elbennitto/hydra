package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	hflags "hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

// clusterTreeSubcommandEnabled registers `hydra gitops inspect` when true.
// Large clusters can still be slow; the action loads template + cluster data once per run.
const clusterTreeSubcommandEnabled = true

type ClusterCommandParams struct {
	ClusterValidateCurrentContext func(flags action.ClusterValidateCurrentContextFlags) (hydra.Hydra, string, error)
	ClusterDump                   func(flags action.ClusterDumpFlags) (hydra.Hydra, string, error)
	ClusterList                   func(flags action.ClusterListFlags) (hydra.Hydra, string, error)
	ClusterRefs                   func(flags action.ClusterRefsFlags) error
	ClusterApply                  func(flags action.ClusterApplyFlags) error
	ClusterDiff                   func(flags action.ClusterDiffFlags) error
	ClusterStatus                 func(flags action.ClusterStatusFlags) error
	ClusterReviewRefs             func(flags action.ReviewRefsFlags) error
	ClusterUninstall              func(flags action.ClusterUninstallFlags) error
	ClusterCertManagerRestore     func(flags action.ClusterCertManagerFlags) error
	ClusterBackupCreate           func(flags action.ClusterBackupCreateFlags) error
	ClusterBackupRestore          func(flags action.ClusterBackupRestoreFlags) error
	ClusterBackupList             func(flags action.ClusterBackupListFlags) error
	ClusterBackupDiff             func(flags action.ClusterBackupDiffFlags) error
	ClusterSyncStatus             func(flags action.ClusterSyncStatusFlags) error
	ClusterSyncAuto               func(flags action.ClusterSyncSetFlags) error
	ClusterSyncManual             func(flags action.ClusterSyncSetFlags) error
	ClusterSyncPrevent            func(flags action.ClusterSyncSetFlags) error
	ClusterScaleUp                func(flags action.ClusterScaleFlags) error
	ClusterScaleDown              func(flags action.ClusterScaleFlags) error
	ClusterScaleStatus            func(flags action.ClusterScaleStatusFlags) error
	ClusterTree                   func(flags action.ClusterTreeFlags) error
	ClusterSystem                 func(flags action.ClusterSystemFlags) (hydra.Hydra, string, error)
	ClusterShow                   func(flags action.ClusterShowFlags) (hydra.Hydra, string, error)
	ClusterUntracked              func(flags action.ClusterUntrackedFlags) (hydra.Hydra, string, error)
	ClusterTemplate               func(flags action.ClusterTemplateFlags) error
	ClusterValues                 func(flags action.ClusterValuesFlags) (hydra.Hydra, string, error)
}

func NewClusterCommandParams() ClusterCommandParams {
	return ClusterCommandParams{
		ClusterValidateCurrentContext: action.ClusterValidateCurrentContext,
		ClusterDump:                   action.ClusterDump,
		ClusterList:                   action.ClusterList,
		ClusterRefs:                   action.ClusterRefs,
		ClusterApply:                  action.ClusterApply,
		ClusterDiff:                   action.ClusterDiff,
		ClusterStatus:                 action.ClusterStatus,
		ClusterReviewRefs:             action.ClusterReviewRefs,
		ClusterUninstall:              action.ClusterUninstall,
		ClusterCertManagerRestore:     action.ClusterCertManagerRestore,
		ClusterBackupCreate:           action.ClusterBackupCreate,
		ClusterBackupRestore:          action.ClusterBackupRestore,
		ClusterBackupList:             action.ClusterBackupList,
		ClusterBackupDiff:             action.ClusterBackupDiff,
		ClusterSyncStatus:             action.ClusterSyncStatus,
		ClusterSyncAuto:               action.ClusterSyncAuto,
		ClusterSyncManual:             action.ClusterSyncManual,
		ClusterSyncPrevent:            action.ClusterSyncPrevent,
		ClusterScaleUp:                action.ClusterScaleUp,
		ClusterScaleDown:              action.ClusterScaleDown,
		ClusterScaleStatus:            action.ClusterScaleStatus,
		ClusterTree:                   action.ClusterTree,
		ClusterSystem:                 action.ClusterSystem,
		ClusterShow:                   action.ClusterShow,
		ClusterUntracked:              action.ClusterUntracked,
		ClusterTemplate:               action.ClusterTemplate,
		ClusterValues:                 action.ClusterValues,
	}
}

// NewClusterCommand creates and returns the gitops command with subcommands.
func NewClusterCommand(params ClusterCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitops",
		Short: "Commands for GitOps workflows using local definitions and cluster state",
		Long: `Commands for viewing, validating, dumping, and managing resources in
Kubernetes clusters managed by Hydra.

These commands use the local Hydra definitions as the source of truth and,
when needed, connect to the Kubernetes API server to compare or apply the
rendered result against live state.`,
	}

	clusterREST := &hflags.ClusterRESTClientFlags{}
	cmd.PersistentFlags().Float32Var(&clusterREST.APIClientQPS, "qps", 0,
		"Kubernetes REST client QPS limit for this command (0 = client-go default ~5; negative disables client-side throttling)")
	cmd.PersistentFlags().IntVar(&clusterREST.APIClientBurst, "api-burst", 0,
		"Kubernetes REST client burst when --qps is positive (0 = client-go default burst; requires --qps)")

	// Add subcommands
	cmd.AddCommand(newClusterValidateCurrentContextCommand(params.ClusterValidateCurrentContext))
	cmd.AddCommand(NewClusterAppsCommand(action.ResolveApps))
	cmd.AddCommand(NewClusterDumpCommand(params.ClusterDump))
	cmd.AddCommand(NewClusterListCommand(params.ClusterList))
	cmd.AddCommand(NewClusterRefsCommand(params.ClusterRefs))
	cmd.AddCommand(NewClusterApplyCommand(params.ClusterApply))
	cmd.AddCommand(NewClusterDiffCommand(params.ClusterDiff))
	cmd.AddCommand(NewClusterTemplateCommand(params.ClusterTemplate))
	cmd.AddCommand(NewClusterValuesCommand(params.ClusterValues))
	cmd.AddCommand(NewClusterStatusCommand(params.ClusterStatus))
	cmd.AddCommand(NewClusterReviewCommand(params.ClusterReviewRefs))
	cmd.AddCommand(NewClusterSystemCommand(params.ClusterSystem))
	cmd.AddCommand(NewClusterShowCommand(params.ClusterShow))
	cmd.AddCommand(NewClusterUntrackedCommand(params.ClusterUntracked))
	cmd.AddCommand(NewClusterUninstallCommand(params.ClusterUninstall))
	cmd.AddCommand(NewClusterCertManagerCommand(ClusterCertManagerCommandParams{
		ClusterCertManagerRestore: params.ClusterCertManagerRestore,
	}))
	cmd.AddCommand(NewClusterBackupCommand(ClusterBackupCommandParams{
		ClusterBackupCreate:  params.ClusterBackupCreate,
		ClusterBackupRestore: params.ClusterBackupRestore,
		ClusterBackupList:    params.ClusterBackupList,
		ClusterBackupDiff:    params.ClusterBackupDiff,
	}))
	cmd.AddCommand(NewClusterSyncCommand(ClusterSyncCommandParams{
		ClusterSyncStatus:  params.ClusterSyncStatus,
		ClusterSyncAuto:    params.ClusterSyncAuto,
		ClusterSyncManual:  params.ClusterSyncManual,
		ClusterSyncPrevent: params.ClusterSyncPrevent,
	}))
	cmd.AddCommand(NewClusterScaleCommand(ClusterScaleCommandParams{
		ClusterScaleUp:     params.ClusterScaleUp,
		ClusterScaleDown:   params.ClusterScaleDown,
		ClusterScaleStatus: params.ClusterScaleStatus,
	}))
	if clusterTreeSubcommandEnabled {
		cmd.AddCommand(NewClusterTreeCommand(params.ClusterTree))
	}

	return cmd
}
