package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func secretEntity(name, namespace string, live *unstructured.Unstructured) entity.Entity {
	tpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithUnstructured(types.KeyTemplateEntity, tpl)
	if live != nil {
		b = b.WithUnstructured(types.KeyClusterEntity, *live)
	}
	return mustBuild(b)
}

func configMapEntity(name, namespace string, live *unstructured.Unstructured) entity.Entity {
	tpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithUnstructured(types.KeyTemplateEntity, tpl)
	if live != nil {
		b = b.WithUnstructured(types.KeyClusterEntity, *live)
	}
	return mustBuild(b)
}

func TestReadyEvaluator_BuiltinSecretConfigMap_ClusterPresence(t *testing.T) {
	t.Run("secret ready when present in cluster inventory", func(t *testing.T) {
		live := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]any{
					"name": "db", "namespace": "default",
				},
			},
		}
		se := secretEntity("db", "default", &live)
		ents, err := entity.NewEntities([]entity.Entity{se})
		require.NoError(t, err)
		re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), ents, types.KeyClusterEntity)
		require.NoError(t, err)
		matched, state, msgs, err := re.ReadyState(se, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyReady, state)
		assert.Empty(t, msgs)
	})

	t.Run("secret not_ready missing when absent from cluster inventory", func(t *testing.T) {
		se := secretEntity("db", "default", nil)
		ents, err := entity.NewEntities([]entity.Entity{})
		require.NoError(t, err)
		re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), ents, types.KeyClusterEntity)
		require.NoError(t, err)
		matched, state, msgs, err := re.ReadyState(se, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		require.Len(t, msgs, 1)
		assert.Equal(t, "missing", msgs[0])
	})

	t.Run("configmap not_ready missing when absent", func(t *testing.T) {
		cm := configMapEntity("cfg", "kube-system", nil)
		ents, err := entity.NewEntities([]entity.Entity{})
		require.NoError(t, err)
		re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), ents, types.KeyClusterEntity)
		require.NoError(t, err)
		matched, state, msgs, err := re.ReadyState(cm, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		require.Len(t, msgs, 1)
		assert.Equal(t, "missing", msgs[0])
	})
}
