package commands

import (
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestNamespacesAllowingUninstallSafe(t *testing.T) {
	selected := sets.New[types.AppId]("a.app", "b.app")
	stakeholders := map[types.Namespace]sets.Set[types.AppId]{
		"ns1": sets.New[types.AppId]("a.app"),
		"ns2": sets.New[types.AppId]("a.app", "b.app"),
		"ns3": sets.New[types.AppId]("c.app"),
		"ns4": sets.New[types.AppId](),
	}
	out := NamespacesAllowingUninstallSafe(stakeholders, selected)
	assert.True(t, out.Has(types.Namespace("ns1")))
	assert.True(t, out.Has(types.Namespace("ns2")))
	assert.False(t, out.Has(types.Namespace("ns3")))
	assert.True(t, out.Has(types.Namespace("ns4")))
}

func TestIntersectNamespaces(t *testing.T) {
	a := sets.New(types.Namespace("x"), types.Namespace("y"))
	b := sets.New(types.Namespace("y"), types.Namespace("z"))
	out := IntersectNamespaces(a, b)
	assert.True(t, out.Has(types.Namespace("y")))
	assert.False(t, out.Has(types.Namespace("x")))
	assert.False(t, out.Has(types.Namespace("z")))
}

func TestTemplateAppsByNamespace(t *testing.T) {
	cm := makeEntity("", "v1", "ConfigMap", "demo", "c1")
	perApp := map[types.AppId]entity.Entities{
		"clickhouse.app": mustEntities(t, []entity.Entity{cm}),
	}
	m := TemplateAppsByNamespace(perApp)
	require.Contains(t, m, types.Namespace("demo"))
	assert.True(t, m[types.Namespace("demo")].Has(types.AppId("clickhouse.app")))
}

func TestTemplateResourceIDToApp(t *testing.T) {
	cm := makeEntity("", "v1", "ConfigMap", "demo", "cfg")
	perApp := map[types.AppId]entity.Entities{
		"my.app": mustEntities(t, []entity.Entity{cm}),
	}
	m := templateResourceIDToApp(perApp)
	id, err := cm.Id()
	require.NoError(t, err)
	assert.Equal(t, types.AppId("my.app"), m[id])
}

// If live inventory contains the same KafkaTopic under a different apiVersion id (e.g. duplicate
// discovery lists) while templates normalize to v1, templateIDToApp only keys the normalized id —
// the alternate id is not considered "owned" by template lookup.
func TestTemplateResourceIDToApp_SameLogicalTopicDifferentAPIVersion_OnlyNormalizedIdInMap(t *testing.T) {
	tpl := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1"), types.Kind("KafkaTopic"))).
		WithName(types.Name("topic-a")).
		WithNamespace(types.Namespace("demo")))
	otherVersion := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("KafkaTopic"))).
		WithName(types.Name("topic-a")).
		WithNamespace(types.Namespace("demo")))
	perApp := map[types.AppId]entity.Entities{
		"kafka.app": mustEntities(t, []entity.Entity{tpl}),
	}
	m := templateResourceIDToApp(perApp)
	idV1, err := tpl.Id()
	require.NoError(t, err)
	idBeta, err := otherVersion.Id()
	require.NoError(t, err)
	assert.NotEqual(t, idV1, idBeta)
	assert.Contains(t, m, idV1)
	assert.NotContains(t, m, idBeta)
}

func TestReconcileClusterOwnership(t *testing.T) {
	id := types.Id("v1/ConfigMap/ns/res")
	a := types.AppId("a.app")
	b := types.AppId("b.app")

	t.Run("template wins with no ref matches", func(t *testing.T) {
		tpl := map[types.Id]types.AppId{id: a}
		owner, ok, err := reconcileClusterOwnership(tpl, id, sets.New[types.AppId]())
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, a, owner)
	})

	t.Run("template wins when refs match same app only", func(t *testing.T) {
		tpl := map[types.Id]types.AppId{id: a}
		owner, ok, err := reconcileClusterOwnership(tpl, id, sets.New(a))
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, a, owner)
	})

	t.Run("error when ref matches different app than template", func(t *testing.T) {
		tpl := map[types.Id]types.AppId{id: a}
		_, _, err := reconcileClusterOwnership(tpl, id, sets.New(b))
		require.Error(t, err)
		assert.Equal(t, errors.ErrUninstallRefOwnershipConflictsWithTemplate, errors.Id(err))
	})

	t.Run("error when refs match template owner and another app", func(t *testing.T) {
		tpl := map[types.Id]types.AppId{id: a}
		_, _, err := reconcileClusterOwnership(tpl, id, sets.New(a, b))
		require.Error(t, err)
		assert.Equal(t, errors.ErrUninstallRefOwnershipConflictsWithTemplate, errors.Id(err))
	})

	t.Run("no template zero refs is unassigned", func(t *testing.T) {
		tpl := map[types.Id]types.AppId{}
		_, ok, err := reconcileClusterOwnership(tpl, id, sets.New[types.AppId]())
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("no template single ref assigns that app", func(t *testing.T) {
		tpl := map[types.Id]types.AppId{}
		owner, ok, err := reconcileClusterOwnership(tpl, id, sets.New(b))
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, b, owner)
	})

	t.Run("no template multiple refs is ambiguous", func(t *testing.T) {
		tpl := map[types.Id]types.AppId{}
		_, _, err := reconcileClusterOwnership(tpl, id, sets.New(a, b))
		require.Error(t, err)
		assert.Equal(t, errors.ErrUninstallAmbiguousRefOwnership, errors.Id(err))
	})
}

