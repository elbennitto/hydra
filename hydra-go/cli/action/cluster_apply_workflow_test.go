package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
)

func emptyEntityList(t *testing.T) entity.Entities {
	t.Helper()
	e, err := entity.NewEntities(nil)
	require.NoError(t, err)
	return e
}

func TestBuildClusterApplyWorkflowPlan_DefaultFlags(t *testing.T) {
	plan := BuildClusterApplyWorkflowPlan(ClusterApplyFlags{})
	ids := WorkflowPlanIDs(plan)
	assert.Equal(t, []string{
		WorkflowApplyCRDs,
		WorkflowApplyNamespaces,
		WorkflowApplyScaleZero,
		WorkflowApplyWebhooks,
		WorkflowDeleteOrphans,
	}, ids)
}

func TestBuildClusterApplyWorkflowPlan_NoUnexpectedPhases(t *testing.T) {
	f := ClusterApplyFlags{}
	plan := BuildClusterApplyWorkflowPlan(f)
	got := make(map[string]bool)
	for _, p := range plan {
		got[p.ID] = true
	}
	assert.False(t, got[WorkflowRestoreBackups], "restore should not appear without backup-restore")
	assert.False(t, got[WorkflowDisableWebhooks], "disable-webhooks should not appear without flag")
	assert.False(t, got[WorkflowScaleUpWorkloads], "scale-up should not appear without flag")
	assert.False(t, got[WorkflowScaleDownOrphans], "orphan scale-down should not appear without flag")
}

func TestBuildClusterApplyWorkflowPlan_AllOptionalEnabled(t *testing.T) {
	f := ClusterApplyFlags{
		ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
			BackupRestore:   true,
			DisableWebhooks: true,
			ScaleUp:         true,
			OrphanScaleDown: true,
		},
		SkipBackupRestoreFlag: flags.SkipBackupRestoreFlag{SkipBackupRestore: false},
	}
	plan := BuildClusterApplyWorkflowPlan(f)
	ids := WorkflowPlanIDs(plan)
	assert.Equal(t, []string{
		WorkflowApplyCRDs,
		WorkflowApplyNamespaces,
		WorkflowRestoreBackups,
		WorkflowDisableWebhooks,
		WorkflowApplyScaleZero,
		WorkflowScaleUpWorkloads,
		WorkflowApplyWebhooks,
		WorkflowScaleDownOrphans,
		WorkflowDeleteOrphans,
	}, ids)
}

func TestMarkClusterApplyWorkflowSkipped_NoCRDsInTemplate(t *testing.T) {
	empty := emptyEntityList(t)
	state := &applyState{
		crds:       empty,
		namespaces: empty,
		nonCrds:    empty,
		orphans:    empty,
		ops:        nil,
	}
	plan := BuildClusterApplyWorkflowPlan(ClusterApplyFlags{})
	res := MarkClusterApplyWorkflowSkipped(plan, state)
	var crd *ResolvedWorkflowPhase
	for i := range res {
		if res[i].ID == WorkflowApplyCRDs {
			crd = &res[i]
			break
		}
	}
	require.NotNil(t, crd)
	assert.True(t, crd.Skipped)
	assert.Equal(t, "no CRDs", crd.Reason)
}

func TestMarkClusterApplyWorkflowSkipped_MainWorkloadNoResources(t *testing.T) {
	empty := emptyEntityList(t)
	state := &applyState{
		crds:       empty,
		namespaces: empty,
		nonCrds:    empty,
		orphans:    empty,
		ops:        nil,
	}
	plan := BuildClusterApplyWorkflowPlan(ClusterApplyFlags{})
	res := MarkClusterApplyWorkflowSkipped(plan, state)
	var main *ResolvedWorkflowPhase
	for i := range res {
		if res[i].ID == WorkflowApplyScaleZero {
			main = &res[i]
			break
		}
	}
	require.NotNil(t, main)
	assert.True(t, main.Skipped)
	assert.Equal(t, "no resources to apply", main.Reason)
}
