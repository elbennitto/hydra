package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func testLogger() log.Logger {
	return log.Default()
}

func mustBuildDiff(b entity.EntityBuilder) entity.Entity {
	e, err := b.Build()
	if err != nil {
		panic(err)
	}
	return e
}

func mustModifyDiff(e entity.Entity, fn func(entity.EntityBuilder) entity.EntityBuilder) entity.Entity {
	result, err := e.Modify(fn)
	if err != nil {
		panic(err)
	}
	return result
}

func makeTestEntity(group, version, kind, resource, namespace, name string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource(resource)).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	return mustBuildDiff(b)
}

func withClusterUnstructured(e entity.Entity, annotations map[string]interface{}, ownerRefs []interface{}) entity.Entity {
	metadata := map[string]interface{}{
		"name": mustName(e),
	}
	ns, _ := e.Namespace()
	if ns != "" {
		metadata["namespace"] = string(ns)
	}
	if len(annotations) > 0 {
		metadata["annotations"] = annotations
	}
	if len(ownerRefs) > 0 {
		metadata["ownerReferences"] = ownerRefs
	}
	group, _ := e.Group()
	version, _ := e.Version()
	kind, _ := e.Kind()
	apiVersion := string(version)
	if group != "" {
		apiVersion = string(group) + "/" + apiVersion
	}
	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       string(kind),
			"metadata":   metadata,
		},
	}
	return mustModifyDiff(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, u)
	})
}

func mustName(e entity.Entity) string {
	n, err := e.Name()
	if err != nil {
		panic(err)
	}
	return string(n)
}

// --- entityDiff tests ---

func TestEntityDiff_IdenticalYaml_ReturnsEmpty(t *testing.T) {
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	result, err := entityDiff(e, "key: value\n", "key: value\n", 3)
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestEntityDiff_Addition(t *testing.T) {
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	result, err := entityDiff(e, "", "key: value\n", 3)
	require.NoError(t, err)
	assert.Contains(t, result, "--- /dev/null")
	assert.Contains(t, result, "+++ b/v1/ConfigMap/default/my-config")
	assert.Contains(t, result, "+key: value")
}

func TestEntityDiff_Deletion(t *testing.T) {
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	result, err := entityDiff(e, "key: value\n", "", 3)
	require.NoError(t, err)
	assert.Contains(t, result, "--- a/v1/ConfigMap/default/my-config")
	assert.Contains(t, result, "+++ /dev/null")
	assert.Contains(t, result, "-key: value")
}

func TestEntityDiff_Modification(t *testing.T) {
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	result, err := entityDiff(e, "key: old\n", "key: new\n", 3)
	require.NoError(t, err)
	assert.Contains(t, result, "--- a/v1/ConfigMap/default/my-config")
	assert.Contains(t, result, "+++ b/v1/ConfigMap/default/my-config")
	assert.Contains(t, result, "-key: old")
	assert.Contains(t, result, "+key: new")
}

func TestEntityDiff_LargerContextIncludesMoreUnchangedLines(t *testing.T) {
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	oldYaml := "a: 1\nb: 2\nc: 3\nd: 4\ne: old\nf: 6\ng: 7\nh: 8\n"
	newYaml := "a: 1\nb: 2\nc: 3\nd: 4\ne: new\nf: 6\ng: 7\nh: 8\n"
	tight, err := entityDiff(e, oldYaml, newYaml, 0)
	require.NoError(t, err)
	wide, err := entityDiff(e, oldYaml, newYaml, 5)
	require.NoError(t, err)
	assert.Greater(t, len(wide), len(tight), "more context lines should produce a longer unified diff")
}

func TestEntityDiff_BothEmpty_ReturnsEmpty(t *testing.T) {
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	result, err := entityDiff(e, "", "", 3)
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

// --- filterManagedOrphans tests ---

func TestFilterManagedOrphans_EmptyCandidates(t *testing.T) {
	appIds := sets.New[types.AppId]("dev.demo.myapp")
	result, err := filterManagedOrphans(testLogger(), entity.Entities{}, appIds)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len())
}

func TestFilterManagedOrphans_MatchingTrackingId(t *testing.T) {
	appIds := sets.New[types.AppId]("dev.demo.myapp")
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	e = withClusterUnstructured(e, map[string]interface{}{
		"argocd.argoproj.io/tracking-id": "dev.demo.myapp:/ConfigMap:default/my-config",
	}, nil)

	candidates, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	result, err := filterManagedOrphans(testLogger(), candidates, appIds)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len())
}