func TestRefOwnershipEventAppsFromAssignedSubjects_BuiltinOnlySubjectsFallThrough(t *testing.T) {
	ingressService := testTemplateEntity("services", "Service", "ingress-nginx", "ingress-nginx-controller", "in-cluster.cluster-infra.ingress-nginx")
	renderedAll := mustEntities(t, []entity.Entity{ingressService})

	daemonSet := makeClusterInventoryEntity("apps", "v1", "DaemonSet", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd", "uid-ds", nil)
	daemonSet = withLiveObject(t, daemonSet, map[string]any{
		"metadata": map[string]any{
			"name":      "svclb-ingress-nginx-controller-a1ba7efd",
			"namespace": "kube-system",
			"uid":       "uid-ds",
			"labels": map[string]any{
				"svccontroller.k3s.cattle.io/svcname":      "ingress-nginx-controller",
				"svccontroller.k3s.cattle.io/svcnamespace": "ingress-nginx",
			},
			"annotations": map[string]any{
				"objectset.rio.cattle.io/owner-gvk":       "/v1, Kind=Service",
				"objectset.rio.cattle.io/owner-name":      "ingress-nginx-controller",
				"objectset.rio.cattle.io/owner-namespace": "ingress-nginx",
			},
		},
	})
	pod := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd-xyz", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "DaemonSet", "svclb-ingress-nginx-controller-a1ba7efd", "uid-ds")})
	event := testLiveEntity("events.k8s.io/v1", "events", "Event", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd.1234")
	event = withLiveObject(t, event, map[string]any{
		"regarding": map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "svclb-ingress-nginx-controller-a1ba7efd-xyz",
			"namespace":  "kube-system",
		},
		"reason":              "Pulling",
		"action":              "Pulling",
		"reportingController": "kubelet",
		"reportingInstance":   "node-1",
		"type":                "Normal",
		"note":                "test",
	})

	liveEnts := mustEntities(t, []entity.Entity{daemonSet, pod, event})
	refs, err := references.Refs(log.Default(), liveEnts, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, nil)
	require.NoError(t, err)
	closure, err := WorkloadClosureMatchInputFromMergedInventory(refs, liveEnts, renderedAll)
	require.NoError(t, err)

	podID, err := pod.Id()
	require.NoError(t, err)
	apps, found, err := refOwnershipEventAppsFromAssignedSubjects(
		event,
		closure,
		map[types.Id]types.AppId{podID: "in-cluster.preset.k3s"},
		templateResourceIDToApp(map[types.AppId]entity.Entities{
			"in-cluster.cluster-infra.ingress-nginx": renderedAll,
		}),
		true,
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 0, apps.Len(), "builtin-only subject ownership should fall through to predicate matching")
}

func TestRefOwnershipMatchingAppsCompiledFast_PrefersSpecificAppsOverBuiltins(t *testing.T) {
	ingressApp := types.AppId("in-cluster.cluster-infra.ingress-nginx")
	k3sApp := types.AppId("in-cluster.preset.k3s")

	ingressService := testTemplateEntity("services", "Service", "ingress-nginx", "ingress-nginx-controller", ingressApp)
	renderedIngress := mustEntities(t, []entity.Entity{ingressService})
	emptyRendered := mustEntities(t, nil)

	event := testLiveEntity("events.k8s.io/v1", "events", "Event", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd.1234")
	event = withLiveObject(t, event, map[string]any{
		"metadata": map[string]any{
			"name":      "svclb-ingress-nginx-controller-a1ba7efd.1234",
			"namespace": "kube-system",
			"labels": map[string]any{
				"svccontroller.k3s.cattle.io/svcname":      "ingress-nginx-controller",
				"svccontroller.k3s.cattle.io/svcnamespace": "ingress-nginx",
			},
			"annotations": map[string]any{
				"objectset.rio.cattle.io/owner-name": "ingress-nginx-controller",
			},
		},
		"regarding": map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "svclb-ingress-nginx-controller-a1ba7efd-xyz",
			"namespace":  "kube-system",
		},
	})
	liveEnts := mustEntities(t, []entity.Entity{event})
	closure, err := WorkloadClosureMatchInputFromMergedInventory(nil, liveEnts, renderedIngress)
	require.NoError(t, err)

	ingressEnv, err := cel.NewEnvWithEntityInventoryOverlay(renderedIngress, liveEnts)
	require.NoError(t, err)
	k3sEnv, err := cel.NewEnvWithEntityInventoryOverlay(emptyRendered, liveEnts)
	require.NoError(t, err)
	ingressPred, err := ingressEnv.CompilePredicate(types.CelPredicate(`clusterEntity.annotations().getOrEmpty("objectset.rio.cattle.io/owner-name") == "ingress-nginx-controller"`))
	require.NoError(t, err)
	k3sPred, err := k3sEnv.CompilePredicate(types.CelPredicate(`name.startsWith("svclb-") && clusterEntity.labels().getOrEmpty("svccontroller.k3s.cattle.io/svcname") != ""`))
	require.NoError(t, err)

	compiled := map[types.AppId][]compiledRefOwnershipPredicate{
		k3sApp: {{
			pred:   k3sPred,
			source: types.RefOwnershipPredicateLine{Cel: `name.startsWith("svclb-")`},
		}},
		ingressApp: {{
			pred:   ingressPred,
			source: types.RefOwnershipPredicateLine{Cel: `clusterEntity.annotations().getOrEmpty("objectset.rio.cattle.io/owner-name") == "ingress-nginx-controller"`},
		}},
	}
	appOrder := []types.AppId{k3sApp, ingressApp}

	matchingFast, err := refOwnershipMatchingAppsCompiledFast(event, appOrder, compiled, nil, closure)
	require.NoError(t, err)
	matchingDetailed, _, err := refOwnershipMatchingAppsCompiledWithReasons(event, appOrder, compiled, nil, closure)
	require.NoError(t, err)

	assert.Equal(t, sets.New[types.AppId](ingressApp), matchingFast)
	assert.Equal(t, matchingDetailed, matchingFast)
}

func TestRefOwnershipMatchingAppsCompiled_PrefersHigherPriorityEventSubject(t *testing.T) {
	kyvernoApp := types.AppId("in-cluster.cluster-infra.kyverno")
	sopsApp := types.AppId("in-cluster.cluster-infra.sops-secrets-operator")

	clusterPolicy := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK("kyverno.io", "v1", "ClusterPolicy")).
		WithResource("clusterpolicies").
		WithName("generate-clone-image-pull-secret-policy").
		WithAppId(kyvernoApp))
	sourceSecret := testLiveEntity("v1", "secrets", "Secret", "sops-secrets-operator", "image-pull-secret")
	event := testLiveEntity("events.k8s.io/v1", "events", "Event", "default", "generate-clone-image-pull-secret-policy.18b0e53143c6027e")
	event = withLiveObject(t, event, map[string]any{
		"regarding": map[string]any{
			"apiVersion": "kyverno.io/v1",
			"kind":       "ClusterPolicy",
			"name":       "generate-clone-image-pull-secret-policy",
		},
		"related": map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"name":       "image-pull-secret",
			"namespace":  "sops-secrets-operator",
		},
	})

	renderedKyverno := mustEntities(t, []entity.Entity{clusterPolicy})
	liveEnts := mustEntities(t, []entity.Entity{event, sourceSecret})
	eventID, err := event.Id()
	require.NoError(t, err)
	clusterPolicyID, err := clusterPolicy.Id()
	require.NoError(t, err)
	sourceSecretID, err := sourceSecret.Id()
	require.NoError(t, err)
	closure, err := workloadclosure.NewMatchInputWithExtraEntities(
		[]types.Ref{
			{From: eventID, To: sourceSecretID, EndpointType: types.RefEndpointTypeId, Labels: []string{"related"}},
			{From: eventID, To: clusterPolicyID, EndpointType: types.RefEndpointTypeId, RefType: types.RefTypeRegarding},
		},
		liveEnts,
		renderedKyverno,
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	kyvernoEnv, err := cel.NewEnvWithEntityInventoryOverlay(renderedKyverno, liveEnts)
	require.NoError(t, err)
	sopsEnv, err := cel.NewEnvWithEntityInventoryOverlay(mustEntities(t, nil), liveEnts)
	require.NoError(t, err)

	kyvernoPred, err := kyvernoEnv.CompilePredicate(types.CelPredicate(`gvk == "kyverno.io/v1/ClusterPolicy"`))
	require.NoError(t, err)
	sopsPred, err := sopsEnv.CompilePredicate(types.CelPredicate(`gvk == "v1/Secret" && ns == "sops-secrets-operator" && name == "image-pull-secret"`))
	require.NoError(t, err)
	kyvernoMatches, err := closure.PredicateMatches(clusterPolicy, kyvernoPred)
	require.NoError(t, err)
	require.True(t, kyvernoMatches)
	sopsMatches, err := closure.PredicateMatches(sourceSecret, sopsPred)
	require.NoError(t, err)
	require.True(t, sopsMatches)

	compiled := map[types.AppId][]compiledRefOwnershipPredicate{
		kyvernoApp: {{
			pred: kyvernoPred,
			source: types.RefOwnershipPredicateLine{
				Cel:      `gvk == "kyverno.io/v1/ClusterPolicy"`,
				Priority: -1,
			},
		}},
		sopsApp: {{
			pred: sopsPred,
			source: types.RefOwnershipPredicateLine{
				Cel: `gvk == "v1/Secret" && ns == "sops-secrets-operator" && name == "image-pull-secret"`,
			},
		}},
	}
	appOrder := []types.AppId{kyvernoApp, sopsApp}

	matchingFast, err := refOwnershipMatchingAppsCompiledFast(event, appOrder, compiled, nil, closure)
	require.NoError(t, err)
	matchingDetailed, reasonsByApp, err := refOwnershipMatchingAppsCompiledWithReasons(event, appOrder, compiled, nil, closure)
	require.NoError(t, err)

	assert.Equal(t, sets.New[types.AppId](sopsApp), matchingFast)
	assert.Equal(t, matchingFast, matchingDetailed)
	assert.Contains(t, reasonsByApp, sopsApp)
	assert.NotContains(t, reasonsByApp, kyvernoApp)
}

func TestAssignWithDetailedReasons_PreservesEventRouteForInitialOwner(t *testing.T) {
	assignment := map[types.Id]types.AppId{}
	assignmentReasons := map[types.Id]map[types.AppId][]AssignmentReason{}
	eventID := types.Id("events.k8s.io/v1/Event/default/generate-clone-image-pull-secret-policy.18b0b480cacd026b")
	appID := types.AppId("in-cluster.cluster-infra.sops-secrets-operator")
	reasons := []AssignmentReason{{
		Kind:          AssignmentReasonKindAssignedViaRefOwnership,
		EventRef:      "regarding",
		EventSubjects: []types.Id{"kyverno.io/v1/ClusterPolicy//generate-clone-image-pull-secret-policy"},
	}}

	assignWithDetailedReasons(assignment, assignmentReasons, eventID, appID, reasons)

	require.Equal(t, appID, assignment[eventID])
	require.Contains(t, assignmentReasons, eventID)
	require.Equal(t, reasons, assignmentReasons[eventID][appID])

	assignWithDetailedReasons(assignment, assignmentReasons, eventID, appID, reasons)
	require.Equal(t, reasons, assignmentReasons[eventID][appID], "duplicate detailed event reasons must stay deduplicated")
}

func TestRefOwnershipEventAppsFromAssignedSubjects_OrphanServiceLBSubjectUsesServiceAnchor(t *testing.T) {
	ingressApp := types.AppId("in-cluster.cluster-infra.ingress-nginx")
	ingressService := testTemplateEntity("services", "Service", "ingress-nginx", "ingress-nginx-controller", ingressApp)
	renderedAll := mustEntities(t, []entity.Entity{ingressService})

	event := testLiveEntity("events.k8s.io/v1", "events", "Event", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd.1234")
	event = withLiveObject(t, event, map[string]any{
		"regarding": map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "svclb-ingress-nginx-controller-a1ba7efd-xyz",
			"namespace":  "kube-system",
		},
		"reason":              "Pulling",
		"action":              "Pulling",
		"reportingController": "kubelet",
		"reportingInstance":   "node-1",
		"type":                "Normal",
		"note":                "test",
	})

	liveEnts := mustEntities(t, []entity.Entity{event})
	refs, err := references.Refs(log.Default(), liveEnts, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, nil)
	require.NoError(t, err)
	closure, err := WorkloadClosureMatchInputFromMergedInventory(refs, liveEnts, renderedAll)
	require.NoError(t, err)

	apps, found, err := refOwnershipEventAppsFromAssignedSubjects(
		event,
		closure,
		map[types.Id]types.AppId{},
		templateResourceIDToApp(map[types.AppId]entity.Entities{ingressApp: renderedAll}),
		true,
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sets.New[types.AppId](ingressApp), apps)
}

