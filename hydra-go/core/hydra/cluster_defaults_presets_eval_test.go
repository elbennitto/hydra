package hydra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func testSecretEntity(name, ns string) (entity.Entity, error) {
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
	return entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
}

func testSecretEntityWithOwnerRefs(name, ns string, refs []map[string]any) (entity.Entity, error) {
	metadata := map[string]any{
		"name":      name,
		"namespace": ns,
	}
	if len(refs) > 0 {
		owners := make([]any, 0, len(refs))
		for _, ref := range refs {
			owners = append(owners, ref)
		}
		metadata["ownerReferences"] = owners
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   metadata,
		},
	}
	return entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
}

func TestClusterDefaultsPresetAuditExpectedIDs_Union(t *testing.T) {
	t.Parallel()
	eff := []ClusterDefaultsPresetEffective{
		{
			ID:      ClusterDefaultsPresetIDKubernetes,
			Enabled: true,
			Predicates: map[string]ClusterDefaultsPredicateEffective{
				"a": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/ConfigMap/ns/x"}}},
				"b": {
					Enabled:            true,
					Ids:                []ClusterDefaultsIdLine{{Id: "rbac.authorization.k8s.io/v1/ClusterRole//system:foo"}},
					KubernetesMinorMin: 31,
				},
			},
		},
	}
	at99 := ClusterDefaultsPresetAuditExpectedIDs(99, eff)
	assert.True(t, at99.Has(types.Id("v1/ConfigMap/ns/x")))
	assert.True(t, at99.Has(types.Id("rbac.authorization.k8s.io/v1/ClusterRole//system:foo")))

	at30 := ClusterDefaultsPresetAuditExpectedIDs(30, eff)
	assert.True(t, at30.Has(types.Id("v1/ConfigMap/ns/x")))
	assert.False(t, at30.Has(types.Id("rbac.authorization.k8s.io/v1/ClusterRole//system:foo")))
}

func TestOmittedExplicitIdsForKubernetesMinor(t *testing.T) {
	t.Parallel()
	eff := ClusterDefaultsPresetEffective{
		ID:      ClusterDefaultsPresetIDKubernetes,
		Enabled: true,
		Predicates: map[string]ClusterDefaultsPredicateEffective{
			"bootstrap-audit-m36": {
				Enabled:            true,
				KubernetesMinorMin: 36,
				Ids: []ClusterDefaultsIdLine{
					{Id: "rbac.authorization.k8s.io/v1/ClusterRole//system:controller:device-taint-eviction-controller"},
				},
			},
		},
	}
	at35 := OmittedExplicitIdsForKubernetesMinor(eff, 35)
	assert.True(t, at35.Has(types.Id("rbac.authorization.k8s.io/v1/ClusterRole//system:controller:device-taint-eviction-controller")))
	at36 := OmittedExplicitIdsForKubernetesMinor(eff, 36)
	assert.Equal(t, 0, at36.Len())
}

func TestClusterDefaultsPresetEvalCache_AgreesWithPresetMatchesEntity(t *testing.T) {
	t.Parallel()
	e, err := testSecretEntity("cache-test", "ns1")
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	eff := ClusterDefaultsPresetEffective{
		ID:      "t",
		Enabled: true,
		Predicates: map[string]ClusterDefaultsPredicateEffective{
			"p": {Enabled: true, CelLines: []ClusterDefaultsCelLine{{Expr: `id == "v1/Secret/ns1/cache-test"`, Optional: false}}},
		},
	}
	effective := []ClusterDefaultsPresetEffective{eff}

	cache, err := NewClusterDefaultsPresetEvalCache(effective, 99, env)
	require.NoError(t, err)
	ids, err := cache.MatchingPresetIDs(e)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.Equal(t, "t", ids[0])

	ok, err := PresetMatchesEntity(eff, 99, env, e)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestClusterDefaultsPresetEvalCache_PrefersActivatedConcretePresetOverActivator(t *testing.T) {
	t.Parallel()

	e, err := testSecretEntity("match-me", "ns1")
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	effective := []ClusterDefaultsPresetEffective{
		{
			ID:      "activator",
			Enabled: true,
			Activates: types.PresetActivateList{
				{Preset: "concrete"},
			},
			Predicates: map[string]ClusterDefaultsPredicateEffective{
				"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/match-me"}}},
			},
		},
		{
			ID:      "concrete",
			Enabled: true,
			Predicates: map[string]ClusterDefaultsPredicateEffective{
				"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/match-me"}}},
			},
		},
	}

	cache, err := NewClusterDefaultsPresetEvalCache(effective, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDs(e)
	require.NoError(t, err)
	assert.Equal(t, []string{"concrete"}, ids)

	byEntity, err := cache.MatchingPresetIDsByEntityWithRegarding(ents, workloadclosure.EmptyMatchInput(types.KeyClusterEntity), nil, nil)
	require.NoError(t, err)
	require.Len(t, byEntity[types.Id("v1/Secret/ns1/match-me")], 1)
	assert.Equal(t, "concrete", byEntity[types.Id("v1/Secret/ns1/match-me")][0].PresetID)
}

