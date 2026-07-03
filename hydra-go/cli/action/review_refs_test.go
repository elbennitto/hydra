package action

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	hlog "hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	hyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
)

// TestReviewRefsResolvesTargetsAgainstAllEnabledAppsOnCluster documents hydra local review:
// targets are templates of every effectively enabled app on the cluster, so selecting only the
// consumer app still resolves references to objects rendered by another enabled peer (provider).
func TestReviewRefsResolvesTargetsAgainstAllEnabledAppsOnCluster(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeReviewRefsTestContext(t)

	output, err := captureStdout(t, func() error {
		return ReviewRefs(ReviewRefsFlags{
			ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
			HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
			ReviewRefsYamlFlag:  flags.ReviewRefsYamlFlag{Yaml: true},
			AppIdPatterns:       []types.AppIdPattern{"prod.platform.consumer"},
		})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 review finding")

	findings, parseErr := hyaml.FromYaml[[]commands.ReviewFinding](types.YamlString(output))
	require.NoError(t, parseErr)
	require.Len(t, findings, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/demo/consumer"), findings[0].Target)
	assert.Contains(t, findings[0].Message, "ref ownership conflicts with standalone template render")
	assert.Contains(t, findings[0].Message, "prod.platform.provider")
	assert.Empty(t, findings[0].Sources)
}

// TestReviewRefsExcludeAppReducesSourcesOnlyNotTemplateTargets documents that --exclude-app
// removes apps from the source render set but must not drop another enabled app's templates
// from the local target index used for resolution.
func TestReviewRefsExcludeAppReducesSourcesOnlyNotTemplateTargets(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeReviewRefsTestContext(t)

	output, err := captureStdout(t, func() error {
		return ReviewRefs(ReviewRefsFlags{
			ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
			HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
			ReviewRefsYamlFlag:  flags.ReviewRefsYamlFlag{Yaml: true},
			AppIdPatterns:       []types.AppIdPattern{"prod.platform.*"},
			ExcludeAppFlag:      flags.ExcludeAppFlag{ExcludeAppPatterns: []types.AppIdPattern{"prod.platform.provider"}},
		})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 review finding")

	findings, parseErr := hyaml.FromYaml[[]commands.ReviewFinding](types.YamlString(output))
	require.NoError(t, parseErr)
	require.Len(t, findings, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/demo/consumer"), findings[0].Target)
	assert.Contains(t, findings[0].Message, "ref ownership conflicts with standalone template render")
	assert.Contains(t, findings[0].Message, "prod.platform.provider")
	assert.Empty(t, findings[0].Sources)
}

// TestReviewRefsDisabledPeerDoesNotSupplyLocalTargets documents hydra local review:
// enabled: false apps are not rendered and therefore do not contribute template targets; a
// reference that only such an app would satisfy stays unresolved.
func TestReviewRefsDisabledPeerDoesNotSupplyLocalTargets(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeReviewRefsTestContextWithDisabledGhostApp(t)

	output, err := captureStdout(t, func() error {
		return ReviewRefs(ReviewRefsFlags{
			ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
			HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
			ReviewRefsYamlFlag:  flags.ReviewRefsYamlFlag{Yaml: true},
			AppIdPatterns:       []types.AppIdPattern{"prod.platform.consumer"},
		})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 review finding")

	findings, parseErr := hyaml.FromYaml[[]commands.ReviewFinding](types.YamlString(output))
	require.NoError(t, parseErr)
	require.Len(t, findings, 2)

	var missingTarget *commands.ReviewFinding
	var ownershipConflict *commands.ReviewFinding
	for i := range findings {
		switch {
		case findings[i].Target == types.Id("v1/ConfigMap/demo/ghost-only"):
			missingTarget = &findings[i]
		case findings[i].Target == types.Id("apps/v1/Deployment/demo/consumer"):
			ownershipConflict = &findings[i]
		}
	}
	require.NotNil(t, missingTarget)
	assert.Equal(t, "missing target resource", missingTarget.Message)
	assert.ElementsMatch(t, []types.Id{
		"apps/v1/Deployment/demo/consumer",
	}, missingTarget.Sources)

	require.NotNil(t, ownershipConflict)
	assert.Contains(t, ownershipConflict.Message, "ref ownership conflicts with standalone template render")
	assert.Contains(t, ownershipConflict.Message, "prod.platform.provider")
	assert.Empty(t, ownershipConflict.Sources)
}

// TestClusterReviewRefsSemantic_DataPathUsesLiveClusterEntityTargets exercises the same data path
// as action.ClusterReviewRefs after app resolution and selected-app rendering: template sources plus
// targets keyed as KeyClusterEntity via commands.ReviewRefsEntitiesWithTargets. ListClusterAll is
// replaced by fixture entities that stand in for a full live snapshot (including objects logically
// owned by apps excluded from the source set), because an integration test against a real API is
// not required here.
func TestClusterReviewRefsSemantic_DataPathUsesLiveClusterEntityTargets(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeReviewRefsTestContext(t)
	cluster := reviewRefsTestHydraCluster(t, contextDir, "prod")

	f := ReviewRefsFlags{
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		AppIdPatterns:       []types.AppIdPattern{"prod.platform.*"},
		ExcludeAppFlag:      flags.ExcludeAppFlag{ExcludeAppPatterns: []types.AppIdPattern{"prod.platform.provider"}},
	}
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedYes)
	appIds, err := commands.ResolveAppIdsFromConfig(
		hlog.Default(),
		f.HydraContext,
		config,
		f.AppIdPatterns,
		f.ExcludeAppPatterns,
		f.HelmNetworkMode,
		false,
	)
	require.NoError(t, err)
	assert.True(t, appIds.Has(types.AppId("prod.platform.consumer")))
	assert.False(t, appIds.Has(types.AppId("prod.platform.provider")))

	sourceEntities, err := commands.RenderClusterSelectedApps(
		cluster,
		f.HelmNetworkMode,
		types.KubernetesVersion(""),
		appIds,
		types.KeyTemplateEntity,
	)
	require.NoError(t, err)

	targetYAML := types.YamlString(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  SPRING_PROFILE: prod
`)
	targetEntities, err := entity.NewEntitiesFromYaml(hlog.Default(), targetYAML, types.KeyClusterEntity)
	require.NoError(t, err)

	findings, err := commands.ReviewRefsEntitiesWithTargets(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		types.KeyClusterEntity,
		appIds,
	)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

// TestClusterReviewRefsSemantic_DisabledAppOmitsRenderedSourcesLiveSnapshotStillResolves documents
// the cluster review path on the action/render boundary: apps with enabled: false are absent from
// resolved app IDs and from RenderClusterSelectedApps output, while a simulated full live snapshot
// (KeyClusterEntity) can still contain objects that satisfy references those disabled charts would
// have defined offline only.
func TestClusterReviewRefsSemantic_DisabledAppOmitsRenderedSourcesLiveSnapshotStillResolves(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeReviewRefsTestContextWithDisabledGhostApp(t)
	cluster := reviewRefsTestHydraCluster(t, contextDir, "prod")

	f := ReviewRefsFlags{
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		AppIdPatterns:       []types.AppIdPattern{"prod.platform.consumer"},
	}
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedYes)
	appIds, err := commands.ResolveAppIdsFromConfig(
		hlog.Default(),
		f.HydraContext,
		config,
		f.AppIdPatterns,
		f.ExcludeAppPatterns,
		f.HelmNetworkMode,
		false,
	)
	require.NoError(t, err)
	assert.True(t, appIds.Has(types.AppId("prod.platform.consumer")))
	assert.False(t, appIds.Has(types.AppId("prod.platform.ghost")), "disabled child app must not be resolved")

	sourceEntities, err := commands.RenderClusterSelectedApps(
		cluster,
		f.HelmNetworkMode,
		types.KubernetesVersion(""),
		appIds,
		types.KeyTemplateEntity,
	)
	require.NoError(t, err)
	ghostApp := types.AppId("prod.platform.ghost")
	for _, item := range sourceEntities.Items {
		ids, err := item.AppIds()
		require.NoError(t, err)
		for _, id := range ids {
			assert.NotEqual(t, ghostApp, id, "rendered sources must not include manifests from disabled apps")
		}
	}

	targetYAML := types.YamlString(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: ghost-only
  namespace: demo
data:
  GHOST_KEY: from-live-cluster
`)
	targetEntities, err := entity.NewEntitiesFromYaml(hlog.Default(), targetYAML, types.KeyClusterEntity)
	require.NoError(t, err)

	findings, err := commands.ReviewRefsEntitiesWithTargets(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		types.KeyClusterEntity,
		appIds,
	)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func reviewRefsTestHydraCluster(t *testing.T, contextDir string, clusterName string) *hydra.Cluster {
	t.Helper()
	h, err := hydra.ResolvePath(
		hlog.Default(),
		types.HydraContext(contextDir),
		types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true),
	)
	require.NoError(t, err)
	ctx := h.AsContext()
	require.NotNil(t, ctx)
	c, err := ctx.WithCluster(types.ClusterName(clusterName), hydra.RESTClientLimits{})
	require.NoError(t, err)
	return c
}

// Regression: exitcode errors skip root ErrorLog, and Cobra's error line goes to a Debug-level
// slog writer — without an explicit ErrorLog here, users saw YAML on stdout but no finding count.
func TestFinishReviewRefsLogsFindingCount(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})
	oldL := hlog.Default()
	hlog.SetDefault(hlog.NewLoggerWithHandler(handler))
	t.Cleanup(func() { hlog.SetDefault(oldL) })

	err := finishReviewRefs(hlog.Default(), 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 review finding")
	assert.Contains(t, buf.String(), "2")
	assert.Contains(t, buf.String(), "review finding")
}

func TestNewReviewFindingStdoutCallbackWritesYamlSequence(t *testing.T) {
	output, err := captureStdout(t, func() error {
		callback := newReviewFindingStdoutCallback(types.ColorNo, true)
		if err := callback(commands.ReviewFinding{
			Target:  types.Id("v1/ConfigMap/demo/a"),
			Message: "missing target resource",
			Sources: []types.Id{"apps/v1/Deployment/demo/source-a"},
		}); err != nil {
			return err
		}
		return callback(commands.ReviewFinding{
			Target:  types.Id("v1/Secret/demo/b"),
			Message: `missing referenced key "PASSWORD"`,
			Sources: []types.Id{"apps/v1/Deployment/demo/source-b"},
		})
	})
	require.NoError(t, err)

	findings, parseErr := hyaml.FromYaml[[]commands.ReviewFinding](types.YamlString(output))
	require.NoError(t, parseErr)
	require.Len(t, findings, 2)
	assert.Equal(t, types.Id("v1/ConfigMap/demo/a"), findings[0].Target)
	assert.Equal(t, types.Id("v1/Secret/demo/b"), findings[1].Target)
}

func TestNewReviewFindingStdoutCallbackRespectsColorFlag(t *testing.T) {
	plainOutput, err := captureStdout(t, func() error {
		plainCallback := newReviewFindingStdoutCallback(types.ColorNo, true)
		return plainCallback(commands.ReviewFinding{
			Target:  types.Id("v1/ConfigMap/demo/plain"),
			Message: "missing target resource",
			Sources: []types.Id{"apps/v1/Deployment/demo/source"},
		})
	})
	require.NoError(t, err)
	assert.NotContains(t, plainOutput, "\x1b[")

	colorOutput, err := captureStdout(t, func() error {
		colorCallback := newReviewFindingStdoutCallback(types.ColorYes, true)
		return colorCallback(commands.ReviewFinding{
			Target:  types.Id("v1/ConfigMap/demo/color"),
			Message: "missing target resource",
			Sources: []types.Id{"apps/v1/Deployment/demo/source"},
		})
	})
	require.NoError(t, err)
	assert.True(t, strings.Contains(colorOutput, "\x1b["), "expected ANSI color escape sequences")
}

func TestNewReviewFindingStdoutCallbackTextFormatPlain(t *testing.T) {
	output, err := captureStdout(t, func() error {
		return writeReviewFindingsHumanOrYaml(os.Stdout, types.ColorNo, false, []commands.ReviewFinding{{
			Target:  types.Id("v1/ConfigMap/demo/plain"),
			Message: "missing target resource",
			Sources: []types.Id{"apps/v1/Deployment/demo/source"},
		}})
	})
	require.NoError(t, err)
	assert.Contains(t, output, "Review finding")
	assert.Contains(t, output, "Message type: missing target resource")
	assert.Contains(t, output, "v1/ConfigMap/demo/plain")
	assert.NotContains(t, output, "\x1b[")
}

func TestNewReviewFindingStdoutCallbackTextFormatColored(t *testing.T) {
	output, err := captureStdout(t, func() error {
		return writeReviewFindingsHumanOrYaml(os.Stdout, types.ColorYes, false, []commands.ReviewFinding{{
			Target:  types.Id("v1/ConfigMap/demo/color-text"),
			Message: "missing target resource",
			Sources: []types.Id{"apps/v1/Deployment/demo/source"},
		}})
	})
	require.NoError(t, err)
	assert.Contains(t, output, "Review finding")
	assert.True(t, strings.Contains(output, "\x1b["))
}

func writeReviewRefsTestContext(t *testing.T) string {
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

	writeClusterValues(t, contextDir, string(types.InCluster))
	writeRootApp(t, contextDir, repoDir, string(types.InCluster), string(types.ArgocdDir), map[string]string{})
	writeChart(t, filepath.Join(repoDir, "apps", string(types.ArgocdDir), "root", "dev"), "argocd-root", nil)

	writeClusterValues(t, contextDir, "prod")
	writeRootApp(t, contextDir, repoDir, "prod", "platform", map[string]string{
		"consumer": "demo",
		"provider": "demo",
	})
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "root", "dev"), "platform-root", nil)
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "consumer", "dev"), "consumer", map[string]string{
		"templates/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer
  namespace: {{ .Release.Namespace }}
spec:
  selector:
    matchLabels:
      app: consumer
  template:
    metadata:
      labels:
        app: consumer
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
`,
	})
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "provider", "dev"), "provider", map[string]string{
		"templates/configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: {{ .Release.Namespace }}
data:
  SPRING_PROFILE: prod
`,
	})

	return contextDir
}

func writeReviewRefsRootAppPlatform(
	t *testing.T,
	contextDir string,
	repoDir string,
	clusterName string,
	children map[string]reviewRefsPlatformChildSpec,
) {
	t.Helper()

	rootAppDir := filepath.Join(contextDir, clusterName, "platform")
	repoPath, err := filepath.Rel(rootAppDir, filepath.Join(repoDir, "apps", "platform", "root", "dev"))
	require.NoError(t, err)

	writeFindTestFile(t, filepath.Join(rootAppDir, "Chart.yaml"), "apiVersion: v2\nname: platform\nversion: 0.1.0\ndependencies:\n  - name: root\n    version: 0.1.0\n    repository: file://"+filepath.ToSlash(repoPath)+"\n")

	names := make([]string, 0, len(children))
	for n := range children {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("platform:\n  apps:\n")
	for _, name := range names {
		spec := children[name]
		b.WriteString("    ")
		b.WriteString(name)
		b.WriteString(":\n")
		if spec.Enabled != nil {
			b.WriteString("      enabled: ")
			if *spec.Enabled {
				b.WriteString("true\n")
			} else {
				b.WriteString("false\n")
			}
		}
		b.WriteString("      namespace: ")
		b.WriteString(spec.Namespace)
		b.WriteString("\n")
	}
	b.WriteString("global:\n  hydra:\n    type: root-app\n")
	writeFindTestFile(t, filepath.Join(rootAppDir, "values.yaml"), b.String())
}

type reviewRefsPlatformChildSpec struct {
	Namespace string
	Enabled   *bool
}

func writeReviewRefsTestContextWithDisabledGhostApp(t *testing.T) string {
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

	writeClusterValues(t, contextDir, string(types.InCluster))
	writeRootApp(t, contextDir, repoDir, string(types.InCluster), string(types.ArgocdDir), map[string]string{})
	writeChart(t, filepath.Join(repoDir, "apps", string(types.ArgocdDir), "root", "dev"), "argocd-root", nil)

	writeClusterValues(t, contextDir, "prod")
	enabledFalse := false
	writeReviewRefsRootAppPlatform(t, contextDir, repoDir, "prod", map[string]reviewRefsPlatformChildSpec{
		"consumer": {Namespace: "demo"},
		"provider": {Namespace: "demo"},
		"ghost":    {Namespace: "demo", Enabled: &enabledFalse},
	})
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "root", "dev"), "platform-root", nil)
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "consumer", "dev"), "consumer", map[string]string{
		"templates/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer
  namespace: {{ .Release.Namespace }}
spec:
  selector:
    matchLabels:
      app: consumer
  template:
    metadata:
      labels:
        app: consumer
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: GHOST_KEY
              valueFrom:
                configMapKeyRef:
                  name: ghost-only
                  key: GHOST_KEY
`,
	})
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "provider", "dev"), "provider", map[string]string{
		"templates/configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: {{ .Release.Namespace }}
data:
  PLACEHOLDER: "1"
`,
	})
	writeChart(t, filepath.Join(repoDir, "apps", "platform", "ghost", "dev"), "ghost", map[string]string{
		"templates/configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: ghost-only
  namespace: {{ .Release.Namespace }}
data:
  GHOST_KEY: from-ghost
`,
	})

	return contextDir
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer

	runErr := fn()

	require.NoError(t, writer.Close())
	os.Stdout = originalStdout

	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, reader)
	require.NoError(t, copyErr)
	require.NoError(t, reader.Close())

	return buf.String(), runErr
}