func TestRefOwnershipEventAppsFromAssignedSubjects_OrphanAddonTemplateAnchorAssignsBuiltinApp(t *testing.T) {
	builtinApp := types.AppId("in-cluster.preset.k3s-addon-coredns")
	addon := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("k3s.cattle.io"), types.Version("v1"), types.Kind("Addon"))).
		WithResource(types.Resource("addons")).
		WithName(types.Name("coredns")).
		WithNamespace(types.Namespace("kube-system")).
		WithAppId(builtinApp))
	renderedAll := mustEntities(t, []entity.Entity{addon})

	event := testLiveEntity("events.k8s.io/v1", "events", "Event", "kube-system", "coredns.1234")
	event = withLiveObject(t, event, map[string]any{
		"regarding": map[string]any{
			"apiVersion": "k3s.cattle.io/v1",
			"kind":       "Addon",
			"name":       "coredns",
			"namespace":  "kube-system",
		},
		"reason":              "Pulled",
		"action":              "Pull",
		"reportingController": "k3s",
		"reportingInstance":   "node-1",
		"type":                "Normal",
		"note":                "test",
	})

	liveEnts := mustEntities(t, []entity.Entity{event})
	refs, err := references.Refs(log.Default(), liveEnts, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, nil)
	require.NoError(t, err)
	closure, err := WorkloadClosureMatchInputFromMergedInventory(refs, liveEnts, renderedAll)
	require.NoError(t, err)

	apps, found, err := refOwnershipEventAppsFromAssignedSubjects(
		event,
		closure,
		map[types.Id]types.AppId{},
		templateResourceIDToApp(map[types.AppId]entity.Entities{builtinApp: renderedAll}),
		true,
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, sets.New[types.AppId](builtinApp), apps)
}

func TestRefOwnershipMatchingAppsCompiledWithReasons_PrefersSpecificAppOverBuiltinForServiceLBEvent(t *testing.T) {
	ingressApp := types.AppId("in-cluster.cluster-infra.ingress-nginx")
	k3sApp := types.AppId("in-cluster.preset.k3s")
	ingressService := testTemplateEntity("services", "Service", "ingress-nginx", "ingress-nginx-controller", ingressApp)
	renderedIngress := mustEntities(t, []entity.Entity{ingressService})
	emptyRendered := mustEntities(t, nil)

	daemonSet := makeClusterInventoryEntity("apps", "v1", "DaemonSet", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd", "uid-ds", nil)
	daemonSet = withLiveObject(t, daemonSet, map[string]any{
		"metadata": map[string]any{
			"name":      "svclb-ingress-nginx-controller-a1ba7efd",
			"namespace": "kube-system",
			"uid":       "uid-ds",
			"labels": map[string]any{
				"svccontroller.k3s.cattle.io/svcname":      "ingress-nginx-controller",
				"svccontroller.k3s.cattle.io/svcnamespace": "ingress-nginx",
			},
			"annotations": map[string]any{
				"objectset.rio.cattle.io/owner-gvk":       "/v1, Kind=Service",
				"objectset.rio.cattle.io/owner-name":      "ingress-nginx-controller",
				"objectset.rio.cattle.io/owner-namespace": "ingress-nginx",
			},
		},
	})
	pod := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd-xyz", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "DaemonSet", "svclb-ingress-nginx-controller-a1ba7efd", "uid-ds")})
	event := testLiveEntity("events.k8s.io/v1", "events", "Event", "kube-system", "svclb-ingress-nginx-controller-a1ba7efd.1234")
	event = withLiveObject(t, event, map[string]any{
		"regarding": map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "svclb-ingress-nginx-controller-a1ba7efd-xyz",
			"namespace":  "kube-system",
		},
		"reason":              "Pulling",
		"action":              "Pulling",
		"reportingController": "kubelet",
		"reportingInstance":   "node-1",
		"type":                "Normal",
		"note":                "test",
	})

	liveEnts := mustEntities(t, []entity.Entity{daemonSet, pod, event})
	refs, err := references.Refs(log.Default(), liveEnts, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, nil)
	require.NoError(t, err)
	renderedAll := mustEntities(t, []entity.Entity{ingressService})
	closure, err := WorkloadClosureMatchInputFromMergedInventory(refs, liveEnts, renderedAll)
	require.NoError(t, err)

	ingressEnv, err := cel.NewEnvWithEntityInventoryOverlay(renderedIngress, liveEnts)
	require.NoError(t, err)
	k3sEnv, err := cel.NewEnvWithEntityInventoryOverlay(emptyRendered, liveEnts)
	require.NoError(t, err)
	ingressPred, err := ingressEnv.CompilePredicate(types.CelPredicate(`clusterEntity.annotations().getOrEmpty("objectset.rio.cattle.io/owner-name") == "ingress-nginx-controller"`))
	require.NoError(t, err)
	k3sPred, err := k3sEnv.CompilePredicate(types.CelPredicate(`name.startsWith("svclb-") && clusterEntity.labels().getOrEmpty("svccontroller.k3s.cattle.io/svcname") != "" && clusterEntity.labels().getOrEmpty("svccontroller.k3s.cattle.io/svcnamespace") != ""`))
	require.NoError(t, err)

	matching, _, err := refOwnershipMatchingAppsCompiledWithReasons(
		event,
		[]types.AppId{k3sApp, ingressApp},
		map[types.AppId][]compiledRefOwnershipPredicate{
			k3sApp: {{
				pred:   k3sPred,
				source: types.RefOwnershipPredicateLine{Cel: `name.startsWith("svclb-")`},
			}},
			ingressApp: {{
				pred: ingressPred,
				source: types.RefOwnershipPredicateLine{
					Cel: `clusterEntity.annotations().getOrEmpty("objectset.rio.cattle.io/owner-name") == "ingress-nginx-controller"`,
				},
			}},
		},
		nil,
		closure,
	)
	require.NoError(t, err)
	assert.Equal(t, sets.New[types.AppId](ingressApp), matching)
}

func TestRefOwnershipScanOneLiveItem_FallbackRuleIgnoredForTemplateTrackedResource(t *testing.T) {
	templateOwner := types.AppId("in-cluster.cluster-infra.cert-manager")
	fallbackOwner := types.AppId("in-cluster.argocd")
	renderedFallback := mustEntities(t, nil)
	live := makeClusterInventoryEntity("argoproj.io", "v1alpha1", "AppProject", "argocd", "in-cluster.cluster-infra.cert-manager", "uid-appproject", nil)
	liveID, err := live.Id()
	require.NoError(t, err)

	fallbackEnv, err := cel.NewEnvWithEntityInventoryOverlay(renderedFallback, mustEntities(t, []entity.Entity{live}))
	require.NoError(t, err)
	fallbackPred, err := fallbackEnv.CompilePredicate(types.CelPredicate(`group == "argoproj.io"`))
	require.NoError(t, err)

	outcome := refOwnershipScanOneLiveItem(
		live,
		30,
		map[types.Id]types.AppId{liveID: templateOwner},
		[]types.AppId{fallbackOwner},
		map[types.AppId][]compiledRefOwnershipPredicate{
			fallbackOwner: {{
				pred: fallbackPred,
				source: types.RefOwnershipPredicateLine{
					Cel:      `group == "argoproj.io"`,
					Priority: -1,
				},
			}},
		},
		map[types.AppId][]compiledRefOwnershipPredicate{
			fallbackOwner: {{
				pred: fallbackPred,
				source: types.RefOwnershipPredicateLine{
					Cel:      `group == "argoproj.io"`,
					Priority: -1,
				},
			}},
		},
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
	)
	require.NoError(t, outcome.err)
	assert.False(t, outcome.ambiguous)
	assert.True(t, outcome.assign)
	assert.Equal(t, liveID, outcome.eid)
	assert.Equal(t, templateOwner, outcome.owner)
}

func TestRefOwnershipScanOneLiveItem_FallbackRuleAssignsClusterOnlyResource(t *testing.T) {
	fallbackOwner := types.AppId("in-cluster.argocd")
	live := makeClusterInventoryEntity("argoproj.io", "v1alpha1", "AppProject", "argocd", "in-cluster.cluster-infra.cert-manager", "uid-appproject", nil)
	liveEnts := mustEntities(t, []entity.Entity{live})

	fallbackEnv, err := cel.NewEnvWithEntityInventoryOverlay(mustEntities(t, nil), liveEnts)
	require.NoError(t, err)
	fallbackPred, err := fallbackEnv.CompilePredicate(types.CelPredicate(`group == "argoproj.io"`))
	require.NoError(t, err)

	outcome := refOwnershipScanOneLiveItem(
		live,
		30,
		map[types.Id]types.AppId{},
		[]types.AppId{fallbackOwner},
		map[types.AppId][]compiledRefOwnershipPredicate{},
		map[types.AppId][]compiledRefOwnershipPredicate{
			fallbackOwner: {{
				pred: fallbackPred,
				source: types.RefOwnershipPredicateLine{
					Cel:      `group == "argoproj.io"`,
					Priority: -1,
				},
			}},
		},
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
	)
	require.NoError(t, outcome.err)
	assert.False(t, outcome.ambiguous)
	assert.True(t, outcome.assign)
	assert.Equal(t, fallbackOwner, outcome.owner)
}

