package k8s

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	clienttesting "k8s.io/client-go/testing"

	"k8s.io/client-go/dynamic/fake"
)

type applyResult struct {
	Resource string
	Action   string
}

func collectCallback(results *[]applyResult) ApplyCallback {
	return func(resource string, action string, id types.Id) {
		_ = id
		*results = append(*results, applyResult{Resource: resource, Action: action})
	}
}

func apiVersionString(group, version string) string {
	if group == "" {
		return version
	}
	return group + "/" + version
}

func metadataMap(namespace, name string) map[string]any {
	m := map[string]any{"name": name}
	if namespace != "" {
		m["namespace"] = namespace
	}
	return m
}

func makeApplyEntity(group, version, kind, namespace, name string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().WithGVK(gvk).WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersionString(group, version),
			"kind":       kind,
			"metadata":   metadataMap(namespace, name),
		},
	}
	return mustBuild(b.WithUnstructured(types.KeyTemplateEntity, u))
}

func newTestRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "", Version: "v1"},
		{Group: "apps", Version: "v1"},
	})
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	return mapper
}

func newFakeApplyClient(objects ...runtime.Object) *fake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme, objects...)
	client.PrependReactor("patch", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchAction := action.(clienttesting.PatchAction)
		if patchAction.GetPatchType() != k8stypes.ApplyPatchType {
			return false, nil, nil
		}
		var obj unstructured.Unstructured
		if err := json.Unmarshal(patchAction.GetPatch(), &obj.Object); err != nil {
			return true, nil, err
		}
		return true, &obj, nil
	})
	return client
}

func newTestClusterClient(objects ...runtime.Object) (*ClusterClient, *fake.FakeDynamicClient) {
	fakeClient := newFakeApplyClient(objects...)
	cc := &ClusterClient{
		Dynamic:    fakeClient,
		RESTMapper: newTestRESTMapper(),
	}
	return cc, fakeClient
}

func TestApply_SSA_NamespacedResource(t *testing.T) {
	cc, fakeClient := newTestClusterClient()
	e := makeApplyEntity("", "v1", "ConfigMap", "default", "test-cm")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, true, nil, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "configmap/test-cm", results[0].Resource)
	assert.Equal(t, "serverside-applied", results[0].Action)

	actions := fakeClient.Actions()
	require.NotEmpty(t, actions, "expected at least one action")

	var foundPatch bool
	for _, action := range actions {
		if patchAction, ok := action.(clienttesting.PatchAction); ok {
			assert.Equal(t, k8stypes.ApplyPatchType, patchAction.GetPatchType())
			assert.Equal(t, "default", patchAction.GetNamespace())
			assert.Equal(t, "test-cm", patchAction.GetName())
			assert.Equal(t, "configmaps", patchAction.GetResource().Resource)
			foundPatch = true
		}
	}
	assert.True(t, foundPatch, "expected a Patch action with ApplyPatchType")
}

func TestApply_SSA_ClusterScopedResource(t *testing.T) {
	cc, fakeClient := newTestClusterClient()
	e := makeApplyEntity("", "v1", "Namespace", "", "test-ns")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, true, nil, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "namespace/test-ns", results[0].Resource)

	actions := fakeClient.Actions()
	require.NotEmpty(t, actions, "expected at least one action")

	var foundPatch bool
	for _, action := range actions {
		if patchAction, ok := action.(clienttesting.PatchAction); ok {
			assert.Equal(t, k8stypes.ApplyPatchType, patchAction.GetPatchType())
			assert.Empty(t, patchAction.GetNamespace(), "cluster-scoped resource should have no namespace")
			assert.Equal(t, "test-ns", patchAction.GetName())
			assert.Equal(t, "namespaces", patchAction.GetResource().Resource)
			foundPatch = true
		}
	}
	assert.True(t, foundPatch, "expected a Patch action with ApplyPatchType for cluster-scoped resource")
}

