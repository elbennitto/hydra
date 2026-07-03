package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNewDiffIgnorePipeline_BuiltinRemovesReplicas(t *testing.T) {
	p, err := NewDiffIgnorePipeline(hydra.BuiltinDiffIgnoreRuleEntries())
	require.NoError(t, err)

	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "x", "namespace": "default"},
		"spec":       map[string]any{"replicas": int64(3)},
	}}
	e := mustEntityWithGVKAndUnstructured(t,
		types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment")),
		types.Name("x"), types.Namespace("default"), &u)

	require.NoError(t, p.ApplyToUnstructured(e, &u))
	_, has := u.Object["spec"].(map[string]any)
	require.True(t, has)
	spec := u.Object["spec"].(map[string]any)
	_, hasRep := spec["replicas"]
	assert.False(t, hasRep)
}

func TestNewDiffIgnorePipeline_RejectsEmptyPatchesWithoutIgnoreWhenMissing(t *testing.T) {
	entries := []types.DiffIgnoreRuleEntry{
		{
			Name: "bad",
			Rule: types.HydraDiffIgnoreRule{
				Predicate: `gvk == "v1/Pod"`,
			},
		},
	}
	_, err := NewDiffIgnorePipeline(entries)
	require.Error(t, err)
}

func TestIgnoreLeftOnlyWhenClusterMissing_MatchesPredicate(t *testing.T) {
	entries := []types.DiffIgnoreRuleEntry{
		{
			Name: "admissionJob",
			Rule: types.HydraDiffIgnoreRule{
				Predicate:                  `id == "batch/v1/Job/monitoring/kube-prometheus-stack-admission-patch"`,
				IgnoreWhenMissingInCluster: true,
			},
		},
	}
	p, err := NewDiffIgnorePipeline(entries)
	require.NoError(t, err)

	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": "kube-prometheus-stack-admission-patch", "namespace": "monitoring"},
	}}
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("Job"))).
		WithName(types.Name("kube-prometheus-stack-admission-patch")).
		WithNamespace(types.Namespace("monitoring")).
		WithUnstructured(types.KeyTemplateEntity, u).
		Build()
	require.NoError(t, err)

	ok, err := p.IgnoreLeftOnlyWhenClusterMissing(e)
	require.NoError(t, err)
	assert.True(t, ok)

	other := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": "other-job", "namespace": "monitoring"},
	}}
	e2, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("Job"))).
		WithName(types.Name("other-job")).
		WithNamespace(types.Namespace("monitoring")).
		WithUnstructured(types.KeyTemplateEntity, other).
		Build()
	require.NoError(t, err)
	ok, err = p.IgnoreLeftOnlyWhenClusterMissing(e2)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestNewDiffIgnorePipeline_UserRuleRemovesAnnotation(t *testing.T) {
	entries := []types.DiffIgnoreRuleEntry{
		{
			Name: "ann",
			Rule: types.HydraDiffIgnoreRule{
				Predicate: `gvk == "monitoring.coreos.com/v1/PrometheusRule"`,
				Patches: []types.HydraDiffYqPatch{
					{Yq: `del(.metadata.annotations."prometheus-operator-validated")`},
				},
			},
		},
	}
	p, err := NewDiffIgnorePipeline(entries)
	require.NoError(t, err)

	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "monitoring.coreos.com/v1",
		"kind":       "PrometheusRule",
		"metadata": map[string]any{
			"name":      "r",
			"namespace": "mon",
			"annotations": map[string]any{
				"prometheus-operator-validated": "true",
				"keep":                          "yes",
			},
		},
		"spec": map[string]any{"groups": []any{}},
	}}
	e := mustEntityWithGVKAndUnstructured(t,
		types.NewGVK(types.Group("monitoring.coreos.com"), types.Version("v1"), types.Kind("PrometheusRule")),
		types.Name("r"), types.Namespace("mon"), &u)

	require.NoError(t, p.ApplyToUnstructured(e, &u))
	ann := u.Object["metadata"].(map[string]any)["annotations"].(map[string]any)
	_, has := ann["prometheus-operator-validated"]
	assert.False(t, has)
	assert.Equal(t, "yes", ann["keep"])
}

func mustEntityWithGVKAndUnstructured(t *testing.T, gvk types.GVK, name types.Name, ns types.Namespace, u *unstructured.Unstructured) entity.Entity {
	t.Helper()
	e, err := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(name).
		WithNamespace(ns).
		WithUnstructured(types.KeyTemplateEntity, *u).
		WithUnstructured(types.KeyClusterEntity, *u).
		Build()
	require.NoError(t, err)
	return e
}
