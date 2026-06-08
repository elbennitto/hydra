package hydra

import (
	"os"
	"path/filepath"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelmTemplateDiskCache_rootAppDoesNotWriteCacheFiles(t *testing.T) {
	resetCaches()
	contextDir, _ := writeDiskCacheTestGitopsLayout(t)
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	app, err := cluster.WithApp("target.platform")
	require.NoError(t, err)
	happ := app.AsApp()
	require.NotNil(t, happ)

	_, err = happ.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)

	rootGitOps := filepath.Join(contextDir, "target", "platform")
	params := filepath.Join(rootGitOps, ".hydra", "cache", "helm", "cache.yaml")
	templates := filepath.Join(rootGitOps, ".hydra", "cache", "helm", "templates.yaml")
	_, err = os.Stat(params)
	require.True(t, os.IsNotExist(err), "disk cache write disabled: %s", params)
	_, err = os.Stat(templates)
	require.True(t, os.IsNotExist(err), "disk cache write disabled: %s", templates)
}

func TestHelmTemplateDiskCache_valuesChangeOverwritesCache(t *testing.T) {
	resetCaches()
	contextDir, repoDir := writeDiskCacheTestGitopsLayout(t)
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	app, err := cluster.WithApp("target.platform.beta")
	require.NoError(t, err)
	happ := app.AsApp()
	require.NotNil(t, happ)

	first, err := happ.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)

	valuesPath := filepath.Join(contextDir, "target", "platform", "values.yaml")
	require.NoError(t, os.WriteFile(valuesPath, []byte(`platform:
  apps:
    beta:
      namespace: beta-ns-changed
`), 0o644))

	resetCaches()
	ctx2, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster2, err := NewCluster(ctx2, "target", RESTClientLimits{})
	require.NoError(t, err)
	app2, err := cluster2.WithApp("target.platform.beta")
	require.NoError(t, err)
	happ2 := app2.AsApp()
	require.NotNil(t, happ2)

	second, err := happ2.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)
	assert.NotEqual(t, string(first), string(second))
	assert.Contains(t, string(second), "beta-ns-changed")

	_ = repoDir
}

func TestHelmTemplateDiskCache_chartChangeVisibleWithoutDiskPersist(t *testing.T) {
	resetCaches()
	contextDir, repoDir := writeDiskCacheTestGitopsLayout(t)
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	app, err := cluster.WithApp("target.platform.beta")
	require.NoError(t, err)
	happ := app.AsApp()
	require.NotNil(t, happ)

	first, err := happ.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)
	require.NotContains(t, string(first), "hydra-disk-cache-broken-marker")

	betaChart := filepath.Join(repoDir, "apps", "platform", "beta", "dev", "templates", "broken-cache-test.yaml")
	require.NoError(t, os.WriteFile(betaChart, []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: hydra-disk-cache-broken-marker
  namespace: {{ .Release.Namespace }}
`), 0o644))

	resetCaches()
	ctx2, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster2, err := NewCluster(ctx2, "target", RESTClientLimits{})
	require.NoError(t, err)
	app2, err := cluster2.WithApp("target.platform.beta")
	require.NoError(t, err)
	happ2 := app2.AsApp()
	require.NotNil(t, happ2)

	second, err := happ2.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)
	assert.NotEqual(t, string(first), string(second))
	assert.Contains(t, string(second), "hydra-disk-cache-broken-marker")
}

func TestHelmTemplateDiskCache_childDoesNotWriteSuffixedFiles(t *testing.T) {
	resetCaches()
	contextDir, _ := writeDiskCacheTestGitopsLayout(t)
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	app, err := cluster.WithApp("target.platform.beta")
	require.NoError(t, err)
	happ := app.AsApp()
	require.NotNil(t, happ)

	_, err = happ.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)

	rootGitOps := filepath.Join(contextDir, "target", "platform")
	childParams := filepath.Join(rootGitOps, ".hydra", "cache", "helm", "cache-beta.yaml")
	childTmpl := filepath.Join(rootGitOps, ".hydra", "cache", "helm", "templates-beta.yaml")
	_, err = os.Stat(childParams)
	require.True(t, os.IsNotExist(err), "disk cache write disabled: %s", childParams)
	_, err = os.Stat(childTmpl)
	require.True(t, os.IsNotExist(err), "disk cache write disabled: %s", childTmpl)
}

func TestHelmTemplateDiskCache_envNoCacheSkipsDisk(t *testing.T) {
	resetCaches()
	t.Setenv(types.HydraNoCacheEnvName, "1")
	defer t.Setenv(types.HydraNoCacheEnvName, "")

	contextDir, _ := writeDiskCacheTestGitopsLayout(t)
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, !types.HelmTemplateCacheDisabledByEnv())
	require.False(t, cfg.HelmTemplateCacheEnabled())

	ctx, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	app, err := cluster.WithApp("target.platform.beta")
	require.NoError(t, err)
	happ := app.AsApp()
	require.NotNil(t, happ)

	_, err = happ.Template(types.HelmNetworkModeLocal, "1.29.0")
	require.NoError(t, err)

	rootGitOps := filepath.Join(contextDir, "target", "platform")
	_, err = os.Stat(filepath.Join(rootGitOps, ".hydra", "cache", "helm", "cache.yaml"))
	require.True(t, os.IsNotExist(err))
}

func writeDiskCacheTestGitopsLayout(t *testing.T) (contextDir string, repoDir string) {
	t.Helper()
	baseDir := t.TempDir()
	contextDir = filepath.Join(baseDir, "context")
	repoDir = filepath.Join(baseDir, "charts-repository")

	require.NoError(t, os.MkdirAll(contextDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n    kubernetesVersion: \"1.29.0\"\n"), 0o644))

	writeClusterDir(t, contextDir, "in-cluster")
	writeRootAppDiskCache(t, contextDir, repoDir, "in-cluster", "argocd", map[string]string{})
	writeChartDiskCache(t, filepath.Join(repoDir, "apps", "argocd", "root", "dev"), "argocd-root", nil)

	writeClusterDir(t, contextDir, "target")
	writeRootAppDiskCache(t, contextDir, repoDir, "target", "platform", map[string]string{
		"beta": "beta-ns",
	})
	writeChartDiskCache(t, filepath.Join(repoDir, "apps", "platform", "root", "dev"), "platform-root", nil)
	writeChartDiskCache(t, filepath.Join(repoDir, "apps", "platform", "beta", "dev"), "beta", map[string]string{
		"templates/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: beta-api
  namespace: {{ .Release.Namespace }}
spec:
  selector:
    matchLabels:
      app: beta-api
  template:
    metadata:
      labels:
        app: beta-api
    spec:
      containers:
        - name: beta-api
          image: nginx:1.27
`,
	})
	return contextDir, repoDir
}

