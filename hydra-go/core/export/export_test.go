package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/chart/loader"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/view"
)

// helper: set up a directory that looks like a previous cluster dump
func setupExistingDump(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hydra.yaml"), []byte("entities: []\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "manifests", "app"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifests", "app", "old.yaml"), []byte("old"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "charts"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "charts", "old.tgz"), []byte("old"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "values", "files"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "values", "files", "old.yaml"), []byte("old"), 0644))
}

// ============================================================================
// ValidateAndPrepareDir (combined)
// ============================================================================

func TestValidateAndPrepareDir_NewDir(t *testing.T) {
	parentDir := t.TempDir()
	dir := filepath.Join(parentDir, "output")

	err := ValidateAndPrepareDir(dir)
	require.NoError(t, err)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestValidateAndPrepareDir_ExistingDump(t *testing.T) {
	dir := t.TempDir()
	setupExistingDump(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("keep"), 0644))

	err := ValidateAndPrepareDir(dir)
	require.NoError(t, err)

	// hydra.yaml, manifests/, charts/, values/ must be gone
	_, err = os.Stat(filepath.Join(dir, "hydra.yaml"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "manifests"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "charts"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "values"))
	assert.True(t, os.IsNotExist(err))

	// extra.txt must still be there
	data, err := os.ReadFile(filepath.Join(dir, "extra.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data))
}

func TestValidateAndPrepareDir_MissingHydraYaml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "manifests"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "charts"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "values"), 0755))

	err := ValidateAndPrepareDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hydra.yaml")
}

func TestValidateAndPrepareDir_MissingManifests(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hydra.yaml"), []byte("x"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "charts"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "values"), 0755))

	err := ValidateAndPrepareDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifests")
}

func TestValidateAndPrepareDir_MissingCharts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hydra.yaml"), []byte("x"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "manifests"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "values"), 0755))

	err := ValidateAndPrepareDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "charts")
}

func TestValidateAndPrepareDir_MissingValues(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hydra.yaml"), []byte("x"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "manifests"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "charts"), 0755))

	err := ValidateAndPrepareDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "values")
}

func TestValidateAndPrepareDir_ParentDoesNotExist(t *testing.T) {
	dir := filepath.Join("/tmp/nonexistent-parent-xyz-abc-123", "output")

	err := ValidateAndPrepareDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parent directory")
}

func TestValidateAndPrepareDir_FileNotDir(t *testing.T) {
	parentDir := t.TempDir()
	filePath := filepath.Join(parentDir, "notadir")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	err := ValidateAndPrepareDir(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// ============================================================================
// ValidateDir (read-only check)
// ============================================================================

func TestValidateDir_NewDir_Succeeds(t *testing.T) {
	parentDir := t.TempDir()
	dir := filepath.Join(parentDir, "output")

	err := ValidateDir(dir)
	require.NoError(t, err)

	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "ValidateDir should not create the directory")
}

func TestValidateDir_ExistingDump_Succeeds(t *testing.T) {
	dir := t.TempDir()
	setupExistingDump(t, dir)

	err := ValidateDir(dir)
	require.NoError(t, err)

	// nothing should be modified
	_, err = os.Stat(filepath.Join(dir, "hydra.yaml"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "manifests"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "charts"))
	assert.NoError(t, err)
}

func TestValidateDir_MissingHydraYaml_Fails(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "manifests"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "charts"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "values"), 0755))

	err := ValidateDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hydra.yaml")
}

func TestValidateDir_MissingManifests_Fails(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hydra.yaml"), []byte("x"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "charts"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "values"), 0755))

	err := ValidateDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifests")
}

func TestValidateDir_ParentDoesNotExist_Fails(t *testing.T) {
	dir := filepath.Join("/tmp/nonexistent-parent-xyz-abc-123", "output")

	err := ValidateDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parent directory")
}

