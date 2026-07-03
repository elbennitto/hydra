package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makeAppProjectEntity(name string, syncWindows []any, key types.EntityKeyUnstructured) entity.Entity {
	gvk := types.NewGVK(types.Group("argoproj.io"), types.Version("v1alpha1"), types.Kind("AppProject"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("appprojects")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace("argocd")))

	obj := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "AppProject",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "argocd",
		},
		"spec": map[string]any{},
	}
	if syncWindows != nil {
		obj["spec"].(map[string]any)["syncWindows"] = syncWindows
	}
	u := unstructured.Unstructured{Object: obj}
	return withUnstructured(e, key, u)
}

func TestPreventSyncWindowsWithMutationCount_AllowToDenyCountsOne(t *testing.T) {
	proj := makeAppProjectEntity("my-project", []any{
		map[string]any{
			"kind":         "allow",
			"schedule":     "0 22 * * *",
			"duration":     "1h",
			"applications": []any{"*"},
			"manualSync":   true,
		},
	}, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, n, err := PreventSyncWindowsWithMutationCount(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Equal(t, 1, result.Len())
}

func TestPreventSyncWindowsWithMutationCount_AlreadyDenyCountsZero(t *testing.T) {
	proj := makeAppProjectEntity("my-project", []any{
		map[string]any{
			"kind":       "deny",
			"manualSync": false,
		},
	}, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, n, err := PreventSyncWindowsWithMutationCount(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	require.Equal(t, 1, result.Len())
}

func TestPreventSyncWindows_SyncWindowsSetToDeny(t *testing.T) {
	proj := makeAppProjectEntity("my-project", []any{
		map[string]any{
			"kind":         "allow",
			"schedule":     "0 22 * * *",
			"duration":     "1h",
			"applications": []any{"*"},
			"manualSync":   true,
		},
	}, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	sw := values.Lookup(u.Object, "spec", "syncWindows")
	require.NotNil(t, sw)
	windows, ok := sw.([]any)
	require.True(t, ok)
	require.Len(t, windows, 1)

	entry, ok := windows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deny", entry["kind"])
	assert.Equal(t, false, entry["manualSync"])
	assert.Equal(t, "0 22 * * *", entry["schedule"])
	assert.Equal(t, "1h", entry["duration"])
	assert.Equal(t, []any{"*"}, entry["applications"])
}

func TestPreventSyncWindows_AlreadyDenyIsIdempotent(t *testing.T) {
	proj := makeAppProjectEntity("my-project", []any{
		map[string]any{
			"kind":       "deny",
			"manualSync": false,
		},
	}, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	sw := values.Lookup(u.Object, "spec", "syncWindows")
	require.NotNil(t, sw)
	windows, ok := sw.([]any)
	require.True(t, ok)
	require.Len(t, windows, 1)

	entry, ok := windows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deny", entry["kind"])
	assert.Equal(t, false, entry["manualSync"])
}

func TestPreventSyncWindows_MissingSyncWindowsLogsWarning(t *testing.T) {
	proj := makeAppProjectEntity("my-project", nil, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	sw := values.Lookup(u.Object, "spec", "syncWindows")
	assert.Nil(t, sw)
}

func TestPreventSyncWindows_EmptySyncWindowsLogsWarning(t *testing.T) {
	proj := makeAppProjectEntity("my-project", []any{}, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	sw := values.Lookup(u.Object, "spec", "syncWindows")
	require.NotNil(t, sw)
	windows, ok := sw.([]any)
	require.True(t, ok)
	assert.Empty(t, windows)
}

func TestPreventSyncWindows_NonAppProjectUnchanged(t *testing.T) {
	cm := makeConfigMapWithData("default", "app-config", types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, "ConfigMap", u.Object["kind"])
	data := values.Lookup(u.Object, "data", "config.yaml")
	assert.Equal(t, "key: value", data)
}

func TestPreventSyncWindows_MixedEntities(t *testing.T) {
	proj := makeAppProjectEntity("my-project", []any{
		map[string]any{
			"kind":       "allow",
			"manualSync": true,
		},
	}, types.KeyTemplateEntity)
	cm := makeConfigMapWithData("default", "app-config", types.KeyTemplateEntity)
	dep := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web-app", 3, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj, cm, dep})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 3, result.Len())

	// AppProject should be modified
	uProj, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	sw := values.Lookup(uProj.Object, "spec", "syncWindows")
	require.NotNil(t, sw)
	windows, ok := sw.([]any)
	require.True(t, ok)
	require.Len(t, windows, 1)
	entry, ok := windows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deny", entry["kind"])
	assert.Equal(t, false, entry["manualSync"])

	// ConfigMap should be unchanged
	uCM, err := result.Items[1].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "ConfigMap", uCM.Object["kind"])

	// Deployment should be unchanged
	uDep, err := result.Items[2].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	replicas := values.Lookup(uDep.Object, "spec", "replicas")
	assert.EqualValues(t, 3, replicas)
}

func TestPreventSyncWindows_MultipleSyncWindows(t *testing.T) {
	proj := makeAppProjectEntity("my-project", []any{
		map[string]any{
			"kind":         "allow",
			"schedule":     "0 22 * * *",
			"duration":     "1h",
			"manualSync":   true,
			"applications": []any{"app-a"},
		},
		map[string]any{
			"kind":         "allow",
			"schedule":     "0 6 * * *",
			"duration":     "2h",
			"manualSync":   true,
			"applications": []any{"app-b"},
		},
	}, types.KeyTemplateEntity)

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	sw := values.Lookup(u.Object, "spec", "syncWindows")
	require.NotNil(t, sw)
	windows, ok := sw.([]any)
	require.True(t, ok)
	require.Len(t, windows, 2)

	for i, w := range windows {
		entry, ok := w.(map[string]any)
		require.True(t, ok, "syncWindow entry %d should be a map", i)
		assert.Equal(t, "deny", entry["kind"], "entry %d kind", i)
		assert.Equal(t, false, entry["manualSync"], "entry %d manualSync", i)
	}

	// Verify other fields preserved
	first, _ := windows[0].(map[string]any)
	assert.Equal(t, "0 22 * * *", first["schedule"])
	assert.Equal(t, "1h", first["duration"])
	assert.Equal(t, []any{"app-a"}, first["applications"])

	second, _ := windows[1].(map[string]any)
	assert.Equal(t, "0 6 * * *", second["schedule"])
	assert.Equal(t, "2h", second["duration"])
	assert.Equal(t, []any{"app-b"}, second["applications"])
}

func TestPreventSyncWindows_EntityWithoutUnstructuredData(t *testing.T) {
	proj := makeEntity("argoproj.io", "v1alpha1", "AppProject", "argocd", "my-project")

	entities, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	result, err := PreventSyncWindows(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	name, err := result.Items[0].Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("my-project"), name)
}
