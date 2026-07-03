package helm

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v4/pkg/chart/loader"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// serviceMobileRolloutAssistantChartDevDir resolves charts-repository/.../service-mobile-rollout-assistant/dev
// relative to the repository root (parent of hydra/).
func serviceMobileRolloutAssistantChartDevDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(1)
	require.True(t, ok)
	coreHelm := filepath.Dir(file)
	repoRoot := filepath.Clean(filepath.Join(coreHelm, "..", "..", "..", ".."))
	return filepath.Join(repoRoot, "charts-repository", "apps", "demo", "service-mobile-rollout-assistant", "dev")
}

// TestTemplateV2Chart_ServiceMobileRolloutAssistant_nginxConfLeadingNewline asserts that the
// packaged demo service-mobile-rollout-assistant umbrella chart renders ConfigMap data.nginx.conf
// with a leading newline — matching `helm template` / Sprig nindent behaviour (Hydra uses the
// same TemplateV2Chart pipeline).
func TestTemplateV2Chart_ServiceMobileRolloutAssistant_nginxConfLeadingNewline(t *testing.T) {
	t.Parallel()

	chartDir := serviceMobileRolloutAssistantChartDevDir(t)
	if _, err := os.Stat(filepath.Join(chartDir, "Chart.yaml")); err != nil {
		t.Skip("chart directory not in workspace (skip in minimal checkouts): ", chartDir, ": ", err)
	}

	chrt, err := loader.Load(chartDir)
	require.NoError(t, err)
	v2chrt, err := convertToV2Chart(chrt)
	require.NoError(t, err)

	vals := types.ValuesMap{
		"global": map[string]any{
			"baseUrl":     "https://helm-unit-test.example",
			"dnsResolver": "kube-dns.kube-system.svc.cluster.local",
			"namespace":   "demo",
		},
	}

	out, err := TemplateV2Chart(log.Default(), v2chrt, RenderChartParams{
		KubernetesVersionOrFallback: "v1.30.0",
		ReleaseName:                 "mra",
		Namespace:                   "demo",
		ValuesMap:                   vals,
		SkipCrds:                    false,
	})
	require.NoError(t, err)

	dec := yaml.NewDecoder(strings.NewReader(string(out)))
	for {
		var doc map[string]interface{}
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if doc == nil {
			continue
		}
		if doc["kind"] != "ConfigMap" {
			continue
		}
		meta, _ := doc["metadata"].(map[string]interface{})
		if meta == nil || meta["name"] != "nginx-config" {
			continue
		}
		data, ok := doc["data"].(map[string]interface{})
		require.True(t, ok, "nginx-config must have data map")
		raw, ok := data["nginx.conf"].(string)
		require.True(t, ok, "data.nginx.conf must be a string, got %T", data["nginx.conf"])
		require.True(t, strings.HasPrefix(raw, "\n"),
			"rendered nginx.conf must keep Helm's leading newline before 'events {'; prefix=%q",
			leadingSnippetForAssert(raw, 48))
		require.Contains(t, raw, "events {")
		return
	}
	t.Fatal("ConfigMap nginx-config not found in TemplateV2Chart output")
}

func leadingSnippetForAssert(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