func TestClusterDefaultsPresetEvalCache_BatchSkipsTemplateBackedLiveEntities(t *testing.T) {
	t.Parallel()

	live, err := testSecretEntity("templated", "ns1")
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{live})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	effective := []ClusterDefaultsPresetEffective{{
		ID:      "direct",
		Enabled: true,
		Predicates: map[string]ClusterDefaultsPredicateEffective{
			"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/templated"}}},
		},
	}}
	cache, err := NewClusterDefaultsPresetEvalCache(effective, 99, env)
	require.NoError(t, err)

	byEntity, err := cache.MatchingPresetIDsByEntityWithRegardingOptions(
		ents,
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		nil,
		nil,
		&ClusterDefaultsBatchMatchOptions{
			SkipIDs: sets.New[types.Id](types.Id("v1/Secret/ns1/templated")),
			OwnerAppsByID: map[types.Id]sets.Set[types.AppId]{
				types.Id("v1/Secret/ns1/templated"): sets.New[types.AppId]("in-cluster.demo.app"),
			},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, byEntity[types.Id("v1/Secret/ns1/templated")])
}

func TestClusterDefaultsPresetEvalCache_BatchInheritsOwnerAppFromParent(t *testing.T) {
	t.Parallel()

	parent, err := testSecretEntity("parent", "ns1")
	require.NoError(t, err)
	child, err := testSecretEntityWithOwnerRefs("child", "ns1", []map[string]any{{
		"apiVersion": "v1",
		"kind":       "Secret",
		"name":       "parent",
	}})
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{parent, child})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	effective := []ClusterDefaultsPresetEffective{{
		ID:      "direct",
		Enabled: true,
		Predicates: map[string]ClusterDefaultsPredicateEffective{
			"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/child"}}},
		},
	}}
	cache, err := NewClusterDefaultsPresetEvalCache(effective, 99, env)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput(nil, ents, types.KeyClusterEntity)
	require.NoError(t, err)

	byEntity, err := cache.MatchingPresetIDsByEntityWithRegardingOptions(
		ents,
		closure,
		nil,
		nil,
		&ClusterDefaultsBatchMatchOptions{
			SkipIDs: sets.New[types.Id](types.Id("v1/Secret/ns1/parent")),
			OwnerAppsByID: map[types.Id]sets.Set[types.AppId]{
				types.Id("v1/Secret/ns1/parent"): sets.New[types.AppId]("in-cluster.demo.app"),
			},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, byEntity[types.Id("v1/Secret/ns1/child")])
}

func TestClusterDefaultsPresetEvalCache_BatchInheritsUniqueParentPreset(t *testing.T) {
	t.Parallel()

	parent, err := testSecretEntity("parent", "ns1")
	require.NoError(t, err)
	child, err := testSecretEntityWithOwnerRefs("child", "ns1", []map[string]any{{
		"apiVersion": "v1",
		"kind":       "Secret",
		"name":       "parent",
	}})
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{parent, child})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	effective := []ClusterDefaultsPresetEffective{{
		ID:      "parent-preset",
		Enabled: true,
		Predicates: map[string]ClusterDefaultsPredicateEffective{
			"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/parent"}}},
		},
	}}
	cache, err := NewClusterDefaultsPresetEvalCache(effective, 99, env)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput(nil, ents, types.KeyClusterEntity)
	require.NoError(t, err)

	byEntity, err := cache.MatchingPresetIDsByEntityWithRegardingOptions(ents, closure, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, byEntity[types.Id("v1/Secret/ns1/child")], 1)
	assert.Equal(t, "parent-preset", byEntity[types.Id("v1/Secret/ns1/child")][0].PresetID)
	assert.False(t, byEntity[types.Id("v1/Secret/ns1/child")][0].Direct)
	assert.Contains(t, byEntity[types.Id("v1/Secret/ns1/child")][0].Rule, "inheritedOwnerPresetDistance: 1")
}