func writeClusterDir(t *testing.T, contextDir string, clusterName string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(contextDir, clusterName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, clusterName, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
}

func writeRootAppDiskCache(t *testing.T, contextDir string, repoDir string, clusterName string, rootAppName string, childNamespaces map[string]string) {
	t.Helper()
	rootAppDir := filepath.Join(contextDir, clusterName, rootAppName)
	repoPath, err := filepath.Rel(rootAppDir, filepath.Join(repoDir, "apps", rootAppName, "root", "dev"))
	require.NoError(t, err)
	chart := "apiVersion: v2\nname: " + rootAppName + "\nversion: 0.1.0\ndependencies:\n  - name: root\n    version: 0.1.0\n    repository: file://" + filepath.ToSlash(repoPath) + "\n"
	require.NoError(t, os.MkdirAll(rootAppDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rootAppDir, "Chart.yaml"), []byte(chart), 0o644))
	valuesYaml := rootAppName + ":\n"
	if len(childNamespaces) == 0 {
		valuesYaml = rootAppName + ": {}\n"
	} else {
		valuesYaml += "  apps:\n"
		for childAppName, namespace := range childNamespaces {
			valuesYaml += "    " + childAppName + ":\n"
			valuesYaml += "      namespace: " + namespace + "\n"
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(rootAppDir, "values.yaml"), []byte(valuesYaml), 0o644))
}

func writeChartDiskCache(t *testing.T, chartDir string, chartName string, templates map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(chartDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: "+chartName+"\nversion: 0.1.0\ntype: application\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte("{}\n"), 0o644))
	for relPath, content := range templates {
		p := filepath.Join(chartDir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
}

func TestRootAppStaticBackupManifestDigest(t *testing.T) {
	root := t.TempDir()
	d1, err := rootAppStaticBackupManifestDigest(root)
	require.NoError(t, err)
	assert.Equal(t, "", d1)

	require.NoError(t, os.WriteFile(filepath.Join(root, "backup-a-b.sops.yaml"), []byte("one"), 0o644))
	d2, err := rootAppStaticBackupManifestDigest(root)
	require.NoError(t, err)
	require.NotEmpty(t, d2)

	require.NoError(t, os.WriteFile(filepath.Join(root, "backup-c-d.sops.yaml"), []byte("two"), 0o644))
	d3, err := rootAppStaticBackupManifestDigest(root)
	require.NoError(t, err)
	require.NotEqual(t, d2, d3, "digest must change when another backup file is added")

	// Non-matching filenames must not affect the digest.
	require.NoError(t, os.WriteFile(filepath.Join(root, "values.sops.yaml"), []byte("noise"), 0o644))
	d4, err := rootAppStaticBackupManifestDigest(root)
	require.NoError(t, err)
	assert.Equal(t, d3, d4)
}
