package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
)

type ArgocdCommandParams struct {
	ArgocdStatus      func(flags action.ArgocdStatusFlags) error
	ArgocdSyncAuto    func(flags action.ArgocdSyncSetFlags) error
	ArgocdSyncManual  func(flags action.ArgocdSyncSetFlags) error
	ArgocdSyncPrevent func(flags action.ArgocdSyncSetFlags) error
}

func NewArgocdCommandParams() ArgocdCommandParams {
	return ArgocdCommandParams{
		ArgocdStatus:      action.ArgocdStatus,
		ArgocdSyncAuto:    action.ArgocdSyncAuto,
		ArgocdSyncManual:  action.ArgocdSyncManual,
		ArgocdSyncPrevent: action.ArgocdSyncPrevent,
	}
}

func NewArgocdCommand(params ArgocdCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "argocd",
		Short: "Read ArgoCD application status and manage sync",
		Long: `Read the real ArgoCD-reported application status and manage sync
through AppProject resources.

Use hydra argocd status for the read-only ArgoCD-facing view.
Use hydra argocd sync for the canonical sync mutation commands.`,
		Example: `  # Show the ArgoCD-reported sync state for all visible applications
  hydra argocd status

  # Show ArgoCD status for selected apps only
  hydra argocd status prod.** --exclude-app prod.infra.argocd

  # Freeze reconciliation during maintenance
  hydra argocd sync prevent prod.apps.*

  # Allow only manual reconciliation for a selected set
  hydra argocd sync manual prod.** --exclude-app prod.infra.argocd

  # Re-enable automatic reconciliation afterwards
  hydra argocd sync auto prod.apps.*`,
	}

	cmd.AddCommand(newArgocdStatusCommand(params.ArgocdStatus))
	cmd.AddCommand(newArgocdSyncCommand(params))
	return cmd
}

func newArgocdStatusCommand(run func(flags action.ArgocdStatusFlags) error) *cobra.Command {
	f := action.ArgocdStatusFlags{}

	cmd := &cobra.Command{
		Use:   "status [appId...]",
		Short: "Show the real ArgoCD-reported sync status",
		Long: `Read-only status command that shows the real application sync state
reported by ArgoCD.

Without app IDs, the command shows all visible applications on the selected
cluster. When multiple apps are selected, repeated --exclude-app patterns are
subtracted from the visible result set.`,
		Example: `  # Show status for all visible applications
  hydra argocd status

  # Show status for selected apps
  hydra argocd status prod.apps.api prod.apps.worker

  # Show status for all prod applications except one excluded app
  hydra argocd status prod.** --exclude-app prod.apps.worker`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIds = args
			return run(f)
		},
	}

	DefineFlags(cmd, &f)
	return cmd
}

func newArgocdSyncCommand(params ArgocdCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manage ArgoCD AppProject sync",
		Long: `Canonical mutation command family for ArgoCD AppProject sync.

These subcommands update AppProject sync so ArgoCD may reconcile the
selected applications automatically, manually, or not at all.`,
		Example: `  # Allow automatic reconciliation
  hydra argocd sync auto prod.apps.*

  # Allow only manual sync, minus one excluded app
  hydra argocd sync manual prod.** --exclude-app prod.infra.argocd

  # Block all reconciliation for a maintenance window
  hydra argocd sync prevent prod.apps.*`,
	}

	cmd.AddCommand(newArgocdSyncLeafCommand("auto", "Enable automatic reconciliation", params.ArgocdSyncAuto))
	cmd.AddCommand(newArgocdSyncLeafCommand("manual", "Allow only manual reconciliation", params.ArgocdSyncManual))
	cmd.AddCommand(newArgocdSyncLeafCommand("prevent", "Block all reconciliation", params.ArgocdSyncPrevent))
	return cmd
}

func newArgocdSyncLeafCommand(use, short string, run func(flags action.ArgocdSyncSetFlags) error) *cobra.Command {
	f := action.ArgocdSyncSetFlags{}

	cmd := &cobra.Command{
		Use:   use + " <appId> [appId...]",
		Short: short,
		Long: `Update AppProject sync for the selected applications.

Repeated --exclude-app patterns are subtracted from the resolved target set
before any sync mutation is applied.`,
		Example: "  hydra argocd sync " + use + " prod.apps.* --exclude-app prod.apps.worker",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIds = args
			return run(f)
		},
	}

	DefineFlags(cmd, &f)
	return cmd
}
