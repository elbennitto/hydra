package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestCloneTagActive(t *testing.T) {
	require.True(t, CloneTagActive("", types.BootstrapNo))
	require.True(t, CloneTagActive("", types.BootstrapYes))
	require.True(t, CloneTagActive("ignored-tag", types.BootstrapNo))
	require.False(t, CloneTagActive("bootstrap", types.BootstrapNo))
	require.True(t, CloneTagActive("bootstrap", types.BootstrapYes))
}

func TestCloneTargetOwner_KubernetesSystemUsesDeclaringApp(t *testing.T) {
	entry := types.HydraCloneRuleEntry{
		Name:         "mirror",
		DeclaringApp: "in-cluster.cluster-infra.kyverno",
	}
	owner, ok := cloneTargetOwner(map[types.Namespace]types.AppId{}, types.Namespace("kube-system"), entry)
	require.True(t, ok)
	require.Equal(t, types.AppId("in-cluster.cluster-infra.kyverno"), owner)
}

func TestCloneTargetOwner_KubernetesSystemWithoutDeclaringAppStillUnset(t *testing.T) {
	entry := types.HydraCloneRuleEntry{Name: "mirror", DeclaringApp: ""}
	_, ok := cloneTargetOwner(map[types.Namespace]types.AppId{}, types.Namespace("kube-system"), entry)
	require.False(t, ok)
}

func TestCloneTargetOwner_NonSystemStillFromMap(t *testing.T) {
	entry := types.HydraCloneRuleEntry{Name: "mirror", DeclaringApp: "c.app"}
	m := map[types.Namespace]types.AppId{"app-ns": "c.owner"}
	owner, ok := cloneTargetOwner(m, types.Namespace("app-ns"), entry)
	require.True(t, ok)
	require.Equal(t, types.AppId("c.owner"), owner)
}

func TestValidateBootstrapTemplateClones_NoBootstrapFlag(t *testing.T) {
	err := ValidateBootstrapTemplateClones(types.BootstrapNo, nil, 0)
	require.NoError(t, err)
}

func TestValidateBootstrapTemplateClones_BootstrapWithoutRules(t *testing.T) {
	err := ValidateBootstrapTemplateClones(types.BootstrapYes, nil, 0)
	require.Error(t, err)
}

func TestValidateBootstrapTemplateClones_BootstrapWithRuleButNoMaterialization(t *testing.T) {
	rules := []types.HydraCloneRuleEntry{
		{Name: "r1", Rule: types.HydraCloneRule{Tag: "bootstrap"}},
	}
	err := ValidateBootstrapTemplateClones(types.BootstrapYes, rules, 0)
	require.Error(t, err)
}

func TestBuildNamespaceOwnerMap_FromNamespaceObject(t *testing.T) {
	nsGVK := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Namespace"))
	ns := mustBuild(entity.NewEntityBuilder().
		WithGVK(nsGVK).
		WithName(types.Name("app-ns")).
		WithAppIds([]types.AppId{"c.app.one"}))
	ents, err := entity.NewEntities([]entity.Entity{ns})
	require.NoError(t, err)
	m, err := BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, nil, entity.Entities{})
	require.NoError(t, err)
	require.Equal(t, types.AppId("c.app.one"), m[types.Namespace("app-ns")])
}

func TestBuildNamespaceOwnerMap_AmbiguousOwners(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	a := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a"))), types.Namespace("shared")), []types.AppId{"c.one"})
	b := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("b"))), types.Namespace("shared")), []types.AppId{"c.two"})
	ents, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)
	_, err = BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, nil, entity.Entities{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "shared: [c.one, c.two]")
}

func TestBuildNamespaceOwnerMap_AmbiguousOwnersListsAllNamespaces(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	sharedA := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a1"))), types.Namespace("shared")), []types.AppId{"c.one"})
	sharedB := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("b1"))), types.Namespace("shared")), []types.AppId{"c.two"})
	otherA := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a2"))), types.Namespace("other")), []types.AppId{"c.three"})
	otherB := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("b2"))), types.Namespace("other")), []types.AppId{"c.four"})
	ents, err := entity.NewEntities([]entity.Entity{sharedA, sharedB, otherA, otherB})
	require.NoError(t, err)
	_, err = BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, nil, entity.Entities{})
	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "2 namespace(s)")
	require.Contains(t, msg, "other: [c.four, c.three]")
	require.Contains(t, msg, "shared: [c.one, c.two]")
	// Stable ordering: namespaces sorted alphabetically (other before shared).
	idxOther := strings.Index(msg, "other:")
	idxShared := strings.Index(msg, "shared:")
	require.Greater(t, idxShared, idxOther)
}

