package commands

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestApplyScopeInfoMap_MissingNamespacePropagatedToUnstructured(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("my-deploy")).
		WithAppNamespace(types.AppNamespace("my-app-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name": "my-deploy",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnore, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	ns, err := result.Items[0].Namespace()
	require.NoError(t, err)
	assert.Equal(t, types.Namespace("my-app-ns"), ns)

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "my-app-ns", uResult.GetNamespace())
}

func TestApplyScopeInfoMap_ExistingNamespacePreserved(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("my-deploy")).
		WithAppNamespace(types.AppNamespace("my-app-ns")).
		WithNamespace(types.Namespace("existing-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "my-deploy",
				"namespace": "existing-ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnore, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	ns, err := result.Items[0].Namespace()
	require.NoError(t, err)
	assert.Equal(t, types.Namespace("existing-ns"), ns)

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "existing-ns", uResult.GetNamespace())
}

func TestApplyScopeInfoMap_ClusterScopedNoNamespace(t *testing.T) {
	gvk := types.NewGVK(types.Group("rbac.authorization.k8s.io"), types.Version("v1"), types.Kind("ClusterRole"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("clusterroles")).
		WithName(types.Name("my-clusterrole")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]any{
				"name": "my-clusterrole",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnore, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	assert.False(t, result.Items[0].HasNamespace())

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "", uResult.GetNamespace())
}

// TestApplyScopeInfoMap_ClusterScopedWithAppNamespaceUnchanged guards that cluster-scoped resources never pick up
// the app namespace on the unstructured object or entity namespace key when metadata.namespace is absent.
func TestApplyScopeInfoMap_ClusterScopedWithAppNamespaceUnchanged(t *testing.T) {
	gvk := types.NewGVK(types.Group("rbac.authorization.k8s.io"), types.Version("v1"), types.Kind("ClusterRole"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("clusterroles")).
		WithName(types.Name("aggregate-role")).
		WithAppNamespace(types.AppNamespace("team-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]any{
				"name": "aggregate-role",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnore, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	assert.False(t, result.Items[0].HasNamespace())

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "", uResult.GetNamespace())
}

func TestApplyScopeInfoMap_ClusterScopedResourceWithMetadataNamespaceErrors(t *testing.T) {
	gvk := types.NewGVK(types.Group("cert-manager.io"), types.Version("v1"), types.Kind("ClusterIssuer"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("clusterissuers")).
		WithName(types.Name("letsencrypt-prod")).
		WithNamespace(types.Namespace("cert-manager")).
		WithTemplatePath(types.TemplatePath("apps/cert-manager/letsencrypt-cluster-issuer.yaml")).
		WithTemplateIndex(types.TemplateIndex(1)))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "cert-manager.io/v1",
			"kind":       "ClusterIssuer",
			"metadata": map[string]any{
				"name":      "letsencrypt-prod",
				"namespace": "cert-manager",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := types.ScopeInfoMap{
		"cert-manager.io/v1/ClusterIssuer": {
			Group:      "cert-manager.io",
			Version:    "v1",
			Kind:       "ClusterIssuer",
			Resource:   "clusterissuers",
			Namespaced: types.NamespacedYes,
		},
	}

	_, err = ApplyScopeInfoMap(types.CrdModeIgnore, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.Error(t, err)
	assert.True(t, errors.ErrInvalidResourceScope.MatchesError(err))
	assert.Contains(t, err.Error(), "cluster-scoped entity cert-manager.io/v1/ClusterIssuer must not set metadata.namespace cert-manager")
	assert.Contains(t, err.Error(), "apps/cert-manager/letsencrypt-cluster-issuer.yaml#1")
}

func TestApplyScopeInfoMap_EntityWithoutUnstructuredHandledGracefully(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("my-deploy")).
		WithAppNamespace(types.AppNamespace("my-app-ns")))

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnore, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	ns, err := result.Items[0].Namespace()
	require.NoError(t, err)
	assert.Equal(t, types.Namespace("my-app-ns"), ns)
}

// --- ApplyScopeInfoMap CrdModeIgnoreOptional tests ---

func TestApplyScopeInfoMap_CrdModeIgnoreOptional_KnownGVK(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("web-app")).
		WithAppNamespace(types.AppNamespace("default")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name": "web-app",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnoreOptional, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	ns, err := result.Items[0].Namespace()
	require.NoError(t, err)
	assert.Equal(t, types.Namespace("default"), ns)
}

func TestApplyScopeInfoMap_CrdModeIgnoreOptional_UnknownNotRequired(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("kafka-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "kafka-ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	requiredGVKs := sets.New[types.GVKString]()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnoreOptional, entities, scopeInfoMap, types.KeyTemplateEntity, requiredGVKs)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len())
}

func TestApplyScopeInfoMap_CrdModeIgnoreOptional_UnknownButRequired(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("kafka-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "kafka-ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	requiredGVKs := sets.New[types.GVKString]("kafka.strimzi.io/v1beta2/Kafka")

	_, err = ApplyScopeInfoMap(types.CrdModeIgnoreOptional, entities, scopeInfoMap, types.KeyTemplateEntity, requiredGVKs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kafka.strimzi.io/v1beta2/Kafka")
}

func TestApplyScopeInfoMap_CrdModeIgnoreOptional_NoRequiredGVKs(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("kafka-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "kafka-ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	scopeInfoMap := DefaultScopeInfoMap()

	result, err := ApplyScopeInfoMap(types.CrdModeIgnoreOptional, entities, scopeInfoMap, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len())
}

func TestApplyScopeInfoMap_CrdModeKeepUnknown_PreservesUnknownEntity(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name("my-kafka")).
		WithAppNamespace(types.AppNamespace("kafka-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name": "my-kafka",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	result, err := ApplyScopeInfoMap(types.CrdModeKeepUnknown, entities, DefaultScopeInfoMap(), types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	assert.False(t, result.Items[0].HasNamespace())

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "", uResult.GetNamespace())
}

// --- NormalizeApiVersions tests ---

func testNormalizeApiVersions(
	t *testing.T,
	entities entity.Entities,
	clusterScope types.ScopeInfoMap,
) (entity.Entities, error) {
	t.Helper()
	c := &hydra.Cluster{}
	c.ResetPreferredVersionsCache()
	return NormalizeApiVersions(log.Default(), entities, types.KeyTemplateEntity, c, func() (types.ScopeInfoMap, error) {
		return clusterScope, nil
	})
}

func TestNormalizeApiVersions_MatchingVersion(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	version, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), version)

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "kafka.strimzi.io/v1", uResult.GetAPIVersion())
}

func TestNormalizeApiVersions_DeprecatedVersion(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	version, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), version)

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "kafka.strimzi.io/v1", uResult.GetAPIVersion())
}

