package action

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestTemplateIncludeFiltersToMatchingKinds(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, full, err := Template(TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		AppId:               "target.platform.beta",
	})
	require.NoError(t, err)
	assert.Contains(t, full, "kind: Deployment")
	// Unfiltered path is sorted multi-document YAML from the entity pipeline (no Helm # Source headers).
	assert.Contains(t, full, "beta-api")

	_, filtered, err := Template(TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`kind == "KafkaUser"`},
		},
		AppId: "target.platform.beta",
	})
	require.NoError(t, err)
	assert.NotContains(t, strings.TrimSpace(filtered), "kind: Deployment")
	assert.Empty(t, strings.TrimSpace(filtered))

	_, kafkaOnly, err := Template(TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`kind == "KafkaUser"`},
		},
		AppId: "target.platform.alpha",
	})
	require.NoError(t, err)
	assert.Contains(t, kafkaOnly, "kind: KafkaUser")
	assert.NotContains(t, kafkaOnly, "kind: Deployment")
}

func TestTemplateExcludeRemovesMatchingKinds(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, filtered, err := Template(TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`!(kind == "KafkaUser")`},
		},
		AppId: "target.platform.alpha",
	})
	require.NoError(t, err)
	assert.NotContains(t, filtered, "KafkaUser")
}

// writeTemplateConfigMapGoldenContext is like writeFindTestContext but the beta chart also includes
// miniBlocksConfigMapChartTemplate (only used by TestTemplateConfigMapGivenVsReceived).
func writeTemplateConfigMapGoldenContext(t *testing.T) string {
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
		"templates/redis-health-configmap.yaml": miniBlocksConfigMapChartTemplate,
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
		"templates/shared-user.yaml": `apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaUser
metadata:
  name: shared-user-delta
  namespace: {{ .Release.Namespace }}
`,
	})

	return contextDir
}

// miniBlocksConfigMapChartTemplate is the chart file body for templates/redis-health-configmap.yaml
// (given: both data entries use literal block |).
const miniBlocksConfigMapChartTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: mini-blocks
  namespace: {{ .Release.Namespace }}
data:
  pipe.txt: |
    a
    b
  strip.txt: |
    c
    d
`

// TestTemplateConfigMapGivenVsReceived compares full stdout for hydra local template
// (unfiltered vs filtered) to fixed golden strings. The chart template is miniBlocksConfigMapChartTemplate.
func TestTemplateConfigMapGivenVsReceived(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeTemplateConfigMapGoldenContext(t)

	_, receivedUnfiltered, err := Template(TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		AppId:               "target.platform.beta",
	})
	require.NoError(t, err)

	_, receivedFiltered, err := Template(TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`kind == "ConfigMap" && name == "mini-blocks"`},
		},
		AppId: "target.platform.beta",
	})
	require.NoError(t, err)

	assert.Equal(t, givenUnfilteredTemplateStdout, receivedUnfiltered)
	assert.Equal(t, givenFilteredMiniBlocksStdout, receivedFiltered)
}

func TestTemplateSortedRenderedEntities_betaResourceCount(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeTemplateConfigMapGoldenContext(t)

	base := TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		AppId:               "target.platform.beta",
	}

	ents, err := TemplateSortedRenderedEntities(base)
	require.NoError(t, err)
	assert.Len(t, ents.Items, 2)

	filtered := base
	filtered.PredicatesFlag = flags.PredicatesFlag{
		Predicates: []types.CelPredicate{`kind == "ConfigMap" && name == "mini-blocks"`},
	}
	entsFiltered, err := TemplateSortedRenderedEntities(filtered)
	require.NoError(t, err)
	assert.Len(t, entsFiltered.Items, 1)
}

// givenUnfilteredTemplateStdout is the exact unfiltered stdout for:
// hydra local template target.platform.beta (sorted by resource id: Deployment before ConfigMap).
const givenUnfilteredTemplateStdout = `apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    argocd.argoproj.io/tracking-id: target.platform.beta:apps/Deployment:beta-ns/beta-api
  name: beta-api
  namespace: beta-ns
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
      - image: nginx:1.27
        name: beta-api
---
apiVersion: v1
data:
  pipe.txt: |
    a
    b
  strip.txt: |
    c
    d
kind: ConfigMap
metadata:
  annotations:
    argocd.argoproj.io/tracking-id: target.platform.beta:/ConfigMap:beta-ns/mini-blocks
  name: mini-blocks
  namespace: beta-ns
`

// givenFilteredMiniBlocksStdout is the exact stdout for:
// hydra local template target.platform.beta --include 'kind == "ConfigMap" && name == "mini-blocks"'
const givenFilteredMiniBlocksStdout = `apiVersion: v1
data:
  pipe.txt: |
    a
    b
  strip.txt: |
    c
    d
kind: ConfigMap
metadata:
  annotations:
    argocd.argoproj.io/tracking-id: target.platform.beta:/ConfigMap:beta-ns/mini-blocks
  name: mini-blocks
  namespace: beta-ns
`
