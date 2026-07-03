package commands

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func testTemplateEntity(resource types.Resource, kind types.Kind, namespace, name string, appID types.AppId) entity.Entity {
	builder := entity.NewEntityBuilder().
		WithGroup("").
		WithVersion("v1").
		WithResource(resource).
		WithKind(kind).
		WithName(types.Name(name)).
		WithNamespaced(namespace != "")
	if namespace != "" {
		builder = builder.WithNamespace(types.Namespace(namespace))
	}
	builder = builder.WithAppId(appID)
	return mustBuild(builder)
}

func testLiveEntity(apiVersion, resource, kind, namespace, name string) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name": name,
		},
	}}
	parsedApiVersion, err := types.ParseApiVersion(apiVersion)
	if err != nil {
		panic(err)
	}
	builder := entity.NewEntityBuilder().
		WithApiVersion(parsedApiVersion).
		WithResource(types.Resource(resource)).
		WithKind(types.Kind(kind)).
		WithName(types.Name(name)).
		WithNamespaced(namespace != "")
	if namespace != "" {
		u.SetNamespace(namespace)
		builder = builder.WithNamespace(types.Namespace(namespace))
	}
	builder = builder.WithUnstructured(types.KeyClusterEntity, u)
	return mustBuild(builder)
}

func testScopeInfo() types.ScopeInfoMap {
	return types.ScopeInfoMap{
		"v1/ConfigMap": {Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true},
		"v1/Pod":       {Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true},
		"v1/Namespace": {Group: "", Version: "v1", Resource: "namespaces", Kind: "Namespace", Namespaced: false},
		"rbac.authorization.k8s.io/v1/ClusterRole": {
			Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles", Kind: "ClusterRole", Namespaced: false,
		},
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestInventoryFromEntities_ClassifiesLiveEntityAsAppOwnedAndBuiltIn(t *testing.T) {
	t.Parallel()

	tpl := mustBuild(entity.NewEntityBuilder().
		WithGroup("rbac.authorization.k8s.io").
		WithVersion("v1").
		WithResource("clusterroles").
		WithKind("ClusterRole").
		WithName("view").
		WithAppId("cluster.app"))
	live := mustBuild(entity.NewEntityBuilder().
		WithGroup("rbac.authorization.k8s.io").
		WithVersion("v1").
		WithResource("clusterroles").
		WithKind("ClusterRole").
		WithName("view").
		WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata":   map[string]any{"name": "view"},
		}}))
	templates, err := entity.NewEntities([]entity.Entity{tpl})
	require.NoError(t, err)
	liveEntities, err := entity.NewEntities([]entity.Entity{live})
	require.NoError(t, err)

	inv, err := newInventoryFromEntities(nil, types.HelmNetworkModeOffline, types.BootstrapNo, templates, liveEntities, withInventoryScopeInfo(testScopeInfo()), withInventoryKubernetesMinor(99))
	require.NoError(t, err)

	out := inv.LiveEntities()
	require.Len(t, out.Items, 1)
	require.True(t, out.Items[0].AppOwned())
	require.True(t, out.Items[0].BuiltIn())
	require.Equal(t, types.EntityOwnershipAppOwned, out.Items[0].Ownership())
	require.Len(t, inv.TemplateEntities().Items, 1)
}

func TestInventoryFromEntities_LiveOnlyLeavesEntityUntracked(t *testing.T) {
	t.Parallel()

	live := testLiveEntity("v1", "configmaps", "ConfigMap", "default", "app-config")
	liveEntities, err := entity.NewEntities([]entity.Entity{live})
	require.NoError(t, err)
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)

	inv, err := newInventory(nil, types.HelmNetworkModeOffline, types.BootstrapNo, empty, liveEntities, testScopeInfo(), 99, true, inventoryBuildOptions{})
	require.NoError(t, err)

	out := inv.LiveEntities()
	require.Len(t, out.Items, 1)
	require.False(t, out.Items[0].AppOwned())
	require.False(t, out.Items[0].BuiltIn())
	require.Equal(t, types.EntityOwnershipUntracked, out.Items[0].Ownership())
	require.Len(t, inv.TemplateEntities().Items, 0)
}