func TestClusterDefaultsPresetEvalCache_BatchErrorsOnAmbiguousParentPresets(t *testing.T) {
	t.Parallel()

	parentA, err := testSecretEntity("parent-a", "ns1")
	require.NoError(t, err)
	parentB, err := testSecretEntity("parent-b", "ns1")
	require.NoError(t, err)
	child, err := testSecretEntityWithOwnerRefs("child", "ns1", []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "Secret",
			"name":       "parent-a",
		},
		{
			"apiVersion": "v1",
			"kind":       "Secret",
			"name":       "parent-b",
		},
	})
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{parentA, parentB, child})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	effective := []ClusterDefaultsPresetEffective{
		{
			ID:      "preset-a",
			Enabled: true,
			Predicates: map[string]ClusterDefaultsPredicateEffective{
				"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/parent-a"}}},
			},
		},
		{
			ID:      "preset-b",
			Enabled: true,
			Predicates: map[string]ClusterDefaultsPredicateEffective{
				"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/parent-b"}}},
			},
		},
	}
	cache, err := NewClusterDefaultsPresetEvalCache(effective, 99, env)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput(nil, ents, types.KeyClusterEntity)
	require.NoError(t, err)

	_, err = cache.MatchingPresetIDsByEntityWithRegardingOptions(ents, closure, nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "preset owner resolution ambiguous:")
	require.Contains(t, err.Error(), "v1/Secret/ns1/child")
	require.Contains(t, err.Error(), "at distance 1/1: multiple presets")
	require.Contains(t, err.Error(), "preset-a:")
	require.Contains(t, err.Error(), "- id: v1/Secret/ns1/child")
	require.Contains(t, err.Error(), "- id: v1/Secret/ns1/parent-a")
	require.Contains(t, err.Error(), "via: owner-ref")
	require.Contains(t, err.Error(), "preset-b:")
}

func TestClusterDefaultsPresetEvalCache_BatchCollectsAllAmbiguitiesBeforeFailing(t *testing.T) {
	t.Parallel()

	parentA, err := testSecretEntity("parent-a", "ns1")
	require.NoError(t, err)
	parentB, err := testSecretEntity("parent-b", "ns1")
	require.NoError(t, err)
	childA, err := testSecretEntityWithOwnerRefs("child-a", "ns1", []map[string]any{
		{"apiVersion": "v1", "kind": "Secret", "name": "parent-a"},
		{"apiVersion": "v1", "kind": "Secret", "name": "parent-b"},
	})
	require.NoError(t, err)
	childB, err := testSecretEntityWithOwnerRefs("child-b", "ns1", []map[string]any{
		{"apiVersion": "v1", "kind": "Secret", "name": "parent-a"},
		{"apiVersion": "v1", "kind": "Secret", "name": "parent-b"},
	})
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{parentA, parentB, childA, childB})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	effective := []ClusterDefaultsPresetEffective{
		{
			ID:      "preset-a",
			Enabled: true,
			Predicates: map[string]ClusterDefaultsPredicateEffective{
				"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/parent-a"}}},
			},
		},
		{
			ID:      "preset-b",
			Enabled: true,
			Predicates: map[string]ClusterDefaultsPredicateEffective{
				"p": {Enabled: true, Ids: []ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/parent-b"}}},
			},
		},
	}
	cache, err := NewClusterDefaultsPresetEvalCache(effective, 99, env)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput(nil, ents, types.KeyClusterEntity)
	require.NoError(t, err)

	_, err = cache.MatchingPresetIDsByEntityWithRegardingOptions(ents, closure, nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "v1/Secret/ns1/child-a")
	require.Contains(t, err.Error(), "v1/Secret/ns1/child-b")
}

