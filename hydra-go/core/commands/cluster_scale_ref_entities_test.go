package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const longhornProviderRefParsersYAML = `ref-parsers:
  - predicate: 'gvk == "storage.k8s.io/v1/CSIDriver" && name == "driver.longhorn.io"'
    pick:
      - cel: '[refBuilder().outgoing(ref("provider", "longhorn"))]'
  - predicate: 'gvk == "apps/v1/StatefulSet" && ns == "longhorn-system"'
    pick:
      - cel: '[refBuilder().incoming(ref("provider", "longhorn"))]'
`

func TestAugmentClusterScaleEntitiesForRefs_ProviderEdgeToLiveOnlyStatefulSet(t *testing.T) {
	parsers, err := references.ParseRefParsers([]byte(longhornProviderRefParsersYAML))
	require.NoError(t, err)

	csiTpl := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "storage.k8s.io/v1",
		"kind":       "CSIDriver",
		"metadata": map[string]any{
			"name": "driver.longhorn.io",
			"uid":  "csi-uid-1",
		},
	}}
	csiLive := *csiTpl.DeepCopy()
	csiGvk := types.NewGVK(types.Group("storage.k8s.io"), types.Version("v1"), types.Kind("CSIDriver"))
	csi := mustBuild(entity.NewEntityBuilder().
		WithGVK(csiGvk).
		WithResource(types.Resource("csidrivers")).
		WithName(types.Name("driver.longhorn.io")).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyTemplateEntity, csiTpl).
		WithUnstructured(types.KeyClusterEntity, csiLive))

	stsU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]any{
			"name":      "instance-manager-e-test",
			"namespace": "longhorn-system",
			"uid":       "sts-uid-1",
		},
		"spec": map[string]any{"replicas": int64(1)},
	}}
	stsGvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))
	sts := mustBuild(entity.NewEntityBuilder().
		WithGVK(stsGvk).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("instance-manager-e-test")).
		WithNamespace(types.Namespace("longhorn-system")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyClusterEntity, stsU))

	merged, err := entity.NewEntities([]entity.Entity{csi, sts})
	require.NoError(t, err)

	stsID, err := sts.Id()
	require.NoError(t, err)

	refsNoAugment, err := references.Refs(log.Default(), merged, types.KeyTemplateEntity, nil, entity.Entities{}, entity.Entities{}, nil, parsers)
	require.NoError(t, err)
	var toSTSWithout int
	for _, r := range refsNoAugment {
		if r.To == stsID {
			toSTSWithout++
		}
	}
	assert.Equal(t, 0, toSTSWithout, "live-only STS must not participate in Refs without template mirror")

	ownerNs := map[types.Namespace]types.AppId{
		types.Namespace("longhorn-system"): "in-cluster.cluster-infra.longhorn",
	}
	augmented, err := AugmentClusterScaleEntitiesForRefs(merged, ownerNs)
	require.NoError(t, err)

	refs, err := references.Refs(log.Default(), augmented, types.KeyTemplateEntity, nil, entity.Entities{}, entity.Entities{}, nil, parsers)
	require.NoError(t, err)

	var toSTS int
	for _, r := range refs {
		if r.To == stsID {
			toSTS++
		}
	}
	assert.GreaterOrEqual(t, toSTS, 1, "expected CSIDriver hub -> STS edge after augment")
}

func TestAugmentClusterScaleEntitiesForRefs_NoAugmentOutsideOwnerNamespaces(t *testing.T) {
	stsU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]any{
			"name":      "other",
			"namespace": "other-ns",
			"uid":       "sts-uid-2",
		},
		"spec": map[string]any{"replicas": int64(1)},
	}}
	stsGvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))
	sts := mustBuild(entity.NewEntityBuilder().
		WithGVK(stsGvk).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("other")).
		WithNamespace(types.Namespace("other-ns")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyClusterEntity, stsU))

	merged, err := entity.NewEntities([]entity.Entity{sts})
	require.NoError(t, err)

	ownerNs := map[types.Namespace]types.AppId{
		types.Namespace("longhorn-system"): "in-cluster.cluster-infra.longhorn",
	}
	out, err := AugmentClusterScaleEntitiesForRefs(merged, ownerNs)
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.False(t, out.Items[0].HasKey(types.KeyTemplateEntity))
}
