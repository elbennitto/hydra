package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIsKubernetesAPIServerManagedKubeRootCAConfigMap(t *testing.T) {
	t.Parallel()
	assert.True(t, IsKubernetesAPIServerManagedKubeRootCAConfigMap(types.Id("v1/ConfigMap/monitoring/kube-root-ca.crt")))
	assert.False(t, IsKubernetesAPIServerManagedKubeRootCAConfigMap(types.Id("v1/ConfigMap/monitoring/other")))
	assert.False(t, IsKubernetesAPIServerManagedKubeRootCAConfigMap(types.Id("v1/Secret/monitoring/kube-root-ca.crt")))
}

func TestExcludeKubernetesAPIServerKubeRootCAConfigMaps(t *testing.T) {
	t.Parallel()
	cm := func(ns, name string) entity.Entity {
		u := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name":      name,
					"namespace": ns,
				},
			},
		}
		gvk := types.NewGVK(types.KubernetesGroupCore, types.KubernetesVersionV1, types.KubernetesKindConfigMap)
		b := entity.NewEntityBuilder().
			WithGVK(gvk).
			WithName(types.Name(name)).
			WithNamespace(types.Namespace(ns)).
			WithNamespaced(types.NamespacedYes)
		e, err := b.WithUnstructured(types.KeyTemplateEntity, u).Build()
		require.NoError(t, err)
		return e
	}
	in, err := entity.NewEntities([]entity.Entity{
		cm("ns1", "kube-root-ca.crt"),
		cm("ns1", "app-config"),
		cm("ns2", "kube-root-ca.crt"),
	})
	require.NoError(t, err)
	out, err := ExcludeKubernetesAPIServerKubeRootCAConfigMaps(in)
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	id, err := out.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/ConfigMap/ns1/app-config"), id)
}