func TestClusterDefaultsPresetEvalCache_BatchOwnerAppAmbiguityShowsDistanceAndRefTypes(t *testing.T) {
	t.Parallel()

	parentA, err := testSecretEntity("parent-a", "ns1")
	require.NoError(t, err)
	parentB, err := testSecretEntity("parent-b", "ns1")
	require.NoError(t, err)
	child, err := testSecretEntityWithOwnerRefs("child", "ns1", []map[string]any{
		{"apiVersion": "v1", "kind": "Secret", "name": "parent-a"},
		{"apiVersion": "v1", "kind": "Secret", "name": "parent-b"},
	})
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{parentA, parentB, child})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	cache, err := NewClusterDefaultsPresetEvalCache(nil, 99, env)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput(nil, ents, types.KeyClusterEntity)
	require.NoError(t, err)

	_, err = cache.MatchingPresetIDsByEntityWithRegardingOptions(
		ents,
		closure,
		nil,
		nil,
		&ClusterDefaultsBatchMatchOptions{
			SkipIDs: sets.New[types.Id](
				types.Id("v1/Secret/ns1/parent-a"),
				types.Id("v1/Secret/ns1/parent-b"),
			),
			OwnerAppsByID: map[types.Id]sets.Set[types.AppId]{
				types.Id("v1/Secret/ns1/parent-a"): sets.New[types.AppId]("in-cluster.demo.a"),
				types.Id("v1/Secret/ns1/parent-b"): sets.New[types.AppId]("in-cluster.demo.b"),
			},
		},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "v1/Secret/ns1/child")
	require.Contains(t, err.Error(), "at distance 1/1: multiple owner apps")
	require.Contains(t, err.Error(), "in-cluster.demo.a:")
	require.Contains(t, err.Error(), "in-cluster.demo.b:")
	require.Contains(t, err.Error(), "- id: v1/Secret/ns1/child")
	require.Contains(t, err.Error(), "- id: v1/Secret/ns1/parent-a")
	require.Contains(t, err.Error(), "- id: v1/Secret/ns1/parent-b")
	require.Contains(t, err.Error(), "via: owner-ref")
}