// TestAssignUnassignedClusterEntitiesInSoleTemplateNamespaces_AssignsImplicitNamespaceRoot verifies that a
// live v1/Namespace object is assigned to the sole app that renders workloads into that namespace,
// matching implicit Helm namespace creation without a Namespace manifest in templates.
func TestAssignUnassignedClusterEntitiesInSoleTemplateNamespaces_AssignsImplicitNamespaceRoot(t *testing.T) {
	appID := types.AppId("sole.app")
	cm := makeEntity("", "v1", "ConfigMap", "argocd", "root")
	tpl, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	perApp := map[types.AppId]entity.Entities{appID: tpl}
	nsLive := clusterNamespaceEntity("argocd", "uid-1")
	live, err := entity.NewEntities([]entity.Entity{nsLive})
	require.NoError(t, err)
	assignment := map[types.Id]types.AppId{}
	unassigned, err := clusterInventoryUnassignedForAbort(live, assignment, types.KeyClusterEntity)
	require.NoError(t, err)
	kept, err := assignUnassignedClusterEntitiesInSoleTemplateNamespaces(unassigned, assignment, perApp)
	require.NoError(t, err)
	assert.Equal(t, 0, kept.Len())
	nsID, err := nsLive.Id()
	require.NoError(t, err)
	assert.Equal(t, appID, assignment[nsID])
}

func TestApplyOwnerNamespacesToNamespaceAssignments(t *testing.T) {
	a := types.AppId("a.app")
	b := types.AppId("b.app")
	ns := clusterNamespaceEntity("owned-ns", "uid-1")
	nsID, err := ns.Id()
	require.NoError(t, err)
	templateIDToApp := map[types.Id]types.AppId{}
	assignment := map[types.Id]types.AppId{}
	live, err := entity.NewEntities([]entity.Entity{ns})
	require.NoError(t, err)
	owners := map[types.Namespace]types.AppId{types.Namespace("owned-ns"): a}
	require.NoError(t, ApplyOwnerNamespacesToNamespaceAssignments(templateIDToApp, owners, live, assignment, sets.New[types.Id](), nil))
	assert.Equal(t, a, assignment[nsID])

	assignment2 := map[types.Id]types.AppId{nsID: b}
	err = ApplyOwnerNamespacesToNamespaceAssignments(templateIDToApp, owners, live, assignment2, sets.New[types.Id](), nil)
	require.Error(t, err)
	assert.Equal(t, errors.ErrUninstallAmbiguousRefOwnership, errors.Id(err))
}

func TestExpandAssignmentByOwnerRefs_TransitiveChain(t *testing.T) {
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "default", "web", "uid-deploy", nil)
	rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "default", "web-abc", "uid-rs",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "web", "uid-deploy")})
	pod := makeClusterInventoryEntity("", "v1", "Pod", "default", "web-abc-xyz", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "web-abc", "uid-rs")})

	ents, err := entity.NewEntities([]entity.Entity{deploy, rs, pod})
	require.NoError(t, err)

	deployID, err := deploy.Id()
	require.NoError(t, err)
	rsID, err := rs.Id()
	require.NoError(t, err)
	podID, err := pod.Id()
	require.NoError(t, err)

	app := types.AppId("my.app")
	assignment := map[types.Id]types.AppId{deployID: app}
	require.NoError(t, expandAssignmentByOwnerRefs(ents, assignment, types.KeyClusterEntity, nil))

	assert.Equal(t, app, assignment[deployID])
	assert.Equal(t, app, assignment[rsID])
	assert.Equal(t, app, assignment[podID])
}

func TestPendingRefOwnershipEntityIndexesRootFirstAndAssignOwnerRefDescendants(t *testing.T) {
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "default", "web", "uid-deploy", nil)
	rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "default", "web-abc", "uid-rs",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "web", "uid-deploy")})
	pod := makeClusterInventoryEntity("", "v1", "Pod", "default", "web-abc-xyz", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "web-abc", "uid-rs")})

	ents, err := entity.NewEntities([]entity.Entity{pod, rs, deploy})
	require.NoError(t, err)

	deployID, err := deploy.Id()
	require.NoError(t, err)
	rsID, err := rs.Id()
	require.NoError(t, err)
	podID, err := pod.Id()
	require.NoError(t, err)

	childIndex, err := buildOwnerRefChildIndex(ents, types.KeyClusterEntity)
	require.NoError(t, err)
	indexes, err := pendingRefOwnershipEntityIndexesRootFirst(ents, map[types.Id]types.AppId{}, sets.New[types.Id](), nil, childIndex)
	require.NoError(t, err)
	require.Len(t, indexes, 3)

	firstID, err := ents.Items[indexes[0]].Id()
	require.NoError(t, err)
	assert.Equal(t, deployID, firstID, "root owner must be tested before ownerRef children")

	app := types.AppId("in-cluster.root.app")
	assignment := map[types.Id]types.AppId{deployID: app}
	assignOwnerRefDescendants(deployID, app, assignment, sets.New[types.Id](), childIndex, nil)
	assert.Equal(t, app, assignment[rsID])
	assert.Equal(t, app, assignment[podID])
}

func TestExpandAssignmentByOwnerRefsSoft_AssignsRootFirstChildren(t *testing.T) {
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "default", "web", "uid-deploy", nil)
	rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "default", "web-abc", "uid-rs",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "web", "uid-deploy")})
	pod := makeClusterInventoryEntity("", "v1", "Pod", "default", "web-abc-xyz", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "web-abc", "uid-rs")})

	ents, err := entity.NewEntities([]entity.Entity{pod, rs, deploy})
	require.NoError(t, err)

	deployID, err := deploy.Id()
	require.NoError(t, err)
	rsID, err := rs.Id()
	require.NoError(t, err)
	podID, err := pod.Id()
	require.NoError(t, err)

	app := types.AppId("in-cluster.root.app")
	assignment := map[types.Id]types.AppId{deployID: app}
	var ambiguous []OwnerRefAmbiguousAssignment
	require.NoError(t, expandAssignmentByOwnerRefsSoft(ents, assignment, types.KeyClusterEntity, nil, &ambiguous))
	assert.Empty(t, ambiguous)
	assert.Equal(t, app, assignment[rsID])
	assert.Equal(t, app, assignment[podID])
}

// Regression: uninstall abort lists PodMetrics as standalone when it never inherits an app via the
// merged inspect graph. Ownership flows Deployment → ReplicaSet → Pod (ownerReferences), then
// Pod → PodMetrics (ref-parser podMetrics). clusterInventoryUnassignedForAbort skips Pods that still
// have ownerReferences even when expansion did not assign them — so an unassigned Pod breaks
// PodMetrics propagation and surfaces only PodMetrics in the WARN list.
func TestPodMetricsInheritsAppViaMergedInspectRefsAfterOwnerRefChain(t *testing.T) {
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "kube-system", "local-path-provisioner", "uid-deploy", nil)
	rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "kube-system", "local-path-provisioner-6bc6568469", "uid-rs",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "local-path-provisioner", "uid-deploy")})
	pod := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "local-path-provisioner-6bc6568469-cj589", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "local-path-provisioner-6bc6568469", "uid-rs")})
	pm := makeClusterInventoryEntity("metrics.k8s.io", "v1beta1", "PodMetrics", "kube-system", "local-path-provisioner-6bc6568469-cj589", "uid-pm", nil)

	ents, err := entity.NewEntities([]entity.Entity{deploy, rs, pod, pm})
	require.NoError(t, err)

	deployID, err := deploy.Id()
	require.NoError(t, err)
	podID, err := pod.Id()
	require.NoError(t, err)
	pmID, err := pm.Id()
	require.NoError(t, err)

	app := types.AppId("local-path-provisioner.app")
	assignment := map[types.Id]types.AppId{deployID: app}
	require.NoError(t, expandAssignmentByOwnerRefs(ents, assignment, types.KeyClusterEntity, nil))
	require.Equal(t, app, assignment[podID], "pod should inherit app from Deployment via RS owner chain")

	refs := []types.Ref{{From: podID, To: pmID, Labels: []string{"podMetrics"}}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, app, assignment[pmID])

	unassigned, uerr := clusterInventoryUnassignedForAbort(ents, assignment, types.KeyClusterEntity)
	require.NoError(t, uerr)
	assert.Empty(t, unassigned.Items, "PodMetrics must not remain standalone once the owning Pod is assigned")
}