// Regression: cluster review ref ownership builds templateIDToApp from per-app standalone renders
// without the same normalization as filtered sources; deprecated Strimzi apiVersions must still map
// to live ListClusterAll ids (kafka.strimzi.io/v1/...).
func TestNormalizePerAppTemplateEntities_KafkaStrimziDeprecatedAPIVersion(t *testing.T) {
	entities := deprecatedKafkaEntities(t)
	perApp := map[types.AppId]entity.Entities{
		types.AppId("in-cluster.demo-infra.demo-kafka"): entities,
	}
	clusterScope := types.ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka": types.ScopeInfo{},
	}
	compute := func() (types.ScopeInfoMap, error) { return clusterScope, nil }
	c := &hydra.Cluster{}
	c.ResetPreferredVersionsCache()

	out, err := NormalizePerAppTemplateEntities(log.Default(), c, perApp, types.KeyTemplateEntity, compute)
	require.NoError(t, err)
	require.Len(t, out, 1)
	var got entity.Entities
	for _, ents := range out {
		got = ents
		break
	}
	require.Equal(t, 1, got.Len())
	eid, err := got.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("kafka.strimzi.io/v1/Kafka/ns/my-kafka"), eid)
}

type normalizeWarnCapture struct {
	messages []string
}

func (n *normalizeWarnCapture) Enabled(context.Context, slog.Level) bool { return true }

func (n *normalizeWarnCapture) Handle(_ context.Context, r slog.Record) error {
	n.messages = append(n.messages, r.Message)
	return nil
}

func (n *normalizeWarnCapture) WithAttrs([]slog.Attr) slog.Handler { return n }
func (n *normalizeWarnCapture) WithGroup(string) slog.Handler      { return n }

func countApiVersionNormalizationWarnings(c *normalizeWarnCapture) int {
	n := 0
	for _, m := range c.messages {
		if strings.Contains(m, "normalizing API version") {
			n++
		}
	}
	return n
}

func deprecatedKafkaEntities(t *testing.T) entity.Entities {
	t.Helper()
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("ns")))
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)
	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)
	return entities
}

// Two renders with the same in-memory cluster must emit the API-version normalization warning only once.
func TestNormalizeApiVersions_WarnOncePerDedupKeyInMemory(t *testing.T) {
	cap := &normalizeWarnCapture{}
	testLogger := log.NewLoggerWithHandler(cap)
	c := &hydra.Cluster{}
	c.ResetPreferredVersionsCache()
	scope := types.ScopeInfoMap{"kafka.strimzi.io/v1/Kafka": types.ScopeInfo{}}
	compute := func() (types.ScopeInfoMap, error) { return scope, nil }

	entities := deprecatedKafkaEntities(t)
	_, err := NormalizeApiVersions(testLogger, entities, types.KeyTemplateEntity, c, compute)
	require.NoError(t, err)
	entities = deprecatedKafkaEntities(t)
	_, err = NormalizeApiVersions(testLogger, entities, types.KeyTemplateEntity, c, compute)
	require.NoError(t, err)
	assert.Equal(t, 1, countApiVersionNormalizationWarnings(cap))
}

