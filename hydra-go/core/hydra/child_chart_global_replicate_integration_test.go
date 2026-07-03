package hydra

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/chart/loader"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

func TestReplicateParentGlobalIntoChildDependencyValues_mergeShape(t *testing.T) {
	t.Parallel()
	m := types.ValuesMap{
		"global": map[string]any{
			"baseUrl": "https://umbrella.example.com",
			"hydra":   map[string]any{"cluster": "c1"},
		},
		"service-auth": map[string]any{
			"global": map[string]any{
				"hydra": map[string]any{
					"refs": map[string]any{"x": "child-only"},
				},
			},
		},
	}
	replicateParentGlobalIntoChildDependencyValues(m, "service-auth")
	require.Equal(t, "https://umbrella.example.com", values.Lookup(m, "service-auth", "global", "baseUrl"))
	require.Equal(t, "child-only", values.Lookup(m, "service-auth", "global", "hydra", "refs", "x"))
	require.Equal(t, "c1", values.Lookup(m, "service-auth", "global", "hydra", "cluster"))
}

// TestHelmCoalesce_afterReplicate_subchartSeesGlobalBaseUrl uses a parent chart with empty
// dependency defaults; user supplies only top-level global (like gitops). replicateParent…
// must nest global under the dependency key so CoalesceValues leaves dependency scope with
// global.baseUrl (Helm may otherwise skip coalesceGlobals when chart defaults conflict).
func TestHelmCoalesce_afterReplicate_subchartSeesGlobalBaseUrl(t *testing.T) {
	t.Parallel()
	const depName = "childdep"

	base := t.TempDir()
	childSrc := filepath.Join(base, depName)
	parentDir := filepath.Join(base, "parent")
	chartsChild := filepath.Join(parentDir, "charts", depName)

	require.NoError(t, os.MkdirAll(filepath.Join(childSrc, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(childSrc, "Chart.yaml"), []byte(`apiVersion: v2
name: childdep
version: 0.1.0
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(childSrc, "templates", "out.yaml"), []byte(
		`apiVersion: v1
kind: ConfigMap
metadata:
  name: dep-out
data:
  url: {{ required "global.baseUrl is required" .Values.global.baseUrl | quote }}
`), 0o644))

	require.NoError(t, os.MkdirAll(chartsChild, 0o755))
	require.NoError(t, copyDirContentsForGlobalTest(childSrc, chartsChild))

	require.NoError(t, os.MkdirAll(filepath.Join(parentDir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "Chart.yaml"), []byte(`apiVersion: v2
name: parent
version: 0.1.0
dependencies:
  - name: childdep
    version: 0.1.0
    repository: "file://charts/childdep"
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "values.yaml"), []byte("# parent defaults\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "templates", "empty.yaml"), []byte("# anchor\n"), 0o644))

	ch, err := loader.Load(parentDir)
	require.NoError(t, err)

	vals := types.ValuesMap{
		"global": map[string]any{
			"baseUrl": "https://integration.example.com",
		},
	}
	replicateParentGlobalIntoChildDependencyValues(vals, depName)

	coalesced, err := chartutil.CoalesceValues(ch, vals)
	require.NoError(t, err)
	require.Equal(t, "https://integration.example.com",
		values.Lookup(types.ValuesMap(coalesced), depName, "global", "baseUrl"))

	l := log.Default()
	out, err := helm.Template(l, ch, helm.RenderChartParams{
		KubernetesVersionOrFallback: "v1.30.0",
		ReleaseName:                 depName,
		Namespace:                   "default",
		ValuesMap:                   vals,
	})
	require.NoError(t, err)
	require.Contains(t, string(out), "https://integration.example.com")
}

func copyDirContentsForGlobalTest(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
