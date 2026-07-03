package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	hlog "hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func testClusterSecretEntity(t *testing.T, name, ns string) entity.Entity {
	t.Helper()
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
			},
		},
	}
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)
	return e
}

func testTemplateConfigMapEntity(t *testing.T, name, ns string) entity.Entity {
	t.Helper()
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
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyTemplateEntity, u).
		Build()
	require.NoError(t, err)
	return e
}

func testClusterConfigMapEntity(t *testing.T, name, ns string) entity.Entity {
	t.Helper()
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
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)
	return e
}

func TestClusterUntrackedEntities(t *testing.T) {
	t.Parallel()

	tpl := testTemplateConfigMapEntity(t, "from-chart", "ns1")
	templateCatalog, err := entity.NewEntities([]entity.Entity{tpl})
	require.NoError(t, err)

	presetMatched := testClusterSecretEntity(t, "preset-hit", "ns1")
	orphan := testClusterSecretEntity(t, "orphan", "ns1")
	rootCA := testClusterConfigMapEntity(t, string(types.Name("kube-root-ca.crt")), "ns1")
	inTemplateLive := testClusterConfigMapEntity(t, "from-chart", "ns1")

	clusterEnts, err := entity.NewEntities([]entity.Entity{presetMatched, orphan, rootCA, inTemplateLive})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(templateCatalog)
	require.NoError(t, err)

	celLine := `gvk == "v1/Secret" && ns == "ns1" && name == "preset-hit"`
	effective := []hydra.ClusterDefaultsPresetEffective{
		hydra.ClusterDefaultsPresetEffectiveTestFixture("stub-preset", true, true, map[string]hydra.ClusterDefaultsPredicateEffective{
			"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: celLine, Optional: false}}},
		}),
	}

	out, err := ClusterUntrackedEntities(clusterEnts, templateCatalog, effective, env, 99, workloadclosure.EmptyMatchInput(types.KeyClusterEntity))
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	id, err := out.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/Secret/ns1/orphan"), id)
}

func TestClusterUntrackedEntities_ExcludesEventRegardingPresetIDAnchor(t *testing.T) {
	t.Parallel()

	templateCatalog, err := entity.NewEntities(nil)
	require.NoError(t, err)

	eventID := types.Id("events.k8s.io/v1/Event/kube-system/coredns.18ac07fdf757ac26")
	addonID := types.Id("k3s.cattle.io/v1/Addon/kube-system/coredns")
	eventU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      "coredns.18ac07fdf757ac26",
			"namespace": "kube-system",
		},
		"regarding": map[string]any{
			"apiVersion": "k3s.cattle.io/v1",
			"kind":       "Addon",
			"name":       "coredns",
			"namespace":  "kube-system",
		},
	}}
	event, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("coredns.18ac07fdf757ac26")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, eventU).
		Build()
	require.NoError(t, err)
	clusterEnts, err := entity.NewEntities([]entity.Entity{event})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(templateCatalog)
	require.NoError(t, err)
	enabled := true
	effective, err := hydra.EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		hydra.ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput([]types.Ref{
		{
			RefType:      types.RefTypeRegarding,
			EndpointType: types.RefEndpointTypeId,
			From:         eventID,
			To:           addonID,
			Labels:       []string{"regarding"},
		},
	}, clusterEnts, types.KeyClusterEntity)
	require.NoError(t, err)

	out, err := ClusterUntrackedEntities(clusterEnts, templateCatalog, effective, env, 99, closure)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len())
}

func TestClusterUntrackedEntities_ExcludesEventRegardingCoreObjectWithoutAPIVersion(t *testing.T) {
	t.Parallel()

	templateCatalog, err := entity.NewEntities(nil)
	require.NoError(t, err)

	node := makeClusterInventoryEntity("", "v1", "Node", "", "k3d-argocd-server-0", "uid-node", nil)
	eventU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      "k3d-argocd-server-0.18ac07fd06f82c9e",
			"namespace": "default",
		},
		"regarding": map[string]any{
			"kind": "Node",
			"name": "k3d-argocd-server-0",
			"uid":  "k3d-argocd-server-0",
		},
	}}
	event, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("k3d-argocd-server-0.18ac07fd06f82c9e")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, eventU).
		Build()
	require.NoError(t, err)
	clusterEnts, err := entity.NewEntities([]entity.Entity{node, event})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(templateCatalog)
	require.NoError(t, err)
	effective, err := hydra.EffectiveClusterDefaultsPresets(nil)
	require.NoError(t, err)
	closure, err := WorkloadClosureMatchInputFromInventory(hlog.Default(), clusterEnts, nil)
	require.NoError(t, err)

	out, err := ClusterUntrackedEntities(clusterEnts, templateCatalog, effective, env, 99, closure)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len())
}