func TestClusterDefaultsBatchEntityResolver_ParentCandidatesPreferMostSpecificPodMetricsParent(t *testing.T) {
	t.Parallel()

	templateDoc := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: cert-manager
  namespace: cert-manager
spec:
  selector:
    matchLabels:
      app: cert-manager
  template:
    metadata:
      labels:
        app: cert-manager
    spec:
      containers:
        - name: c
          image: busybox
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cert-manager-webhook-hetzner
  namespace: cert-manager
spec:
  selector:
    matchLabels:
      app: cert-manager-webhook-hetzner
  template:
    metadata:
      labels:
        app: cert-manager-webhook-hetzner
    spec:
      containers:
        - name: c
          image: busybox
`
	podMetricsDoc := `apiVersion: metrics.k8s.io/v1beta1
kind: PodMetrics
metadata:
  name: cert-manager-webhook-hetzner-55595f6f98-pq6w7
  namespace: cert-manager
timestamp: "2021-01-01T00:00:00Z"
window: 30s
containers: []
`

	templateEnts, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(templateDoc), types.KeyTemplateEntity)
	require.NoError(t, err)
	overlayEnts, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(podMetricsDoc), types.KeyClusterEntity)
	require.NoError(t, err)
	refs, err := references.Refs(log.Default(), templateEnts, types.KeyTemplateEntity, nil, entity.Entities{}, overlayEnts, nil)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInputWithExtraEntities(refs, overlayEnts, templateEnts, types.KeyClusterEntity)
	require.NoError(t, err)

	liveByID := make(map[types.Id]entity.Entity, overlayEnts.Len())
	for _, item := range overlayEnts.Items {
		id, idErr := item.Id()
		require.NoError(t, idErr)
		liveByID[id] = item
	}

	podMetricsID := types.Id("metrics.k8s.io/v1beta1/PodMetrics/cert-manager/cert-manager-webhook-hetzner-55595f6f98-pq6w7")
	specificDeploymentID := types.Id("apps/v1/Deployment/cert-manager/cert-manager-webhook-hetzner")
	genericDeploymentID := types.Id("apps/v1/Deployment/cert-manager/cert-manager")
	_, hasGenericRef := closure.EntityByID[genericDeploymentID]
	require.True(t, hasGenericRef)
	_, hasSpecificRef := closure.EntityByID[specificDeploymentID]
	require.True(t, hasSpecificRef)

	resolver := &clusterDefaultsBatchEntityResolver{
		closure:  closure,
		liveByID: liveByID,
	}
	parents, err := resolver.parentCandidates(clusterDefaultsParentCandidate{
		id:   podMetricsID,
		path: []clusterDefaultsPathHop{{ID: podMetricsID}},
	})
	require.NoError(t, err)
	require.Len(t, parents, 1)
	assert.Equal(t, specificDeploymentID, parents[0].id)
	assert.Equal(t, workloadclosure.ParentViaFromRefLabel("podMetrics"), parents[0].path[len(parents[0].path)-1].Via)
}

func TestMatchingEntityIdsForCEL(t *testing.T) {
	t.Parallel()
	a, err := testSecretEntity("match-me", "ns1")
	require.NoError(t, err)
	b, err := testSecretEntity("other", "ns1")
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	ids, err := MatchingEntityIdsForCEL(env, ents, `id == "v1/Secret/ns1/match-me"`)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.Contains(t, string(ids[0]), "match-me")
}

func testAPIServiceClusterEntity(name types.Name, serviceNS, serviceName string) (entity.Entity, error) {
	spec := map[string]any{}
	if serviceNS != "" || serviceName != "" {
		spec["service"] = map[string]any{
			"name":      serviceName,
			"namespace": serviceNS,
		}
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apiregistration.k8s.io/v1",
			"kind":       "APIService",
			"metadata": map[string]any{
				"name": string(name),
			},
			"spec": spec,
		},
	}
	return entity.NewEntityBuilder().
		WithGroup("apiregistration.k8s.io").
		WithVersion("v1").
		WithResource("apiservices").
		WithKind("APIService").
		WithName(name).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
}

// Documents kubernetes preset apiregistration-cluster-services: only APIServices without spec.service
// (local kube-apiserver aggregation) match; delegated webhook APIs must not be classified as cluster machinery.
func TestKubernetesPreset_APIServiceDelegatedVsLocalClusterDefaults(t *testing.T) {
	t.Parallel()
	delegated, err := testAPIServiceClusterEntity(types.Name("v1alpha1.acme.example.dev"), "cert-manager", "cert-manager-webhook")
	require.NoError(t, err)
	local, err := testAPIServiceClusterEntity(types.Name("v1.apps"), "", "")
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{delegated, local})
	require.NoError(t, err)

	effs, err := EffectiveClusterDefaultsPresets(nil)
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	idsDel, err := cache.MatchingPresetIDs(delegated)
	require.NoError(t, err)
	assert.NotContains(t, idsDel, ClusterDefaultsPresetIDKubernetes)

	idsLoc, err := cache.MatchingPresetIDs(local)
	require.NoError(t, err)
	assert.Contains(t, idsLoc, ClusterDefaultsPresetIDKubernetes)
}

func TestK3sBuiltinAPIServicesResolveOnlyToKubernetesPreset(t *testing.T) {
	t.Parallel()

	helmAPI, err := testAPIServiceClusterEntity(types.Name("v1.helm.cattle.io"), "", "")
	require.NoError(t, err)
	k3sAPI, err := testAPIServiceClusterEntity(types.Name("v1.k3s.cattle.io"), "", "")
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{helmAPI, k3sAPI})
	require.NoError(t, err)

	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	for _, api := range []entity.Entity{helmAPI, k3sAPI} {
		ids, err := cache.MatchingPresetIDs(api)
		require.NoError(t, err)
		assert.Equal(t, []string{ClusterDefaultsPresetIDK3s}, ids)
	}
}

func TestClusterDefaultsPresetEvalCache_MatchesAddonEventOnlyBaseK3sPreset(t *testing.T) {
	t.Parallel()

	eventID := types.Id("events.k8s.io/v1/Event/kube-system/coredns.18ac07fdf757ac26")
	addonID := types.Id("k3s.cattle.io/v1/Addon/kube-system/coredns")
	u := unstructured.Unstructured{Object: map[string]any{
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
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)

	addonU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "k3s.cattle.io/v1",
		"kind":       "Addon",
		"metadata": map[string]any{
			"name":      "coredns",
			"namespace": "kube-system",
		},
	}}
	addon, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("k3s.cattle.io"), types.Version("v1"), types.Kind("Addon"))).
		WithResource(types.Resource("addons")).
		WithName(types.Name("coredns")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, addonU).
		Build()
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{event, addon})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput([]types.Ref{
		{
			RefType:      types.RefTypeRegarding,
			EndpointType: types.RefEndpointTypeId,
			From:         eventID,
			To:           addonID,
			Labels:       []string{"regarding"},
		},
	}, ents, types.KeyClusterEntity)
	require.NoError(t, err)

	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDsWithRegarding(event, closure, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{ClusterDefaultsPresetIDK3s}, ids)
}

func TestClusterDefaultsPresetEvalCache_DoesNotMatchK3sEventRuleForPodRegardingEvent(t *testing.T) {
	t.Parallel()

	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      "coredns-7566b5ff58-zpk45.18afbe8a40c844a0",
			"namespace": "kube-system",
		},
		"regarding": map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "coredns-7566b5ff58-zpk45",
			"namespace":  "kube-system",
		},
	}}
	event, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("coredns-7566b5ff58-zpk45.18afbe8a40c844a0")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{event})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDsWithRegarding(event, workloadclosure.EmptyMatchInput(types.KeyClusterEntity), nil)
	require.NoError(t, err)
	assert.NotContains(t, ids, ClusterDefaultsPresetIDK3s)
}

func TestClusterDefaultsPresetEvalCache_MatchesAddonEventWithoutConcreteAddonPreset(t *testing.T) {
	t.Parallel()

	eventID := types.Id("events.k8s.io/v1/Event/kube-system/traefik.18ac07ff7692b74f")
	addonID := types.Id("k3s.cattle.io/v1/Addon/kube-system/traefik")
	u := unstructured.Unstructured{Object: map[string]any{
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
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{event})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput([]types.Ref{
		{
			RefType:      types.RefTypeRegarding,
			EndpointType: types.RefEndpointTypeId,
			From:         eventID,
			To:           addonID,
			Labels:       []string{"regarding"},
		},
	}, ents, types.KeyClusterEntity)
	require.NoError(t, err)
	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDsWithRegarding(event, closure, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{ClusterDefaultsPresetIDK3s}, ids)
}

func TestClusterDefaultsPresetEvalCache_MatchesAddonObjectConcretePresetNotBaseK3s(t *testing.T) {
	t.Parallel()

	addonU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "k3s.cattle.io/v1",
		"kind":       "Addon",
		"metadata": map[string]any{
			"name":      "coredns",
			"namespace": "kube-system",
		},
	}}
	addon, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("k3s.cattle.io"), types.Version("v1"), types.Kind("Addon"))).
		WithResource(types.Resource("addons")).
		WithName(types.Name("coredns")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, addonU).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{addon})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDs(addon)
	require.NoError(t, err)
	assert.Equal(t, []string{"k3s-addon-coredns"}, ids)
}

func TestClusterDefaultsPresetEvalCache_MatchesCorednsDeploymentEventOnlyConcreteAddonPreset(t *testing.T) {
	t.Parallel()

	eventID := types.Id("events.k8s.io/v1/Event/kube-system/coredns.18ac07fdf757ac26")
	deployID := types.Id("apps/v1/Deployment/kube-system/coredns")
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      "coredns.18ac07fdf757ac26",
			"namespace": "kube-system",
		},
	}}
	event, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("coredns.18ac07fdf757ac26")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{event})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput([]types.Ref{
		{
			RefType:      types.RefTypeRegarding,
			EndpointType: types.RefEndpointTypeId,
			From:         eventID,
			To:           deployID,
			Labels:       []string{"regarding"},
		},
	}, ents, types.KeyClusterEntity)
	require.NoError(t, err)
	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDsWithRegarding(event, closure, nil)
	require.NoError(t, err)
	assert.Contains(t, ids, "k3s-addon-coredns")
	assert.Contains(t, ids, "coredns")
	assert.NotContains(t, ids, ClusterDefaultsPresetIDK3s)
}

func TestClusterDefaultsPresetEvalCache_MatchesCorednsPodEventOnlyConcreteAddonPreset(t *testing.T) {
	t.Parallel()

	eventID := types.Id("events.k8s.io/v1/Event/kube-system/coredns-65bb548646-jztjg.18ac07fdf757ac26")
	podID := types.Id("v1/Pod/kube-system/coredns-65bb548646-jztjg")
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      "coredns-65bb548646-jztjg.18ac07fdf757ac26",
			"namespace": "kube-system",
		},
	}}
	event, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("coredns-65bb548646-jztjg.18ac07fdf757ac26")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)

	podU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      "coredns-65bb548646-jztjg",
			"namespace": "kube-system",
			"ownerReferences": []any{
				map[string]any{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"name":       "coredns",
				},
			},
		},
	}}
	pod, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("coredns-65bb548646-jztjg")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, podU).
		Build()
	require.NoError(t, err)

	deployU := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      "coredns",
			"namespace": "kube-system",
		},
	}}
	deploy, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("coredns")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, deployU).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{event, pod, deploy})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput([]types.Ref{
		{
			RefType:      types.RefTypeRegarding,
			EndpointType: types.RefEndpointTypeId,
			From:         eventID,
			To:           podID,
			Labels:       []string{"regarding"},
		},
	}, ents, types.KeyClusterEntity)
	require.NoError(t, err)
	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDsWithRegarding(event, closure, nil)
	require.NoError(t, err)
	assert.Contains(t, ids, "k3s-addon-coredns")
	assert.Contains(t, ids, "coredns")
	assert.NotContains(t, ids, ClusterDefaultsPresetIDK3s)
}

func TestClusterDefaultsPresetEvalCache_DebugCorednsDeploymentEventMatches(t *testing.T) {

	eventID := types.Id("events.k8s.io/v1/Event/kube-system/coredns.18ac07fdf757ac26")
	deployID := types.Id("apps/v1/Deployment/kube-system/coredns")
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      "coredns.18ac07fdf757ac26",
			"namespace": "kube-system",
		},
	}}
	event, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("coredns.18ac07fdf757ac26")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{event})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInput([]types.Ref{{
		RefType:      types.RefTypeRegarding,
		EndpointType: types.RefEndpointTypeId,
		From:         eventID,
		To:           deployID,
		Labels:       []string{"regarding"},
	}}, ents, types.KeyClusterEntity)
	require.NoError(t, err)
	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDsWithRegarding(event, closure, nil)
	require.NoError(t, err)
	t.Logf("ids=%v", ids)
	byEntity, err := cache.MatchingPresetIDsByEntityWithRegarding(ents, closure, nil, nil)
	require.NoError(t, err)
	t.Logf("matches=%+v", byEntity[eventID])
}

func TestClusterDefaultsPresetEvalCache_MatchesGenericK3sServiceLBDaemonSetByLabels(t *testing.T) {
	t.Parallel()

	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "DaemonSet",
		"metadata": map[string]any{
			"name":      "svclb-demo-service-a1ba7efd",
			"namespace": "kube-system",
			"labels": map[string]any{
				"svccontroller.k3s.cattle.io/svcname":      "demo-service",
				"svccontroller.k3s.cattle.io/svcnamespace": "demo-namespace",
			},
		},
	}}
	daemonSet, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("DaemonSet"))).
		WithResource(types.Resource("daemonsets")).
		WithName(types.Name("svclb-demo-service-a1ba7efd")).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{daemonSet})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)

	enabled := true
	effs, err := EffectiveClusterDefaultsPresets(&types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {Enabled: &enabled},
	})
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(effs, 99, env)
	require.NoError(t, err)

	ids, err := cache.MatchingPresetIDs(daemonSet)
	require.NoError(t, err)
	assert.Contains(t, ids, ClusterDefaultsPresetIDK3s)
}
