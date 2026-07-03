package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func refOwnershipTestPredicateLines(m map[types.AppId][]string) map[types.AppId][]types.RefOwnershipPredicateLine {
	out := make(map[types.AppId][]types.RefOwnershipPredicateLine, len(m))
	for id, ss := range m {
		var lines []types.RefOwnershipPredicateLine
		for _, s := range ss {
			lines = append(lines, types.RefOwnershipPredicateLine{Cel: s})
		}
		out[id] = lines
	}
	return out
}

func templateSecretEntity(name, ns string) entity.Entity {
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
		WithUnstructured(types.KeyTemplateEntity, u).
		Build()
	if err != nil {
		panic(err)
	}
	return e
}

func templateConfigMapEntity(name, ns string) entity.Entity {
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
	if err != nil {
		panic(err)
	}
	return e
}

func templateDeploymentEntity(name, ns string) entity.Entity {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
			},
		},
	}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyTemplateEntity, u))
}

func clusterSecretEntity(name, ns string) entity.Entity {
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
	if err != nil {
		panic(err)
	}
	return e
}

func clusterConfigMapEntity(name, ns string) entity.Entity {
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
	if err != nil {
		panic(err)
	}
	return e
}

func clusterServiceAccountEntity(name, ns string) entity.Entity {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
			},
		},
	}
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ServiceAccount"))).
		WithResource(types.Resource("serviceaccounts")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	if err != nil {
		panic(err)
	}
	return e
}

// refOwnershipAppendFindings must use non-runtime predicates for template-backed resources so
// [runtime]-only uninstall rules cannot false-positive against another app's template.
func TestRefOwnershipAppendFindings_TemplateIgnoresRuntimeOnlyPredicates(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	bApp := types.AppId("b.app")
	allAppIds := sets.New(aApp, bApp)

	sec := templateSecretEntity("owned", "ns1")
	cm := templateConfigMapEntity("b-only", "ns1")

	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{sec}),
		bApp: mustEntities(t, []entity.Entity{cm}),
	}

	predNoRuntime := map[types.AppId][]string{
		aApp: {`kind == "Secret"`},
	}
	predWithRuntime := map[types.AppId][]string{
		bApp: {`true`},
	}

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		entity.Entities{},
		nil,
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, findings)
}

// Live cluster objects whose id is template-mapped must not be matched via [runtime]-only
// ownership predicates from another app (second loop uses predNoRuntime for that id).
func TestRefOwnershipAppendFindings_LiveTemplateMappedIgnoresRuntimeOnlyPredicates(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	bApp := types.AppId("b.app")
	allAppIds := sets.New(aApp, bApp)

	secTpl := templateSecretEntity("live-mapped", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{secTpl}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{
		bApp: {`true`},
	}

	live := mustEntities(t, []entity.Entity{clusterSecretEntity("live-mapped", "ns1")})

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		nil,
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, findings)
}

func TestRefOwnershipAppendFindings_ClusterOnlyResourceUsesRuntimePredicates_Ambiguous(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	bApp := types.AppId("b.app")
	allAppIds := sets.New(aApp, bApp)

	cmA := templateConfigMapEntity("cm-a", "ns1")
	cmB := templateConfigMapEntity("cm-b", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{cmA}),
		bApp: mustEntities(t, []entity.Entity{cmB}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{
		aApp: {`true`},
		bApp: {`true`},
	}

	orphan := clusterSecretEntity("orphan", "ns1")
	live := mustEntities(t, []entity.Entity{orphan})

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		nil,
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, 1, n)
	assert.Contains(t, findings[0].Message, RefOwnershipAmbiguousClusterOnlyFinding)
}

func TestRefOwnershipAppendFindings_ClusterOnly_InheritsAppViaOwnerChain_NoUnassigned(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	deployTpl := templateDeploymentEntity("web", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{deployTpl}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{}

	deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "ns1", "web", "uid-deploy", nil)
	rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "ns1", "web-abc", "uid-rs",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "web", "uid-deploy")})
	pod := makeClusterInventoryEntity("", "v1", "Pod", "ns1", "web-p", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "web-abc", "uid-rs")})

	live, err := entity.NewEntities([]entity.Entity{deploy, rs, pod})
	require.NoError(t, err)

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		sets.New(types.Namespace("ns1")),
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, findings)
}

func TestRefOwnershipAppendFindings_ClusterOnly_UnassignedWhenNoTemplateRefsOrOwners(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	cm := templateConfigMapEntity("cfg", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{cm}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{}

	orphan := clusterSecretEntity("orphan", "ns1")
	live := mustEntities(t, []entity.Entity{orphan})

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		sets.New(types.Namespace("ns1")),
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, 1, n)
	assert.Contains(t, findings[0].Message, RefOwnershipUnassignedClusterOnlyFinding)
}

