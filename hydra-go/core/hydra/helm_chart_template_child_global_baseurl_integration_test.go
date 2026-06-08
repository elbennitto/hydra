package hydra

import (
	"os"
	"path/filepath"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	"github.com/stretchr/testify/require"
)

// TestHelmTemplate_childApp_usesMergedHelmValuesForTemplate passes user/context values,
// cluster-level values (simulating an extra merge step), and expects the child chart to
// render with coalesced umbrella-global (incl. global.baseUrl replicated under the
// dependency key). Regression: ChildApp embeds *RootApp — helmChartValuesForTemplate must
// take the child branch first, otherwise helm.Template sees only cluster-wide globals and
// subchart-required global.baseUrl is missing.
func TestHelmTemplate_childApp_usesMergedHelmValuesForTemplate(t *testing.T) {
	t.Parallel()
	resetCaches()

	contextDir, _ := writeGitopsLayoutChildRequiresGlobalBaseURL(t)
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, false)
	ctx, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	app, err := cluster.WithApp("target.platform.beta")
	require.NoError(t, err)
	happ := app.AsApp()
	require.NotNil(t, happ)

	child := happ.AsChildApp()
	require.NotNil(t, child, "regression guard: WithApp(beta) must resolve to ChildApp")
	templateVals, err := helmChartValuesForTemplate(child.AsApp(), types.HelmNetworkModeLocal)
	require.NoError(t, err)
	require.NotEmpty(t, values.Lookup(templateVals, "beta", "global", "baseUrl"),
		"merged umbrella global.baseUrl must be replicated under the dependency key for helm.Template; wrong AsRootApp-first branch skips MergedChildValuesForHelmInstall")

	out, err := happ.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)
	s := string(out)
	require.Contains(t, s, "https://user-values.example.com", "expected merged global.baseUrl in rendered child chart")
	require.Contains(t, s, "from-cluster-merge", "expected cluster-level values to merge into globals used by child template")
}

func writeGitopsLayoutChildRequiresGlobalBaseURL(t *testing.T) (contextDir string, repoDir string) {
	t.Helper()
	baseDir := t.TempDir()
	contextDir = filepath.Join(baseDir, "context")
	repoDir = filepath.Join(baseDir, "charts-repository")

	require.NoError(t, os.MkdirAll(contextDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "values.yaml"), []byte("global:\n  baseUrl: https://user-values.example.com\n  hydra:\n    type: context\n    kubernetesVersion: \"1.29.0\"\n"), 0o644))

	writeClusterDirWithValues(t, contextDir, "target", "global:\n  hydra:\n    type: cluster\n  childTemplateMergeMarker: from-cluster-merge\n")

	writeClusterDir(t, contextDir, "in-cluster")
	writeRootAppDiskCache(t, contextDir, repoDir, "in-cluster", "argocd", map[string]string{})
	writeChartDiskCache(t, filepath.Join(repoDir, "apps", "argocd", "root", "dev"), "argocd-root", nil)

	writeRootAppDiskCache(t, contextDir, repoDir, "target", "platform", map[string]string{
		"beta": "beta-ns",
	})
	writeChartDiskCache(t, filepath.Join(repoDir, "apps", "platform", "root", "dev"), "platform-root", nil)
	childTemplates := map[string]string{
		"templates/merged-globals-proof.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: helm-template-child-global-proof
  namespace: {{ .Release.Namespace }}
data:
  baseUrl: {{ required "global.baseUrl is required" .Values.global.baseUrl | quote }}
  mergeMarker: {{ .Values.global.childTemplateMergeMarker | required "cluster merge marker missing" | quote }}
`,
	}
	writeChartDiskCache(t, filepath.Join(repoDir, "apps", "platform", "beta", "dev"), "beta", childTemplates)
	return contextDir, repoDir
}

func writeClusterDirWithValues(t *testing.T, contextDir string, clusterName string, yamlContent string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(contextDir, clusterName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, clusterName, "values.yaml"), []byte(yamlContent), 0o644))
}