func TestClusterUntrackedEntities_ExcludesObjectsetOwnedPresetChild(t *testing.T) {
	t.Parallel()

	templateCatalog, err := entity.NewEntities(nil)
	require.NoError(t, err)

	apiServiceU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiregistration.k8s.io/v1",
		"kind":       "APIService",
		"metadata": map[string]any{
			"name": "v1beta1.metrics.k8s.io",
			"annotations": map[string]any{
				"objectset.rio.cattle.io/owner-gvk":       "k3s.cattle.io/v1, Kind=Addon",
				"objectset.rio.cattle.io/owner-name":      "metrics-apiservice",
				"objectset.rio.cattle.io/owner-namespace": "kube-system",
			},
		},
		"spec": map[string]any{
			"group": "metrics.k8s.io",
			"service": map[string]any{
				"name":      "metrics-server",
				"namespace": "kube-system",
			},
			"version": "v1beta1",
		},
	}}
	apiService, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apiregistration.k8s.io"), types.Version("v1"), types.Kind("APIService"))).
		WithResource(types.Resource("apiservices")).
		WithName(types.Name("v1beta1.metrics.k8s.io")).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, apiServiceU).
		Build()
	require.NoError(t, err)
	clusterEnts, err := entity.NewEntities([]entity.Entity{apiService})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(templateCatalog)
	require.NoError(t, err)
	enabled := true
	effective, err := hydra.EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		hydra.ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	closure, err := WorkloadClosureMatchInputFromInventory(hlog.Default(), clusterEnts, nil)
	require.NoError(t, err)

	out, err := ClusterUntrackedEntities(clusterEnts, templateCatalog, effective, env, 99, closure)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len())
}

func TestClusterUntrackedEntities_ExcludesEventRegardingMissingPresetIDAnchor(t *testing.T) {
	t.Parallel()

	templateCatalog, err := entity.NewEntities(nil)
	require.NoError(t, err)

	eventU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      "traefik.18ac07ff7692b74f",
			"namespace": "kube-system",
		},
		"regarding": map[string]any{
			"apiVersion": "k3s.cattle.io/v1",
			"kind":       "Addon",
			"name":       "traefik",
			"namespace":  "kube-system",
		},
	}}
	event, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("traefik.18ac07ff7692b74f")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, eventU).
		Build()
	require.NoError(t, err)
	clusterEnts, err := entity.NewEntities([]entity.Entity{event})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(templateCatalog)
	require.NoError(t, err)
	enabled := true
	effective, err := hydra.EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		hydra.ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
		"k3s-addon-traefik":              {Enabled: &enabled},
	})
	require.NoError(t, err)
	closure, err := WorkloadClosureMatchInputFromInventory(hlog.Default(), clusterEnts, nil)
	require.NoError(t, err)

	out, err := ClusterUntrackedEntities(clusterEnts, templateCatalog, effective, env, 99, closure)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len())
}

func TestClusterUntrackedEntities_ExcludesChildWithParentUidInSnapshot(t *testing.T) {
	t.Parallel()

	templateCatalog, err := entity.NewEntities(nil)
	require.NoError(t, err)

	dep := makeClusterInventoryEntity("apps", "v1", "Deployment", "ns1", "web", "uid-dep", nil)
	rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "ns1", "web-hash", "uid-rs", []map[string]any{
		clusterInventoryOwnerRef("apps/v1", "Deployment", "web", "uid-dep"),
	})
	clusterEnts, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(templateCatalog)
	require.NoError(t, err)

	out, err := ClusterUntrackedEntities(clusterEnts, templateCatalog, nil, env, 99, workloadclosure.EmptyMatchInput(types.KeyClusterEntity))
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	id, err := out.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns1/web"), id)
}

