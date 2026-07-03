package hydra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestOrderedUnionPerAppHydraConfigMapDocuments_DedupesAndSortsById(t *testing.T) {
	t.Parallel()
	perApp := map[types.AppId][]HydraConfigMapDocument{
		"app-z": {{Id: "v1/ConfigMap/ns/cm-z", Name: "cm-z", Hydra: map[string]any{"z": 1}}},
		"app-a": {
			{Id: "v1/ConfigMap/ns/cm-a", Name: "cm-a", Hydra: map[string]any{"a": 1}},
			{Id: "v1/ConfigMap/ns/cm-shared", Name: "cm-shared", Hydra: map[string]any{"shared": "from-a"}},
		},
		"app-b": {{Id: "v1/ConfigMap/ns/cm-shared", Name: "cm-shared", Hydra: map[string]any{"shared": "from-b"}}},
	}
	appIds := sets.New[types.AppId]("app-z", "app-a", "app-b")
	out := orderedUnionPerAppHydraConfigMapDocuments(perApp, appIds)
	require.Len(t, out, 3)
	assert.Equal(t, types.Id("v1/ConfigMap/ns/cm-a"), out[0].Id)
	assert.Equal(t, types.Id("v1/ConfigMap/ns/cm-shared"), out[1].Id)
	assert.Equal(t, types.Id("v1/ConfigMap/ns/cm-z"), out[2].Id)
}

func TestMergeHelmHydraWithClusterWideDocs_MergesFragmentsFromAllApps(t *testing.T) {
	t.Parallel()
	docs := []HydraConfigMapDocument{
		{Id: "v1/ConfigMap/ns/cm-a", Name: "cm-a", Hydra: map[string]any{
			"templatePatches": map[string]any{"ruleA": map[string]any{"predicate": "true"}},
		}},
		{Id: "v1/ConfigMap/ns/cm-b", Name: "cm-b", Hydra: map[string]any{
			"refs": map[string]any{"g": map[string]any{"enabled": true}},
		}},
	}
	helm := map[string]any{"stage": "dev"}
	merged := MergeHelmHydraWithConfigMapDocuments(helm, docs)
	assert.Equal(t, "dev", merged["stage"])
	tp, ok := merged["templatePatches"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, tp, "ruleA")
	refs, ok := merged["refs"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, refs, "g")
}
