package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestIsKubernetesClusterScopedUnionCandidate(t *testing.T) {
	t.Parallel()

	apiSvc := mustBuild(entity.NewEntityBuilder().
		WithGroup("apiregistration.k8s.io").
		WithVersion("v1").
		WithResource("apiservices").
		WithKind("APIService").
		WithName("v1.example.com").
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apiregistration.k8s.io/v1",
			"kind":       "APIService",
			"metadata":   map[string]any{"name": "v1.example.com"},
		}}))
	assert.True(t, isKubernetesClusterScopedUnionCandidate(apiSvc))

	nsObj := mustBuild(entity.NewEntityBuilder().
		WithGroup("").
		WithVersion("v1").
		WithResource("namespaces").
		WithKind("Namespace").
		WithName("kube-system").
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata":   map[string]any{"name": "kube-system"},
		}}))
	assert.False(t, isKubernetesClusterScopedUnionCandidate(nsObj))

	cm := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(types.ApiVersion{Group: "", Version: types.Version("v1")}).
		WithResource("configmaps").
		WithKind("ConfigMap").
		WithName("c1").
		WithNamespace("ns1").
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "c1",
				"namespace": "ns1",
			},
		}}))
	assert.False(t, isKubernetesClusterScopedUnionCandidate(cm))
}

func TestLiveEntitiesInHydraWorkloadScope_EmptyLive(t *testing.T) {
	t.Parallel()

	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)
	tpl, err := entity.NewEntities(nil)
	require.NoError(t, err)

	out, preds, err := LiveEntitiesInHydraWorkloadScope(
		empty,
		sets.New[types.Namespace](),
		tpl,
		nil,
		nil,
		tpl,
		types.HelmNetworkModeOffline,
	)
	require.NoError(t, err)
	require.Equal(t, 0, out.Len())
	require.Equal(t, 0, preds.Len())
}
