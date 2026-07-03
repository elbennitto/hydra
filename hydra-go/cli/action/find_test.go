package action

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	herrors "hydra-gitops.org/hydra/hydra-go/base/errors"
	hlog "hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	hyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
)

func TestFindProjectsUniqueAppIds(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, result, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag:      flags.PredicatesFlag{Predicates: []types.CelPredicate{`kind == "KafkaUser"`}},
		PickFlag:            flags.PickFlag{Pick: `appIds[0]`},
		UniqFlag:            flags.UniqFlag{Uniq: true},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.NoError(t, err)

	actual, err := hyaml.FromYaml[[]string](types.YamlString(result))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"target.platform.alpha", "target.platform.delta"}, actual)
}

func TestFindAggregatesAppIdsForDuplicateResources(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContextEx(t, "shared-user")

	_, result, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag:      flags.PredicatesFlag{Predicates: []types.CelPredicate{`name == "shared-user"`}},
		PickFlag:            flags.PickFlag{Pick: `appIds`},
		UniqFlag:            flags.UniqFlag{Uniq: true},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.NoError(t, err)

	actual, err := hyaml.FromYaml[[][]string](types.YamlString(result))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"target.platform.alpha", "target.platform.delta"}}, actual)
}

func TestFindSupportsCrossClusterMapProjection(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, result, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag:      flags.PredicatesFlag{Predicates: []types.CelPredicate{`kind == "KafkaUser"`}},
		PickFlag:            flags.PickFlag{Pick: `{"appId": appIds[0], "kind": kind}`},
		UniqFlag:            flags.UniqFlag{Uniq: true},
		AppIdPatterns:       []types.AppIdPattern{"**"},
	})
	require.NoError(t, err)

	actual, err := hyaml.FromYaml[[]map[string]any](types.YamlString(result))
	require.NoError(t, err)
	assert.ElementsMatch(t, []map[string]any{
		{"appId": "target.platform.alpha", "kind": "KafkaUser"},
		{"appId": "target.platform.delta", "kind": "KafkaUser"},
		{"appId": "dev.platform.gamma", "kind": "KafkaUser"},
	}, actual)
}

func TestFindSupportsMapProjectionWithDynamicValues(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, result, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag:      flags.PredicatesFlag{Predicates: []types.CelPredicate{`kind == "KafkaUser"`}},
		PickFlag:            flags.PickFlag{Pick: `{"id": id, "images": []}`},
		UniqFlag:            flags.UniqFlag{Uniq: true},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.NoError(t, err)

	actual, err := hyaml.FromYaml[[]map[string]any](types.YamlString(result))
	require.NoError(t, err)
	assert.ElementsMatch(t, []map[string]any{
		{"id": "kafka.strimzi.io/v1beta2/KafkaUser/alpha-ns/alpha-user-a", "images": []any{}},
		{"id": "kafka.strimzi.io/v1beta2/KafkaUser/alpha-ns/alpha-user-b", "images": []any{}},
		{"id": "kafka.strimzi.io/v1beta2/KafkaUser/alpha-ns/shared-user", "images": []any{}},
		{"id": "kafka.strimzi.io/v1beta2/KafkaUser/alpha-ns/shared-user-delta", "images": []any{}},
	}, actual)
}

func TestFindRejectsMissingNestedKeysForIncludeFilter(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, result, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag:      flags.PredicatesFlag{Predicates: []types.CelPredicate{`templateEntity.spec.template.spec.containers.exists(c, c.name == "beta-api")`}},
		PickFlag:            flags.PickFlag{Pick: `appIds[0]`},
		UniqFlag:            flags.UniqFlag{Uniq: true},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.NoError(t, err)

	actual, err := hyaml.FromYaml[[]string](types.YamlString(result))
	require.NoError(t, err)
	assert.Equal(t, []string{"target.platform.beta"}, actual)
}

func TestFindSkipsMissingPickKeysPerEntity(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, result, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag:      flags.PredicatesFlag{Predicates: []types.CelPredicate{`kind == "KafkaUser" || kind == "Deployment"`}},
		PickFlag:            flags.PickFlag{Pick: `templateEntity.spec.template.spec.containers[0].name`},
		UniqFlag:            flags.UniqFlag{Uniq: true},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.NoError(t, err)

	actual, err := hyaml.FromYaml[[]string](types.YamlString(result))
	require.NoError(t, err)
	assert.Equal(t, []string{"beta-api"}, actual)
}

func TestFindLogsHintWhenPickSkipsAllMatches(t *testing.T) {
	var logOutput bytes.Buffer
	configureFindTestLoggingWithOutput(slog.LevelInfo, &logOutput)
	contextDir := writeFindTestContext(t)

	_, result, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag:      flags.PredicatesFlag{Predicates: []types.CelPredicate{`kind == "KafkaUser"`}},
		PickFlag:            flags.PickFlag{Pick: `templateEntity.spec.template.spec.containers[0].name`},
		UniqFlag:            flags.UniqFlag{Uniq: true},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.NoError(t, err)

	actual, err := hyaml.FromYaml[[]string](types.YamlString(result))
	require.NoError(t, err)
	assert.Empty(t, actual)

	logs := logOutput.String()
	assert.Contains(t, logs, "Hint: --pick")
	assert.Contains(t, logs, "key not found")
}