// If the ReplicaSet (or other direct owner) is missing from the live inventory UidMap and its
// cluster id is not present in template ownership, expandAssignmentByOwnerRefs never assigns the Pod,
// merged inspect cannot propagate to PodMetrics, and PodMetrics remains on the uninstall abort list
// (the Pod is skipped there because it still has ownerReferences).
func TestPodMetricsStaysUnassignedWhenReplicaSetMissingFromInventory(t *testing.T) {
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "kube-system", "local-path-provisioner", "uid-deploy", nil)
	pod := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "local-path-provisioner-6bc6568469-cj589", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "local-path-provisioner-6bc6568469", "uid-rs-not-listed")})
	pm := makeClusterInventoryEntity("metrics.k8s.io", "v1beta1", "PodMetrics", "kube-system", "local-path-provisioner-6bc6568469-cj589", "uid-pm", nil)

	ents, err := entity.NewEntities([]entity.Entity{deploy, pod, pm})
	require.NoError(t, err)

	deployID, err := deploy.Id()
	require.NoError(t, err)
	podID, err := pod.Id()
	require.NoError(t, err)
	pmID, err := pm.Id()
	require.NoError(t, err)

	app := types.AppId("local-path-provisioner.app")
	assignment := map[types.Id]types.AppId{deployID: app}
	require.NoError(t, expandAssignmentByOwnerRefs(ents, assignment, types.KeyClusterEntity, nil))
	assert.NotContains(t, assignment, podID)

	refs := []types.Ref{{From: podID, To: pmID, Labels: []string{"podMetrics"}}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.NotContains(t, assignment, pmID)

	unassigned, uerr := clusterInventoryUnassignedForAbort(ents, assignment, types.KeyClusterEntity)
	require.NoError(t, uerr)
	ids := entityIds(t, unassigned)
	assert.Contains(t, ids, pmID)
}

func TestExpandAssignmentByOwnerRefs_InheritsFromTemplateWhenLiveOwnerUIDMissing(t *testing.T) {
	rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "kube-system", "lpp-6bc6568469", "uid-rs",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "local-path-provisioner", "uid-deploy-wrong")})
	ents, err := entity.NewEntities([]entity.Entity{rs})
	require.NoError(t, err)
	rsID, err := rs.Id()
	require.NoError(t, err)
	deployID := types.Id("apps/v1/Deployment/kube-system/local-path-provisioner")
	app := types.AppId("lpp.app")
	templateOwners := map[types.Id]types.AppId{deployID: app}
	assignment := map[types.Id]types.AppId{}
	require.NoError(t, expandAssignmentByOwnerRefs(ents, assignment, types.KeyClusterEntity, templateOwners))
	assert.Equal(t, app, assignment[rsID])
}

// When the ReplicaSet object is absent from live inventory but the chart's standalone template render
// still maps that ReplicaSet id to an app, the Pod can inherit the app and merged inspect can reach PodMetrics.
func TestPodMetricsInheritsWhenReplicaSetOnlyDeclaredInTemplateOwnership(t *testing.T) {
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "kube-system", "local-path-provisioner", "uid-deploy", nil)
	pod := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "local-path-provisioner-6bc6568469-cj589", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "local-path-provisioner-6bc6568469", "uid-rs-not-listed")})
	pm := makeClusterInventoryEntity("metrics.k8s.io", "v1beta1", "PodMetrics", "kube-system", "local-path-provisioner-6bc6568469-cj589", "uid-pm", nil)

	ents, err := entity.NewEntities([]entity.Entity{deploy, pod, pm})
	require.NoError(t, err)

	deployID, err := deploy.Id()
	require.NoError(t, err)
	podID, err := pod.Id()
	require.NoError(t, err)
	pmID, err := pm.Id()
	require.NoError(t, err)
	rsTemplateID := types.Id("apps/v1/ReplicaSet/kube-system/local-path-provisioner-6bc6568469")

	app := types.AppId("local-path-provisioner.app")
	assignment := map[types.Id]types.AppId{deployID: app}
	templateIDToApp := map[types.Id]types.AppId{rsTemplateID: app}
	require.NoError(t, expandAssignmentByOwnerRefs(ents, assignment, types.KeyClusterEntity, templateIDToApp))
	require.Equal(t, app, assignment[podID], "pod should inherit from ReplicaSet id present only in template ownership")

	refs := []types.Ref{{From: podID, To: pmID, Labels: []string{"podMetrics"}}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, app, assignment[pmID])

	unassigned, uerr := clusterInventoryUnassignedForAbort(ents, assignment, types.KeyClusterEntity)
	require.NoError(t, uerr)
	assert.Empty(t, unassigned.Items)
}

func TestClusterInventoryUnassignedForAbort_SkipsObjectsWithOwnerReferences(t *testing.T) {
	// Child object: owner not in inventory → expand does not assign; must not count as standalone unassigned.
	pod := makeClusterInventoryEntity("", "v1", "Pod", "default", "p1", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("v1", "Secret", "parent", "uid-missing")})
	standalone := makeClusterInventoryEntity("", "v1", "ConfigMap", "default", "orphan", "uid-cm", nil)

	ents, err := entity.NewEntities([]entity.Entity{pod, standalone})
	require.NoError(t, err)

	podID, err := pod.Id()
	require.NoError(t, err)
	cmID, err := standalone.Id()
	require.NoError(t, err)

	assignment := map[types.Id]types.AppId{}
	out, err := clusterInventoryUnassignedForAbort(ents, assignment, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	id, err := out.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, cmID, id)
	assert.NotEqual(t, podID, id)
}

func TestExpandAssignmentByOwnerRefs_AmbiguousOwners(t *testing.T) {
	a := types.AppId("a.app")
	b := types.AppId("b.app")
	cmA := makeClusterInventoryEntity("", "v1", "ConfigMap", "default", "ca", "uid-cma", nil)
	cmB := makeClusterInventoryEntity("", "v1", "ConfigMap", "default", "cb", "uid-cmb", nil)
	secret := makeClusterInventoryEntity("", "v1", "Secret", "default", "s", "uid-sec",
		[]map[string]any{
			clusterInventoryOwnerRef("v1", "ConfigMap", "ca", "uid-cma"),
			clusterInventoryOwnerRef("v1", "ConfigMap", "cb", "uid-cmb"),
		})

	ents, err := entity.NewEntities([]entity.Entity{cmA, cmB, secret})
	require.NoError(t, err)

	idA, err := cmA.Id()
	require.NoError(t, err)
	idB, err := cmB.Id()
	require.NoError(t, err)

	assignment := map[types.Id]types.AppId{idA: a, idB: b}
	err = expandAssignmentByOwnerRefs(ents, assignment, types.KeyClusterEntity, nil)
	require.Error(t, err)
	assert.Equal(t, errors.ErrUninstallAmbiguousRefOwnership, errors.Id(err))
}

func TestOwnerRefReasonForEntity_IncludesResolvedOwnerIDs(t *testing.T) {
	parent := makeClusterInventoryEntity("isindir.github.com", "v1alpha3", "SopsSecret", "sops-secrets-operator", "image-pull-secret", "uid-sops", nil)
	child := makeClusterInventoryEntity("", "v1", "Secret", "sops-secrets-operator", "image-pull-secret", "uid-secret",
		[]map[string]any{
			clusterInventoryOwnerRef("isindir.github.com/v1alpha3", "SopsSecret", "image-pull-secret", "uid-sops"),
		})

	ents, err := entity.NewEntities([]entity.Entity{parent, child})
	require.NoError(t, err)

	reason := ownerRefReasonForEntity(child, types.KeyClusterEntity, ents.UidMap(types.KeyClusterEntity))
	assert.Equal(t, AssignmentReason{
		Kind:      AssignmentReasonKindAssignedViaOwnerRef,
		OwnerRefs: []types.Id{"isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret"},
	}, reason)
}

func TestStakeholderAppsByNamespace_MergesTemplateAndCluster(t *testing.T) {
	templateByNs := map[types.Namespace]sets.Set[types.AppId]{
		"demo": sets.New[types.AppId]("a.app", "b.app"),
	}
	sec := makeEntity("", "v1", "Secret", "demo", "s1")
	ents, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	id, err := sec.Id()
	require.NoError(t, err)
	assignment := map[types.Id]types.AppId{
		id: "a.app",
	}
	out, err := StakeholderAppsByNamespace(templateByNs, assignment, ents)
	require.NoError(t, err)
	assert.True(t, out[types.Namespace("demo")].Has("a.app"))
	assert.True(t, out[types.Namespace("demo")].Has("b.app"))
}

func TestStakeholderAppsByNamespace_IgnoresPresetApps(t *testing.T) {
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "kube-system", "local-path-provisioner", "uid-d", nil)
	ents, err := entity.NewEntities([]entity.Entity{deploy})
	require.NoError(t, err)
	deployID, err := deploy.Id()
	require.NoError(t, err)
	builtin, err := types.NewPresetAppId(types.InCluster, "local-path-provisioner")
	require.NoError(t, err)
	assignment := map[types.Id]types.AppId{deployID: builtin}
	out, err := StakeholderAppsByNamespace(map[types.Namespace]sets.Set[types.AppId]{}, assignment, ents)
	require.NoError(t, err)
	_, hasKubeSystem := out[types.Namespace("kube-system")]
	assert.False(t, hasKubeSystem, "preset app must not appear as a namespace stakeholder")
}