func TestValidateDir_FileNotDir_Fails(t *testing.T) {
	parentDir := t.TempDir()
	filePath := filepath.Join(parentDir, "notadir")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	err := ValidateDir(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// ============================================================================
// PrepareDir
// ============================================================================

func TestPrepareDir_NewDir_Creates(t *testing.T) {
	parentDir := t.TempDir()
	dir := filepath.Join(parentDir, "output")

	err := PrepareDir(dir)
	require.NoError(t, err)

	info, statErr := os.Stat(dir)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestPrepareDir_ExistingDump_RemovesOnlyDumpFiles(t *testing.T) {
	dir := t.TempDir()
	setupExistingDump(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("keep"), 0644))

	err := PrepareDir(dir)
	require.NoError(t, err)

	// dump artifacts must be gone
	_, err = os.Stat(filepath.Join(dir, "hydra.yaml"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "manifests"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "charts"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "values"))
	assert.True(t, os.IsNotExist(err))

	// extra file must survive
	data, err := os.ReadFile(filepath.Join(dir, "extra.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data))
}

// ============================================================================
// WriteManifests / WriteHydraYaml
// ============================================================================

func TestWriteManifests(t *testing.T) {
	dir := t.TempDir()

	manifests := map[string][]byte{
		"dev.app/apps/v1/Deployment/default/demo.yaml":     []byte("apiVersion: apps/v1\nkind: Deployment\n"),
		"dev.app/(core)/v1/Service/(cluster)/webhook.yaml": []byte("apiVersion: v1\nkind: Service\n"),
		"": nil,
	}

	err := WriteManifests(testLogger(), dir, manifests)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "manifests", "dev.app", "apps", "v1", "Deployment", "default", "demo.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "apiVersion: apps/v1\nkind: Deployment\n", string(content))

	content, err = os.ReadFile(filepath.Join(dir, "manifests", "dev.app", "(core)", "v1", "Service", "(cluster)", "webhook.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "apiVersion: v1\nkind: Service\n", string(content))
}

func TestWriteHydraYaml(t *testing.T) {
	dir := t.TempDir()

	model := testDependenciesModel()
	err := WriteHydraYaml(dir, model)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "hydra.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "entities:")
}

// ============================================================================
// WriteValueFiles
// ============================================================================

func TestWriteValueFiles(t *testing.T) {
	// Set up a fake context parent with value files
	contextParent := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextParent, "values.yaml"), []byte("global:\n  stage: prod\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(contextParent, "dev"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(contextParent, "dev", "values.yaml"), []byte("context: dev\n"), 0644))

	outputDir := t.TempDir()

	valueFiles := []view.ValueFileModel{
		{Path: "values.yaml", Type: "group"},
		{Path: "dev/values.yaml", Type: "context"},
		{Path: "nonexistent/values.yaml", Type: "cluster"},
	}

	err := WriteValueFiles(testLogger(), outputDir, valueFiles, contextParent)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(outputDir, "values", "files", "values.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "global:\n  stage: prod\n", string(content))

	content, err = os.ReadFile(filepath.Join(outputDir, "values", "files", "dev", "values.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "context: dev\n", string(content))

	// Nonexistent file should be skipped (no error)
	_, err = os.Stat(filepath.Join(outputDir, "values", "files", "nonexistent", "values.yaml"))
	assert.True(t, os.IsNotExist(err))
}

// ============================================================================
// WriteMergedValues
// ============================================================================

func TestWriteMergedValues(t *testing.T) {
	dir := t.TempDir()

	appValues := map[types.AppId]types.ValuesMap{
		"dev.my-app": {
			"global":   map[string]any{"domain": "example.com"},
			"replicas": 3,
		},
		"dev.my-app.child": {
			"enabled": true,
		},
	}

	err := WriteMergedValues(testLogger(), dir, appValues)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "values", "merged", "dev.my-app.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "domain: example.com")
	assert.Contains(t, string(content), "replicas: 3")

	content, err = os.ReadFile(filepath.Join(dir, "values", "merged", "dev.my-app.child.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "enabled: true")
}

// Integration test: create a chart on disk, load it with Helm's loader,
// merge values via CoalesceValues, export with WriteMergedValues, and verify
// the YAML output contains plain integers instead of scientific notation.
//
// Helm's loader uses sigs.k8s.io/yaml which routes through encoding/json,
// turning all YAML numbers into float64. CoalesceValues preserves that type.
// Without correction, yaml.Marshal then writes e.g. 1.048576e+07 instead of 10485760.
func TestExportedMergedValues_NoScientificNotation(t *testing.T) {
	// 1. Create a minimal Helm chart on disk
	chartDir := filepath.Join(t.TempDir(), "mini-chart")
	require.NoError(t, os.MkdirAll(filepath.Join(chartDir, "templates"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`apiVersion: v2
name: mini-chart
version: 0.1.0
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(`config:
  segment-bytes: 10485760
  buffer-size: 1048576
  small-number: 42
  rate: 1.5
  enabled: true
  name: test
`), 0644))
	require.NoError(t, os.WriteFile(
		filepath.Join(chartDir, "templates", "configmap.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"), 0644,
	))

	// 2. Load chart with Helm's loader (numbers become float64 via sigs.k8s.io/yaml)
	ch, err := loader.Load(chartDir)
	require.NoError(t, err)

	// 3. Merge chart defaults with override values via Helm's CoalesceValues
	givenValues := types.ValuesMap{
		"config": map[string]any{
			"extra-key": "from-override",
		},
	}
	mergedValues, err := chartutil.CoalesceValues(ch, givenValues)
	require.NoError(t, err)

	// 4. Export via WriteMergedValues (same path as real cluster dump)
	outputDir := t.TempDir()
	appValues := map[types.AppId]types.ValuesMap{
		"dev.mini-app": mergedValues,
	}
	err = WriteMergedValues(testLogger(), outputDir, appValues)
	require.NoError(t, err)

	// 5. Read the exported file and verify format
	content, err := os.ReadFile(filepath.Join(outputDir, "values", "merged", "dev.mini-app.yaml"))
	require.NoError(t, err)
	out := string(content)

	assert.Contains(t, out, "segment-bytes: 10485760", "large integer must not use scientific notation")
	assert.Contains(t, out, "buffer-size: 1048576", "large integer must not use scientific notation")
	assert.Contains(t, out, "small-number: 42")
	assert.Contains(t, out, "rate: 1.5")
	assert.Contains(t, out, "enabled: true")
	assert.Contains(t, out, "name: test")
	assert.Contains(t, out, "extra-key: from-override")

	for i, line := range strings.Split(out, "\n") {
		assert.NotContains(t, line, "e+", "line %d contains scientific notation: %s", i+1, line)
	}
}

// ============================================================================
// ValidateContextDir
// ============================================================================

func TestValidateContextDir_NewDir_Succeeds(t *testing.T) {
	parentDir := t.TempDir()
	dir := filepath.Join(parentDir, "output")

	err := ValidateContextDir(dir)
	require.NoError(t, err)

	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "ValidateContextDir should not create the directory")
}

func TestValidateContextDir_ExistingEmptyDir_Succeeds(t *testing.T) {
	dir := t.TempDir()

	err := ValidateContextDir(dir)
	require.NoError(t, err)
}

func TestValidateContextDir_ExistingDirWithClusterSubdirs_Succeeds(t *testing.T) {
	dir := t.TempDir()

	clusterDir := filepath.Join(dir, "dev")
	setupExistingDump(t, clusterDir)

	err := ValidateContextDir(dir)
	require.NoError(t, err)
}

func TestValidateContextDir_ParentDoesNotExist_Fails(t *testing.T) {
	dir := filepath.Join("/tmp/nonexistent-parent-xyz-ctx-123", "output")

	err := ValidateContextDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parent directory")
}

func TestValidateContextDir_FileNotDir_Fails(t *testing.T) {
	parentDir := t.TempDir()
	filePath := filepath.Join(parentDir, "notadir")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	err := ValidateContextDir(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestValidateContextDir_InvalidClusterSubdir_Fails(t *testing.T) {
	dir := t.TempDir()

	clusterDir := filepath.Join(dir, "dev")
	require.NoError(t, os.MkdirAll(clusterDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "hydra.yaml"), []byte("x"), 0644))
	// missing manifests/, charts/, values/ → should fail

	err := ValidateContextDir(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifests")
}
