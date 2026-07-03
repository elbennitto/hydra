package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

var clusterApplyBootstrapNoFlagNames = []string{
	"no-sops-decode",
	"no-down-scaled",
	"no-scale-up",
	"no-orphan-scale-down",
	"no-bootstrap-guard",
	"no-bootstrap-clones",
	"no-backup-restore",
	"no-disable-webhooks",
}

var clusterApplyPositiveBehaviorFlagNames = []string{
	"sops-decode",
	"down-scaled",
	"scale-up",
	"orphan-scale-down",
	"bootstrap-guard",
	"bootstrap-clones",
	"backup-restore",
	"disable-webhooks",
}

// resolveClusterApplyImpliedFlags turns on optional apply behaviors when --bootstrap is set, unless the user
// opted out with a matching --no-* flag or set the positive flag on the command line.
func resolveClusterApplyImpliedFlags(cmd *cobra.Command, f *action.ClusterApplyFlags) {
	if f.Bootstrap != types.BootstrapYes {
		return
	}
	type implyRow struct {
		positive string
		noFlag   string
		noField  *bool
		dst      *bool
	}
	implied := []implyRow{
		{"sops-decode", "no-sops-decode", &f.NoSopsDecode, &f.SopsDecode},
		{"down-scaled", "no-down-scaled", &f.NoDownScaled, &f.DownScaled},
		{"scale-up", "no-scale-up", &f.NoScaleUp, &f.ScaleUp},
		{"orphan-scale-down", "no-orphan-scale-down", &f.NoOrphanScaleDown, &f.OrphanScaleDown},
		{"bootstrap-guard", "no-bootstrap-guard", &f.NoBootstrapGuard, &f.BootstrapGuard},
		{"bootstrap-clones", "no-bootstrap-clones", &f.NoBootstrapClones, &f.BootstrapClones},
		{"disable-webhooks", "no-disable-webhooks", &f.NoDisableWebhooks, &f.DisableWebhooks},
	}
	for _, im := range implied {
		if cmd.Flags().Changed(im.noFlag) && *im.noField {
			continue
		}
		if !cmd.Flags().Changed(im.positive) {
			*im.dst = true
		}
	}
	if !cmd.Flags().Changed("backup-restore") && !cmd.Flags().Changed("skip-backup-restore") {
		if !(cmd.Flags().Changed("no-backup-restore") && f.NoBackupRestore) {
			f.BackupRestore = true
		}
	}
}

// resolveClusterApplySyncWindow sets f.EffectiveSyncWindow from --sync and --bootstrap.
func resolveClusterApplySyncWindow(cmd *cobra.Command, f *action.ClusterApplyFlags) error {
	if cmd == nil {
		if f.EffectiveSyncWindow == "" {
			f.EffectiveSyncWindow = types.ClusterApplySyncWindowDefault
		}
		return nil
	}
	if cmd.Flags().Changed("sync") {
		raw := strings.TrimSpace(f.SyncWindow)
		if raw == "" {
			return fmt.Errorf("--sync value must not be empty")
		}
		m, err := types.ParseClusterApplySyncWindow(raw)
		if err != nil {
			return err
		}
		f.EffectiveSyncWindow = m
		return nil
	}
	if f.Bootstrap == types.BootstrapYes {
		f.EffectiveSyncWindow = types.ClusterApplySyncWindowKeepOrPrevent
		return nil
	}
	f.EffectiveSyncWindow = types.ClusterApplySyncWindowDefault
	return nil
}

func validateClusterApplyFlags(cmd *cobra.Command, f *action.ClusterApplyFlags) error {
	if cmd != nil {
		for _, name := range clusterApplyBootstrapNoFlagNames {
			if cmd.Flags().Changed(name) && f.Bootstrap != types.BootstrapYes {
				return fmt.Errorf("--%s requires --bootstrap", name)
			}
		}
		if f.Bootstrap == types.BootstrapYes {
			for _, name := range clusterApplyPositiveBehaviorFlagNames {
				if cmd.Flags().Changed(name) {
					return fmt.Errorf("cannot combine --bootstrap with --%s; use --bootstrap with --no-* flags to tune the apply bundle", name)
				}
			}
		}
	}
	if f.ScaleUp && !f.DownScaled {
		return fmt.Errorf("--scale-up requires --down-scaled")
	}
	if f.BootstrapGuard && f.SkipBootstrapGuard {
		return fmt.Errorf("--bootstrap-guard and --skip-bootstrap-guard cannot be used together")
	}
	if f.Bootstrap == types.BootstrapYes && f.SkipRefChecks {
		return fmt.Errorf("cannot combine --bootstrap with --skip-ref-checks")
	}
	if len(f.Predicates) > 0 {
		if f.Replace {
			return fmt.Errorf("cannot combine --include/--exclude with --replace (delete-before-apply)")
		}
		if f.OrphanScaleDown {
			return fmt.Errorf("cannot combine --include/--exclude with --orphan-scale-down (including when implied by --bootstrap); use --no-orphan-scale-down or --orphan-scale-down=false to opt out while keeping other bootstrap behaviors")
		}
	}
	if f.Parallel < 0 {
		return fmt.Errorf("--parallel must not be negative (use 0 for GOMAXPROCS)")
	}
	if f.Parallel > 64 {
		return fmt.Errorf("--parallel must be at most 64")
	}
	return nil
}