func TestMergeRenderedWithClones_RealWins(t *testing.T) {
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))
	real := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns1")).
		WithName(types.Name("s1")).
		WithAppIds([]types.AppId{"c.app"}))
	clone := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns1")).
		WithName(types.Name("s1")).
		WithAppIds([]types.AppId{"c.app"}))
	base, err := entity.NewEntities([]entity.Entity{real})
	require.NoError(t, err)
	cl, err := entity.NewEntities([]entity.Entity{clone})
	require.NoError(t, err)
	out, err := MergeRenderedWithClones(base, cl)
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
}

func TestBuildNamespaceOwnerMap_DeclaredOwnerHighestPriority(t *testing.T) {
	nsGVK := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Namespace"))
	ns := mustBuild(entity.NewEntityBuilder().
		WithGVK(nsGVK).
		WithName(types.Name("app-ns")).
		WithAppIds([]types.AppId{"c.app.one"}))
	ents, err := entity.NewEntities([]entity.Entity{ns})
	require.NoError(t, err)

	declaredOwners := map[types.Namespace]types.AppId{
		"app-ns": "c.app.two",
	}
	m, err := BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, declaredOwners, entity.Entities{})
	require.NoError(t, err)
	require.Equal(t, types.AppId("c.app.two"), m[types.Namespace("app-ns")])
}

func TestBuildNamespaceOwnerMap_DeclaredOwnerResolvesAmbiguity(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	a := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a"))), types.Namespace("shared")), []types.AppId{"c.one"})
	b := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("b"))), types.Namespace("shared")), []types.AppId{"c.two"})
	ents, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	declaredOwners := map[types.Namespace]types.AppId{
		"shared": "c.one",
	}
	m, err := BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, declaredOwners, entity.Entities{})
	require.NoError(t, err)
	require.Equal(t, types.AppId("c.one"), m[types.Namespace("shared")])
}

func TestBuildNamespaceOwnerMap_NilDeclaredOwnersUsesExistingStrategies(t *testing.T) {
	nsGVK := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Namespace"))
	ns := mustBuild(entity.NewEntityBuilder().
		WithGVK(nsGVK).
		WithName(types.Name("app-ns")).
		WithAppIds([]types.AppId{"c.app.one"}))
	ents, err := entity.NewEntities([]entity.Entity{ns})
	require.NoError(t, err)

	m, err := BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, nil, entity.Entities{})
	require.NoError(t, err)
	require.Equal(t, types.AppId("c.app.one"), m[types.Namespace("app-ns")])
}

func TestBuildNamespaceOwnerMap_KubernetesSystemNamespacesNotOwnedByApps(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	a := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a"))), types.Namespace("kube-system")), []types.AppId{"c.one"})
	b := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("b"))), types.Namespace("kube-system")), []types.AppId{"c.two"})
	ents, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)
	m, err := BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, nil, entity.Entities{})
	require.NoError(t, err)
	_, hasKubeSystem := m[types.Namespace("kube-system")]
	require.False(t, hasKubeSystem, "kube-system must not resolve to an app owner")
}

func TestBuildNamespaceOwnerMap_DeclaredOwnerIgnoredForKubeSystem(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	dep := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("x"))), types.Namespace("kube-system")), []types.AppId{"c.one"})
	ents, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)
	m, err := BuildNamespaceOwnerMap(ents, types.KeyTemplateEntity, map[types.Namespace]types.AppId{
		"kube-system": "c.one",
	}, entity.Entities{})
	require.NoError(t, err)
	_, ok := m[types.Namespace("kube-system")]
	require.False(t, ok, "declared ownerNamespaces for kube-system must be ignored")
}

func TestBuildNamespaceOwnerMap_OwnershipIndexDetectsSharedNamespace(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	subA := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a"))), types.Namespace("shared")), []types.AppId{"c.one"})
	fullB := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("b"))), types.Namespace("shared")), []types.AppId{"c.two"})
	subEnts, err := entity.NewEntities([]entity.Entity{subA})
	require.NoError(t, err)
	fullEnts, err := entity.NewEntities([]entity.Entity{subA, fullB})
	require.NoError(t, err)

	m, err := BuildNamespaceOwnerMap(subEnts, types.KeyTemplateEntity, nil, entity.Entities{})
	require.NoError(t, err)
	require.Equal(t, types.AppId("c.one"), m[types.Namespace("shared")])

	_, err = BuildNamespaceOwnerMap(subEnts, types.KeyTemplateEntity, nil, fullEnts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous app owners")
}

func TestDiffEntities(t *testing.T) {
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))
	a := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns")).
		WithName(types.Name("x")))
	b := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns")).
		WithName(types.Name("y")))
	base, err := entity.NewEntities([]entity.Entity{a})
	require.NoError(t, err)
	merged, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)
	diff, err := DiffEntities(base, merged)
	require.NoError(t, err)
	require.Equal(t, 1, diff.Len())
}
