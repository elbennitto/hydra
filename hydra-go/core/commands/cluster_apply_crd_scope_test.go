package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// This file specifies the contracts for cluster apply CRD scope and apply eligibility (see hydra docs:
// rendering-and-listing — cluster apply CRD sources for scope vs apply eligibility).

const exampleWidgetGVK = types.GVKString("example.com/v1/Widget")

func exampleWidgetCRDUnstructured() unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": "widgets.example.com",
			},
			"spec": map[string]any{
				"group": "example.com",
				"scope": "Namespaced",
				"names": map[string]any{
					"plural":   "widgets",
					"singular": "widget",
					"kind":     "Widget",
				},
				"versions": []any{
					map[string]any{
						"name":    "v1",
						"served":  true,
						"storage": true,
						"schema": map[string]any{
							"openAPIV3Schema": map[string]any{
								"type": "object",
							},
						},
					},
				},
			},
		},
	}
}

func exampleWidgetCRDEntity() entity.Entity {
	u := exampleWidgetCRDUnstructured()
	e := withUnstructured(
		makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "widgets.example.com"),
		types.KeyTemplateEntity,
		u,
	)
	return withResource(e, types.Resource("customresourcedefinitions"))
}

// exampleWidgetInstanceEntity is a namespaced Widget with no metadata.namespace on the template object.
func exampleWidgetInstanceEntity() entity.Entity {
	gvk := types.NewGVK(types.Group("example.com"), types.Version("v1"), types.Kind("Widget"))
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "example.com/v1",
			"kind":       "Widget",
			"metadata": map[string]any{
				"name": "w1",
			},
		},
	}
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("widgets")).
		WithName(types.Name("w1")).
		WithAppNamespace(types.AppNamespace("app-a-ns")))
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

// TestClusterApplyCrdScopeMap_fullCatalogSuppliesScopeForSelectedCustomResources is the regression for
// "full-cluster render CRD scope + selected apply entities": scope for example.com/v1 Widget must be
// derivable from CRD manifests in the full catalog even when the selected apply set omits that CRD.
func TestClusterApplyCrdScopeMap_fullCatalogSuppliesScopeForSelectedCustomResources(t *testing.T) {
	widget := exampleWidgetInstanceEntity()
	crd := exampleWidgetCRDEntity()

	fullCatalog, err := entity.NewEntities([]entity.Entity{crd, widget})
	require.NoError(t, err)

	selectedOnly, err := entity.NewEntities([]entity.Entity{widget})
	require.NoError(t, err)

	liveEmpty := types.ScopeInfoMap{}

	scopeMap, err := ClusterApplyCrdScopeMap(fullCatalog, liveEmpty, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Contains(t, scopeMap, exampleWidgetGVK)

	merged, err := ApplyScopeInfoMaps(types.CrdModeError, selectedOnly, types.KeyTemplateEntity, scopeMap)
	require.NoError(t, err)
	require.Equal(t, 1, merged.Len())

	ns, err := merged.Items[0].Namespace()
	require.NoError(t, err)
	assert.Equal(t, types.Namespace("app-a-ns"), ns)

	uOut, err := merged.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, "app-a-ns", uOut.GetNamespace())
}

func TestValidateClusterApplyCrdEligibility(t *testing.T) {
	widget := exampleWidgetInstanceEntity()
	crd := exampleWidgetCRDEntity()

	selectedInstanceOnly, err := entity.NewEntities([]entity.Entity{widget})
	require.NoError(t, err)

	selectedWithCRD, err := entity.NewEntities([]entity.Entity{crd, widget})
	require.NoError(t, err)

	liveWithWidget := types.ScopeInfoMap{
		exampleWidgetGVK: {Namespaced: types.NamespacedYes, Resource: types.Resource("widgets")},
	}

	tests := []struct {
		name string
		live types.ScopeInfoMap
		sel  entity.Entities
		err  bool
	}{
		{
			name: "rejects when required CRD is neither live nor in selected apply manifests",
			live: types.ScopeInfoMap{},
			sel:  selectedInstanceOnly,
			err:  true,
		},
		{
			name: "allows when API is established on the cluster",
			live: liveWithWidget,
			sel:  selectedInstanceOnly,
			err:  false,
		},
		{
			name: "allows when defining CRD is part of the selected apply set",
			live: types.ScopeInfoMap{},
			sel:  selectedWithCRD,
			err:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClusterApplyCrdEligibility(tt.sel, tt.live, types.KeyTemplateEntity)
			if tt.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateClusterApplyCrdEligibility_ignoresCrdsOutsideSelectedApplySet ensures CRD objects that exist
// only in the full-catalog render must not make eligibility pass; only live cluster APIs and CRDs in the
// selected apply manifests may satisfy admission for the selected set.
func TestValidateClusterApplyCrdEligibility_ignoresCrdsOutsideSelectedApplySet(t *testing.T) {
	widget := exampleWidgetInstanceEntity()
	crd := exampleWidgetCRDEntity()

	fullCatalog, err := entity.NewEntities([]entity.Entity{crd, widget})
	require.NoError(t, err)

	selectedOnly, err := entity.NewEntities([]entity.Entity{widget})
	require.NoError(t, err)

	_ = fullCatalog // full catalog includes the CRD; eligibility must still fail without live API or CRD in selection.

	err = ValidateClusterApplyCrdEligibility(selectedOnly, types.ScopeInfoMap{}, types.KeyTemplateEntity)
	require.Error(t, err)
}
