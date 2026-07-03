package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestApplyTemplatePatchesToEntities_AnnotationOnCRD(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": "widgets.example.com",
			},
			"spec": map[string]any{},
		},
	}
	e := withUnstructured(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "widgets.example.com"),
		types.KeyTemplateEntity, u)
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	entries := []types.TemplatePatchRuleEntry{
		{
			Name:         "crdArgo",
			DeclaringApp: "",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `gvk == "apiextensions.k8s.io/v1/CustomResourceDefinition"`,
				Patches: []types.HydraDiffYqPatch{
					{Yq: `.metadata.annotations."argocd.argoproj.io/sync-options" = "ServerSideApply=true"`},
				},
			},
		},
	}
	pipe, err := NewTemplatePatchPipeline(entries)
	require.NoError(t, err)

	out, err := ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	u2, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	require.True(t, ok)
	ann := u2.GetAnnotations()
	require.Equal(t, "ServerSideApply=true", ann["argocd.argoproj.io/sync-options"])
}

func TestApplyTemplatePatchesToEntities_DeclaringAppSkipsOtherApp(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "x",
				"namespace": "ns",
			},
		},
	}
	e := withUnstructured(makeEntity("", "v1", "ConfigMap", "ns", "x"),
		types.KeyTemplateEntity, u)
	e, err := e.Modify(func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithAppIds([]types.AppId{"cluster.app.a"})
	})
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	entries := []types.TemplatePatchRuleEntry{
		{
			Name:         "onlyB",
			DeclaringApp: "cluster.app.b",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `kind == "ConfigMap"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.metadata.labels.patched = "true"`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipeline(entries)
	require.NoError(t, err)
	out, err := ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	u2, _ := out.Items[0].Unstructured(types.KeyTemplateEntity)
	_, has := u2.GetLabels()["patched"]
	assert.False(t, has)
}

func TestApplyTemplatePatchesToEntities_RejectsIdentityChange(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "x",
				"namespace": "ns",
			},
		},
	}
	e := withUnstructured(makeEntity("", "v1", "ConfigMap", "ns", "x"),
		types.KeyTemplateEntity, u)
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	entries := []types.TemplatePatchRuleEntry{
		{
			Name: "bad",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `kind == "ConfigMap"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.metadata.name = "renamed"`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipeline(entries)
	require.NoError(t, err)
	_, err = ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "templatePatches changed resource identity")
}

