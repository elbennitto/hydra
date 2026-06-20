package cel

import (
	"testing"

	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// mustBuild is for test helpers; panics if the entity is invalid.
func mustBuild(b entity.EntityBuilder) entity.Entity {
	e, err := b.Build()
	if err != nil {
		panic(err)
	}
	return e
}

func mustModify(e entity.Entity, fn func(entity.EntityBuilder) entity.EntityBuilder) entity.Entity {
	out, err := e.Modify(fn)
	if err != nil {
		panic(err)
	}
	return out
}

func makeTestEntity(group, version, kind, namespace, name string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().WithGVK(gvk).WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	return mustBuild(b)
}

func TestLabelsOnNullClusterEntity(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`clusterEntity.labels().getOrEmpty("app.kubernetes.io/managed-by") == "prometheus-operator"`,
	)
	require.NoError(t, err)

	e := makeTestEntity("", "v1", "Secret", "monitoring", "prometheus-secret")

	result, err := predicate.EvalBool(e, types.MissingKeysAccept)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestAnnotationsOnNullClusterEntity(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`clusterEntity.annotations().getOrEmpty("cert-manager.io/issuer-name") == "letsencrypt-prod"`,
	)
	require.NoError(t, err)

	e := makeTestEntity("", "v1", "Secret", "default", "tls-cert")

	result, err := predicate.EvalBool(e, types.MissingKeysAccept)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestLabelsOnClusterEntityWithMatchingLabel(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`clusterEntity.labels().getOrEmpty("app.kubernetes.io/managed-by") == "prometheus-operator"`,
	)
	require.NoError(t, err)

	e := mustModify(makeTestEntity("", "v1", "Secret", "monitoring", "prometheus-secret"), func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]any{
					"name":      "prometheus-secret",
					"namespace": "monitoring",
					"labels": map[string]any{
						"app.kubernetes.io/managed-by": "prometheus-operator",
					},
				},
			},
		})
	})

	result, err := predicate.EvalBool(e, types.MissingKeysAccept)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestLabelsOnClusterEntityWithNonMatchingLabel(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`clusterEntity.labels().getOrEmpty("app.kubernetes.io/managed-by") == "prometheus-operator"`,
	)
	require.NoError(t, err)

	e := mustModify(makeTestEntity("", "v1", "Secret", "monitoring", "some-secret"), func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]any{
					"name":      "some-secret",
					"namespace": "monitoring",
					"labels": map[string]any{
						"app.kubernetes.io/managed-by": "kyverno",
					},
				},
			},
		})
	})

	result, err := predicate.EvalBool(e, types.MissingKeysAccept)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestNamespacePredicateOnResourceLevelEntity(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`ns=="default"`)
	require.NoError(t, err)

	// Resource-level entity: only kind/apiVersion/resource/namespaced are set, no namespace.
	// This mirrors how visit.go builds entities before K8s API calls.
	e := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(types.NewApiVersion("apps", "v1")).
		WithResource("deployments").
		WithKind(types.Kind("Deployment")).
		WithNamespaced(types.Namespaced(false)).
		WithName(""))

	// The ns reader returns "" (not ErrKeyNotFound) when namespace is unset,
	// so MissingKeysAccept does not kick in. This is expected — ListCluster
	// compensates via retry when the resource-level filter rejects everything.
	result, err := predicate.EvalBool(e, types.MissingKeysAccept)
	require.NoError(t, err)
	assert.False(t, result, "ns predicate evaluates to false at resource level (ns is empty string, not missing key)")
}

func TestNamespacePredicateOnItemLevelEntity(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`ns=="default"`)
	require.NoError(t, err)

	e := makeTestEntity("apps", "v1", "Deployment", "default", "nginx")
	result, err := predicate.EvalBool(e, types.MissingKeysError)
	require.NoError(t, err)
	assert.True(t, result)

	e2 := makeTestEntity("apps", "v1", "Deployment", "kube-system", "coredns")
	result2, err := predicate.EvalBool(e2, types.MissingKeysError)
	require.NoError(t, err)
	assert.False(t, result2)
}