func TestStakeholderAppsByNamespaceFromUnifiedEntities_IgnoresPresetApps(t *testing.T) {
	builtin, err := types.NewPresetAppId(types.InCluster, "local-path-provisioner")
	require.NoError(t, err)
	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "kube-system", "local-path-provisioner", "uid-d", nil)
	deployID, err := deploy.Id()
	require.NoError(t, err)
	out, err := StakeholderAppsByNamespaceFromUnifiedEntities(map[types.Namespace]sets.Set[types.AppId]{}, []InventoryEntity{{
		ID:             deployID,
		Live:           deploy,
		HasLive:        true,
		AssignedApp:    builtin,
		HasAssignedApp: true,
	}})
	require.NoError(t, err)
	_, hasKubeSystem := out[types.Namespace("kube-system")]
	assert.False(t, hasKubeSystem)
}

func TestAssignUnassignedClusterEntitiesInSoleTemplateNamespaces_AssignsWhenSingleAppRendersToNS(t *testing.T) {
	sec := makeEntity("", "v1", "Secret", "argocd", "ext")
	ents, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	perApp := map[types.AppId]entity.Entities{
		"in-cluster.argocd": mustEntities(t, []entity.Entity{makeEntity("", "v1", "ConfigMap", "argocd", "x")}),
		"in-cluster.other":  mustEntities(t, []entity.Entity{makeEntity("", "v1", "ConfigMap", "other", "y")}),
	}
	assignment := map[types.Id]types.AppId{}
	out, aerr := assignUnassignedClusterEntitiesInSoleTemplateNamespaces(ents, assignment, perApp)
	require.NoError(t, aerr)
	assert.Empty(t, out.Items)
	id, err := sec.Id()
	require.NoError(t, err)
	assert.Equal(t, types.AppId("in-cluster.argocd"), assignment[id])
}

func TestAssignUnassignedClusterEntitiesByOwnerNamespaces(t *testing.T) {
	sec := makeEntity("", "v1", "Secret", "argocd", "client")
	ents, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	assignment := map[types.Id]types.AppId{}
	owners := map[types.Namespace]types.AppId{
		"argocd": "in-cluster.argocd",
	}
	out, aerr := assignUnassignedClusterEntitiesByOwnerNamespaces(ents, assignment, owners)
	require.NoError(t, aerr)
	assert.Empty(t, out.Items)
	id, err := sec.Id()
	require.NoError(t, err)
	assert.Equal(t, types.AppId("in-cluster.argocd"), assignment[id])
}

func TestPropagateOwnershipFromMergedInspectRefs_YieldsWeakOwner(t *testing.T) {
	secretID := types.Id("v1/Secret/monitoring/image-pull-secret")
	dsID := types.Id("apps/v1/DaemonSet/monitoring/prometheus-node")
	kyverno := types.AppId("in-cluster.cluster-infra.kyverno")
	prom := types.AppId("in-cluster.cluster-infra.kube-prometheus-stack")
	assignment := map[types.Id]types.AppId{
		secretID: kyverno,
		dsID:     prom,
	}
	weak := sets.New[types.Id](secretID)
	refs := []types.Ref{{From: dsID, To: secretID}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, &weak))
	assert.Equal(t, prom, assignment[secretID])
	assert.False(t, weak.Has(secretID))
}

// TestPropagateOwnershipFromMergedInspectRefs_K3sAddonsCRDStaysWithK3sBuiltinViaTemplatePrimat
// pins the post-Phase-2 mechanism for the k3s.cattle.io Addon CRD: the CRD is a preset anchor
// materialized as a builtin app template entity (templateIDToApp[crd] = k3sBuiltin), and
// template-primat inside the propagation pass blocks cross-app Addon references from overriding
// that ownership without needing a special-case skip helper.
func TestPropagateOwnershipFromMergedInspectRefs_K3sAddonsCRDStaysWithK3sBuiltinViaTemplatePrimat(t *testing.T) {
	crdID := types.Id("apiextensions.k8s.io/v1/CustomResourceDefinition//addons.k3s.cattle.io")
	addOnAgg := types.Id("k3s.cattle.io/v1/Addon/kube-system/aggregated-metrics-reader")
	addOnAuth := types.Id("k3s.cattle.io/v1/Addon/kube-system/auth-delegator")
	k3sBuiltin := types.AppId("in-cluster.preset.k3s")
	aggBuiltin := types.AppId("in-cluster.preset.k3s-addon-aggregated-metrics-reader")
	authBuiltin := types.AppId("in-cluster.preset.k3s-addon-auth-delegator")
	templateIDToApp := map[types.Id]types.AppId{crdID: k3sBuiltin}
	assignment := map[types.Id]types.AppId{
		crdID:     k3sBuiltin,
		addOnAgg:  aggBuiltin,
		addOnAuth: authBuiltin,
	}
	refs := []types.Ref{
		{From: addOnAgg, To: crdID},
		{From: addOnAuth, To: crdID},
	}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, templateIDToApp, assignment, nil, nil))
	assert.Equal(t, k3sBuiltin, assignment[crdID])
}

func TestPropagateOwnershipFromMergedInspectRefs_RealAppOverridesBuiltinAssignment(t *testing.T) {
	// roleID is NOT a preset anchor (no entry in templateIDToApp), so the builtin-existing /
	// real-fromApp branch swaps ownership to the real app.
	roleID := types.Id("rbac.authorization.k8s.io/v1/Role/kube-system/cert-manager-webhook-hetzner-authentication-reader")
	rbID := types.Id("rbac.authorization.k8s.io/v1/RoleBinding/kube-system/cert-manager-webhook-hetzner:webhook-authentication-reader")
	kubeBuiltin := types.AppId("in-cluster.preset.kubernetes")
	hetzner := types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner")
	assignment := map[types.Id]types.AppId{
		roleID: kubeBuiltin,
		rbID:   hetzner,
	}
	refs := []types.Ref{{From: rbID, To: roleID}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, hetzner, assignment[roleID])
}

func TestPropagateOwnershipFromMergedInspectRefs_SkipsBuiltinOverrideUnderTemplatePrimacy(t *testing.T) {
	roleID := types.Id("rbac.authorization.k8s.io/v1/Role/kube-system/cert-manager-webhook-hetzner-authentication-reader")
	rbID := types.Id("rbac.authorization.k8s.io/v1/RoleBinding/kube-system/cert-manager-webhook-hetzner:webhook-authentication-reader")
	kubeBuiltin := types.AppId("in-cluster.preset.kubernetes")
	hetzner := types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner")
	templateOwners := map[types.Id]types.AppId{roleID: kubeBuiltin}
	assignment := map[types.Id]types.AppId{
		roleID: kubeBuiltin,
		rbID:   hetzner,
	}
	refs := []types.Ref{{From: rbID, To: roleID}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, templateOwners, assignment, nil, nil))
	assert.Equal(t, kubeBuiltin, assignment[roleID])
}

func TestPropagateOwnershipFromMergedInspectRefs_SharedExtensionAPIServerAuthReaderRoleStaysWithKubernetesBuiltinViaTemplatePrimat(t *testing.T) {
	// extension-apiserver-authentication-reader is a kubernetes-preset anchor; after Phase 2 it
	// lives in templateIDToApp owned by the kubernetes builtin app. The standard template-primat
	// rule (existing == tplOwner) keeps cross-app RoleBinding refs from overriding ownership.
	roleID := types.Id("rbac.authorization.k8s.io/v1/Role/kube-system/extension-apiserver-authentication-reader")
	rbID := types.Id("rbac.authorization.k8s.io/v1/RoleBinding/kube-system/cert-manager-webhook-hetzner:webhook-authentication-reader")
	kubeBuiltin := types.AppId("in-cluster.preset.kubernetes")
	hetzner := types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner")
	templateIDToApp := map[types.Id]types.AppId{roleID: kubeBuiltin}
	assignment := map[types.Id]types.AppId{
		roleID: kubeBuiltin,
		rbID:   hetzner,
	}
	refs := []types.Ref{{From: rbID, To: roleID}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, templateIDToApp, assignment, nil, nil))
	assert.Equal(t, kubeBuiltin, assignment[roleID])
}

func TestPropagateOwnershipFromMergedInspectRefs_IgnoresBuiltinClaimantWhenTargetOwnedByRealApp(t *testing.T) {
	secID := types.Id("v1/Secret/kube-system/image-pull-secret")
	saID := types.Id("v1/ServiceAccount/kube-system/default")
	ingress := types.AppId("in-cluster.cluster-infra.ingress-nginx")
	kubeBuiltin := types.AppId("in-cluster.preset.kubernetes")
	assignment := map[types.Id]types.AppId{
		secID: ingress,
		saID:  kubeBuiltin,
	}
	refs := []types.Ref{{From: saID, To: secID}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, ingress, assignment[secID])
}

