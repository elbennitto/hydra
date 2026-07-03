package references

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"slices"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

var updateGolden = flag.Bool("update", false, "update golden files")

//go:embed all:testdata
var testdataFS embed.FS

type expectedFile struct {
	RefDefinitions []types.RefDefinition `yaml:"refDefinitions"`
	Refs           []types.Ref           `yaml:"refs"`
}

func TestFindRefsParameterized(t *testing.T) {
	// Find all .given.yaml files recursively
	var testCases []string
	err := fs.WalkDir(testdataFS, "testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if base, found := strings.CutSuffix(path, ".given.yaml"); found {
				testCases = append(testCases, base)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, testCases, "no test cases found in testdata")

	for _, tc := range testCases {
		// Create test name from path: testdata/kubernetes/v1/Pod/imagepullsecret -> kubernetes/v1/Pod/imagepullsecret
		testName := strings.TrimPrefix(tc, "testdata/")
		t.Run(testName, func(t *testing.T) {
			// Read given YAML
			givenBytes, err := testdataFS.ReadFile(tc + ".given.yaml")
			require.NoError(t, err)

			// Try to read optional parsers YAML
			var extraParsers []types.RefParser
			if parsersBytes, err := testdataFS.ReadFile(tc + ".parsers.yaml"); err == nil {
				extraParsers, err = ParseRefParsers(parsersBytes)
				require.NoError(t, err)
			}

			// Parse entities
			entities, err := entity.NewEntitiesFromYaml(
				log.Default(),
				types.YamlString(givenBytes),
				types.KeyClusterEntity,
			)
			require.NoError(t, err)

			// Run refDefinitions
			refDefsResult, err := RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, extraParsers)
			require.NoError(t, err)

			// Run Refs
			refsResult, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, extraParsers)
			require.NoError(t, err)
			refsResult = AnnotateRefsWithSource(refsResult, types.RefSourceTest)

			if *updateGolden {
				// Write expected file
				expected := expectedFile{
					RefDefinitions: refDefsResult,
					Refs:           refsResult,
				}
				expectedBytes, err := yaml.Marshal(expected)
				require.NoError(t, err)

				expectedPath := tc + ".expected.yaml"
				err = os.WriteFile(expectedPath, expectedBytes, 0644)
				require.NoError(t, err)
				t.Logf("Updated golden file: %s", expectedPath)
				return
			}

			// Read expected YAML
			expectedBytes, err := testdataFS.ReadFile(tc + ".expected.yaml")
			require.NoError(t, err)

			// Parse expected file
			var expected expectedFile
			err = yaml.Unmarshal(expectedBytes, &expected)
			require.NoError(t, err)

			// Assert refDefinitions
			assert.NotNil(t, refDefsResult)
			assert.ElementsMatch(t, expected.RefDefinitions, refDefsResult, "refDefinitions mismatch")

			// Assert refs
			assert.ElementsMatch(t, expected.Refs, refsResult, "refs mismatch")
		})
	}
}

func TestParseRefParsersNormalizesSelectorInputs(t *testing.T) {
	parsers, err := ParseRefParsers([]byte(`ref-parsers:
  - id: apps/v1/Deployment/demo/api
    pick:
      - cel: "[refBuilder().outgoing(id('v1/Secret', ns, 'x'))]"
  - apiVersion: apps/v1
    kind: Deployment
    namespace: demo
    name: worker
    pick:
      - cel: "[refBuilder().outgoing(id('v1/Secret', ns, 'y'))]"
  - gvkn: apps/v1/StatefulSet/demo
    name: db
    pick:
      - cel: "[refBuilder().outgoing(id('v1/Secret', ns, 'z'))]"
  - predicate: 'gvk == "v1/ConfigMap"'
    pick:
      - cel: "[refBuilder().outgoing(id('v1/Secret', ns, 'legacy'))]"
`))
	require.NoError(t, err)
	require.Len(t, parsers, 4)

	assert.Equal(t, types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "demo", Name: "api"}, parsers[0].Selector)
	assert.Empty(t, parsers[0].Cel)

	assert.Equal(t, types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "demo", Name: "worker"}, parsers[1].Selector)
	assert.Empty(t, parsers[1].Cel)

	assert.Equal(t, types.RefSelector{Group: "apps", Version: "v1", Kind: "StatefulSet", Namespace: "demo", Name: "db"}, parsers[2].Selector)
	assert.Empty(t, parsers[2].Cel)

	assert.Equal(t, types.CelPredicate(`gvk == "v1/ConfigMap"`), parsers[3].Cel)
	assert.True(t, parsers[3].Selector.IsZero())
}