func TestApiVersionPredicateMatchesCoreV1Entity(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`apiVersion == "v1"`)
	require.NoError(t, err)

	e := makeTestEntity("", "v1", "ConfigMap", "default", "demo")
	result, err := predicate.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestLabelsOnClusterEntityWithNoLabels(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`clusterEntity.labels().getOrEmpty("app.kubernetes.io/managed-by") == "prometheus-operator"`,
	)
	require.NoError(t, err)

	e := mustModify(makeTestEntity("", "v1", "Secret", "monitoring", "no-labels-secret"), func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]any{
					"name":      "no-labels-secret",
					"namespace": "monitoring",
				},
			},
		})
	})

	result, err := predicate.EvalBool(e, types.MissingKeysAccept)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestRefHasEndpointFunction(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	pred, err := env.CompilePredicateAt("test ref.hasEndpoint", types.CelPredicate(`size([refBuilder().outgoing(id('v1/Secret', ns, 'target-secret')).label('demo')].filter(ref, ref.hasEndpoint('v1/Secret/default/target-secret'))) == 1`))
	require.NoError(t, err)

	e := makeTestEntity("apps", "v1", "Deployment", "default", "api")
	ok, err := pred.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	assert.True(t, ok)

	predMiss, err := env.CompilePredicateAt("test ref.hasEndpoint miss", types.CelPredicate(`size([refBuilder().outgoing(id('v1/Secret', ns, 'target-secret'))].filter(ref, ref.hasEndpoint('v1/Secret/default/other-secret'))) == 0`))
	require.NoError(t, err)

	okMiss, err := predMiss.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	assert.True(t, okMiss)
}

func TestGvknCelVariableNamespacedLease(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	want := "coordination.k8s.io/v1/Lease/longhorn-system"
	predicate, err := env.CompilePredicate(types.CelPredicate(`gvkn == "` + want + `"`))
	require.NoError(t, err)

	e := makeTestEntity("coordination.k8s.io", "v1", "Lease", "longhorn-system", "shard-0")
	ok, err := predicate.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestGVKFunctionOnEventRegardingPod(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`gvk(clusterEntity.regarding) == "v1/Pod" && gvkn(clusterEntity.regarding) == "v1/Pod/kube-system"`,
	)
	require.NoError(t, err)

	e := mustModify(makeTestEntity("events.k8s.io", "v1", "Event", "kube-system", "coredns-7566b5ff58-zpk45.18afbe8a40c844a0"), func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "events.k8s.io/v1",
				"kind":       "Event",
				"metadata": map[string]any{
					"name":      "coredns-7566b5ff58-zpk45.18afbe8a40c844a0",
					"namespace": "kube-system",
				},
				"regarding": map[string]any{
					"apiVersion":      "v1",
					"kind":            "Pod",
					"name":            "coredns-7566b5ff58-zpk45",
					"namespace":       "kube-system",
					"resourceVersion": "477270",
					"uid":             "7e3bcb1b-e13b-4e35-9c4d-4c2e59ebccb0",
				},
			},
		})
	})

	result, err := predicate.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestGVKFunctionsReturnEmptyStringForMissingRegarding(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`gvk(clusterEntity.regarding) == "" && gvkn(clusterEntity.regarding) == ""`,
	)
	require.NoError(t, err)

	e := mustModify(makeTestEntity("events.k8s.io", "v1", "Event", "kube-system", "no-regarding"), func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "events.k8s.io/v1",
				"kind":       "Event",
				"metadata": map[string]any{
					"name":      "no-regarding",
					"namespace": "kube-system",
				},
			},
		})
	})

	result, err := predicate.EvalBool(e, types.MissingKeysAccept)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestGVKFunctionsReturnEmptyStringForNullRegarding(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(
		`gvk(clusterEntity.regarding) == "" && gvkn(clusterEntity.regarding) == ""`,
	)
	require.NoError(t, err)

	e := mustModify(makeTestEntity("events.k8s.io", "v1", "Event", "kube-system", "null-regarding"), func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "events.k8s.io/v1",
				"kind":       "Event",
				"metadata": map[string]any{
					"name":      "null-regarding",
					"namespace": "kube-system",
				},
				"regarding": nil,
			},
		})
	})

	result, err := predicate.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestGvknCelVariableClusterScopedNoNamespaceSuffix(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	want := "rbac.authorization.k8s.io/v1/ClusterRole"
	predicate, err := env.CompilePredicate(types.CelPredicate(`gvkn == "` + want + `"`))
	require.NoError(t, err)

	e := makeTestEntity("rbac.authorization.k8s.io", "v1", "ClusterRole", "", "nodes")
	ok, err := predicate.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	assert.True(t, ok)
}