func TestApplyTemplatePatchesToEntitiesBeforeScope_AllowsNamespaceRemovalThenScopeValidation(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "scheduling.k8s.io/v1",
			"kind":       "PriorityClass",
			"metadata": map[string]any{
				"name":      "postgres-operator-pod",
				"namespace": "operator-postgres",
			},
			"value": int64(1000000),
		},
	}
	e := withUnstructured(makeEntity("scheduling.k8s.io", "v1", "PriorityClass", "", "postgres-operator-pod"),
		types.KeyTemplateEntity, u)
	e = withAppIds(e, []types.AppId{"in-cluster.demo-infra.operator-postgres"})
	e = withAppNamespace(e, types.AppNamespace("operator-postgres"))
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	entries := []types.TemplatePatchRuleEntry{
		{
			Name:         "priorityClassNamespace",
			DeclaringApp: "in-cluster.demo-infra.operator-postgres",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `gvk == "scheduling.k8s.io/v1/PriorityClass"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `del(.metadata.namespace)`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipeline(entries)
	require.NoError(t, err)

	patched, err := ApplyTemplatePatchesToEntitiesBeforeScope(pipe, ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	scoped, err := ApplyScopeInfoMap(types.CrdModeError, patched, DefaultScopeInfoMap(), types.KeyTemplateEntity)
	require.NoError(t, err)
	u2, ok := scoped.Items[0].Unstructured(types.KeyTemplateEntity)
	require.True(t, ok)
	assert.Empty(t, u2.GetNamespace())
}

func TestApplyTemplatePatchesToEntities_RejectsHydraConfigMapMutation(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "hydra-cfg",
				"namespace": "ns",
				"annotations": map[string]any{
					"hydra-gitops.org/hydra-config": "true",
				},
			},
			"data": map[string]any{
				"hydra": "refs: {}\n",
			},
		},
	}
	e := withUnstructured(makeEntity("", "v1", "ConfigMap", "ns", "hydra-cfg"),
		types.KeyTemplateEntity, u)
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	entries := []types.TemplatePatchRuleEntry{
		{
			Name: "touchHydra",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `kind == "ConfigMap"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.data.hydra = "refs:\n  x: {}\n"`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipeline(entries)
	require.NoError(t, err)
	_, err = ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not mutate Hydra configuration ConfigMap")
}

func TestValidateHydraTemplatePatchRules_EmptyPredicate(t *testing.T) {
	err := types.ValidateHydraTemplatePatchRules(map[string]types.HydraTemplatePatchRule{
		"bad": {Predicate: "  ", Patches: []types.HydraDiffYqPatch{{Yq: `.x=1`}}},
	})
	require.Error(t, err)
}

// Regression: yaml.FromYaml + yq round-trip decodes whole numbers as Go int; EntityBuilder.WithUnstructured
// calls unstructured.DeepCopy which panics on int unless we normalize to int64.
func TestApplyTemplatePatchesToEntities_YqIntegerDoesNotPanicOnDeepCopy(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "p",
				"namespace": "ns",
			},
			"spec": map[string]any{
				"containers": []any{
					map[string]any{"name": "c", "image": "nginx:latest"},
				},
			},
		},
	}
	e := withUnstructured(makeEntity("", "v1", "Pod", "ns", "p"),
		types.KeyTemplateEntity, u)
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	entries := []types.TemplatePatchRuleEntry{
		{
			Name: "deadline",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `kind == "Pod"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.spec.activeDeadlineSeconds = 120`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipeline(entries)
	require.NoError(t, err)

	out, err := ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	u2, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	require.True(t, ok)
	sec, found, err := unstructured.NestedInt64(u2.Object, "spec", "activeDeadlineSeconds")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(120), sec)
}

func findSyntheticDefaultServiceAccount(t *testing.T) entity.Entity {
	t.Helper()
	synth, err := CreateNamespaceEntities(sets.New(types.Namespace("dex")), types.KeyTemplateEntity)
	require.NoError(t, err)
	for _, it := range synth.Items {
		u, ok := it.Unstructured(types.KeyTemplateEntity)
		if !ok {
			continue
		}
		if u.GetKind() == "ServiceAccount" && u.GetName() == "default" && u.GetNamespace() == "dex" {
			return it
		}
	}
	t.Fatal("expected synthetic default ServiceAccount in dex")
	return entity.Entity{}
}

func TestApplyTemplatePatches_SyntheticDefault_NoAppIdsWhenUnpatched(t *testing.T) {
	saEnt := findSyntheticDefaultServiceAccount(t)
	ents, err := entity.NewEntities([]entity.Entity{saEnt})
	require.NoError(t, err)

	owner := map[types.Namespace]types.AppId{types.Namespace("dex"): "cluster.app.owner"}
	entries := []types.TemplatePatchRuleEntry{
		{
			Name:         "onlyOtherApp",
			DeclaringApp: "cluster.app.other",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `gvk == "v1/ServiceAccount"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.imagePullSecrets = [{"name": "x"}]`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipelineWithNamespaceOwners(entries, owner)
	require.NoError(t, err)
	out, err := ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	_, err = out.Items[0].AppIds()
	require.Error(t, err, "synthetic default SA must not gain AppIds when no template patch applies")
}

func TestApplyTemplatePatches_SyntheticDefault_AppIdAfterDeclaringOwnerPatch(t *testing.T) {
	saEnt := findSyntheticDefaultServiceAccount(t)
	ents, err := entity.NewEntities([]entity.Entity{saEnt})
	require.NoError(t, err)

	owner := map[types.Namespace]types.AppId{types.Namespace("dex"): "cluster.app.owner"}
	entries := []types.TemplatePatchRuleEntry{
		{
			Name:         "pull",
			DeclaringApp: "cluster.app.owner",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `gvk == "v1/ServiceAccount"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.imagePullSecrets = [{"name": "regcred"}]`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipelineWithNamespaceOwners(entries, owner)
	require.NoError(t, err)
	out, err := ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	appIDs, err := out.Items[0].AppIds()
	require.NoError(t, err)
	require.Len(t, appIDs, 1)
	assert.Equal(t, types.AppId("cluster.app.owner"), appIDs[0])
	u, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	require.True(t, ok)
	sec, found, err := unstructured.NestedSlice(u.Object, "imagePullSecrets")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, sec, 1)
}

func TestApplyTemplatePatches_SyntheticDefault_GlobalRuleUsesNamespaceOwner(t *testing.T) {
	saEnt := findSyntheticDefaultServiceAccount(t)
	ents, err := entity.NewEntities([]entity.Entity{saEnt})
	require.NoError(t, err)

	owner := map[types.Namespace]types.AppId{types.Namespace("dex"): "cluster.app.nsowner"}
	entries := []types.TemplatePatchRuleEntry{
		{
			Name: "globalPull",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `gvk == "v1/ServiceAccount"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.imagePullSecrets = [{"name": "global"}]`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipelineWithNamespaceOwners(entries, owner)
	require.NoError(t, err)
	out, err := ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	appIDs, err := out.Items[0].AppIds()
	require.NoError(t, err)
	require.Len(t, appIDs, 1)
	assert.Equal(t, types.AppId("cluster.app.nsowner"), appIDs[0])
}

func TestApplyTemplatePatches_SyntheticDefault_GlobalPatchWithoutOwnerMapErrors(t *testing.T) {
	saEnt := findSyntheticDefaultServiceAccount(t)
	ents, err := entity.NewEntities([]entity.Entity{saEnt})
	require.NoError(t, err)

	entries := []types.TemplatePatchRuleEntry{
		{
			Name: "globalPull",
			Rule: types.HydraTemplatePatchRule{
				Predicate: `gvk == "v1/ServiceAccount"`,
				Patches:   []types.HydraDiffYqPatch{{Yq: `.imagePullSecrets = [{"name": "x"}]`}},
			},
		},
	}
	pipe, err := NewTemplatePatchPipelineWithNamespaceOwners(entries, nil)
	require.NoError(t, err)
	_, err = ApplyTemplatePatchesToEntities(pipe, ents, types.KeyTemplateEntity)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace owner map is unavailable")
}

func TestMergeRenderedForHydraPartition_DedupesById(t *testing.T) {
	nsYAML := types.YamlString(`# Source: chart/ns.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: shared
`)
	a, err := entity.NewEntitiesFromYaml(log.Default(), nsYAML, types.KeyTemplateEntity)
	require.NoError(t, err)
	b, err := entity.NewEntitiesFromYaml(log.Default(), nsYAML, types.KeyTemplateEntity)
	require.NoError(t, err)
	merged, err := hydra.MergeRenderedForHydraPartition(a, b)
	require.NoError(t, err)
	assert.Equal(t, 1, merged.Len())
}