func TestPropagateOwnershipFromMergedInspectRefs_IgnoresCrossBuiltinConflictsOnSharedImagePullSecret(t *testing.T) {
	secID := types.Id("v1/Secret/kube-system/image-pull-secret")
	saCoreDNS := types.Id("v1/ServiceAccount/kube-system/coredns")
	kubeBuiltin := types.AppId("in-cluster.preset.kubernetes")
	coreDNSBuiltin := types.AppId("in-cluster.preset.coredns")
	assignment := map[types.Id]types.AppId{
		secID:     kubeBuiltin,
		saCoreDNS: coreDNSBuiltin,
	}
	refs := []types.Ref{{From: saCoreDNS, To: secID, Labels: []string{"imagePullSecret"}}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, kubeBuiltin, assignment[secID])
}

func TestPropagateOwnershipFromMergedInspectRefs_ConflictWhenNoNamespaceOwner(t *testing.T) {
	cmID := types.Id("v1/ConfigMap/monitoring/config")
	dsA := types.Id("apps/v1/Deployment/monitoring/prom-a")
	dsB := types.Id("apps/v1/Deployment/monitoring/prom-b")
	assignment := map[types.Id]types.AppId{
		cmID: "a.app",
		dsA:  "a.app",
		dsB:  "b.app",
	}
	refs := []types.Ref{
		{From: dsA, To: cmID},
		{From: dsB, To: cmID},
	}
	err := propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil)
	require.Error(t, err)
	assert.Equal(t, errors.ErrUninstallAmbiguousRefOwnership, errors.Id(err))
}

func TestPropagateOwnershipFromMergedInspectRefs_CollectsMultipleDistinctConflicts(t *testing.T) {
	role1 := types.Id("rbac.authorization.k8s.io/v1/Role/kube-system/shared-bootstrap-one")
	role2 := types.Id("rbac.authorization.k8s.io/v1/Role/kube-system/shared-bootstrap-two")
	rb1 := types.Id("rbac.authorization.k8s.io/v1/RoleBinding/kube-system/consumer-one")
	rb2 := types.Id("rbac.authorization.k8s.io/v1/RoleBinding/kube-system/consumer-two")
	appKeep1 := types.AppId("in-cluster.app.keep-one")
	appKeep2 := types.AppId("in-cluster.app.keep-two")
	appWant1 := types.AppId("in-cluster.app.want-one")
	appWant2 := types.AppId("in-cluster.app.want-two")
	assignment := map[types.Id]types.AppId{
		role1: appKeep1,
		role2: appKeep2,
		rb1:   appWant1,
		rb2:   appWant2,
	}
	refs := []types.Ref{
		{From: rb1, To: role1},
		{From: rb2, To: role2},
	}
	err := propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil)
	require.Error(t, err)
	assert.Equal(t, errors.ErrUninstallAmbiguousRefOwnership, errors.Id(err))
	msg := err.Error()
	assert.Contains(t, msg, string(role1))
	assert.Contains(t, msg, string(role2))
	assert.Contains(t, msg, "\n")
	var joinErr ambiguousRefOwnershipJoinError
	require.ErrorAs(t, err, &joinErr)
	assert.Len(t, joinErr.errs, 2)
}

func TestAssignmentReasonSubset_MatchesIdenticalRefOwnershipRule(t *testing.T) {
	rule := AssignmentReason{
		Kind: AssignmentReasonKindAssignedViaRefOwnership,
		RefOwnership: &types.RefOwnershipPredicateLine{
			Cel: `gvk == "v1/Secret" && ns == "demo"`,
			Source: &types.RefOwnershipRuleSource{
				Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
				BlockPath: "global.hydra.refs.demo.ref-parsers[0]",
				Sources:   []string{"values.yaml"},
			},
		},
	}

	assert.True(t, assignmentReasonSubset([]AssignmentReason{rule}, []AssignmentReason{rule}))
}

func TestAssignmentReasonMapSubset_DropsAppsWhoseRulesWereAlreadyUsed(t *testing.T) {
	shared := AssignmentReason{
		Kind: AssignmentReasonKindAssignedViaRefOwnership,
		RefOwnership: &types.RefOwnershipPredicateLine{
			Cel: `gvk == "v1/Secret" && ns == "demo"`,
			Source: &types.RefOwnershipRuleSource{
				Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
				BlockPath: "global.hydra.refs.demo.ref-parsers[0]",
				Sources:   []string{"values.yaml"},
			},
		},
	}
	other := AssignmentReason{
		Kind: AssignmentReasonKindAssignedViaRefOwnership,
		RefOwnership: &types.RefOwnershipPredicateLine{
			Cel: `gvk == "v1/Secret" && ns == "demo" && name == "tls"`,
			Source: &types.RefOwnershipRuleSource{
				Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
				BlockPath: "global.hydra.refs.demo.ref-parsers[1]",
				Sources:   []string{"values.yaml"},
			},
		},
	}

	filtered := assignmentReasonMapSubset([]AssignmentReason{shared}, map[types.AppId][]AssignmentReason{
		"a.app": {shared},
		"b.app": {other},
	})

	assert.NotContains(t, filtered, types.AppId("a.app"))
	assert.Equal(t, []AssignmentReason{other}, filtered[types.AppId("b.app")])
}

func TestPropagateOwnershipFromMergedInspectRefs_MultiConsumerResolvesToNamespaceOwner(t *testing.T) {
	cmID := types.Id("v1/ConfigMap/monitoring/shared")
	dsA := types.Id("apps/v1/Deployment/monitoring/prom-a")
	dsB := types.Id("apps/v1/Deployment/monitoring/prom-b")
	nsOwner := types.AppId("in-cluster.stack.monitoring")
	aApp := types.AppId("a.app")
	bApp := types.AppId("b.app")
	assignment := map[types.Id]types.AppId{
		cmID: aApp,
		dsA:  aApp,
		dsB:  bApp,
	}
	owners := map[types.Namespace]types.AppId{
		types.Namespace("monitoring"): nsOwner,
	}
	refs := []types.Ref{
		{From: dsA, To: cmID},
		{From: dsB, To: cmID},
	}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, owners, nil))
	assert.Equal(t, nsOwner, assignment[cmID])
}

func TestPropagateOwnershipFromMergedInspectRefs_SkipsConflictWhenSopsGeneratedSecretOwnedByOtherApp(t *testing.T) {
	sopsID := types.Id("isindir.github.com/v1alpha3/SopsSecret/cert-manager/hetzner-credentials")
	secretID := types.Id("v1/Secret/cert-manager/hetzner-credentials")
	certMgr := types.AppId("in-cluster.cluster-infra.cert-manager")
	extDNS := types.AppId("in-cluster.cluster-infra.external-dns")
	assignment := map[types.Id]types.AppId{
		secretID: extDNS,
		sopsID:   certMgr,
	}
	refs := []types.Ref{{
		From:   sopsID,
		To:     secretID,
		Labels: []string{"sops"},
		Attributes: []types.RefAttribute{{
			Type:  types.RefAttributeOriginGenerated,
			Value: types.RefGeneratedController,
		}},
	}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, extDNS, assignment[secretID])
	assert.Equal(t, certMgr, assignment[sopsID])
}

func TestPropagateOwnershipFromMergedInspectRefs_SkipsConflictWhenNamespacedTargetMatchesNamespaceOwnerAssignment(t *testing.T) {
	saID := types.Id("v1/ServiceAccount/cert-manager/cert-manager-webhook-hetzner")
	secretID := types.Id("v1/Secret/cert-manager/image-pull-secret")
	certMgr := types.AppId("in-cluster.cluster-infra.cert-manager")
	hetzner := types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner")
	assignment := map[types.Id]types.AppId{
		secretID: certMgr,
		saID:     hetzner,
	}
	owners := map[types.Namespace]types.AppId{
		types.Namespace("cert-manager"): certMgr,
	}
	refs := []types.Ref{{From: saID, To: secretID, Labels: []string{"imagePullSecret"}}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, owners, nil))
	assert.Equal(t, certMgr, assignment[secretID])
	assert.Equal(t, hetzner, assignment[saID])
}

func TestPropagateOwnershipFromMergedInspectRefs_ConflictResolvesToNamespaceOwnerWhenNotAssignmentOrSource(t *testing.T) {
	saID := types.Id("v1/ServiceAccount/cert-manager/cert-manager-webhook-hetzner")
	secretID := types.Id("v1/Secret/cert-manager/image-pull-secret")
	certMgr := types.AppId("in-cluster.cluster-infra.cert-manager")
	extDNS := types.AppId("in-cluster.cluster-infra.external-dns")
	hetzner := types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner")
	assignment := map[types.Id]types.AppId{
		secretID: extDNS,
		saID:     hetzner,
	}
	owners := map[types.Namespace]types.AppId{
		types.Namespace("cert-manager"): certMgr,
	}
	refs := []types.Ref{{From: saID, To: secretID, Labels: []string{"imagePullSecret"}}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, owners, nil))
	assert.Equal(t, certMgr, assignment[secretID])
}