func TestFilterManagedOrphans_NonMatchingTrackingId(t *testing.T) {
	appIds := sets.New[types.AppId]("dev.demo.myapp")
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	e = withClusterUnstructured(e, map[string]interface{}{
		"argocd.argoproj.io/tracking-id": "dev.demo.otherapp:/ConfigMap:default/my-config",
	}, nil)

	candidates, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	result, err := filterManagedOrphans(testLogger(), candidates, appIds)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len())
}

func TestFilterManagedOrphans_NoTrackingId(t *testing.T) {
	appIds := sets.New[types.AppId]("dev.demo.myapp")
	e := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "my-config")
	e = withClusterUnstructured(e, nil, nil)

	candidates, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	result, err := filterManagedOrphans(testLogger(), candidates, appIds)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len())
}

func TestFilterManagedOrphans_MatchingButWithOwnerRefs(t *testing.T) {
	appIds := sets.New[types.AppId]("dev.demo.myapp")
	e := makeTestEntity("apps", "v1", "ReplicaSet", "replicasets", "default", "my-rs")
	e = withClusterUnstructured(e, map[string]interface{}{
		"argocd.argoproj.io/tracking-id": "dev.demo.myapp:apps/ReplicaSet:default/my-rs",
	}, []interface{}{
		map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"name":       "my-deploy",
			"uid":        "abc-123",
		},
	})

	candidates, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	result, err := filterManagedOrphans(testLogger(), candidates, appIds)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "resources with ownerReferences should be excluded")
}

func TestFilterManagedOrphans_MixedCandidates(t *testing.T) {
	appIds := sets.New[types.AppId]("dev.demo.myapp")

	managed := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "managed-cm")
	managed = withClusterUnstructured(managed, map[string]interface{}{
		"argocd.argoproj.io/tracking-id": "dev.demo.myapp:/ConfigMap:default/managed-cm",
	}, nil)

	unrelated := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "unrelated-cm")
	unrelated = withClusterUnstructured(unrelated, nil, nil)

	withOwner := makeTestEntity("", "v1", "Secret", "secrets", "default", "owned-secret")
	withOwner = withClusterUnstructured(withOwner, map[string]interface{}{
		"argocd.argoproj.io/tracking-id": "dev.demo.myapp:/Secret:default/owned-secret",
	}, []interface{}{
		map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"name":       "sa",
			"uid":        "xyz-789",
		},
	})

	candidates, err := entity.NewEntities([]entity.Entity{managed, unrelated, withOwner})
	require.NoError(t, err)

	result, err := filterManagedOrphans(testLogger(), candidates, appIds)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "only managed-cm should remain")

	id, err := result.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/ConfigMap/default/managed-cm"), id)
}

// --- filterDiffEntitiesByPredicate tests ---

func TestFilterDiffEntitiesByPredicate_NilPredicate(t *testing.T) {
	cm := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "cm1")
	out, err := filterDiffEntitiesByPredicate([]entity.Entity{cm}, nil)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestFilterDiffEntitiesByPredicate_EmptyItems(t *testing.T) {
	env, err := cel.NewEnv()
	require.NoError(t, err)
	pred, err := env.CompilePredicate(types.CelPredicate(`kind == "ConfigMap"`))
	require.NoError(t, err)
	out, err := filterDiffEntitiesByPredicate(nil, pred)
	require.NoError(t, err)
	assert.Len(t, out, 0)
}

func TestFilterDiffEntitiesByPredicate_KindInclude(t *testing.T) {
	env, err := cel.NewEnv()
	require.NoError(t, err)
	pred, err := env.CompilePredicate(types.CelPredicate(`kind == "ConfigMap"`))
	require.NoError(t, err)

	cm := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "cm1")
	sec := makeTestEntity("", "v1", "Secret", "secrets", "default", "s1")

	out, err := filterDiffEntitiesByPredicate([]entity.Entity{cm, sec}, pred)
	require.NoError(t, err)
	require.Len(t, out, 1)
	id, err := out[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/ConfigMap/default/cm1"), id)
}

func TestFilterDiffEntitiesByPredicate_CombinedIncludeExclude(t *testing.T) {
	env, err := cel.NewEnv()
	require.NoError(t, err)
	// Same order as definePredicateFlags: excludes become !(expr) and append after includes.
	pred, err := env.CompilePredicate(
		types.CelPredicate(`kind == "ConfigMap"`),
		types.CelPredicate(`!(name == "skip-me")`),
	)
	require.NoError(t, err)

	keep := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "keep")
	skip := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "skip-me")
	sec := makeTestEntity("", "v1", "Secret", "secrets", "default", "s1")

	out, err := filterDiffEntitiesByPredicate([]entity.Entity{keep, skip, sec}, pred)
	require.NoError(t, err)
	require.Len(t, out, 1)
	id, err := out[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/ConfigMap/default/keep"), id)
}
