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

func clusterOnlyReplicaSet(ns, name, uid, deployName, deployUID string, replicas int64) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "ReplicaSet",
		"metadata": map[string]any{
			"name":      name,
			"namespace": ns,
			"uid":       uid,
			"ownerReferences": []any{
				map[string]any{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"name":       deployName,
					"uid":        deployUID,
				},
			},
		},
		"spec": map[string]any{
			"replicas": replicas,
		},
	}}
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("ReplicaSet"))
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("replicasets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestCollectClusterOnlyWorkloadEntities_ExcludesReplicaSetOwnedByTemplatedDeployment(t *testing.T) {
	dep := liveDeploymentEntity("ns", "dep", "dep-uid", 1)
	rs := clusterOnlyReplicaSet("ns", "rs-1", "rs-uid", "dep", "dep-uid", 1)
	ents, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	out, err := collectClusterOnlyWorkloadEntities(ents, types.KeyTemplateEntity, types.KeyClusterEntity, nil, nil)
	require.NoError(t, err)
	assert.Len(t, out, 0)
}

func TestCollectClusterOnlyWorkloadEntities_ExcludesWhenReplicasZero(t *testing.T) {
	dep := liveDeploymentEntity("ns", "dep", "dep-uid", 1)
	rs := clusterOnlyReplicaSet("ns", "rs-1", "rs-uid", "dep", "dep-uid", 0)
	ents, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	out, err := collectClusterOnlyWorkloadEntities(ents, types.KeyTemplateEntity, types.KeyClusterEntity, nil, nil)
	require.NoError(t, err)
	assert.Len(t, out, 0)
}

func TestCollectClusterOnlyWorkloadEntities_ExcludesTemplateWorkloads(t *testing.T) {
	dep := liveDeploymentEntity("ns", "dep", "dep-uid", 1)
	ents, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	out, err := collectClusterOnlyWorkloadEntities(ents, types.KeyTemplateEntity, types.KeyClusterEntity, nil, nil)
	require.NoError(t, err)
	assert.Len(t, out, 0)
}

func TestClusterOnlyScaleDownWillMutate_NoReplicaSetUnderDeployment(t *testing.T) {
	dep := liveDeploymentEntity("ns", "dep", "dep-uid", 1)
	rs := clusterOnlyReplicaSet("ns", "rs-1", "rs-uid", "dep", "dep-uid", 1)
	ents, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	yes, err := ClusterOnlyScaleDownWillMutate(ents, types.KeyTemplateEntity, types.KeyClusterEntity, nil, nil)
	require.NoError(t, err)
	assert.False(t, yes)
}

func TestCollectClusterOnlyWorkloadEntities_LinkedByHydraRefWithoutOwnerRefs(t *testing.T) {
	pgTpl := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata": map[string]any{
			"name": "psql-demo", "namespace": "demo", "uid": "pg-uid",
		},
		"spec": map[string]any{"numberOfInstances": int64(2)},
	}}
	pgLive := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata": map[string]any{
			"name": "psql-demo", "namespace": "demo", "uid": "pg-uid",
		},
		"spec": map[string]any{"numberOfInstances": int64(0)},
	}}
	pgGvk := types.NewGVK(types.Group("acid.zalan.do"), types.Version("v1"), types.Kind("postgresql"))
	pg := mustBuild(entity.NewEntityBuilder().
		WithGVK(pgGvk).
		WithResource(types.Resource("postgresqls")).
		WithName(types.Name("psql-demo")).
		WithNamespace(types.Namespace("demo")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyTemplateEntity, pgTpl).
		WithUnstructured(types.KeyClusterEntity, pgLive))

	stsLive := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]any{
			"name": "psql-demo", "namespace": "demo", "uid": "sts-uid",
		},
		"spec": map[string]any{"replicas": int64(2)},
	}}
	stsGvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))
	sts := mustBuild(entity.NewEntityBuilder().
		WithGVK(stsGvk).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("psql-demo")).
		WithNamespace(types.Namespace("demo")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyClusterEntity, stsLive))

	ents, err := entity.NewEntities([]entity.Entity{pg, sts})
	require.NoError(t, err)
	pgID, err := pg.Id()
	require.NoError(t, err)
	stsID, err := sts.Id()
	require.NoError(t, err)

	refs := []types.Ref{{
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		From:         pgID,
		To:           stsID,
	}}

	out, err := collectClusterOnlyWorkloadEntities(ents, types.KeyTemplateEntity, types.KeyClusterEntity, refs, nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	gvk, err := out[0].GVKString()
	require.NoError(t, err)
	assert.Equal(t, types.KubernetesGvkAppsV1StatefulSet, gvk)
}