func TestParseRefParsersRejectsConflictingSelectorInputs(t *testing.T) {
	_, err := ParseRefParsers([]byte(`ref-parsers:
  - gvk: apps/v1/Deployment
    kind: StatefulSet
    pick:
      - cel: "[refBuilder().outgoing(id('v1/Secret', ns, 'x'))]"
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ref-parsers[0]")
	assert.Contains(t, err.Error(), "conflicts")
}

func TestRefDefinitionsSelectorOnlyParserMatchesWithoutCel(t *testing.T) {
	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
`),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	refDefs, err := RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, []types.RefParser{{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "demo", Name: "api"},
		Pick: []types.RefPicker{{
			Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'target-secret'))]`),
		}},
	}})
	require.NoError(t, err)
	assert.Contains(t, refDefs, types.RefDefinition{
		Owner:     "apps/v1/Deployment/demo/api",
		Type:      types.RefTypeDirect,
		Direction: types.RefDirectionOutgoing,
		Endpoint:  types.RefEndpoint{Type: types.RefEndpointTypeId, Value: "v1/Secret/demo/target-secret"},
	})
}

func TestRefDefinitionsSelectorMismatchSkipsCelEvaluation(t *testing.T) {
	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
`),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	_, err = RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, []types.RefParser{{
		Selector: types.RefSelector{Version: "v1", Kind: "Secret", Namespace: "demo", Name: "api"},
		Cel:      types.CelPredicate(`entity.spec.strategy.type == 'RollingUpdate'`),
		Pick: []types.RefPicker{{
			Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'target-secret'))]`),
		}},
	}})
	require.NoError(t, err)
}

func TestRefDefinitionsSelectorMatchStillRespectsCelResult(t *testing.T) {
	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
`),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	refDefs, err := RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, []types.RefParser{{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "demo", Name: "api"},
		Cel:      types.CelPredicate(`name == 'other'`),
		Pick: []types.RefPicker{{
			Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'target-secret'))]`),
		}},
	}})
	require.NoError(t, err)
	for _, refDef := range refDefs {
		assert.NotEqual(t, types.RefEndpoint{Type: types.RefEndpointTypeId, Value: "v1/Secret/demo/target-secret"}, refDef.Endpoint)
	}
}

// Pod and PodMetrics refs are defined in ref-parsers/metrics.k8s.io_v1beta1/PodMetrics.yaml (v1/Pod
// and metrics.k8s.io/v1beta1/PodMetrics predicates). The same edges feed ResourceModel ownership
// and merged inspect ref graphs.
func TestRefsPodToPodMetrics(t *testing.T) {
	doc := `apiVersion: v1
kind: Pod
metadata:
  name: local-path-provisioner-6bc6568469-cj589
  namespace: kube-system
spec:
  containers:
    - name: c
      image: busybox
---
apiVersion: metrics.k8s.io/v1beta1
kind: PodMetrics
metadata:
  name: local-path-provisioner-6bc6568469-cj589
  namespace: kube-system
timestamp: "2021-01-01T00:00:00Z"
window: 30s
containers: []
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(doc), types.KeyClusterEntity)
	require.NoError(t, err)
	out, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil)
	require.NoError(t, err)
	podID := types.Id("v1/Pod/kube-system/local-path-provisioner-6bc6568469-cj589")
	pmID := types.Id("metrics.k8s.io/v1beta1/PodMetrics/kube-system/local-path-provisioner-6bc6568469-cj589")
	var podToPM *types.Ref
	var pmToPod *types.Ref
	for i := range out {
		if out[i].From == podID && out[i].To == pmID {
			podToPM = &out[i]
		}
		if out[i].From == pmID && out[i].To == podID {
			pmToPod = &out[i]
		}
	}
	require.NotNil(t, podToPM)
	assert.True(t, slices.Contains(podToPM.Labels, "podMetrics"))
	require.NotNil(t, pmToPod)
	assert.True(t, slices.Contains(pmToPod.Labels, "podMetricsPod"))
}