func TestFindRejectsInvalidPickExpression(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, _, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PickFlag:            flags.PickFlag{Pick: `kind ==`},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.Error(t, err)
	assert.Equal(t, herrors.ErrCelCompileFailed, herrors.Id(err))
}

func TestFindRejectsUnsupportedPickType(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, _, err := Find(FindFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PickFlag:            flags.PickFlag{Pick: `{1: name}`},
		AppIdPatterns:       []types.AppIdPattern{"target.platform.*"},
	})
	require.Error(t, err)
	assert.Equal(t, herrors.ErrEvaluationFailed, herrors.Id(err))
}

func writeFindTestContext(t *testing.T) string {
	t.Helper()
	return writeFindTestContextEx(t, "shared-user-delta")
}

// writeFindTestContextEx builds the standard find/template fixture. deltaSharedKafkaName is the
// KafkaUser metadata.name in the delta chart (use "shared-user" only for tests that merge the same
// id across alpha and delta; cluster-wide batch renders require distinct names per app).
func writeFindTestContextEx(t *testing.T, deltaSharedKafkaName string) string {
	t.Helper()

	baseDir := t.TempDir()
	contextDir := filepath.Join(baseDir, "context")
	repoDir := filepath.Join(baseDir, "charts-repository")

	writeFindTestFile(t, filepath.Join(contextDir, "values.yaml"), `global:
  hydra:
    type: context
    path: test-context
    kubernetesVersion: "1.29.0"
`)

	writeClusterValues(t, contextDir, "in-cluster")
	writeRootApp(t, contextDir, repoDir, "in-cluster", "argocd", map[string]string{})
	writeChart(t, filepath.Join(repoDir, "apps", "argocd", "root", "dev"), "argocd-root", nil)

	writeClusterValues(t, contextDir, "target")
	writeRootApp(t, contextDir, repoDir, "target", "platform", map[string]string{
		"alpha": "alpha-ns",
		"beta":  "beta-ns",
		"delta": "alpha-ns",
	})

	writeClusterValues(t, contextDir, "dev")
	writeRootApp(t, contextDir, repoDir, "dev", "platform", map[string]string{
		"gamma": "zzz-ns",
	})

	writeChart(t, filepath.Join(repoDir, "apps", "platform", "root", "dev"), "platform-root", nil)
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "alpha", "dev"), "alpha", map[string]string{
		"templates/kafkauser-a.yaml": `apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaUser
metadata:
  name: alpha-user-a
  namespace: {{ .Release.Namespace }}
`,
		"templates/kafkauser-b.yaml": `apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaUser
metadata:
  name: alpha-user-b
  namespace: {{ .Release.Namespace }}
`,
		"templates/shared-user.yaml": `apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaUser
metadata:
  name: shared-user
  namespace: {{ .Release.Namespace }}
`,
	})
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "beta", "dev"), "beta", map[string]string{
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
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "gamma", "dev"), "gamma", map[string]string{
		"templates/kafkauser.yaml": `apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaUser
metadata:
  name: zzz-user
  namespace: {{ .Release.Namespace }}
`,
	})
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "delta", "dev"), "delta", map[string]string{
		"templates/shared-user.yaml": fmt.Sprintf(`apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaUser
metadata:
  name: %s
  namespace: {{ .Release.Namespace }}
`, deltaSharedKafkaName),
	})

	return contextDir
}

func writeClusterValues(t *testing.T, contextDir string, clusterName string) {
	t.Helper()
	writeFindTestFile(t, filepath.Join(contextDir, clusterName, "values.yaml"), "global:\n  hydra:\n    type: cluster\n")
}

func writeRootApp(t *testing.T, contextDir string, repoDir string, clusterName string, rootAppName string, childNamespaces map[string]string) {
	t.Helper()

	rootAppDir := filepath.Join(contextDir, clusterName, rootAppName)
	repoPath, err := filepath.Rel(rootAppDir, filepath.Join(repoDir, "apps", rootAppName, "root", "dev"))
	require.NoError(t, err)

	writeFindTestFile(t, filepath.Join(rootAppDir, "Chart.yaml"), "apiVersion: v2\nname: "+rootAppName+"\nversion: 0.1.0\ndependencies:\n  - name: root\n    version: 0.1.0\n    repository: file://"+filepath.ToSlash(repoPath)+"\n")

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
	valuesYaml += "global:\n  hydra:\n    type: root-app\n"
	writeFindTestFile(t, filepath.Join(rootAppDir, "values.yaml"), valuesYaml)
}

func writeChart(t *testing.T, chartDir string, chartName string, templates map[string]string) {
	t.Helper()

	writeFindTestFile(t, filepath.Join(chartDir, "Chart.yaml"), "apiVersion: v2\nname: "+chartName+"\nversion: 0.1.0\ntype: application\n")
	writeFindTestFile(t, filepath.Join(chartDir, "values.yaml"), "{}\n")
	for relPath, content := range templates {
		writeFindTestFile(t, filepath.Join(chartDir, relPath), content)
	}
}

func writeFindTestFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func configureFindTestLogging() {
	configureFindTestLoggingWithOutput(slog.LevelWarn, nil)
}

func configureFindTestLoggingWithOutput(level slog.Level, output io.Writer) {
	var progressBars hlog.ProgressBars
	if output != nil {
		progressBars = hlog.NewDummyProgressBarsTo(output)
	}

	hlog.Configure(hlog.Config{
		Level:        level,
		Timestamps:   false,
		ProgressBars: progressBars,
	})
}
