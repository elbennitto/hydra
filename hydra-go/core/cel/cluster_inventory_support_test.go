package cel

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestClusterInventorySupport_ClusterEntitiesAndInvolvedObjectEvents(t *testing.T) {
	base, err := NewEnv()
	require.NoError(t, err)

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name": "p1", "namespace": "default",
			},
		},
	}
	evU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Event",
			"metadata": map[string]any{
				"name": "ev1", "namespace": "default",
			},
			"involvedObject": map[string]any{
				"kind":      "Pod",
				"name":      "p1",
				"namespace": "default",
			},
			"type":    "Warning",
			"reason":  "Failed",
			"message": "something failed",
		},
	}
	pod, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("p1")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)
	evEnt, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name("ev1")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, evU).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{pod, evEnt})
	require.NoError(t, err)

	opt, err := ClusterInventorySupport(base, ents, entity.Entities{}, entity.Entities{})
	require.NoError(t, err)
	env, err := NewEnv(opt)
	require.NoError(t, err)

	expr, err := env.CompileExpression(types.CelExpression(`size(clusterEntities())`))
	require.NoError(t, err)
	v, err := expr.Eval(pod)
	require.NoError(t, err)
	require.Equal(t, int64(2), v.Value())

	evExpr, err := env.CompileExpression(types.CelExpression(
		`involvedObjectEvents(10, "Pod", "p1", "default")`,
	))
	require.NoError(t, err)
	v2, err := evExpr.Eval(pod)
	require.NoError(t, err)
	raw := v2.Value()
	sl, ok := raw.([]string)
	require.True(t, ok)
	require.Len(t, sl, 1)
	require.Contains(t, sl[0], "event:")
	require.Contains(t, sl[0], "Failed")
}

func TestClusterInventorySupport_TemplateClusterSplitAndNamespaceFilter(t *testing.T) {
	base, err := NewEnv()
	require.NoError(t, err)

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "cm1", "namespace": "apps",
			},
		},
	}
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name("cm1")).
		WithNamespace(types.Namespace("apps")).
		WithUnstructured(types.KeyTemplateEntity, u).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	opt, err := ClusterInventorySupport(base, ents, entity.Entities{}, entity.Entities{})
	require.NoError(t, err)
	env, err := NewEnv(opt)
	require.NoError(t, err)

	v1, err := env.CompileExpression(types.CelExpression(`size(templateEntities())`))
	require.NoError(t, err)
	r1, err := v1.Eval(e)
	require.NoError(t, err)
	require.Equal(t, int64(1), r1.Value())

	v2, err := env.CompileExpression(types.CelExpression(`size(clusterEntities())`))
	require.NoError(t, err)
	r2, err := v2.Eval(e)
	require.NoError(t, err)
	require.Equal(t, int64(0), r2.Value())

	v3, err := env.CompileExpression(types.CelExpression(`size(templateEntities({"namespace": "apps"}))`))
	require.NoError(t, err)
	r3, err := v3.Eval(e)
	require.NoError(t, err)
	require.Equal(t, int64(1), r3.Value())

	v4, err := env.CompileExpression(types.CelExpression(`size(templateEntities({"namespace": "other"}))`))
	require.NoError(t, err)
	r4, err := v4.Eval(e)
	require.NoError(t, err)
	require.Equal(t, int64(0), r4.Value())

	pred, err := env.CompilePredicate(types.CelPredicate(`managedNamespaces() == ["apps"]`))
	require.NoError(t, err)
	ok, err := pred.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestClusterInventorySupport_ObjectSelectorFields(t *testing.T) {
	base, err := NewEnv()
	require.NoError(t, err)

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "cm1", "namespace": "apps",
			},
		},
	}
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name("cm1")).
		WithNamespace(types.Namespace("apps")).
		WithUnstructured(types.KeyTemplateEntity, u).
		Build()
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	opt, err := ClusterInventorySupport(base, ents, entity.Entities{}, entity.Entities{})
	require.NoError(t, err)
	env, err := NewEnv(opt)
	require.NoError(t, err)

	expr, err := env.CompileExpression(types.CelExpression(`size(templateEntities({"gvk": "v1/ConfigMap", "namespace": "apps", "name": "cm1"}))`))
	require.NoError(t, err)
	got, err := expr.Eval(e)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.Value())

	expr, err = env.CompileExpression(types.CelExpression(`size(templateEntities({"id": "v1/ConfigMap/apps/cm1"}))`))
	require.NoError(t, err)
	got, err = expr.Eval(e)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.Value())

	expr, err = env.CompileExpression(types.CelExpression(`size(templateEntities({"ns": "apps"}))`))
	require.NoError(t, err)
	got, err = expr.Eval(e)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.Value())
}
