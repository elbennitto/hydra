package hydra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func configMapEntity(key types.EntityKeyUnstructured, u unstructured.Unstructured) (entity.Entity, error) {
	gvk := types.NewGVKFromK8s(u.GroupVersionKind())
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name(u.GetName()))
	if ns := u.GetNamespace(); ns != "" {
		b = b.WithNamespace(types.Namespace(ns))
	}
	return b.WithUnstructured(key, u).Build()
}

func TestRefParsersFromHydraConfigMaps(t *testing.T) {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "hydra-refs",
			"namespace": "demo",
			"annotations": map[string]any{
				AnnotationHydraConfig: "true",
			},
		},
		"data": map[string]any{
			"hydra": `refs:
  g1:
    tag:
      - testtag
    ref-parsers:
      - predicate: 'gvk == "v1/ConfigMap"'
        attributes:
          - "origin:workload": generator-job
        pick:
          - cel: 'refBuilder().outgoing(id("v1/Secret", ns, "x"))'
`,
		},
	}}
	e, err := configMapEntity(types.KeyTemplateEntity, u)
	require.NoError(t, err)
	ents := entity.Entities{Items: []entity.Entity{e}}

	uu := sets.New(types.AppId("c.app"))
	parsers, err := RefParsersFromHydraConfigMaps(ents, types.KeyTemplateEntity, nil, uu, uu)
	require.NoError(t, err)
	require.Len(t, parsers, 1)
	assert.Equal(t, types.CelPredicate(`gvk == "v1/ConfigMap"`), parsers[0].MatchPredicate())
	assert.Equal(t, []string{"testtag"}, parsers[0].Tags)
	assert.Equal(t, []types.RefAttribute{{
		Type:  types.RefAttributeOriginWorkload,
		Value: "generator-job",
	}}, parsers[0].Attributes)
	require.Len(t, parsers[0].Pick, 1)
}

func TestRefParsersFromHydraConfigMaps_SkipsWithoutAnnotation(t *testing.T) {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "plain",
			"namespace": "default",
		},
		"data": map[string]any{
			"hydra": `refs:
  g1:
    ref-parsers:
      - predicate: 'true'
        pick: []
`,
		},
	}}
	e, err := configMapEntity(types.KeyTemplateEntity, u)
	require.NoError(t, err)
	ents := entity.Entities{Items: []entity.Entity{e}}

	uu := sets.New(types.AppId("c.app"))
	parsers, err := RefParsersFromHydraConfigMaps(ents, types.KeyTemplateEntity, nil, uu, uu)
	require.NoError(t, err)
	assert.Len(t, parsers, 0)
}

func TestRefParsersFromHydraConfigMaps_SeenDedup(t *testing.T) {
	makeCM := func(name string) entity.Entity {
		u := unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "ns",
				"annotations": map[string]any{
					AnnotationHydraConfig: "true",
				},
			},
			"data": map[string]any{
				"hydra": `refs:
  g1:
    ref-parsers:
      - predicate: 'name == "a"'
        pick: []
`,
			},
		}}
		e, err := configMapEntity(types.KeyTemplateEntity, u)
		require.NoError(t, err)
		return e
	}

	a := makeCM("same")
	b := makeCM("same")
	seen := sets.New[types.Id]()

	uu := sets.New(types.AppId("c.app"))
	parsers, err := RefParsersFromHydraConfigMaps(entity.Entities{Items: []entity.Entity{a, b}}, types.KeyTemplateEntity, seen, uu, uu)
	require.NoError(t, err)
	assert.Len(t, parsers, 1, "duplicate ConfigMap id should contribute parsers only once")
}

