package hydra

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
)

func TestClusterDefaultsPresetMatchTiming_RecordMinMaxSum(t *testing.T) {
	var tr ClusterDefaultsPresetMatchTiming
	tr.Record("a", 5*time.Millisecond)
	tr.Record("a", 1*time.Millisecond)
	tr.Record("a", 3*time.Millisecond)
	out := tr.FormatReport("phase", 3)
	require.Contains(t, out, "phase:")
	require.Contains(t, out, "preset=a count=3 min=1ms max=5ms sum=9ms")
}

func TestClusterDefaultsBatchPresetProfile_FormatReport(t *testing.T) {
	profile := NewClusterDefaultsBatchPresetProfile("kubernetes", 2869)
	require.True(t, profile.EnabledForPreset("kubernetes"))
	require.False(t, profile.EnabledForPreset("coredns"))

	profile.RecordExplicitIDs(2*time.Millisecond, 11)
	profile.RecordCEL(5 * time.Millisecond)
	profile.RecordCEL(7 * time.Millisecond)
	profile.SetAnchorPredicateCount(23)
	profile.RecordTemplateSkippedEntity()
	profile.RecordDirectMatchedEntity()
	profile.RecordAnchorCandidateEntity()
	profile.RecordResolvedByOwnerApp()
	profile.RecordResolvedByParentPreset()
	profile.RecordClosureLayer(4)
	profile.RecordAnchor(3 * time.Millisecond)
	profile.RecordAnchorRule("rule-a", 3*time.Millisecond, true, workloadclosure.PredicateMatchStats{
		ClosureWalk:         true,
		VisitedEntities:     4,
		OwnerCandidates:     2,
		RegardingCandidates: 1,
		RefCandidatesByVia: map[workloadclosure.ParentVia]int{
			workloadclosure.ParentViaFromRefLabel("podMetrics"): 1,
		},
	})
	profile.RecordTotal(19 * time.Millisecond)

	out := profile.FormatReport("inventory model")
	require.Contains(t, out, "inventory model: batch preset profile (preset=kubernetes, entities=2869)")
	require.Contains(t, out, "total=19ms")
	require.Contains(t, out, "explicitIds count=11 sum=2ms")
	require.Contains(t, out, "celSelects count=2 sum=12ms")
	require.Contains(t, out, "anchorChecks count=1 sum=3ms")
	require.Contains(t, out, "templateSkippedEntities=1 resolvedByOwnerApp=1 resolvedByParentPreset=1")
	require.Contains(t, out, "anchorPredicates=23")
	require.Contains(t, out, "anchorCandidateEntities=1")
	require.Contains(t, out, "closureVisited sum=8 avg=4.00 max=4 ownerCandidates=2 regardingCandidates=1 podMetricsCandidates=1")
	require.Contains(t, out, `slowAnchor[1] calls=1 matches=1 sum=3ms max=3ms rule="rule-a"`)
}