func TestRefsTemplateDeploymentToClusterPodMetricsWithOverlay(t *testing.T) {
	deployDoc := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: local-path-provisioner
  namespace: kube-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: local-path
  template:
    metadata:
      labels:
        app: local-path
    spec:
      containers:
        - name: c
          image: busybox
`
	podMetricsDoc := `apiVersion: metrics.k8s.io/v1beta1
kind: PodMetrics
metadata:
  name: local-path-provisioner-6bc6568469-chlbm
  namespace: kube-system
timestamp: "2021-01-01T00:00:00Z"
window: 30s
containers: []
`
	templateEnts, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(deployDoc), types.KeyTemplateEntity)
	require.NoError(t, err)
	overlayEnts, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(podMetricsDoc), types.KeyClusterEntity)
	require.NoError(t, err)

	out, err := Refs(log.Default(), templateEnts, types.KeyTemplateEntity, nil, entity.Entities{}, overlayEnts, nil)
	require.NoError(t, err)
	deployID := types.Id("apps/v1/Deployment/kube-system/local-path-provisioner")
	pmID := types.Id("metrics.k8s.io/v1beta1/PodMetrics/kube-system/local-path-provisioner-6bc6568469-chlbm")
	var edge *types.Ref
	for i := range out {
		if out[i].From == deployID && out[i].To == pmID {
			edge = &out[i]
			break
		}
	}
	require.NotNil(t, edge, "expected template Deployment -> cluster PodMetrics edge via overlay")
	assert.True(t, slices.Contains(edge.Labels, "podMetrics"))
}

func TestRefsEventToRelatedSubject(t *testing.T) {
	doc := `apiVersion: v1
kind: Secret
metadata:
  name: image-pull-secret
  namespace: sops-secrets-operator
type: kubernetes.io/dockerconfigjson
data: {}
---
apiVersion: events.k8s.io/v1
kind: Event
metadata:
  name: policy-event
  namespace: sops-secrets-operator
related:
  apiVersion: v1
  kind: Secret
  name: image-pull-secret
  namespace: sops-secrets-operator
type: Normal
reason: Synced
action: Sync
note: ""
reportingController: ""
reportingInstance: ""
eventTime: "2021-01-01T00:00:00.000000Z"
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(doc), types.KeyClusterEntity)
	require.NoError(t, err)
	out, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil)
	require.NoError(t, err)
	eventID := types.Id("events.k8s.io/v1/Event/sops-secrets-operator/policy-event")
	secretID := types.Id("v1/Secret/sops-secrets-operator/image-pull-secret")
	var edge *types.Ref
	for i := range out {
		if out[i].From == eventID && out[i].To == secretID {
			edge = &out[i]
			break
		}
	}
	require.NotNil(t, edge, "expected Event -> related object edge")
	assert.True(t, slices.Contains(edge.Labels, "related"))
}

func TestRefsEventToRegardingSubject(t *testing.T) {
	doc := `apiVersion: v1
kind: Pod
metadata:
  name: metrics-server
  namespace: kube-system
spec:
  containers:
    - name: c
      image: x
---
apiVersion: events.k8s.io/v1
kind: Event
metadata:
  name: metrics-server-abc123
  namespace: kube-system
regarding:
  apiVersion: v1
  kind: Pod
  name: metrics-server
  namespace: kube-system
type: Normal
reason: Scheduled
action: Scheduled
note: ""
reportingController: ""
reportingInstance: ""
eventTime: "2021-01-01T00:00:00.000000Z"
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(doc), types.KeyClusterEntity)
	require.NoError(t, err)
	out, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil)
	require.NoError(t, err)
	eventID := types.Id("events.k8s.io/v1/Event/kube-system/metrics-server-abc123")
	podID := types.Id("v1/Pod/kube-system/metrics-server")
	var edge *types.Ref
	for i := range out {
		if out[i].From == eventID && out[i].To == podID {
			edge = &out[i]
			break
		}
	}
	require.NotNil(t, edge, "expected Event -> regarding object edge")
	assert.Equal(t, types.RefTypeRegarding, edge.RefType)
	assert.True(t, slices.Contains(edge.Labels, "regarding"))
}

type collectHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *collectHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *collectHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *collectHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *collectHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *collectHandler) warnings() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var result []slog.Record
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			result = append(result, r)
		}
	}
	return result
}

func TestRefsConflictingDescWarning(t *testing.T) {
	handler := &collectHandler{}
	l := log.NewLoggerWithHandler(handler)

	givenYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deploy
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: my-sa
---
apiVersion: v1
kind: Secret
metadata:
  name: target-secret
  namespace: default
`

	entities, err := entity.NewEntitiesFromYaml(
		l,
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	parser1 := types.RefParser{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment"},
		Pick: []types.RefPicker{{
			Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'target-secret')).desc('first description')]`),
		}},
	}
	parser2 := types.RefParser{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment"},
		Pick: []types.RefPicker{{
			Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'target-secret')).desc('second description')]`),
		}},
	}

	refs, err := Refs(l, entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, []types.RefParser{parser1, parser2})
	require.NoError(t, err)

	// The edge should exist with the first desc kept
	var found bool
	for _, ref := range refs {
		if strings.Contains(string(ref.From), "Deployment") && strings.Contains(string(ref.To), "target-secret") {
			assert.Equal(t, "first description", ref.Desc, "first desc should be kept")
			found = true
		}
	}
	assert.True(t, found, "expected ref from Deployment to target-secret")

	warnings := handler.warnings()
	var foundWarning bool
	for _, w := range warnings {
		if strings.Contains(w.Message, "Discarding conflicting desc") {
			foundWarning = true
		}
	}
	assert.True(t, foundWarning, "expected a warning about conflicting desc")
}