func TestApply_ClientSide_CreateNew(t *testing.T) {
	cc, fakeClient := newTestClusterClient()
	e := makeApplyEntity("", "v1", "ConfigMap", "default", "test-cm")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, false, nil, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "configmap/test-cm", results[0].Resource)
	assert.Equal(t, "created", results[0].Action)

	actions := fakeClient.Actions()
	require.NotEmpty(t, actions)

	var foundCreate bool
	for _, action := range actions {
		if createAction, ok := action.(clienttesting.CreateAction); ok {
			obj := createAction.GetObject().(*unstructured.Unstructured)
			annotations := obj.GetAnnotations()
			assert.Contains(t, annotations, "kubectl.kubernetes.io/last-applied-configuration",
				"created object should have last-applied-configuration annotation")
			foundCreate = true
		}
	}
	assert.True(t, foundCreate, "expected a Create action for new resource")
}

func TestApply_ClientSide_UpdateExisting(t *testing.T) {
	lastApplied := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "test-cm",
			"namespace": "default",
		},
		"data": map[string]any{
			"key": "old-value",
		},
	}
	lastAppliedJSON, err := json.Marshal(lastApplied)
	require.NoError(t, err)

	existing := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-cm",
				"namespace": "default",
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": string(lastAppliedJSON),
				},
			},
			"data": map[string]any{
				"key": "old-value",
			},
		},
	}

	cc, fakeClient := newTestClusterClient(existing)

	desiredObj := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   metadataMap("default", "test-cm"),
		"data": map[string]any{
			"key": "new-value",
		},
	}
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("test-cm")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyTemplateEntity, unstructured.Unstructured{Object: desiredObj}))

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, false, nil, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "configured", results[0].Action)

	actions := fakeClient.Actions()
	var foundPatch bool
	for _, action := range actions {
		if patchAction, ok := action.(clienttesting.PatchAction); ok {
			assert.Equal(t, k8stypes.MergePatchType, patchAction.GetPatchType(),
				"client-side update should use MergePatchType")
			foundPatch = true
		}
	}
	assert.True(t, foundPatch, "expected a Patch action for updating existing resource")
}

func TestApply_Replace_ClientSideDeletesBeforeCreate(t *testing.T) {
	lastApplied := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "test-cm",
			"namespace": "default",
		},
		"data": map[string]any{
			"key": "old-value",
		},
	}
	lastAppliedJSON, err := json.Marshal(lastApplied)
	require.NoError(t, err)

	existing := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-cm",
				"namespace": "default",
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": string(lastAppliedJSON),
				},
			},
			"data": map[string]any{
				"key": "old-value",
			},
		},
	}

	cc, fakeClient := newTestClusterClient(existing)

	desiredObj := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   metadataMap("default", "test-cm"),
		"data": map[string]any{
			"key": "new-value",
		},
	}
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("test-cm")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyTemplateEntity, unstructured.Unstructured{Object: desiredObj}))

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	entityID, err := e.Id()
	require.NoError(t, err)
	deleteSet := sets.New[types.Id](entityID)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, false, deleteSet, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "created", results[0].Action)

	actions := fakeClient.Actions()
	var deleteIdx, createIdx = -1, -1
	for i, action := range actions {
		if _, ok := action.(clienttesting.DeleteAction); ok {
			deleteIdx = i
		}
		if _, ok := action.(clienttesting.CreateAction); ok {
			createIdx = i
		}
	}
	require.NotEqual(t, -1, deleteIdx, "expected Delete before replace when entity ID is in delete set")
	require.NotEqual(t, -1, createIdx, "expected Create after delete")
	assert.Less(t, deleteIdx, createIdx, "delete should run before create")
}

func TestApply_ClientSide_Unchanged(t *testing.T) {
	desiredObj := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "test-cm",
			"namespace": "default",
		},
		"data": map[string]any{
			"key": "same-value",
		},
	}
	lastAppliedJSON, err := json.Marshal(desiredObj)
	require.NoError(t, err)

	existing := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-cm",
				"namespace": "default",
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": string(lastAppliedJSON),
				},
			},
			"data": map[string]any{
				"key": "same-value",
			},
		},
	}

	cc, _ := newTestClusterClient(existing)

	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("test-cm")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyTemplateEntity, unstructured.Unstructured{Object: desiredObj}))

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, false, nil, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "unchanged", results[0].Action)
}