func TestResourceModelUntrackedRootClusterEntities_UsesAssignments(t *testing.T) {
	t.Parallel()

	tpl := testTemplateConfigMapEntity(t, "from-chart", "ns1")
	templateCatalog, err := entity.NewEntities([]entity.Entity{tpl})
	require.NoError(t, err)
	assigned := testClusterSecretEntity(t, "assigned", "ns1")
	orphan := testClusterSecretEntity(t, "orphan", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{assigned, orphan, testClusterConfigMapEntity(t, "from-chart", "ns1")})
	require.NoError(t, err)
	model, err := BuildResourceModel(ResourceModelInput{
		TemplateEntities: &templateCatalog,
		ClusterEntities:  &clusterEnts,
		PerAppTemplateEntities: map[types.AppId]entity.Entities{
			"in-cluster.app.test": templateCatalog,
		},
		NetworkMode: types.HelmNetworkModeOffline,
		Bootstrap:   types.BootstrapNo,
	}, false)
	require.NoError(t, err)
	assignedID, err := assigned.Id()
	require.NoError(t, err)
	row := model.rows[assignedID]
	row.AssignedApp = "in-cluster.preset.stub"
	row.HasAssignedApp = true
	model.rows[assignedID] = row

	out, err := model.UntrackedRootClusterEntities()
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	id, err := out.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/Secret/ns1/orphan"), id)
}

func TestNamespacesFromClusterInventoryEntities(t *testing.T) {
	t.Parallel()

	secret := makeClusterInventoryEntity("", "v1", "Secret", "demo", "sj", "u1", nil)
	inventory, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	nsSet := namespacesFromClusterInventoryEntities(inventory)
	require.True(t, nsSet.Has(types.Namespace("demo")))
}

func TestMatchedClusterEntityIdsUninstallStyle_ExactId(t *testing.T) {
	t.Parallel()

	secret := makeClusterInventoryEntity("", "v1", "Secret", "demo", "service-jobs", "u1", nil)
	ents, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(entity.Entities{})
	require.NoError(t, err)

	id, err := secret.Id()
	require.NoError(t, err)
	pred := `id == "` + string(id) + `"`

	matched, err := matchedClusterEntityIdsUninstallStyle(env, ents, []string{pred})
	require.NoError(t, err)
	require.True(t, matched.Has(id))
}

func TestMatchedClusterEntityIdsForceStyle_KindSecret(t *testing.T) {
	t.Parallel()

	secret := makeClusterInventoryEntity("", "v1", "Secret", "demo", "tls", "u2", nil)
	cm := makeClusterInventoryEntity("", "v1", "ConfigMap", "demo", "cfg", "u3", nil)
	ents, err := entity.NewEntities([]entity.Entity{secret, cm})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(entity.Entities{})
	require.NoError(t, err)

	matched, err := matchedClusterEntityIdsForceStyle(env, ents, []string{`kind == "Secret"`})
	require.NoError(t, err)

	idSecret, err := secret.Id()
	require.NoError(t, err)
	idCM, err := cm.Id()
	require.NoError(t, err)
	require.True(t, matched.Has(idSecret))
	require.False(t, matched.Has(idCM))
}

func TestMatchedClusterEntityIdsSafeStyle_RespectsNamespacesGate(t *testing.T) {
	t.Parallel()

	secret := makeClusterInventoryEntity("", "v1", "Secret", "demo", "x", "u4", nil)
	ents, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(
		entity.Entities{},
		cel.SetSupport("namespaces", sets.New[types.Namespace]("demo")),
	)
	require.NoError(t, err)

	matched, err := matchedClusterEntityIdsSafeStyle(env, ents, []string{`kind == "Secret"`})
	require.NoError(t, err)

	id, err := secret.Id()
	require.NoError(t, err)
	require.True(t, matched.Has(id))
}

