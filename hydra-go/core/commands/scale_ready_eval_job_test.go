package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// testJobEntityForReady builds a batch/v1 Job entity with optional spec/status maps for live cluster view.
func testJobEntityForReady(t *testing.T, spec, status map[string]any) entity.Entity {
	t.Helper()
	obj := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      "test-job",
			"namespace": "demo",
		},
	}
	if spec != nil {
		obj["spec"] = spec
	} else {
		obj["spec"] = map[string]any{"suspend": false}
	}
	if status != nil {
		obj["status"] = status
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("Job"))).
		WithResource(types.Resource("jobs")).
		WithName(types.Name("test-job")).
		WithNamespace(types.Namespace("demo")).
		WithUnstructured(types.KeyClusterEntity, u))
}

// testJobEntityMissingSpec builds a Job live object without a spec key (partial API / merge edge cases).
func testJobEntityMissingSpec(t *testing.T, status map[string]any) entity.Entity {
	t.Helper()
	obj := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      "test-job",
			"namespace": "demo",
		},
	}
	if status != nil {
		obj["status"] = status
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("Job"))).
		WithResource(types.Resource("jobs")).
		WithName(types.Name("test-job")).
		WithNamespace(types.Namespace("demo")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestReadyEvaluator_BuiltinJobReadyStates(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	t.Run("job without spec key does not error on completions", func(t *testing.T) {
		e := testJobEntityMissingSpec(t, map[string]any{"succeeded": int64(0)})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "job not complete")
	})

	t.Run("new job no status yet (not started)", func(t *testing.T) {
		e := testJobEntityForReady(t, map[string]any{"suspend": false}, nil)
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "job not complete")
	})

	t.Run("new job empty status (scheduler has not written counters)", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "job not complete")
	})

	t.Run("running active workload not complete", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{
			"active":    int64(1),
			"succeeded": int64(0),
			"startTime": "2026-01-01T00:00:00Z",
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "job not complete")
	})

	t.Run("partial completions multi-completion job", func(t *testing.T) {
		e := testJobEntityForReady(t,
			map[string]any{"suspend": false, "completions": int64(3)},
			map[string]any{
				"succeeded": int64(2),
			},
		)
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "job not complete")
	})

	t.Run("complete via Complete condition", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{
			"conditions": []any{
				map[string]any{"type": "Complete", "status": "True"},
			},
			"succeeded": int64(1),
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyReady, state)
		assert.Empty(t, msgs)
	})

	t.Run("complete via succeeded count only", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{
			"succeeded": int64(1),
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyReady, state)
		assert.Empty(t, msgs)
	})

	t.Run("failed with Failed condition True", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{
			"conditions": []any{
				map[string]any{"type": "Failed", "status": "True"},
			},
			"failed": int64(1),
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		joined := strings.Join(msgs, " ")
		assert.Contains(t, joined, "job failed")
		assert.Contains(t, joined, "terminal")
		assert.Contains(t, joined, "not auto-restarted")
	})

	t.Run("failed with Failed condition surfaces API message", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{
			"conditions": []any{
				map[string]any{
					"type":    "Failed",
					"status":  "True",
					"reason":  "BackoffLimitExceeded",
					"message": "Job has reached the specified backoff limit",
				},
			},
			"failed": int64(1),
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		joined := strings.Join(msgs, " ")
		assert.Contains(t, joined, "job failed [reason=BackoffLimitExceeded]: Job has reached the specified backoff limit")
		assert.Contains(t, joined, "terminal")
		assert.NotContains(t, joined, "job not complete")
	})

	t.Run("failed with Failed condition reason only when message empty", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{
			"conditions": []any{
				map[string]any{"type": "Failed", "status": "True", "reason": "BackoffLimitExceeded"},
			},
			"failed": int64(1),
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		joined := strings.Join(msgs, " ")
		assert.Contains(t, joined, "job failed [reason=BackoffLimitExceeded]")
		assert.Contains(t, joined, "terminal")
	})

	t.Run("failed with FailureTarget only (no Failed yet)", func(t *testing.T) {
		e := testJobEntityForReady(t, nil, map[string]any{
			"conditions": []any{
				map[string]any{"type": "FailureTarget", "status": "True", "reason": "BackoffLimitExceeded"},
			},
			"active":    int64(1),
			"succeeded": int64(0),
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "job failed [reason=BackoffLimitExceeded]")
	})

	t.Run("failed job must not be ready even if succeeded count matches completions", func(t *testing.T) {
		e := testJobEntityForReady(t,
			map[string]any{"completions": int64(1)},
			map[string]any{
				"conditions": []any{
					map[string]any{"type": "Failed", "status": "True"},
				},
				"succeeded": int64(1),
			},
		)
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		joined := strings.Join(msgs, " ")
		assert.Contains(t, joined, "job failed")
		assert.Contains(t, joined, "terminal")
	})
}

func TestReadyEvaluator_BuiltinJobPodFailureDoesNotMakeJobReady(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	e := testJobEntityForReady(t, nil, map[string]any{
		"active":    int64(1),
		"succeeded": int64(0),
		"failed":    int64(0),
	})
	matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.True(t, matched)
	assert.Equal(t, ClusterScaleReadyNotReady, state)
	assert.Contains(t, strings.Join(msgs, " "), "job not complete")
}