// When a chain of cluster-only objects is unassigned, only the ownership root (no owner in the
// live snapshot) is reported — not child objects that have ownerReferences to other live objects.
func TestRefOwnershipAppendFindings_ClusterOnly_UnassignedOnlyRootSkipsOwnedChildren(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	cm := templateConfigMapEntity("cfg", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{cm}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{}

	job := makeClusterInventoryEntity("batch", "v1", "Job", "ns1", "orphan-job", "uid-job", nil)
	pod := makeClusterInventoryEntity("", "v1", "Pod", "ns1", "orphan-job-abc", "uid-pod",
		[]map[string]any{clusterInventoryOwnerRef("batch/v1", "Job", "orphan-job", "uid-job")})

	live, err := entity.NewEntities([]entity.Entity{job, pod})
	require.NoError(t, err)

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		sets.New(types.Namespace("ns1")),
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, 1, n)
	assert.Contains(t, findings[0].Message, RefOwnershipUnassignedClusterOnlyFinding)
	jobID, err := job.Id()
	require.NoError(t, err)
	assert.Equal(t, jobID, findings[0].Target, "expected unassigned finding for Job root only, not owned Pod")
}

// hydra gitops review app passes nil leftover namespaces so "no Hydra app assignment" is never reported.
func TestRefOwnershipAppendFindings_ClusterOnly_UnassignedSuppressedWhenNamespacesNil(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	cm := templateConfigMapEntity("cfg", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{cm}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{}

	orphan := clusterSecretEntity("orphan", "ns1")
	live := mustEntities(t, []entity.Entity{orphan})

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		nil,
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, findings)
}

func TestRefOwnershipAppendFindings_ClusterOnly_KubernetesInjectedNamespaceDefaultsNotReportedUnassigned(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	cm := templateConfigMapEntity("cfg", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{cm}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{}

	rootCA := clusterConfigMapEntity("kube-root-ca.crt", "ns1")
	defSA := clusterServiceAccountEntity("default", "ns1")
	live := mustEntities(t, []entity.Entity{rootCA, defSA})

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		sets.New(types.Namespace("ns1")),
		nil,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, findings)
}

func TestRefOwnershipAppendFindings_PresetOnlySuppressesUnassigned(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	cm := templateConfigMapEntity("cfg", "ns1")
	rendered := mustEntities(t, []entity.Entity{cm})
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: rendered,
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{}

	orphan := clusterSecretEntity("orphan", "ns1")
	live := mustEntities(t, []entity.Entity{orphan})

	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)
	effective := []hydra.ClusterDefaultsPresetEffective{{
		ID:      "diag",
		Enabled: true,
		Predicates: map[string]hydra.ClusterDefaultsPredicateEffective{
			"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `gvk == "v1/Secret" && ns == "ns1" && name == "orphan"`, Optional: false}}},
		},
	}}

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		sets.New(types.Namespace("ns1")),
		nil,
		99,
		effective,
		env,
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, findings)
}

func TestRefOwnershipAppendFindings_AppAndPresetEmitsOverlapFinding(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	cm := templateConfigMapEntity("cfg", "ns1")
	rendered := mustEntities(t, []entity.Entity{cm})
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: rendered,
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{
		aApp: {`true`},
	}

	orphan := clusterSecretEntity("orphan", "ns1")
	live := mustEntities(t, []entity.Entity{orphan})

	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)
	effective := []hydra.ClusterDefaultsPresetEffective{{
		ID:      "diag",
		Enabled: true,
		Predicates: map[string]hydra.ClusterDefaultsPredicateEffective{
			"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `gvk == "v1/Secret" && ns == "ns1" && name == "orphan"`, Optional: false}}},
		},
	}}

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		sets.New(types.Namespace("ns1")),
		nil,
		99,
		effective,
		env,
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, 1, n)
	assert.Contains(t, findings[0].Message, RefOwnershipClusterResourceMatchesClusterDefaultsPresetFinding)
}

func TestRefOwnershipAppendFindings_OwnerNamespacesAssignsLiveNamespace(t *testing.T) {
	t.Parallel()

	aApp := types.AppId("a.app")
	allAppIds := sets.New(aApp)

	cm := templateConfigMapEntity("cfg", "ns1")
	perAppRendered := map[types.AppId]entity.Entities{
		aApp: mustEntities(t, []entity.Entity{cm}),
	}

	predNoRuntime := map[types.AppId][]string{}
	predWithRuntime := map[types.AppId][]string{}

	nsLive := clusterNamespaceEntity("argocd", "uid-argocd")
	live := mustEntities(t, []entity.Entity{nsLive})
	owners := map[types.Namespace]types.AppId{
		types.Namespace("argocd"): aApp,
	}

	var findings []ReviewFinding
	n, err := refOwnershipAppendFindings(
		log.NewLogger(),
		allAppIds,
		perAppRendered,
		refOwnershipTestPredicateLines(predNoRuntime),
		refOwnershipTestPredicateLines(predWithRuntime),
		workloadclosure.EmptyMatchInput(types.KeyClusterEntity),
		live,
		sets.New(types.Namespace("argocd")),
		owners,
		99,
		nil,
		cel.Env{},
		false,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, findings)
}

func TestIsKubernetesStandardRefOwnershipExempt(t *testing.T) {
	t.Parallel()

	assert.True(t, IsKubernetesStandardRefOwnershipExempt(types.Id("v1/ConfigMap/my-workload-ns/kube-root-ca.crt"), 30))
	assert.True(t, IsKubernetesStandardRefOwnershipExempt(types.Id("v1/ServiceAccount/my-workload-ns/default"), 30))
	assert.True(t, IsKubernetesStandardRefOwnershipExempt(kubernetesBuiltinClusterRoleID("view"), 30))

	assert.False(t, IsKubernetesStandardRefOwnershipExempt(types.Id("v1/Secret/ns1/custom"), 30))
}