func TestRefsMergeKeyAttributesFromBuilder(t *testing.T) {
	givenYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: demo
data:
  LOG_LEVEL: info
  SPRING_PROFILE: prod
`

	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	parser := types.RefParser{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment"},
		Pick: []types.RefPicker{
			{Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/ConfigMap', ns, 'app-config')).key('SPRING_PROFILE')]`), Label: "env"},
			{Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/ConfigMap', ns, 'app-config')).attribute('key', 'LOG_LEVEL')]`), Label: "env"},
			{Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/ConfigMap', ns, 'app-config')).key('SPRING_PROFILE')]`), Label: "env"},
		},
	}

	refDefs, err := RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, []types.RefParser{parser})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(refDefs), 3)

	refs, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, []types.RefParser{parser})
	require.NoError(t, err)
	refs = AnnotateRefsWithSource(refs, types.RefSourceTest)

	var mergedRef *types.Ref
	for i := range refs {
		if refs[i].From == "apps/v1/Deployment/demo/api" && refs[i].To == "v1/ConfigMap/demo/app-config" {
			mergedRef = &refs[i]
			break
		}
	}
	require.NotNil(t, mergedRef)
	assert.ElementsMatch(t, []types.RefAttribute{
		{Type: "key", Value: "LOG_LEVEL"},
		{Type: "key", Value: "SPRING_PROFILE"},
		{Type: types.RefAttributeOriginSource, Value: types.RefSourceTest},
	}, mergedRef.Attributes)
}

func TestRefDefinitionsPickerExprErrorFields(t *testing.T) {
	givenYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
`
	badPick := `refBuilder().outgoing(id('v1/ConfigMap', ns, 'app-config'))`
	parser := types.RefParser{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment"},
		Pick:     []types.RefPicker{{Cel: types.CelExpression(badPick)}},
	}

	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	_, err = RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, []types.RefParser{parser})
	require.Error(t, err)
	var pe *PickerExprError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, badPick, pe.Expr)
	assert.Equal(t, "list", pe.Expected)
	assert.Contains(t, pe.GotType, "CelRef")
}

func TestRefsLogsPickerExprError(t *testing.T) {
	givenYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
`
	badPick := `refBuilder().outgoing(id('v1/ConfigMap', ns, 'app-config'))`
	parser := types.RefParser{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment"},
		Pick:     []types.RefPicker{{Cel: types.CelExpression(badPick)}},
	}

	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	var buf strings.Builder
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})
	l := log.NewLoggerWithHandler(h)

	_, err = Refs(l, entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, []types.RefParser{parser})
	require.Error(t, err)
	assert.Contains(t, buf.String(), `"expr"`)
	assert.Contains(t, buf.String(), badPick)
	assert.Contains(t, buf.String(), `"expected":"list"`)
}