func TestNormalizeApiVersions_NotInClusterScope(t *testing.T) {
	gvk := types.NewGVK(types.Group("custom.io"), types.Version("v1alpha1"), types.Kind("MyResource"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-res")).
		WithNamespace(types.Namespace("ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "custom.io/v1alpha1",
			"kind":       "MyResource",
			"metadata": map[string]any{
				"name":      "my-res",
				"namespace": "ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	version, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1alpha1"), version)

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "custom.io/v1alpha1", uResult.GetAPIVersion())
}

func TestNormalizeApiVersions_WithoutUnstructuredData(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("ns")))

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	version, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), version)
}

func TestNormalizeApiVersions_MixedEntities(t *testing.T) {
	matchingGvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1"), types.Kind("Kafka"))
	eMatching := mustBuild(entity.NewEntityBuilder().
		WithGVK(matchingGvk).
		WithName(types.Name("matching-kafka")).
		WithNamespace(types.Namespace("ns")))
	uMatching := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "matching-kafka",
				"namespace": "ns",
			},
		},
	}
	eMatching = withUnstructured(eMatching, types.KeyTemplateEntity, uMatching)

	deprecatedGvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("KafkaTopic"))
	eDeprecated := mustBuild(entity.NewEntityBuilder().
		WithGVK(deprecatedGvk).
		WithName(types.Name("my-topic")).
		WithNamespace(types.Namespace("ns")))
	uDeprecated := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "KafkaTopic",
			"metadata": map[string]any{
				"name":      "my-topic",
				"namespace": "ns",
			},
		},
	}
	eDeprecated = withUnstructured(eDeprecated, types.KeyTemplateEntity, uDeprecated)

	unknownGvk := types.NewGVK(types.Group("custom.io"), types.Version("v1alpha1"), types.Kind("Widget"))
	eUnknown := mustBuild(entity.NewEntityBuilder().
		WithGVK(unknownGvk).
		WithName(types.Name("my-widget")).
		WithNamespace(types.Namespace("ns")))
	uUnknown := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "custom.io/v1alpha1",
			"kind":       "Widget",
			"metadata": map[string]any{
				"name":      "my-widget",
				"namespace": "ns",
			},
		},
	}
	eUnknown = withUnstructured(eUnknown, types.KeyTemplateEntity, uUnknown)

	entities, err := entity.NewEntities([]entity.Entity{eMatching, eDeprecated, eUnknown})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka":      types.ScopeInfo{},
		"kafka.strimzi.io/v1/KafkaTopic": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 3, result.Len())

	matchingVersion, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), matchingVersion)

	deprecatedVersion, err := result.Items[1].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), deprecatedVersion, "deprecated entity should be updated to preferred version")

	uDepResult, err := result.Items[1].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "kafka.strimzi.io/v1", uDepResult.GetAPIVersion())

	unknownVersion, err := result.Items[2].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1alpha1"), unknownVersion, "unknown entity should remain unchanged")
}

func TestNormalizeApiVersions_CoreResource(t *testing.T) {
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-cm")).
		WithNamespace(types.Namespace("ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "my-cm",
				"namespace": "ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{
		"v1/ConfigMap": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	version, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), version)

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "v1", uResult.GetAPIVersion(), "core resource apiVersion must be 'v1', not '/v1'")
}

func TestNormalizeApiVersions_DuplicateIDsAfterNormalization(t *testing.T) {
	gvkOld := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	eOld := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvkOld).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("ns")))
	uOld := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "ns",
			},
		},
	}
	eOld = withUnstructured(eOld, types.KeyTemplateEntity, uOld)

	gvkNew := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1"), types.Kind("Kafka"))
	eNew := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvkNew).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("ns")))
	uNew := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "ns",
			},
		},
	}
	eNew = withUnstructured(eNew, types.KeyTemplateEntity, uNew)

	entities, err := entity.NewEntities([]entity.Entity{eOld, eNew})
	require.NoError(t, err)
	require.Equal(t, 2, entities.Len())

	clusterScope := types.ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len())

	version, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), version)
}

func TestNormalizeApiVersions_CoreResourceDeprecated(t *testing.T) {
	gvk := types.NewGVK(types.Group("events.k8s.io"), types.Version("v1beta1"), types.Kind("Event"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-event")).
		WithNamespace(types.Namespace("ns")))
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "events.k8s.io/v1beta1",
			"kind":       "Event",
			"metadata": map[string]any{
				"name":      "my-event",
				"namespace": "ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{
		"events.k8s.io/v1/Event": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	version, err := result.Items[0].Version()
	require.NoError(t, err)
	assert.Equal(t, types.Version("v1"), version)

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "events.k8s.io/v1", uResult.GetAPIVersion())
}
