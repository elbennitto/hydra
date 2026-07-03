package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReadyEvaluator_CelListReturnFlattened(t *testing.T) {
	rules := []readyRule{{
		name:      "list-rule",
		predicate: `gvk == "v1/ConfigMap"`,
		cel:       toCelExpressions([]string{`["a", "b"]`}),
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
	_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, ClusterScaleReadyNotReady, state)
	assert.Equal(t, []string{"a", "b"}, msgs)
}

func TestReadyEvaluator_CelEmptyListPasses(t *testing.T) {
	rules := []readyRule{{
		name:      "empty-list",
		predicate: `gvk == "v1/ConfigMap"`,
		cel:       toCelExpressions([]string{`[]`}),
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
	_, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, ClusterScaleReadyReady, state)
	assert.Nil(t, msgs)
}
