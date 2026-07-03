package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	hcel "hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func resourceLevelEntity() entity.Entity {
	return mustBuild(entity.NewEntityBuilder().
		WithApiVersion(types.NewApiVersion("apps", "v1")).
		WithResource("deployments").
		WithKind("Deployment").
		WithName("").
		WithNamespaced(types.Namespaced(false)))
}

func TestResourceFilterRejectsNamespacePredicate(t *testing.T) {
	env, err := hcel.NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`ns=="default"`)
	require.NoError(t, err)

	resourceAccepted := false
	resourceFilter := func(e entity.Entity) (bool, error) {
		evalResult, err := predicate.EvalBool(e, types.MissingKeysAccept)
		if err != nil {
			return false, err
		}
		if evalResult {
			resourceAccepted = true
		}
		return evalResult, nil
	}

	e := resourceLevelEntity()
	apiResource := &metav1.APIResource{Name: "deployments", Kind: "Deployment"}

	result := []entity.Entity{}
	handlers := listClusterHandlers(log.Default(), &result, resourceFilter, predicate.EvalBool, nil)

	accepted, err := handlers.HandleNamespacedResource(e, apiResource)
	require.NoError(t, err)
	assert.False(t, accepted, "resource-level filter should reject ns predicate (ns is empty at resource level)")
	assert.False(t, resourceAccepted, "resourceAccepted should remain false")
}

func TestAcceptAllResourceFilterAcceptsEverything(t *testing.T) {
	env, err := hcel.NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`ns=="default"`)
	require.NoError(t, err)

	acceptAll := func(e entity.Entity) (bool, error) { return true, nil }

	e := resourceLevelEntity()
	apiResource := &metav1.APIResource{Name: "deployments", Kind: "Deployment"}

	result := []entity.Entity{}
	handlers := listClusterHandlers(log.Default(), &result, acceptAll, predicate.EvalBool, nil)

	accepted, err := handlers.HandleNamespacedResource(e, apiResource)
	require.NoError(t, err)
	assert.True(t, accepted, "acceptAll resource filter should accept everything")
}

func TestNamespaceListFilterWorksWithNamespaceSet(t *testing.T) {
	env, err := hcel.NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`ns=="default"`)
	require.NoError(t, err)

	acceptAll := func(e entity.Entity) (bool, error) { return true, nil }

	result := []entity.Entity{}
	handlers := listClusterHandlers(log.Default(), &result, acceptAll, predicate.EvalBool, nil)

	eDefault := withNamespace(resourceLevelEntity(), "default")
	accepted, err := handlers.HandleNamespacedResourceList(eDefault, nil)
	require.NoError(t, err)
	assert.True(t, accepted, "namespace 'default' should be accepted")

	eKubeSystem := withNamespace(resourceLevelEntity(), "kube-system")
	accepted, err = handlers.HandleNamespacedResourceList(eKubeSystem, nil)
	require.NoError(t, err)
	assert.False(t, accepted, "namespace 'kube-system' should be rejected")
}

func TestItemFilterAcceptsMatchingNamespace(t *testing.T) {
	env, err := hcel.NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`ns=="default"`)
	require.NoError(t, err)

	acceptAll := func(e entity.Entity) (bool, error) { return true, nil }

	result := []entity.Entity{}
	handlers := listClusterHandlers(log.Default(), &result, acceptAll, predicate.EvalBool, nil)

	eMatch := withUnstructured(makeEntity("apps", "v1", "Deployment", "default", "nginx"),
		types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment"},
		})
	err = handlers.HandleNamespacedResourceItem(eMatch)
	require.NoError(t, err)
	assert.Len(t, result, 1, "matching item should be added to result")

	eNoMatch := withUnstructured(makeEntity("apps", "v1", "Deployment", "kube-system", "coredns"),
		types.KeyClusterEntity, unstructured.Unstructured{
			Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment"},
		})
	err = handlers.HandleNamespacedResourceItem(eNoMatch)
	require.NoError(t, err)
	assert.Len(t, result, 1, "non-matching item should not be added to result")
}

func TestRetryDecision(t *testing.T) {
	env, err := hcel.NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`ns=="default"`)
	require.NoError(t, err)

	resourceAccepted := false
	resourceFilter := func(e entity.Entity) (bool, error) {
		evalResult, err := predicate.EvalBool(e, types.MissingKeysAccept)
		if err != nil {
			return false, err
		}
		if evalResult {
			resourceAccepted = true
		}
		return evalResult, nil
	}

	e := resourceLevelEntity()
	apiResource := &metav1.APIResource{Name: "deployments", Kind: "Deployment"}

	result := []entity.Entity{}
	handlers := listClusterHandlers(log.Default(), &result, resourceFilter, predicate.EvalBool, nil)

	// Simulate visiting multiple resource types — none match at resource level
	for _, kind := range []string{"Deployment", "Service", "ConfigMap"} {
		re := withKind(e, types.Kind(kind))
		accepted, err := handlers.HandleNamespacedResource(re, apiResource)
		require.NoError(t, err)
		assert.False(t, accepted)
	}

	assert.False(t, resourceAccepted, "no resource should have been accepted")

	// Retry with acceptAll — now the namespace-list and item handlers should filter correctly
	acceptAll := func(e entity.Entity) (bool, error) { return true, nil }
	result = []entity.Entity{}
	retryHandlers := listClusterHandlers(log.Default(), &result, acceptAll, predicate.EvalBool, nil)

	accepted, err := retryHandlers.HandleNamespacedResource(e, apiResource)
	require.NoError(t, err)
	assert.True(t, accepted, "retry should accept all resources")

	eDefault := withNamespace(e, "default")
	accepted, err = retryHandlers.HandleNamespacedResourceList(eDefault, nil)
	require.NoError(t, err)
	assert.True(t, accepted, "namespace 'default' should match in retry")

	eItem := makeEntity("apps", "v1", "Deployment", "default", "nginx")
	err = retryHandlers.HandleNamespacedResourceItem(eItem)
	require.NoError(t, err)
	assert.Len(t, result, 1, "item in namespace 'default' should be collected in retry")
}

func TestKindPredicateNoRetryNeeded(t *testing.T) {
	env, err := hcel.NewEnv()
	require.NoError(t, err)

	predicate, err := env.CompilePredicate(`kind=="Deployment"`)
	require.NoError(t, err)

	resourceAccepted := false
	resourceFilter := func(e entity.Entity) (bool, error) {
		evalResult, err := predicate.EvalBool(e, types.MissingKeysAccept)
		if err != nil {
			return false, err
		}
		if evalResult {
			resourceAccepted = true
		}
		return evalResult, nil
	}

	apiResource := &metav1.APIResource{Name: "deployments", Kind: "Deployment"}

	result := []entity.Entity{}
	handlers := listClusterHandlers(log.Default(), &result, resourceFilter, predicate.EvalBool, nil)

	eDeployment := resourceLevelEntity()
	accepted, err := handlers.HandleNamespacedResource(eDeployment, apiResource)
	require.NoError(t, err)
	assert.True(t, accepted, "kind==Deployment should match at resource level")
	assert.True(t, resourceAccepted, "resourceAccepted should be true — no retry needed")
}
