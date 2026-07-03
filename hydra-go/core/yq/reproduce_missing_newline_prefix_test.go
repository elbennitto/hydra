package yq

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	hyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
)

// TestHydraToYamlYqFromYaml_preservesLeadingNewlineInConfigMapData is the desired contract for the
// templatePatches-style chain (yaml.ToYaml → yq → yaml.FromYaml): in-memory data.app.txt "\nhello"
// must round-trip unchanged. Today this fails — fix the encoder/parser, not this assertion.
func TestHydraToYamlYqFromYaml_preservesLeadingNewlineInConfigMapData(t *testing.T) {
	m := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "minimal-newline-cm",
			"namespace": "argocd",
		},
		"data": map[string]any{"app.txt": "\nhello"},
	}
	ys, err := hyaml.ToYaml(m)
	require.NoError(t, err)
	ys2, err := Yq(ys, `.metadata.labels."hydra.test/pipeline" = "1"`)
	require.NoError(t, err)
	out, err := hyaml.FromYaml[map[string]any](types.YamlString(ys2))
	require.NoError(t, err)
	got := out["data"].(map[string]any)["app.txt"].(string)
	require.Equal(t, "\nhello", got,
		"data.app.txt must keep leading newline through hydra ToYaml + yq + FromYaml (templatePatches path)")
}
