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

func TestReadyEvaluator_BooleanCelIsRejected(t *testing.T) {
	rules := []readyRule{{
		name:      "test-bool",
		predicate: `gvk == "v1/ConfigMap"`,
		cel:       toCelExpressions([]string{`false`}),
	}}
	re, err := NewReadyEvaluator(rules, entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "cm1", "namespace": "default",
			},
		},
	}
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name("cm1")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, u))
	_, _, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "bool")
}

func TestReadyEvaluator_EmptyCelLinesAreIgnored(t *testing.T) {
	rules := []readyRule{{
		name:      "test-empty-cel-lines",
		predicate: `  gvk == "v1/ConfigMap"  `,
		cel:       toCelExpressions([]string{"", "   ", `""`}),
	}}
	_, err := NewReadyEvaluator(rules, entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)
}

func TestReadyEvaluator_AllEmptyCelLinesAreRejected(t *testing.T) {
	rules := []readyRule{{
		name:      "test-all-empty-cel-lines",
		predicate: `gvk == "v1/ConfigMap"`,
		cel:       toCelExpressions([]string{"", "   "}),
	}}
	_, err := NewReadyEvaluator(rules, entity.Entities{}, types.KeyClusterEntity)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one non-empty cel expression is required")
}

func testDeploymentEntityForReady(t *testing.T, specReplicas *int64, status map[string]any) entity.Entity {
	t.Helper()
	spec := map[string]any{}
	if specReplicas != nil {
		spec["replicas"] = *specReplicas
	}
	obj := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      "web",
			"namespace": "default",
		},
		"spec": spec,
	}
	if status != nil {
		obj["status"] = status
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("web")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestReadyEvaluator_BuiltinDeploymentFailureMessages(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	t.Run("scaled to zero explains desired zero", func(t *testing.T) {
		z := int64(0)
		e := testDeploymentEntityForReady(t, &z, map[string]any{"readyReplicas": int64(0)})
		_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "desired replica count is zero")
	})

	t.Run("below desired includes counts", func(t *testing.T) {
		n := int64(3)
		e := testDeploymentEntityForReady(t, &n, map[string]any{"readyReplicas": int64(1)})
		_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "readyReplicas 1 is below desired 3")
	})

	t.Run("missing readyReplicas while running", func(t *testing.T) {
		n := int64(2)
		e := testDeploymentEntityForReady(t, &n, map[string]any{})
		_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		assert.Contains(t, strings.Join(msgs, " "), "status.readyReplicas missing")
	})

	t.Run("ReplicaFailure True", func(t *testing.T) {
		n := int64(1)
		e := testDeploymentEntityForReady(t, &n, map[string]any{
			"readyReplicas": int64(1),
			"conditions": []any{
				map[string]any{"type": "ReplicaFailure", "status": "True"},
			},
		})
		_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "ReplicaFailure")
		assert.Contains(t, j, "pod create or delete failed")
	})

	t.Run("Available False", func(t *testing.T) {
		n := int64(2)
		e := testDeploymentEntityForReady(t, &n, map[string]any{
			"readyReplicas": int64(2),
			"conditions": []any{
				map[string]any{"type": "Available", "status": "False", "reason": "MinimumReplicasUnavailable"},
			},
		})
		_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "Available")
		assert.Contains(t, j, "minimum availability")
	})

	t.Run("Progressing False", func(t *testing.T) {
		n := int64(1)
		e := testDeploymentEntityForReady(t, &n, map[string]any{
			"readyReplicas": int64(1),
			"conditions": []any{
				map[string]any{"type": "Available", "status": "True"},
				map[string]any{"type": "Progressing", "status": "False", "reason": "ProgressDeadlineExceeded"},
			},
		})
		_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "Progressing")
		assert.Contains(t, j, "progress deadline")
	})
}

func testStatefulSetEntityForReady(t *testing.T, specReplicas *int64, status map[string]any) entity.Entity {
	t.Helper()
	spec := map[string]any{}
	if specReplicas != nil {
		spec["replicas"] = *specReplicas
	}
	obj := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]any{
			"name":      "db",
			"namespace": "default",
		},
		"spec": spec,
	}
	if status != nil {
		obj["status"] = status
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("db")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestReadyEvaluator_BuiltinStatefulSetConditions(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	n := int64(2)
	e := testStatefulSetEntityForReady(t, &n, map[string]any{
		"readyReplicas": int64(2),
		"conditions": []any{
			map[string]any{"type": "Available", "status": "False"},
		},
	})
	_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, ClusterScaleReadyNotReady, state)
	assert.Contains(t, strings.Join(msgs, " "), "StatefulSet condition Available")
}

func testDaemonSetEntityForReady(t *testing.T, status map[string]any) entity.Entity {
	t.Helper()
	obj := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "DaemonSet",
		"metadata": map[string]any{
			"name":      "node-exporter",
			"namespace": "default",
		},
	}
	if status != nil {
		obj["status"] = status
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("DaemonSet"))).
		WithResource(types.Resource("daemonsets")).
		WithName(types.Name("node-exporter")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestReadyEvaluator_BuiltinDaemonSetConditions(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	e := testDaemonSetEntityForReady(t, map[string]any{
		"desiredNumberScheduled": int64(3),
		"numberReady":            int64(3),
		"conditions": []any{
			map[string]any{"type": "Available", "status": "False"},
		},
	})
	_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, ClusterScaleReadyNotReady, state)
	assert.Contains(t, strings.Join(msgs, " "), "DaemonSet condition Available")
}

func testReplicaSetEntityForReady(t *testing.T, specReplicas *int64, status map[string]any) entity.Entity {
	t.Helper()
	spec := map[string]any{}
	if specReplicas != nil {
		spec["replicas"] = *specReplicas
	}
	obj := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "ReplicaSet",
		"metadata": map[string]any{
			"name":      "web-rs",
			"namespace": "default",
		},
		"spec": spec,
	}
	if status != nil {
		obj["status"] = status
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("ReplicaSet"))).
		WithResource(types.Resource("replicasets")).
		WithName(types.Name("web-rs")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestReadyEvaluator_BuiltinReplicaSetConditions(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	n := int64(2)
	e := testReplicaSetEntityForReady(t, &n, map[string]any{
		"readyReplicas": int64(2),
		"conditions": []any{
			map[string]any{"type": "CustomFuture", "status": "False"},
		},
	})
	_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, ClusterScaleReadyNotReady, state)
	assert.Contains(t, strings.Join(msgs, " "), "ReplicaSet has a failing status condition")
}

func TestReadyEvaluator_CustomStatusReadyPathReplicaMessage(t *testing.T) {
	scaleMap := map[types.GVKString]types.HydraScaleGroup{
		"example.com/v1/Widget": {StatusReadyPath: "status.readyReplicas"},
	}
	re, err := NewReadyEvaluator(mergeReadyRules(nil, scaleMap), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "example.com/v1",
			"kind":       "Widget",
			"metadata": map[string]any{
				"name":      "w1",
				"namespace": "ns1",
			},
			"status": map[string]any{"readyReplicas": int64(0)},
		},
	}
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("example.com"), types.Version("v1"), types.Kind("Widget"))).
		WithResource(types.Resource("widgets")).
		WithName(types.Name("w1")).
		WithNamespace(types.Namespace("ns1")).
		WithUnstructured(types.KeyClusterEntity, u))
	_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, ClusterScaleReadyNotReady, state)
	j := strings.Join(msgs, " ")
	assert.Contains(t, j, "readyReplicas at status.readyReplicas")
	assert.Contains(t, j, "must be > 0")
}
