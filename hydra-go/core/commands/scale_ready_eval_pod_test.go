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

func testPodEntityForReady(t *testing.T, phase string, conditions []map[string]any) entity.Entity {
	t.Helper()
	return testPodEntityForReadyExt(t, phase, conditions, nil)
}

// testPodEntityForReadyExt optionally sets status.containerStatuses (list of maps).
func testPodEntityForReadyExt(t *testing.T, phase string, conditions []map[string]any, containerStatuses []map[string]any) entity.Entity {
	t.Helper()
	pod := unstructured.Unstructured{}
	pod.SetAPIVersion("v1")
	pod.SetKind("Pod")
	pod.SetNamespace("default")
	pod.SetName("p1")
	require.NoError(t, unstructured.SetNestedField(pod.Object, phase, "status", "phase"))
	if len(conditions) > 0 {
		sl := make([]interface{}, len(conditions))
		for i, c := range conditions {
			sl[i] = c
		}
		require.NoError(t, unstructured.SetNestedSlice(pod.Object, sl, "status", "conditions"))
	}
	if len(containerStatuses) > 0 {
		sl := make([]interface{}, len(containerStatuses))
		for i, c := range containerStatuses {
			sl[i] = c
		}
		require.NoError(t, unstructured.SetNestedSlice(pod.Object, sl, "status", "containerStatuses"))
	}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("p1")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, pod))
}

func TestReadyEvaluator_BuiltinPodReadyStates(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	t.Run("running with Ready True", func(t *testing.T) {
		e := testPodEntityForReady(t, "Running", []map[string]any{
			{"type": "Ready", "status": "True"},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyReady, state)
		assert.Empty(t, msgs)
	})

	t.Run("running without Ready condition", func(t *testing.T) {
		e := testPodEntityForReady(t, "Running", nil)
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "Ready condition missing from status")
	})

	t.Run("running PodScheduled False", func(t *testing.T) {
		e := testPodEntityForReady(t, "Running", []map[string]any{
			{"type": "PodScheduled", "status": "False", "reason": "Unschedulable"},
			{"type": "Ready", "status": "False"},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "PodScheduled")
		assert.Contains(t, j, "not assigned to a node")
	})

	t.Run("running DisruptionTarget True", func(t *testing.T) {
		e := testPodEntityForReady(t, "Running", []map[string]any{
			{"type": "Ready", "status": "True"},
			{"type": "DisruptionTarget", "status": "True", "reason": "EvictionByEvictionAPI"},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "DisruptionTarget")
		assert.Contains(t, j, "eviction")
	})

	t.Run("running unknown condition type False", func(t *testing.T) {
		e := testPodEntityForReady(t, "Running", []map[string]any{
			{"type": "PodResizePending", "status": "False"},
			{"type": "Ready", "status": "True"},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "failing status condition of another type")
	})

	t.Run("pending", func(t *testing.T) {
		e := testPodEntityForReady(t, "Pending", nil)
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "phase Pending")
	})

	t.Run("succeeded", func(t *testing.T) {
		e := testPodEntityForReady(t, "Succeeded", nil)
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyReady, state)
		assert.Empty(t, msgs)
	})

	t.Run("failed", func(t *testing.T) {
		e := testPodEntityForReady(t, "Failed", nil)
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "pod failed")
	})

	t.Run("pending with ImagePullBackOff", func(t *testing.T) {
		e := testPodEntityForReadyExt(t, "Pending", nil, []map[string]any{
			{
				"name": "c1",
				"state": map[string]any{
					"waiting": map[string]any{"reason": "ImagePullBackOff"},
				},
			},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "ImagePullBackOff")
		assert.Contains(t, j, "phase Pending")
	})

	t.Run("pending with ErrImagePull", func(t *testing.T) {
		e := testPodEntityForReadyExt(t, "Pending", nil, []map[string]any{
			{
				"name": "c1",
				"state": map[string]any{
					"waiting": map[string]any{"reason": "ErrImagePull"},
				},
			},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "ErrImagePull")
	})

	t.Run("pending with CreateContainerConfigError secret or configmap", func(t *testing.T) {
		e := testPodEntityForReadyExt(t, "Pending", nil, []map[string]any{
			{
				"name": "c1",
				"state": map[string]any{
					"waiting": map[string]any{"reason": "CreateContainerConfigError"},
				},
			},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "config error")
		assert.Contains(t, j, "secret")
	})

	t.Run("running with CrashLoopBackOff", func(t *testing.T) {
		e := testPodEntityForReadyExt(t, "Running", []map[string]any{
			{"type": "Ready", "status": "False"},
		}, []map[string]any{
			{
				"name": "c1",
				"state": map[string]any{
					"waiting": map[string]any{"reason": "CrashLoopBackOff"},
				},
			},
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "CrashLoopBackOff")
	})
}
