package hydra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestEvaluateHydraScope_NoRules_AllowsAll(t *testing.T) {
	u := sets.New(types.AppId("a.b"), types.AppId("c.d"))
	assert.True(t, EvaluateHydraScope(nil, u, "a.b"))
}

func TestEvaluateHydraScope_IncludeThenExclude(t *testing.T) {
	u := sets.New(types.AppId("dev.argocd"), types.AppId("dev.other"), types.AppId("prod.x"))
	rules := []HydraConfigScopeRule{
		{Mode: "include", Values: []string{"**"}},
		{Mode: "exclude", Values: []string{"dev.argocd"}},
	}
	assert.False(t, EvaluateHydraScope(rules, u, "dev.argocd"))
	assert.True(t, EvaluateHydraScope(rules, u, "dev.other"))
}

func TestEvaluateHydraScope_IncludeProdPrefix(t *testing.T) {
	u := sets.New(types.AppId("prod.a.b"), types.AppId("dev.a.b"))
	rules := []HydraConfigScopeRule{
		{Mode: "include", Values: []string{"prod.**"}},
	}
	assert.True(t, EvaluateHydraScope(rules, u, "prod.a.b"))
	assert.False(t, EvaluateHydraScope(rules, u, "dev.a.b"))
}

func TestHydraConfigMapDocumentsForApp_FiltersByScope(t *testing.T) {
	perApp := map[types.AppId][]HydraConfigMapDocument{
		"app-a": {{
			Id: "v1/ConfigMap/ns/cm1", Name: "cm1",
			Scope: []HydraConfigScopeRule{{Mode: "include", Values: []string{"app-a"}}},
			Hydra: map[string]any{"refs": map[string]any{}},
		}},
	}
	cluster := sets.New(types.AppId("app-a"), types.AppId("app-b"))

	outA := HydraConfigMapDocumentsForApp(perApp, nil, cluster, "app-a")
	require.Len(t, outA, 1)

	outB := HydraConfigMapDocumentsForApp(perApp, nil, cluster, "app-b")
	require.Len(t, outB, 0)
}

func TestExtractAndRemoveScope_InvalidMode(t *testing.T) {
	doc := map[string]any{
		"scope": []any{
			map[string]any{"mode": "maybe", "values": []any{"**"}},
		},
	}
	_, err := ExtractAndRemoveScope(doc, "v1/ConfigMap/x/y")
	require.Error(t, err)
}