func TestHydraConfigDocumentsFromEntities_FullDocument(t *testing.T) {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "hydra-refs",
			"namespace": "demo",
			"annotations": map[string]any{
				AnnotationHydraConfig: "true",
			},
		},
		"data": map[string]any{
			"hydra": `refs:
  g1:
    tag:
      - testtag
    ref-parsers:
      - predicate: 'true'
        pick: []
customKey: 42
`,
		},
	}}
	e, err := configMapEntity(types.KeyTemplateEntity, u)
	require.NoError(t, err)
	ents := entity.Entities{Items: []entity.Entity{e}}

	docs, err := HydraConfigDocumentsFromEntities(ents, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, "demo", docs[0].Namespace)
	assert.Equal(t, "hydra-refs", docs[0].Name)
	require.NotNil(t, docs[0].Hydra)
	assert.Contains(t, docs[0].Hydra, "refs")
	assert.Equal(t, 42, docs[0].Hydra["customKey"])
}

func TestMergeHelmHydraWithConfigMapDocuments(t *testing.T) {
	helm := map[string]any{
		"refs": map[string]any{
			"fromHelm": map[string]any{"tag": []any{"helm"}},
		},
		"stage": "dev",
	}
	docs := []HydraConfigMapDocument{{
		Hydra: map[string]any{
			"refs": map[string]any{
				"fromCm": map[string]any{"tag": []any{"cm"}},
			},
			"stage": "prod",
		},
	}}

	merged := MergeHelmHydraWithConfigMapDocuments(helm, docs)
	refs, ok := merged["refs"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, refs, "fromHelm")
	assert.Contains(t, refs, "fromCm")
	assert.Equal(t, "prod", merged["stage"], "scalar from later ConfigMap replaces Helm")
}

func TestPartitionHydraConfigDocumentsByApp_NoAppIdsReturnsError(t *testing.T) {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "hydra-refs",
			"namespace": "demo",
			"annotations": map[string]any{
				AnnotationHydraConfig: "true",
			},
		},
		"data": map[string]any{
			"hydra": `refs:
  g1:
    ref-parsers:
      - predicate: 'true'
        pick: []
`,
		},
	}}
	e, err := configMapEntity(types.KeyTemplateEntity, u)
	require.NoError(t, err)
	appSet := sets.New[types.AppId]("service-auth")
	_, _, err = PartitionHydraConfigDocumentsByApp(
		entity.Entities{Items: []entity.Entity{e}}, types.KeyTemplateEntity, appSet)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "noAppIds")
}

func TestPartitionHydraConfigDocumentsByApp_GlobalAndPerApp(t *testing.T) {
	makeCM := func(name string, appIds []types.AppId) entity.Entity {
		u := unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "ns",
				"annotations": map[string]any{
					AnnotationHydraConfig: "true",
				},
			},
			"data": map[string]any{
				"hydra": `refs:
  g1:
    ref-parsers:
      - predicate: 'true'
        pick: []
`,
			},
		}}
		b := entity.NewEntityBuilder().
			WithGVK(types.NewGVKFromK8s(u.GroupVersionKind())).
			WithResource(types.Resource("configmaps")).
			WithName(types.Name(u.GetName())).
			WithNamespace(types.Namespace(u.GetNamespace())).
			WithUnstructured(types.KeyTemplateEntity, u).
			WithAppIds(appIds)
		ent, err := b.Build()
		require.NoError(t, err)
		return ent
	}

	globalCM := makeCM("global", []types.AppId{})
	perAppCM := makeCM("for-app", []types.AppId{"app-a"})
	ents := entity.Entities{Items: []entity.Entity{globalCM, perAppCM}}
	appSet := sets.New[types.AppId]("app-a", "app-b")

	perApp, global, err := PartitionHydraConfigDocumentsByApp(ents, types.KeyTemplateEntity, appSet)
	require.NoError(t, err)
	require.Len(t, global, 1)
	assert.Equal(t, "global", global[0].Name)
	require.Len(t, perApp["app-a"], 1)
	assert.Equal(t, "for-app", perApp["app-a"][0].Name)
	assert.Empty(t, perApp["app-b"])
}