func TestInventoryWatch_ApplyAndDeleteUpdatesPresenceAndOwnership(t *testing.T) {
	t.Parallel()

	watcher := k8swatch.NewRaceFreeFake()
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependWatchReactor("configmaps", func(action clienttesting.Action) (bool, k8swatch.Interface, error) {
		return true, watcher, nil
	})

	templates, err := entity.NewEntities([]entity.Entity{testTemplateEntity("configmaps", "ConfigMap", "default", "app-config", "cluster.app")})
	require.NoError(t, err)
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inv, err := newInventoryFromEntities(nil, types.HelmNetworkModeOffline, types.BootstrapNo, templates, empty,
		withInventoryScopeInfo(testScopeInfo()),
		withInventoryDynamicClient(client),
		withInventoryWatch(ctx),
		withInventoryKubernetesMinor(99),
	)
	require.NoError(t, err)
	defer inv.Close()

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "app-config",
			"namespace": "default",
		},
	}}
	watcher.Add(obj)
	waitForCondition(t, time.Second, func() bool {
		live := inv.LiveEntities()
		return live.Len() == 1 && live.Items[0].AppOwned()
	})
	id, err := templates.Items[0].Id()
	require.NoError(t, err)
	require.Equal(t, "ok", inv.PresenceStatus(id))

	watcher.Delete(obj)
	waitForCondition(t, time.Second, func() bool {
		return inv.LiveEntities().Len() == 0
	})
	require.Equal(t, "template only", inv.PresenceStatus(id))
}

func TestInventoryWatch_RecomputesRefsAfterChange(t *testing.T) {
	t.Parallel()

	watcherPods := k8swatch.NewRaceFreeFake()
	watcherConfigMaps := k8swatch.NewRaceFreeFake()
	cmObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "app-config",
			"namespace": "default",
		},
	}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), cmObj)
	client.PrependWatchReactor("pods", func(action clienttesting.Action) (bool, k8swatch.Interface, error) {
		return true, watcherPods, nil
	})
	client.PrependWatchReactor("configmaps", func(action clienttesting.Action) (bool, k8swatch.Interface, error) {
		return true, watcherConfigMaps, nil
	})

	liveEntities, err := entity.NewEntities([]entity.Entity{testLiveEntity("v1", "configmaps", "ConfigMap", "default", "app-config")})
	require.NoError(t, err)
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inv, err := newInventoryFromEntities(nil, types.HelmNetworkModeOffline, types.BootstrapNo, empty, liveEntities,
		withInventoryScopeInfo(testScopeInfo()),
		withInventoryDynamicClient(client),
		withInventoryWatch(ctx),
		withInventoryKubernetesMinor(99),
	)
	require.NoError(t, err)
	defer inv.Close()

	refs, err := inv.RefsMerged()
	require.NoError(t, err)
	initialRefCount := len(refs)

	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      "consumer",
			"namespace": "default",
		},
		"spec": map[string]any{
			"volumes": []any{map[string]any{
				"name":      "cfg",
				"configMap": map[string]any{"name": "app-config"},
			}},
		},
	}}
	watcherPods.Add(pod)
	waitForCondition(t, time.Second, func() bool {
		refs, err := inv.RefsMerged()
		return err == nil && len(refs) > initialRefCount
	})
}

func TestInventory_LiveEntitiesFiltered_UsesUnifiedLiveView(t *testing.T) {
	t.Parallel()

	liveA := testLiveEntity("v1", "configmaps", "ConfigMap", "default", "keep")
	liveB := testLiveEntity("v1", "configmaps", "ConfigMap", "default", "drop")
	liveEntities, err := entity.NewEntities([]entity.Entity{liveA, liveB})
	require.NoError(t, err)
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)

	inv, err := newInventoryFromEntities(nil, types.HelmNetworkModeOffline, types.BootstrapNo, empty, liveEntities, withInventoryKubernetesMinor(99))
	require.NoError(t, err)
	defer inv.Close()

	env, err := cel.NewEnv()
	require.NoError(t, err)
	predicate, err := env.CompilePredicate(`name == "keep"`)
	require.NoError(t, err)

	filtered, err := inv.LiveEntitiesFiltered(predicate)
	require.NoError(t, err)
	require.Len(t, filtered.Items, 1)
	id, err := filtered.Items[0].Id()
	require.NoError(t, err)
	require.Equal(t, types.Id("v1/ConfigMap/default/keep"), id)
}