func TestRefDefinitionsPickerExprErrorFields_ForInvalidItemType(t *testing.T) {
	givenYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
`
	badPick := `[true]`
	parser := types.RefParser{
		Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment"},
		Pick:     []types.RefPicker{{Cel: types.CelExpression(badPick)}},
	}

	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	_, err = RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, []types.RefParser{parser})
	require.Error(t, err)
	var pe *PickerExprError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, badPick, pe.Expr)
	assert.Equal(t, "[]RefDefinition", pe.Expected)
	assert.Contains(t, pe.GotType, "Bool")
}

func TestRefsRecursivelyExpandProvisionedTargetsAndKeepParserProvenance(t *testing.T) {
	givenYaml := `
apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: image-pull-secret
  namespace: sops-secrets-operator
spec:
  secretTemplates:
    - name: image-pull-secret
      stringData:
        .dockerconfigjson: "{}"
`

	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	parsers := []types.RefParser{
		{
			Selector: types.RefSelector{Group: "isindir.github.com", Version: "v1alpha3", Kind: "SopsSecret"},
			Pick: []types.RefPicker{{
				Cel:     types.CelExpression(`entity.spec.secretTemplates.map(t, refBuilder().outgoing(id('v1/Secret', ns, t.name)))`),
				Label:   "sops",
				Reverse: true,
			}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.cluster-infra.sops-secrets-operator"},
				{Type: types.RefAttributeOriginWorkload, Value: "sops-secrets-operator"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
		{
			Selector: types.RefSelector{Version: "v1", Kind: "Secret", Namespace: "sops-secrets-operator", Name: "image-pull-secret"},
			Pick: []types.RefPicker{{
				Cel:   types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', 'demo', 'image-pull-secret'))]`),
				Label: "clone-target",
			}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.cluster-infra.kyverno"},
				{Type: types.RefAttributeOriginWorkload, Value: "kyverno-admission-controller"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}

	refsResult, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, parsers)
	require.NoError(t, err)
	refsResult = AnnotateRefsWithSource(refsResult, types.RefSourceTest)

	assert.Contains(t, refsResult, types.Ref{
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		From:         "isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret",
		To:           "v1/Secret/sops-secrets-operator/image-pull-secret",
		Labels:       []string{"sops"},
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginApp, Value: "prod.cluster-infra.sops-secrets-operator"},
			{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			{Type: types.RefAttributeOriginSource, Value: types.RefSourceTest},
			{Type: types.RefAttributeOriginWorkload, Value: "sops-secrets-operator"},
		},
		Reverse: true,
	})
	assert.Contains(t, refsResult, types.Ref{
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		From:         "v1/Secret/sops-secrets-operator/image-pull-secret",
		To:           "v1/Secret/demo/image-pull-secret",
		Labels:       []string{"clone-target"},
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginApp, Value: "prod.cluster-infra.kyverno"},
			{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			{Type: types.RefAttributeOriginSource, Value: types.RefSourceTest},
			{Type: types.RefAttributeOriginWorkload, Value: "kyverno-admission-controller"},
		},
	})
}

