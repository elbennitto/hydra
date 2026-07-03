package action

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	basephase "hydra-gitops.org/hydra/hydra-go/base/phase"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// applyStateWithAllOptionalApplyPhases enables every phase that buildApplyPhases can include.
func applyStateWithAllOptionalApplyPhases() *applyState {
	return &applyState{
		flags: ClusterApplyFlags{
			ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
				BackupRestore:   true,
				DisableWebhooks: true,
				ScaleUp:         true,
				OrphanScaleDown: true,
			},
			SkipBackupRestoreFlag: flags.SkipBackupRestoreFlag{
				SkipBackupRestore: false,
			},
		},
	}
}

func phaseNames(items []basephase.Item[applyState]) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestBuildApplyPhases_AutomaticNumbering(t *testing.T) {
	phases := buildApplyPhases(applyStateWithAllOptionalApplyPhases())

	require.Len(t, phases.Items, 9)
	for i, item := range phases.Items {
		assert.Equal(t, i+1, item.Number)
	}
	assert.Equal(t, "restore-backups", phases.Items[2].Name)
	assert.Equal(t, "restoring backup secrets", phases.Items[2].Description)
	assert.Equal(t, "delete-orphans", phases.Items[8].Name)
	assert.Equal(t, "apply-webhooks", phases.Items[6].Name)
}

func TestBuildApplyPhases_DefaultFlags_OmitsOptionalPhases(t *testing.T) {
	phases := buildApplyPhases(&applyState{})

	names := phaseNames(phases.Items)
	require.Len(t, names, 5)
	assert.Equal(t, []string{
		"apply-crds",
		"apply-namespaces",
		"apply-scale-zero",
		"apply-webhooks",
		"delete-orphans",
	}, names)
	assert.NotContains(t, names, "restore-backups")
	assert.NotContains(t, names, "scale-up-workloads")
	assert.NotContains(t, names, "scale-down-orphans")
	assert.NotContains(t, names, "delete-webhooks")
}

func TestBuildApplyPhases_IncludesDisableWebhooksWhenDisableWebhooks(t *testing.T) {
	phases := buildApplyPhases(&applyState{
		flags: ClusterApplyFlags{
			ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
				DisableWebhooks: true,
			},
		},
	})
	names := phaseNames(phases.Items)
	require.GreaterOrEqual(t, len(names), 3)
	assert.Equal(t, "disable-webhooks", names[2])
}

func TestBuildApplyPhases_OmitsRestoreBackupsWhenSkipOrNoBackupRestore(t *testing.T) {
	phases := buildApplyPhases(&applyState{
		flags: ClusterApplyFlags{
			ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
				BackupRestore: true,
			},
			SkipBackupRestoreFlag: flags.SkipBackupRestoreFlag{
				SkipBackupRestore: true,
			},
		},
	})
	assert.NotContains(t, phaseNames(phases.Items), "restore-backups")

	phases2 := buildApplyPhases(&applyState{
		flags: ClusterApplyFlags{
			ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
				BackupRestore: false,
			},
		},
	})
	assert.NotContains(t, phaseNames(phases2.Items), "restore-backups")
}

func TestPhaseRestoreBackups_UpToDateResultContinues(t *testing.T) {
	originalBackupRestoreWithOptions := backupRestoreWithOptions
	originalPrintBackupResults := printBackupResults
	t.Cleanup(func() {
		backupRestoreWithOptions = originalBackupRestoreWithOptions
		printBackupResults = originalPrintBackupResults
	})

	printCalled := false
	backupRestoreWithOptions = func(
		_ *hydra.Cluster,
		_ []types.AppId,
		_ types.HelmNetworkMode,
		_ types.KubernetesVersion,
		_ bool,
		_ types.Color,
		_ types.DryRun,
		_ commands.BackupRestoreOptions,
	) ([]commands.BackupResult, error) {
		return []commands.BackupResult{
			{
				SecretId: "v1/Secret/default/my-secret",
				Status:   commands.BackupStatusUpToDate,
			},
		}, nil
	}
	printBackupResults = func(_ log.Logger, results []commands.BackupResult, _ types.Color) {
		printCalled = true
		require.Len(t, results, 1)
		assert.Equal(t, commands.BackupStatusUpToDate, results[0].Status)
	}

	state := &applyState{
		l:            log.Default(),
		currentPhase: 3,
		totalPhases:  10,
		flags: ClusterApplyFlags{
			ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
				BackupRestore: true,
			},
		},
	}

	result := phaseRestoreBackups(context.Background(), state)

	assert.True(t, printCalled)
	assert.Equal(t, basephase.StatusNext, result.Status)
	assert.NotEqual(t, basephase.StatusAborted, result.Status)
	assert.NoError(t, result.Err)
}