func clusterOnlyStatefulSetOwnedByCR(ns, stsUID, crName, crUID string, replicas int64) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]any{
			"name":      "sts",
			"namespace": ns,
			"uid":       stsUID,
			"ownerReferences": []any{
				map[string]any{
					"apiVersion": "acid.zalan.do/v1",
					"kind":       "postgresql",
					"name":       crName,
					"uid":        crUID,
				},
			},
		},
		"spec": map[string]any{"replicas": replicas},
	}}
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("sts")).
		WithNamespace(types.Namespace(ns)).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestCollectClusterOnlyWorkloadEntities_IncludesStatefulSetOwnedByTemplateCR(t *testing.T) {
	crTpl := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata":   map[string]any{"name": "psql", "namespace": "demo", "uid": "cr-uid"},
	}}
	crLive := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata":   map[string]any{"name": "psql", "namespace": "demo", "uid": "cr-uid"},
	}}
	crGvk := types.NewGVK(types.Group("acid.zalan.do"), types.Version("v1"), types.Kind("postgresql"))
	cr := mustBuild(entity.NewEntityBuilder().
		WithGVK(crGvk).
		WithResource(types.Resource("postgresqls")).
		WithName(types.Name("psql")).
		WithNamespace(types.Namespace("demo")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyTemplateEntity, crTpl).
		WithUnstructured(types.KeyClusterEntity, crLive))
	sts := clusterOnlyStatefulSetOwnedByCR("demo", "sts-uid", "psql", "cr-uid", 2)

	ents, err := entity.NewEntities([]entity.Entity{cr, sts})
	require.NoError(t, err)

	out, err := collectClusterOnlyWorkloadEntities(ents, types.KeyTemplateEntity, types.KeyClusterEntity, nil, nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	gvk, err := out[0].GVKString()
	require.NoError(t, err)
	assert.Equal(t, types.KubernetesGvkAppsV1StatefulSet, gvk)
}

func liveClusterOnlyDeployment(namespace, name, uid string, replicas int64) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"uid":       uid,
		},
		"spec": map[string]any{"replicas": replicas},
	}}
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestCollectClusterOnlyWorkloadEntities_ExemptsClusterBuiltinPresetAuditIDs(t *testing.T) {
	pgTpl := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata": map[string]any{
			"name": "psql-demo", "namespace": "demo", "uid": "pg-uid",
		},
		"spec": map[string]any{"numberOfInstances": int64(2)},
	}}
	pgLive := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata": map[string]any{
			"name": "psql-demo", "namespace": "demo", "uid": "pg-uid",
		},
		"spec": map[string]any{"numberOfInstances": int64(0)},
	}}
	pgGvk := types.NewGVK(types.Group("acid.zalan.do"), types.Version("v1"), types.Kind("postgresql"))
	pg := mustBuild(entity.NewEntityBuilder().
		WithGVK(pgGvk).
		WithResource(types.Resource("postgresqls")).
		WithName(types.Name("psql-demo")).
		WithNamespace(types.Namespace("demo")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyTemplateEntity, pgTpl).
		WithUnstructured(types.KeyClusterEntity, pgLive))

	dep := liveClusterOnlyDeployment("kube-system", "coredns", "coredns-dep-uid", 2)
	rs := clusterOnlyReplicaSet("kube-system", "coredns-abc", "rs-uid", "coredns", "coredns-dep-uid", 2)

	ents, err := entity.NewEntities([]entity.Entity{pg, dep, rs})
	require.NoError(t, err)
	pgID, err := pg.Id()
	require.NoError(t, err)
	rsID, err := rs.Id()
	require.NoError(t, err)
	refs := []types.Ref{{
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		From:         pgID,
		To:           rsID,
	}}

	outWithout, err := collectClusterOnlyWorkloadEntities(ents, types.KeyTemplateEntity, types.KeyClusterEntity, refs, nil)
	require.NoError(t, err)
	require.Len(t, outWithout, 1)

	corednsDepID := types.Id("apps/v1/Deployment/kube-system/coredns")
	exempt := sets.New(corednsDepID)
	outWith, err := collectClusterOnlyWorkloadEntities(ents, types.KeyTemplateEntity, types.KeyClusterEntity, refs, exempt)
	require.NoError(t, err)
	assert.Len(t, outWith, 0, "ReplicaSet owned by exempt preset Deployment must not be scaled as cluster-only")
}
