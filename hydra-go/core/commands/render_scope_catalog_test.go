package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// mergedScopeInfoMapLikeRenderClusterSelectedApps mirrors the scope merge in [RenderClusterSelectedApps]
// (batch CRDs + optional WithScopeCatalog) before [applyScopeInfoMap].
func mergedScopeInfoMapLikeRenderClusterSelectedApps(
	batch entity.Entities,
	scopeCatalog entity.Entities,
	key types.EntityKeyUnstructured,
) (types.ScopeInfoMap, error) {
	templatesScopeInfoMap, err := batch.ScopeInfoMapFromCrds(key)
	if err != nil {
		return nil, err
	}
	mergedScopeInfoMap, err := MergeScopeInfoMaps(DefaultScopeInfoMap(), templatesScopeInfoMap)
	if err != nil {
		return nil, err
	}
	if scopeCatalog.Len() > 0 {
		catalogScope, err := scopeCatalog.ScopeInfoMapFromCrds(key)
		if err != nil {
			return nil, err
		}
		mergedScopeInfoMap, err = MergeScopeInfoMaps(mergedScopeInfoMap, catalogScope)
		if err != nil {
			return nil, err
		}
	}
	return mergedScopeInfoMap, nil
}

func applyScopeLikeRenderClusterSelectedApps(
	batch entity.Entities,
	scopeCatalog entity.Entities,
	key types.EntityKeyUnstructured,
) (entity.Entities, error) {
	merged, err := mergedScopeInfoMapLikeRenderClusterSelectedApps(batch, scopeCatalog, key)
	if err != nil {
		return entity.Entities{}, err
	}
	return applyScopeInfoMap(types.CrdModeKeepUnknown, batch, merged, key)
}

func findWidgetID(t *testing.T, ents entity.Entities) types.Id {
	t.Helper()
	for _, e := range ents.Items {
		kind, err := e.Kind()
		if err != nil {
			continue
		}
		if kind == "Widget" {
			id, err := e.Id()
			require.NoError(t, err)
			return id
		}
	}
	t.Fatal("no Widget entity in batch")
	return ""
}

// TestRenderScopeCatalog_SingleAppBatchMatchesFullBatchIds guards the regression where a namespaced CR
// (Helm omits metadata.namespace) appears in one app while the CRD manifest lives in another app:
// without merging CRD scope from a catalog render, [applyScopeInfoMap] keeps the GVK unknown and the
// template id keeps an empty namespace segment; [WithScopeCatalog] supplies the CRD scope so ids match
// a combined batch render (same as [RenderClusterEachAppSeparate] + catalog).
func TestRenderScopeCatalog_SingleAppBatchMatchesFullBatchIds(t *testing.T) {
	t.Parallel()

	const manifest = `
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.scopecatalog.example.com
spec:
  group: scopecatalog.example.com
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
  scope: Namespaced
  names:
    plural: widgets
    singular: widget
    kind: Widget
---
apiVersion: scopecatalog.example.com/v1
kind: Widget
metadata:
  name: my-widget
`

	key := types.KeyTemplateEntity
	loaded, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(manifest), key)
	require.NoError(t, err)
	require.GreaterOrEqual(t, loaded.Len(), 2)

	var crdOnly []entity.Entity
	var widgetOnly []entity.Entity
	var combined []entity.Entity
	for _, e := range loaded.Items {
		kind, kerr := e.Kind()
		require.NoError(t, kerr)
		switch kind {
		case "CustomResourceDefinition":
			crdOnly = append(crdOnly, e)
			combined = append(combined, e)
		case "Widget":
			w := withAppNamespace(e, types.AppNamespace("catalog-ns"))
			widgetOnly = append(widgetOnly, w)
			combined = append(combined, w)
		default:
			t.Fatalf("unexpected kind %s", kind)
		}
	}

	combinedEnts, err := entity.NewEntities(combined)
	require.NoError(t, err)
	widgetBatch, err := entity.NewEntities(widgetOnly)
	require.NoError(t, err)
	catalogEnts, err := entity.NewEntities(crdOnly)
	require.NoError(t, err)

	fullOut, err := applyScopeLikeRenderClusterSelectedApps(combinedEnts, entity.Entities{}, key)
	require.NoError(t, err)
	wantID := findWidgetID(t, fullOut)

	withCatalogOut, err := applyScopeLikeRenderClusterSelectedApps(widgetBatch, catalogEnts, key)
	require.NoError(t, err)
	gotID := findWidgetID(t, withCatalogOut)
	require.Equal(t, wantID, gotID, "per-app render with WithScopeCatalog must match combined-batch id")

	withoutCatalogOut, err := applyScopeLikeRenderClusterSelectedApps(widgetBatch, entity.Entities{}, key)
	require.NoError(t, err)
	staleID := findWidgetID(t, withoutCatalogOut)
	require.NotEqual(t, wantID, staleID, "without catalog scope, id must differ from combined render")
	require.True(t, strings.Contains(string(staleID), "Widget//"), "expected empty namespace segment in id when GVK scope unknown: %s", staleID)
	require.Contains(t, string(wantID), "Widget/catalog-ns/")
}