func TestApply_DryRun(t *testing.T) {
	cc, fakeClient := newTestClusterClient()
	e := makeApplyEntity("", "v1", "ConfigMap", "default", "test-cm")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, true, true, nil, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)

	actions := fakeClient.Actions()
	require.NotEmpty(t, actions)

	var foundDryRunAction bool
	for _, action := range actions {
		if patchImpl, ok := action.(clienttesting.PatchActionImpl); ok {
			assert.Contains(t, patchImpl.PatchOptions.DryRun, metav1.DryRunAll,
				"DryRun option should be set on PatchOptions")
			foundDryRunAction = true
		}
		if createImpl, ok := action.(clienttesting.CreateActionImpl); ok {
			assert.Contains(t, createImpl.CreateOptions.DryRun, metav1.DryRunAll,
				"DryRun option should be set on CreateOptions")
			foundDryRunAction = true
		}
	}
	assert.True(t, foundDryRunAction, "expected at least one action with DryRun option verified")
}

func TestApply_EmptyEntities(t *testing.T) {
	cc, fakeClient := newTestClusterClient()

	entities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, true, nil, collectCallback(&results), false)
	require.NoError(t, err)
	assert.Empty(t, results, "empty entities should produce no callback invocations")
	assert.Empty(t, fakeClient.Actions(), "no actions should be performed for empty entities")
}

func TestApply_MultipleEntities(t *testing.T) {
	cc, fakeClient := newTestClusterClient()

	e1 := makeApplyEntity("", "v1", "ConfigMap", "default", "cm-1")
	e2 := makeApplyEntity("", "v1", "ConfigMap", "default", "cm-2")
	e3 := makeApplyEntity("apps", "v1", "Deployment", "default", "deploy-1")

	entities, err := entity.NewEntities([]entity.Entity{e1, e2, e3})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, true, nil, collectCallback(&results), false)
	require.NoError(t, err)
	assert.Len(t, results, 3, "callback should be invoked once per entity")

	assert.Equal(t, "configmap/cm-1", results[0].Resource)
	assert.Equal(t, "configmap/cm-2", results[1].Resource)
	assert.Equal(t, "deployment.apps/deploy-1", results[2].Resource)

	patchCount := 0
	for _, action := range fakeClient.Actions() {
		if _, ok := action.(clienttesting.PatchAction); ok {
			patchCount++
		}
	}
	assert.Equal(t, 3, patchCount, "expected 3 Patch actions for 3 entities")
}

func TestApply_EntityWithoutUnstructured(t *testing.T) {
	cc, _ := newTestClusterClient()

	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("no-unstructured")).
		WithNamespace(types.Namespace("default")))

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, true, nil, nil, false)
	assert.Error(t, err, "entity without unstructured data should return an error")
}

func TestApply_OutputFormat(t *testing.T) {
	cc, _ := newTestClusterClient()
	e := makeApplyEntity("", "v1", "ConfigMap", "default", "test-cm")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	var results []applyResult
	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, false, nil, collectCallback(&results), false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "configmap/test-cm", results[0].Resource)
	assert.Equal(t, "created", results[0].Action)
}

func TestApply_GVRResolutionFailure(t *testing.T) {
	cc, _ := newTestClusterClient()
	e := makeApplyEntity("example.com", "v1", "UnknownCRD", "default", "test")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, true, nil, nil, false)
	assert.Error(t, err, "unknown GVK should cause GVR resolution failure")
}

func TestApply_NilCallback_UsesLogger(t *testing.T) {
	cc, _ := newTestClusterClient()
	e := makeApplyEntity("", "v1", "ConfigMap", "default", "test-cm")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = Apply(context.Background(), log.Default(), cc, entities, types.KeyTemplateEntity, false, true, nil, nil, false)
	require.NoError(t, err)
}
