package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestApplyOperations_FilterCRDs(t *testing.T) {
	crd := makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "widgets.example.com")
	cm := makeEntity("", "v1", "ConfigMap", "default", "cfg")

	newE, err := entity.NewEntities([]entity.Entity{crd, cm})
	require.NoError(t, err)
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)

	ops := ApplyOperations{New: newE, Update: empty, Replace: empty, Unchanged: empty, Delete: empty}
	filtered, err := ops.FilterCRDs()
	require.NoError(t, err)
	assert.Equal(t, 1, filtered.New.Len())
	gvk, err := filtered.New.Items[0].GVKString()
	require.NoError(t, err)
	assert.Equal(t, types.KubernetesGvkApiextensionsK8sIoV1CustomResourceDefinition, gvk)
}

func TestApplyOperations_MergeMutating(t *testing.T) {
	a := makeEntity("", "v1", "ConfigMap", "default", "a")
	b := makeEntity("", "v1", "ConfigMap", "default", "b")
	c := makeEntity("", "v1", "ConfigMap", "default", "c")
	newE, err := entity.NewEntities([]entity.Entity{a})
	require.NoError(t, err)
	upd, err := entity.NewEntities([]entity.Entity{b})
	require.NoError(t, err)
	rep, err := entity.NewEntities([]entity.Entity{c})
	require.NoError(t, err)
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)

	ops := ApplyOperations{New: newE, Update: upd, Replace: rep, Unchanged: empty, Delete: empty}
	merged, err := ops.MergeMutating()
	require.NoError(t, err)
	assert.Equal(t, 3, merged.Len())
}

// TestClassifyApplyOperations_syncPreventAlignsTemplateForPlan documents the regression fix: the apply
// plan runs the same ApplyClusterApplySyncWindowToEntities step as ClassifyApplyOperations before
// SSA dry-run so an AppProject that is already "prevent" on the cluster is not classified as update
// when the rendered template still has different syncWindows.
func TestClassifyApplyOperations_syncPreventAlignsTemplateForPlan(t *testing.T) {
	templateSW := []any{map[string]any{"kind": "allow", "manualSync": true}}
	clusterSW := []any{map[string]any{"kind": "deny", "manualSync": false}}

	gvk := types.NewGVK(types.Group("argoproj.io"), types.Version("v1alpha1"), types.Kind("AppProject"))
	tmplObj := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "AppProject",
		"metadata":   map[string]any{"name": "myproj", "namespace": "argocd"},
		"spec":       map[string]any{"syncWindows": templateSW},
	}
	clusterObj := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "AppProject",
		"metadata":   map[string]any{"name": "myproj", "namespace": "argocd"},
		"spec":       map[string]any{"syncWindows": clusterSW},
	}
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("myproj")).
		WithNamespace(types.Namespace("argocd")).
		WithUnstructured(types.KeyTemplateEntity, unstructured.Unstructured{Object: tmplObj}).
		WithUnstructured(types.KeyClusterEntity, unstructured.Unstructured{Object: clusterObj})
	ent := mustBuildApply(b)

	existingEnt, err := entity.NewEntities([]entity.Entity{ent})
	require.NoError(t, err)
	emptyCluster, err := entity.NewEntities(nil)
	require.NoError(t, err)

	out, _, err := commands.ApplyClusterApplySyncWindowToEntities(
		discardActionLogger, existingEnt, emptyCluster, types.KeyTemplateEntity, types.KeyClusterEntity,
		types.ClusterApplySyncWindowPrevent, false)
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	tu, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	require.True(t, ok)
	cu, ok := out.Items[0].Unstructured(types.KeyClusterEntity)
	require.True(t, ok)

	tSW, _, err := unstructured.NestedSlice(tu.Object, "spec", "syncWindows")
	require.NoError(t, err)
	cSW, _, err := unstructured.NestedSlice(cu.Object, "spec", "syncWindows")
	require.NoError(t, err)
	assert.Equal(t, cSW, tSW)
}