func TestRefsRecursiveProvisionedExpansionStopsOnCycle(t *testing.T) {
	givenYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cycle-root
  namespace: demo
data:
  ok: "1"
`

	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	parsers := []types.RefParser{
		{
			Selector: types.RefSelector{Version: "v1", Kind: "ConfigMap", Name: "cycle-root"},
			Pick:     []types.RefPicker{{Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'first'))]`)}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.platform.root"},
				{Type: types.RefAttributeOriginWorkload, Value: "root-controller"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
		{
			Selector: types.RefSelector{Version: "v1", Kind: "Secret", Name: "first"},
			Pick:     []types.RefPicker{{Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'second'))]`)}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.platform.first"},
				{Type: types.RefAttributeOriginWorkload, Value: "first-controller"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
		{
			Selector: types.RefSelector{Version: "v1", Kind: "Secret", Name: "second"},
			Pick:     []types.RefPicker{{Cel: types.CelExpression(`[refBuilder().outgoing(id('v1/Secret', ns, 'first'))]`)}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.platform.second"},
				{Type: types.RefAttributeOriginWorkload, Value: "second-controller"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}

	refsResult, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, parsers)
	require.NoError(t, err)

	firstToSecond := 0
	secondToFirst := 0
	for _, ref := range refsResult {
		if ref.From == "v1/Secret/demo/first" && ref.To == "v1/Secret/demo/second" {
			firstToSecond++
		}
		if ref.From == "v1/Secret/demo/second" && ref.To == "v1/Secret/demo/first" {
			secondToFirst++
		}
	}
	assert.Equal(t, 1, firstToSecond)
	assert.Equal(t, 1, secondToFirst)
}

func TestRefsAddsNamespaceLabelForNamespacedEntitiesIncludingStrimziPodSet(t *testing.T) {
	givenYaml := `
apiVersion: v1
kind: Namespace
metadata:
  name: demo
---
apiVersion: v1
kind: Pod
metadata:
  name: broker-0
  namespace: demo
spec:
  containers:
    - name: broker
      image: nginx:1.27
---
apiVersion: core.strimzi.io/v1beta2
kind: StrimziPodSet
metadata:
  name: demo-kafka-demo-kafka-broker
  namespace: demo
spec: {}
`

	entities, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(givenYaml),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	refsResult, err := Refs(log.Default(), entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil)
	require.NoError(t, err)

	expected := map[types.Id]types.Ref{
		types.Id("v1/Pod/demo/broker-0"): {
			RefType:      types.RefTypeDirect,
			EndpointType: types.RefEndpointTypeId,
			From:         types.Id("v1/Pod/demo/broker-0"),
			To:           types.Id("v1/Namespace//demo"),
			Labels:       []string{types.RefLabelNamespace},
		},
		types.Id("core.strimzi.io/v1beta2/StrimziPodSet/demo/demo-kafka-demo-kafka-broker"): {
			RefType:      types.RefTypeDirect,
			EndpointType: types.RefEndpointTypeId,
			From:         types.Id("core.strimzi.io/v1beta2/StrimziPodSet/demo/demo-kafka-demo-kafka-broker"),
			To:           types.Id("v1/Namespace//demo"),
			Labels:       []string{types.RefLabelNamespace},
		},
	}

	for from, want := range expected {
		var got *types.Ref
		for i := range refsResult {
			if refsResult[i].From == from && refsResult[i].To == want.To {
				got = &refsResult[i]
				break
			}
		}
		require.NotNilf(t, got, "expected namespace ref for %s", from)
		assert.Equal(t, want, *got)
	}
}

func TestNormalizeRefDefinitionEndpoints_rewritesIdEndpointVersion(t *testing.T) {
	pv := map[types.GroupKindKey]types.Version{
		types.NewGroupKindKey("kafka.strimzi.io", "KafkaTopic"): "v1",
	}
	defs := []types.RefDefinition{
		{
			Owner:     "apps/v1/Deployment/ns/app",
			Direction: types.RefDirectionOutgoing,
			Endpoint: types.RefEndpoint{
				Type:  types.RefEndpointTypeId,
				Value: "kafka.strimzi.io/v1beta2/KafkaTopic/demo/my-topic",
			},
		},
	}
	out := normalizeRefDefinitionEndpoints(defs, pv)
	require.Len(t, out, 1)
	assert.Equal(t, "kafka.strimzi.io/v1/KafkaTopic/demo/my-topic", out[0].Endpoint.Value)
}

func TestNormalizeRefDefinitionEndpoints_skipsNonIdEndpoints(t *testing.T) {
	pv := map[types.GroupKindKey]types.Version{
		types.NewGroupKindKey("kafka.strimzi.io", "KafkaTopic"): "v1",
	}
	defs := []types.RefDefinition{
		{
			Owner:     "apps/v1/Deployment/ns/app",
			Direction: types.RefDirectionOutgoing,
			Endpoint: types.RefEndpoint{
				Type:  types.RefEndpointTypeProvider,
				Value: "some-provider",
			},
		},
	}
	out := normalizeRefDefinitionEndpoints(defs, pv)
	assert.Equal(t, defs[0].Endpoint, out[0].Endpoint)
}

func TestNormalizeRefDefinitionEndpoints_emptyPreferredMapNoOp(t *testing.T) {
	defs := []types.RefDefinition{
		{
			Endpoint: types.RefEndpoint{Type: types.RefEndpointTypeId, Value: "a/b/c/d/e"},
		},
	}
	out := normalizeRefDefinitionEndpoints(defs, nil)
	assert.Equal(t, defs, out)
}
