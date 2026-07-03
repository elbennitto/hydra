package view

import (
	"embed"
	"flag"
	"io/fs"
	"os"
	"strings"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

var updateGolden = flag.Bool("update", false, "update golden files")

//go:embed testdata
var testdataFS embed.FS

func TestToModelWithCharts(t *testing.T) {
	givenYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  namespace: default
data:
  key: value
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(givenYaml), types.KeyTemplateEntity)
	require.NoError(t, err)

	charts := []ChartModel{
		{
			AppId:      "prod.myapp.child1",
			Name:       "child1",
			Version:    "1.2.3",
			AppVersion: "4.5.6",
			Dependencies: []ChartDependencyModel{
				{Name: "dep-a", Version: "0.1.0", Repository: "https://example.com"},
				{Name: "dep-b", Version: "0.2.0", Repository: "file://./charts/dep-b"},
			},
		},
		{
			AppId:   "prod.myapp.child2",
			Name:    "child2",
			Version: "2.0.0",
		},
	}

	model, err := ToModel(log.Default(), entities, charts)
	require.NoError(t, err)

	require.Len(t, model.Charts, 2)
	assert.Equal(t, types.AppId("prod.myapp.child1"), model.Charts[0].AppId)
	assert.Equal(t, "child1", model.Charts[0].Name)
	assert.Equal(t, "1.2.3", model.Charts[0].Version)
	assert.Equal(t, "4.5.6", model.Charts[0].AppVersion)
	require.Len(t, model.Charts[0].Dependencies, 2)
	assert.Equal(t, "dep-a", model.Charts[0].Dependencies[0].Name)
	assert.Equal(t, "https://example.com", model.Charts[0].Dependencies[0].Repository)

	assert.Equal(t, types.AppId("prod.myapp.child2"), model.Charts[1].AppId)
	assert.Equal(t, "child2", model.Charts[1].Name)
	assert.Nil(t, model.Charts[1].Dependencies)
}

func TestToModelWithParsers_EntityInventoryCelFunctions(t *testing.T) {
	// Ref-parsers may use managedNamespaces() / templateEntities() (e.g. Kyverno clone refs). RefDefinitions
	// registers ClusterInventorySupport so these compile without extra env options.
	givenYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  namespace: apps
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(givenYaml), types.KeyTemplateEntity)
	require.NoError(t, err)

	extraParsers := []types.RefParser{
		{
			Cel: types.CelPredicate(`gvk == "v1/ConfigMap" && size(managedNamespaces()) >= 0`),
		},
	}

	_, err = ToModelWithParsers(log.Default(), entities, extraParsers)
	require.NoError(t, err, "CEL must register entity inventory helpers when building the dependency model")
}

func TestToModelWithParsers_RecursiveProvisionedTargetsUseParserProvenance(t *testing.T) {
	givenYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer
  template:
    metadata:
      labels:
        app: consumer
    spec:
      imagePullSecrets:
        - name: image-pull-secret
      containers:
        - name: api
          image: nginx:1.27
---
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
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(givenYaml), types.KeyTemplateEntity)
	require.NoError(t, err)

	extraParsers := []types.RefParser{
		{
			Cel: types.CelPredicate(`gvk == "isindir.github.com/v1alpha3/SopsSecret" && name == "image-pull-secret" && ns == "sops-secrets-operator"`),
			Pick: []types.RefPicker{{
				Cel:     types.CelExpression(`entity.spec.secretTemplates.map(t, refBuilder().outgoing(id("v1/Secret", ns, t.name)))`),
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
			Cel: types.CelPredicate(`gvk == "v1/Secret" && name == "image-pull-secret" && ns == "sops-secrets-operator"`),
			Pick: []types.RefPicker{{
				Cel:   types.CelExpression(`[refBuilder().outgoing(id("v1/Secret", "demo", "image-pull-secret"))]`),
				Label: "clone-target",
			}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.cluster-infra.kyverno"},
				{Type: types.RefAttributeOriginWorkload, Value: "kyverno-admission-controller"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}

	model, err := ToModelWithParsers(log.Default(), entities, extraParsers)
	require.NoError(t, err)

	byID := map[types.Id]IdModel{}
	for _, item := range model.Entities {
		byID[item.Id] = item
	}

	assert.Contains(t, byID, types.Id("v1/Secret/sops-secrets-operator/image-pull-secret"))
	assert.Contains(t, byID, types.Id("v1/Secret/demo/image-pull-secret"))
	assert.ElementsMatch(t,
		[]string{"app:prod.cluster-infra.sops-secrets-operator", "controller:sops-secrets-operator"},
		byID["v1/Secret/sops-secrets-operator/image-pull-secret"].Tags,
	)
	assert.ElementsMatch(t,
		[]string{"app:prod.cluster-infra.kyverno", "controller:kyverno-admission-controller"},
		byID["v1/Secret/demo/image-pull-secret"].Tags,
	)
}

func TestToModelWithoutCharts(t *testing.T) {
	givenYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  namespace: default
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(givenYaml), types.KeyTemplateEntity)
	require.NoError(t, err)

	model, err := ToModel(log.Default(), entities)
	require.NoError(t, err)
	assert.Nil(t, model.Charts)
}

func TestRenderDependenciesWithCharts(t *testing.T) {
	givenYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
  namespace: default
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(givenYaml), types.KeyTemplateEntity)
	require.NoError(t, err)

	charts := []ChartModel{
		{
			AppId:   "prod.myapp",
			Name:    "myapp",
			Version: "1.0.0",
		},
	}

	var buf strings.Builder
	err = RenderDependencies(log.Default(), &buf, entities, charts)
	require.NoError(t, err)

	var result DependenciesModel
	err = yaml.Unmarshal([]byte(buf.String()), &result)
	require.NoError(t, err)

	require.Len(t, result.Charts, 1)
	assert.Equal(t, "myapp", result.Charts[0].Name)
	assert.Equal(t, "1.0.0", result.Charts[0].Version)
}

func TestDependenciesModelParameterized(t *testing.T) {
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
		// Create test name from path: testdata/kubernetes/simple -> kubernetes/simple
		testName := strings.TrimPrefix(tc, "testdata/")
		t.Run(testName, func(t *testing.T) {
			// Read given YAML
			givenBytes, err := testdataFS.ReadFile(tc + ".given.yaml")
			require.NoError(t, err)

			// Try to read optional parsers YAML
			var extraParsers []types.RefParser
			if parsersBytes, pErr := testdataFS.ReadFile(tc + ".parsers.yaml"); pErr == nil {
				extraParsers, err = references.ParseRefParsers(parsersBytes)
				require.NoError(t, err)
			}

			// Parse entities
			entities, err := entity.NewEntitiesFromYaml(
				log.Default(),
				types.YamlString(givenBytes),
				types.KeyTemplateEntity,
			)
			require.NoError(t, err)

			model, err := ToModelWithParsers(log.Default(), entities, extraParsers)
			require.NoError(t, err)

			if *updateGolden {
				expectedBytes, err := yaml.Marshal(model)
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
			var expected DependenciesModel
			err = yaml.Unmarshal(expectedBytes, &expected)
			require.NoError(t, err)

			assert.Equal(t, expected.Entities, model.Entities, "entities mismatch")
			assert.Equal(t, expected.Groups, model.Groups, "groups mismatch")
			assert.Equal(t, expected.References, model.References, "references mismatch")
			assert.Equal(t, expected.Charts, model.Charts, "charts mismatch")
		})
	}
}
