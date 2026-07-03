package helm

import (
	"os"
	"path/filepath"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/loader"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

// TestHelmChildTemplate_HybridGlobalFromFullUmbrellaValues documents the regression where
// child helm.Template must receive global.* (e.g. baseUrl) from the umbrella’s fully merged
// LoadValuesMap (post–ToRenderValues), while child-scoped keys should still come from a single
// Coalesce pass (coalesced umbrella values). ValuesMap input to Install.Run is raw user-style;
// global for subcharts must match what templates read after the umbrella render pipeline.
//
// Setup: umbrella chart defaults set global.baseUrl; user “cluster” values only add global.hydra.
// The merged global map must still contain baseUrl, and the child chart render must succeed when
// global is taken from full umbrella values (mirrors LoadValuesFromRootApp in child_app.go).
func TestHelmChildTemplate_HybridGlobalFromFullUmbrellaValues(t *testing.T) {
	t.Parallel()
	l := log.Default()
	base := t.TempDir()
	childSrc := filepath.Join(base, "child")
	umbrellaDir := filepath.Join(base, "umbrella")
	chartsChild := filepath.Join(umbrellaDir, "charts", "child")

	require.NoError(t, os.MkdirAll(filepath.Join(childSrc, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(childSrc, "Chart.yaml"), []byte(`apiVersion: v2
name: child
version: 0.1.0
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(childSrc, "values.yaml"), []byte(`# Child defaults; global.baseUrl comes from umbrella + merge
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(childSrc, "templates", "configmap.yaml"), []byte(
		`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-child-out
data:
  baseUrl: {{ required "global.baseUrl is required" .Values.global.baseUrl | quote }}
  hydraCluster: {{ .Values.global.hydra.cluster | default "" | quote }}
`), 0o644))

	require.NoError(t, os.MkdirAll(chartsChild, 0o755))
	require.NoError(t, copyDirContents(childSrc, chartsChild))

	require.NoError(t, os.MkdirAll(filepath.Join(umbrellaDir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(umbrellaDir, "Chart.yaml"), []byte(`apiVersion: v2
name: umbrella
version: 0.1.0
dependencies:
  - name: child
    version: 0.1.0
    repository: "file://charts/child"
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(umbrellaDir, "values.yaml"), []byte(`global:
  baseUrl: "https://from-umbrella-chart.example"
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(umbrellaDir, "templates", "empty.yaml"), []byte("# anchor\n"), 0o644))

	userLikeClusterVals := types.ValuesMap{
		"global": map[string]any{
			"hydra": map[string]any{
				"cluster": "integration-cluster",
			},
		},
	}

	ch, err := loader.Load(umbrellaDir)
	require.NoError(t, err)

	coalesced, err := CoalescedValuesMapBeforeRender(l, ch, userLikeClusterVals)
	require.NoError(t, err)

	full, err := LoadValuesMap(l, ch, userLikeClusterVals)
	require.NoError(t, err)

	gFull, err := values.LookupMap(full, "global")
	require.NoError(t, err)
	require.NotNil(t, gFull, "full umbrella values must expose global")
	baseURL, ok := gFull["baseUrl"].(string)
	require.True(t, ok, "global.baseUrl must be present after merge with chart defaults; got %#v", gFull["baseUrl"])
	require.Equal(t, "https://from-umbrella-chart.example", baseURL)

	gCoa, err := values.LookupMap(coalesced, "global")
	require.NoError(t, err)
	require.NotNil(t, gCoa)
	_, hasBase := gCoa["baseUrl"]
	t.Logf("coalesced global.baseUrl present: %v", hasBase)

	childName := "child"
	childSub, err := values.LookupMap(coalesced, childName)
	require.NoError(t, err)

	hybrid := types.ValuesMap{}
	if gg, err := values.LookupMap(full, "global"); err == nil && gg != nil {
		hybrid["global"] = gg
	}
	if childSub != nil {
		hybrid = values.MergeValues(hybrid, childSub)
	}

	chChild, err := loader.Load(chartsChild)
	require.NoError(t, err)

	out, err := Template(l, chChild, RenderChartParams{
		ReleaseName:                 childName,
		Namespace:                   "default",
		ValuesMap:                   hybrid,
		KubernetesVersionOrFallback: "v1.30.0",
	})
	require.NoError(t, err, "child template must render when global comes from full umbrella merge")

	s := string(out)
	require.Contains(t, s, "https://from-umbrella-chart.example")
	require.Contains(t, s, "integration-cluster")

	// Control: same hybrid but global taken only from coalesced (older bug path): if baseUrl
	// were missing, rendering would fail — we still assert explicit failure mode when global is stripped.
	badHybrid := types.ValuesMap{
		"global": gCoa,
	}
	if childSub != nil {
		badHybrid = values.MergeValues(badHybrid, childSub)
	}
	if _, ok := gCoa["baseUrl"]; !ok {
		_, errBad := Template(l, chChild, RenderChartParams{
			ReleaseName:                 childName,
			Namespace:                   "default",
			ValuesMap:                   badHybrid,
			KubernetesVersionOrFallback: "v1.30.0",
		})
		require.Error(t, errBad, "when coalesced global lacks baseUrl, child must fail; if this fails, the control assertion needs updating")
		require.Contains(t, errBad.Error(), "global.baseUrl is required")
	}
}

func copyDirContents(src, dst string) error {
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