func TestMatchedClusterEntityIdsSafeStyle_EmptyNamespacesExcludesNamespaced(t *testing.T) {
	t.Parallel()

	secret := makeClusterInventoryEntity("", "v1", "Secret", "demo", "x", "u5", nil)
	ents, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(
		entity.Entities{},
		cel.SetSupport("namespaces", sets.New[types.Namespace]()),
	)
	require.NoError(t, err)

	matched, err := matchedClusterEntityIdsSafeStyle(env, ents, []string{`kind == "Secret"`})
	require.NoError(t, err)
	assert.Equal(t, 0, matched.Len())
}

func TestPruneUntrackedByInventoryRefClosure_PvViaPvcRef(t *testing.T) {
	t.Parallel()

	pvc := makeClusterInventoryEntity("", "v1", "PersistentVolumeClaim", "ns1", "data", "uid-pvc", nil)
	pv := makeClusterInventoryEntity("", "v1", "PersistentVolume", "", "pvc-abc", "uid-pv", nil)
	full, err := entity.NewEntities([]entity.Entity{pvc, pv})
	require.NoError(t, err)
	untracked, err := entity.NewEntities([]entity.Entity{pv})
	require.NoError(t, err)

	pvcID, err := pvc.Id()
	require.NoError(t, err)
	pvID, err := pv.Id()
	require.NoError(t, err)
	refs := []types.Ref{{From: pvcID, To: pvID}}

	out, err := pruneUntrackedByInventoryRefClosure(refs, untracked, full)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len(), "PV bound from a PVC whose root is not untracked must be pruned")
}

func TestPruneUntrackedByInventoryRefClosure_Transitive(t *testing.T) {
	t.Parallel()

	tracked := makeClusterInventoryEntity("", "v1", "ConfigMap", "ns1", "tracked", "uid-a", nil)
	b := makeClusterInventoryEntity("", "v1", "ConfigMap", "ns1", "orphan-b", "uid-b", nil)
	c := makeClusterInventoryEntity("", "v1", "Secret", "ns1", "orphan-c", "uid-c", nil)
	full, err := entity.NewEntities([]entity.Entity{tracked, b, c})
	require.NoError(t, err)
	untracked, err := entity.NewEntities([]entity.Entity{b, c})
	require.NoError(t, err)

	idA, err := tracked.Id()
	require.NoError(t, err)
	idB, err := b.Id()
	require.NoError(t, err)
	idC, err := c.Id()
	require.NoError(t, err)
	refs := []types.Ref{
		{From: idA, To: idB},
		{From: idB, To: idC},
	}

	out, err := pruneUntrackedByInventoryRefClosure(refs, untracked, full)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len())
}

func TestPruneUntrackedByInventoryRefClosure_PodUnderTrackedDepExplainsSecret(t *testing.T) {
	t.Parallel()

	dep := makeClusterInventoryEntity("apps", "v1", "Deployment", "ns1", "web", "uid-dep", nil)
	pod := makeClusterInventoryEntity("", "v1", "Pod", "ns1", "web-abc", "uid-pod", []map[string]any{
		clusterInventoryOwnerRef("apps/v1", "Deployment", "web", "uid-dep"),
	})
	secret := makeClusterInventoryEntity("", "v1", "Secret", "ns1", "data", "uid-sec", nil)
	full, err := entity.NewEntities([]entity.Entity{dep, pod, secret})
	require.NoError(t, err)
	untracked, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	podID, err := pod.Id()
	require.NoError(t, err)
	secID, err := secret.Id()
	require.NoError(t, err)
	refs := []types.Ref{{From: podID, To: secID}}

	out, err := pruneUntrackedByInventoryRefClosure(refs, untracked, full)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len())
}

func TestPruneUntrackedByInventoryRefClosure_KeepsTargetWhenRefSourceRootStillUntracked(t *testing.T) {
	t.Parallel()

	a := makeClusterInventoryEntity("", "v1", "ConfigMap", "ns1", "orphan-a", "uid-a", nil)
	b := makeClusterInventoryEntity("", "v1", "Secret", "ns1", "orphan-b", "uid-b", nil)
	full, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)
	untracked, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	idA, err := a.Id()
	require.NoError(t, err)
	idB, err := b.Id()
	require.NoError(t, err)
	refs := []types.Ref{{From: idA, To: idB}}

	out, err := pruneUntrackedByInventoryRefClosure(refs, untracked, full)
	require.NoError(t, err)
	require.Equal(t, 2, out.Len())
}