func TestPropagateOwnershipFromMergedInspectRefs_SkipsConflictWhenCertificateMaterializesSecretOwnedByOtherApp(t *testing.T) {
	certificateID := types.Id("cert-manager.io/v1/Certificate/cert-manager/cert-manager-webhook-hetzner-webhook-tls")
	secretID := types.Id("v1/Secret/cert-manager/cert-manager-webhook-hetzner-webhook-tls")
	certMgr := types.AppId("in-cluster.cluster-infra.cert-manager")
	hetzner := types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner")
	assignment := map[types.Id]types.AppId{
		certificateID: certMgr,
		secretID:      hetzner,
	}
	refs := []types.Ref{{
		From:   certificateID,
		To:     secretID,
		Labels: []string{"cert-manager-certificate-tls-secret"},
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginApp, Value: string(certMgr)},
			{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
		},
		Reverse: true,
	}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, certMgr, assignment[certificateID])
	assert.Equal(t, hetzner, assignment[secretID])
}

func TestPropagateOwnershipFromMergedInspectRefs_SkipsPropagationAlongKyvernoCloneTargetEdge(t *testing.T) {
	sourceSecretID := types.Id("v1/Secret/sops-secrets-operator/image-pull-secret")
	targetSecretID := types.Id("v1/Secret/ingress-nginx/image-pull-secret")
	sopsOperator := types.AppId("in-cluster.cluster-infra.sops-secrets-operator")
	ingress := types.AppId("in-cluster.cluster-infra.ingress-nginx")
	assignment := map[types.Id]types.AppId{
		sourceSecretID: sopsOperator,
		targetSecretID: ingress,
	}
	refs := []types.Ref{{
		From:   sourceSecretID,
		To:     targetSecretID,
		Labels: []string{"clone-target"},
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginApp, Value: string(ingress)},
			{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
		},
	}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
	assert.Equal(t, sopsOperator, assignment[sourceSecretID])
	assert.Equal(t, ingress, assignment[targetSecretID])
}

func TestPropagateOwnershipFromMergedInspectRefs_SoleWorkloadConsumerAssignsTarget(t *testing.T) {
	t.Run("secret", func(t *testing.T) {
		depID := types.Id("apps/v1/Deployment/ingress-nginx/ingress-nginx-controller")
		secretID := types.Id("v1/Secret/ingress-nginx/image-pull-secret")
		ingress := types.AppId("in-cluster.cluster-infra.ingress-nginx")
		assignment := map[types.Id]types.AppId{depID: ingress}
		refs := []types.Ref{{From: depID, To: secretID, Labels: []string{"imagePullSecret"}}}
		require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
		assert.Equal(t, ingress, assignment[secretID])
	})
	t.Run("pvc", func(t *testing.T) {
		stsID := types.Id("apps/v1/StatefulSet/data/clickhouse-shard0")
		pvcID := types.Id("v1/PersistentVolumeClaim/data/clickhouse-data-clickhouse-shard0-0")
		ch := types.AppId("in-cluster.data.clickhouse")
		assignment := map[types.Id]types.AppId{stsID: ch}
		refs := []types.Ref{{From: stsID, To: pvcID, Labels: []string{"volume"}}}
		require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil))
		assert.Equal(t, ch, assignment[pvcID])
	})
}

func TestPropagateOwnershipFromMergedInspectRefs_SkipsConflictWhenTargetMatchesTemplateOwner(t *testing.T) {
	sopsDep := types.Id("apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator")
	fromSops := types.AppId("in-cluster.cluster-infra.sops-secrets-operator")
	argocdSS := types.Id("isindir.github.com/v1alpha3/SopsSecret/argocd/argocd-client-secret")
	argocd := types.AppId("in-cluster.argocd")
	templateOwners := map[types.Id]types.AppId{sopsDep: fromSops}
	assignment := map[types.Id]types.AppId{
		sopsDep:  fromSops,
		argocdSS: argocd,
	}
	refs := []types.Ref{{From: argocdSS, To: sopsDep}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, templateOwners, assignment, nil, nil))
	assert.Equal(t, fromSops, assignment[sopsDep])
	assert.Equal(t, argocd, assignment[argocdSS])
}

func TestPropagateOwnershipFromMergedInspectRefs_SkipsNamespaceTargetWhenOwnerNamespacesMatchesExisting(t *testing.T) {
	nsID := types.Id("v1/Namespace//cert-manager")
	fromID := types.Id("apps/v1/Deployment/cert-manager/cert-manager-webhook-hetzner")
	certManager := types.AppId("in-cluster.cluster-infra.cert-manager")
	hetzner := types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner")
	assignment := map[types.Id]types.AppId{
		nsID:   certManager,
		fromID: hetzner,
	}
	owners := map[types.Namespace]types.AppId{
		types.Namespace("cert-manager"): certManager,
	}
	refs := []types.Ref{{From: fromID, To: nsID}}
	require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, owners, nil))
	assert.Equal(t, certManager, assignment[nsID])
}

func TestPropagateOwnershipFromMergedInspectRefs_NamespaceConflictWithoutOwnerNamespacesStillErrors(t *testing.T) {
	nsID := types.Id("v1/Namespace//cert-manager")
	fromID := types.Id("apps/v1/Deployment/cert-manager/webhook")
	a := types.AppId("a.app")
	b := types.AppId("b.app")
	assignment := map[types.Id]types.AppId{nsID: a, fromID: b}
	refs := []types.Ref{{From: fromID, To: nsID}}
	err := propagateOwnershipFromMergedInspectRefs(refs, map[types.Id]types.AppId{}, assignment, nil, nil)
	require.Error(t, err)
	assert.Equal(t, errors.ErrUninstallAmbiguousRefOwnership, errors.Id(err))
}

func TestPropagateOwnershipFromMergedInspectRefs_BuiltinSystemNamespaceTargetStaysWithBuiltinViaTemplatePrimat(t *testing.T) {
	// kube-system and kube-public Namespace objects are kubernetes-preset anchors; after Phase 2
	// they live in templateIDToApp owned by the kubernetes builtin app. Cross-app refs from
	// objects inside those namespaces are blocked by the standard template-primat rule
	// (existing == tplOwner) without any name-based skip helper.
	cases := []struct {
		name string
		nsID types.Id
	}{
		{"kube-system", types.Id("v1/Namespace//kube-system")},
		{"kube-public", types.Id("v1/Namespace//kube-public")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fromID := types.Id("v1/ConfigMap/" + tc.name + "/x")
			kubeBuiltin := types.AppId("in-cluster.preset.kubernetes")
			b := types.AppId("b.app")
			templateIDToApp := map[types.Id]types.AppId{tc.nsID: kubeBuiltin}
			assignment := map[types.Id]types.AppId{
				tc.nsID: kubeBuiltin,
				fromID:  b,
			}
			refs := []types.Ref{{From: fromID, To: tc.nsID}}
			require.NoError(t, propagateOwnershipFromMergedInspectRefs(refs, templateIDToApp, assignment, nil, nil))
			assert.Equal(t, kubeBuiltin, assignment[tc.nsID])
		})
	}
}

func TestAssignUnassignedClusterEntitiesInSoleTemplateNamespaces_SkipsKubeSystem(t *testing.T) {
	sec := makeEntity("", "v1", "Secret", "kube-system", "image-pull-secret")
	ents, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	perApp := map[types.AppId]entity.Entities{
		"x.app": mustEntities(t, []entity.Entity{makeEntity("", "v1", "ConfigMap", "kube-system", "c")}),
	}
	assignment := map[types.Id]types.AppId{}
	out, aerr := assignUnassignedClusterEntitiesInSoleTemplateNamespaces(ents, assignment, perApp)
	require.NoError(t, aerr)
	require.Len(t, out.Items, 1)
	assert.Empty(t, assignment)
}

func TestAssignUnassignedClusterEntitiesInSoleTemplateNamespaces_AssignsDefaultWhenSingleAppRendersToNS(t *testing.T) {
	sec := makeEntity("", "v1", "Secret", "default", "image-pull-secret")
	ents, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	perApp := map[types.AppId]entity.Entities{
		"x.app": mustEntities(t, []entity.Entity{makeEntity("", "v1", "ConfigMap", "default", "c")}),
	}
	assignment := map[types.Id]types.AppId{}
	out, aerr := assignUnassignedClusterEntitiesInSoleTemplateNamespaces(ents, assignment, perApp)
	require.NoError(t, aerr)
	require.Empty(t, out.Items)
	id, err := sec.Id()
	require.NoError(t, err)
	assert.Equal(t, types.AppId("x.app"), assignment[id])
}

func TestValidateLiveNamespacesFullyOwned_SkipsDefault(t *testing.T) {
	live := mustEntities(t, []entity.Entity{clusterNamespaceEntity("default", "uid-default")})
	err := validateLiveNamespacesFullyOwned(live, map[types.Id]types.AppId{}, map[types.AppId]entity.Entities{}, map[types.Namespace]types.AppId{})
	require.NoError(t, err)
}
