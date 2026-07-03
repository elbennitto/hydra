package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/view"
	"k8s.io/apimachinery/pkg/util/sets"
)

// --- helpers ---

func mustBuildView(b entity.EntityBuilder) entity.Entity {
	e, err := b.Build()
	if err != nil {
		panic(err)
	}
	return e
}

func makeEntityWithAppIds(group, version, kind, namespace, name string, appIds ...types.AppId) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	b = b.WithAppIds(appIds)
	return mustBuildView(b)
}

func emptyRenderResult() clusterRenderResult {
	return clusterRenderResult{
		entities:        entity.Entities{},
		chartModels:     nil,
		chartObjects:    nil,
		manifests:       nil,
		valueFiles:      nil,
		appValuesModels: nil,
		appValuesData:   nil,
		fallbackModels:  nil,
		fallbackData:    nil,
	}
}

// --- splitRenderResult ---

func TestSplitRenderResult_SeparatesRootAndChildEntities(t *testing.T) {
	rootEntity := makeEntityWithAppIds("argoproj.io", "v1alpha1", "Application", "argocd", "dev.demo.service-auth", "dev.demo")
	childEntity := makeEntityWithAppIds("apps", "v1", "Deployment", "service-auth", "service-auth", "dev.demo.service-auth")

	entities, err := entity.NewEntities([]entity.Entity{rootEntity, childEntity})
	require.NoError(t, err)

	allAppIds := sets.New[types.AppId]("dev.demo", "dev.demo.service-auth")

	result := emptyRenderResult()
	result.entities = entities
	result.chartModels = []view.ChartModel{
		{AppId: "dev.demo", Name: "demo"},
		{AppId: "dev.demo.service-auth", Name: "service-auth"},
	}
	result.manifests = map[string][]byte{
		"dev.demo/argoproj.io/v1alpha1/Application/argocd/dev.demo.service-auth.yaml": []byte("app-manifest"),
		"dev.demo.service-auth/apps/v1/Deployment/service-auth/service-auth.yaml":     []byte("deploy-manifest"),
	}
	result.appValuesModels = []view.AppValuesModel{
		{AppId: "dev.demo"},
		{AppId: "dev.demo.service-auth"},
	}
	result.appValuesData = map[types.AppId]types.ValuesMap{
		"dev.demo":              {"key": "root-val"},
		"dev.demo.service-auth": {"key": "child-val"},
	}

	rootResult, childResult, err := splitRenderResult(result, allAppIds)
	require.NoError(t, err)

	assert.Equal(t, 1, rootResult.entities.Len(), "root result should have 1 entity")
	assert.Equal(t, 1, childResult.entities.Len(), "child result should have 1 entity")

	assert.Len(t, rootResult.chartModels, 1)
	assert.Equal(t, "demo", rootResult.chartModels[0].Name)
	assert.Len(t, childResult.chartModels, 1)
	assert.Equal(t, "service-auth", childResult.chartModels[0].Name)

	assert.Len(t, rootResult.manifests, 1)
	assert.Len(t, childResult.manifests, 1)

	assert.Len(t, rootResult.appValuesData, 1)
	assert.Contains(t, rootResult.appValuesData, types.AppId("dev.demo"))
	assert.Len(t, childResult.appValuesData, 1)
	assert.Contains(t, childResult.appValuesData, types.AppId("dev.demo.service-auth"))
}

func TestSplitRenderResult_AllChildApps(t *testing.T) {
	child1 := makeEntityWithAppIds("apps", "v1", "Deployment", "ns", "svc1", "dev.demo.svc1")
	child2 := makeEntityWithAppIds("apps", "v1", "Deployment", "ns", "svc2", "dev.demo.svc2")

	entities, err := entity.NewEntities([]entity.Entity{child1, child2})
	require.NoError(t, err)

	allAppIds := sets.New[types.AppId]("dev.demo.svc1", "dev.demo.svc2")

	result := emptyRenderResult()
	result.entities = entities

	rootResult, childResult, err := splitRenderResult(result, allAppIds)
	require.NoError(t, err)

	assert.Equal(t, 0, rootResult.entities.Len(), "no root entities expected")
	assert.Equal(t, 2, childResult.entities.Len(), "both entities are child entities")
}

func TestSplitRenderResult_AllRootApps(t *testing.T) {
	root1 := makeEntityWithAppIds("argoproj.io", "v1alpha1", "Application", "argocd", "app1", "dev.demo")
	root2 := makeEntityWithAppIds("argoproj.io", "v1alpha1", "Application", "argocd", "app2", "dev.infra")

	entities, err := entity.NewEntities([]entity.Entity{root1, root2})
	require.NoError(t, err)

	allAppIds := sets.New[types.AppId]("dev.demo", "dev.infra")

	result := emptyRenderResult()
	result.entities = entities

	rootResult, childResult, err := splitRenderResult(result, allAppIds)
	require.NoError(t, err)

	assert.Equal(t, 2, rootResult.entities.Len(), "both entities are root entities")
	assert.Equal(t, 0, childResult.entities.Len(), "no child entities expected")
}

func TestSplitRenderResult_EmptyEntities(t *testing.T) {
	result := emptyRenderResult()
	allAppIds := sets.New[types.AppId]()

	rootResult, childResult, err := splitRenderResult(result, allAppIds)
	require.NoError(t, err)

	assert.Equal(t, 0, rootResult.entities.Len())
	assert.Equal(t, 0, childResult.entities.Len())
}

// --- mergeRenderResults ---

func TestMergeRenderResults_MergesEntities(t *testing.T) {
	e1 := makeEntityWithAppIds("", "v1", "ConfigMap", "default", "cm1", "in-cluster.argocd")
	e2 := makeEntityWithAppIds("argoproj.io", "v1alpha1", "Application", "argocd", "dev.demo.svc", "dev.demo")

	base, err := entity.NewEntities([]entity.Entity{e1})
	require.NoError(t, err)
	extra, err := entity.NewEntities([]entity.Entity{e2})
	require.NoError(t, err)

	baseResult := emptyRenderResult()
	baseResult.entities = base

	extraResult := emptyRenderResult()
	extraResult.entities = extra

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Equal(t, 2, merged.entities.Len())
}

func TestMergeRenderResults_MergesChartModels(t *testing.T) {
	baseResult := emptyRenderResult()
	baseResult.chartModels = []view.ChartModel{{AppId: "in-cluster.argocd", Name: "argo-cd"}}

	extraResult := emptyRenderResult()
	extraResult.chartModels = []view.ChartModel{{AppId: "dev.demo", Name: "demo"}}

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Len(t, merged.chartModels, 2)
}

func TestMergeRenderResults_MergesManifests(t *testing.T) {
	baseResult := emptyRenderResult()
	baseResult.manifests = map[string][]byte{
		"path/a.yaml": []byte("a"),
	}

	extraResult := emptyRenderResult()
	extraResult.manifests = map[string][]byte{
		"path/b.yaml": []byte("b"),
	}

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Len(t, merged.manifests, 2)
	assert.Equal(t, []byte("a"), merged.manifests["path/a.yaml"])
	assert.Equal(t, []byte("b"), merged.manifests["path/b.yaml"])
}

func TestMergeRenderResults_MergesAppValues(t *testing.T) {
	baseResult := emptyRenderResult()
	baseResult.appValuesModels = []view.AppValuesModel{{AppId: "in-cluster.argocd"}}
	baseResult.appValuesData = map[types.AppId]types.ValuesMap{
		"in-cluster.argocd": {"key": "val"},
	}

	extraResult := emptyRenderResult()
	extraResult.appValuesModels = []view.AppValuesModel{{AppId: "dev.demo"}}
	extraResult.appValuesData = map[types.AppId]types.ValuesMap{
		"dev.demo": {"key": "val2"},
	}

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Len(t, merged.appValuesModels, 2)
	assert.Len(t, merged.appValuesData, 2)
}

func TestMergeRenderResults_MergesFallbackValues(t *testing.T) {
	baseResult := emptyRenderResult()
	baseResult.fallbackModels = []view.AppValuesModel{{AppId: "in-cluster.argocd"}}
	baseResult.fallbackData = map[types.AppId]types.ValuesMap{
		"in-cluster.argocd": {"fb": "1"},
	}

	extraResult := emptyRenderResult()
	extraResult.fallbackModels = []view.AppValuesModel{{AppId: "dev.demo"}}
	extraResult.fallbackData = map[types.AppId]types.ValuesMap{
		"dev.demo": {"fb": "2"},
	}

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Len(t, merged.fallbackModels, 2)
	assert.Len(t, merged.fallbackData, 2)
}

func TestMergeRenderResults_MergesValueFiles(t *testing.T) {
	baseResult := emptyRenderResult()
	baseResult.valueFiles = []view.ValueFileModel{{Path: "values.yaml", Type: "group"}}

	extraResult := emptyRenderResult()
	extraResult.valueFiles = []view.ValueFileModel{{Path: "dev/values.yaml", Type: "context"}}

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Len(t, merged.valueFiles, 2)
}

func TestMergeRenderResults_BaseNilMaps(t *testing.T) {
	baseResult := emptyRenderResult()

	e := makeEntityWithAppIds("argoproj.io", "v1alpha1", "Application", "argocd", "app1", "dev.demo")
	extraEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	extraResult := emptyRenderResult()
	extraResult.entities = extraEntities
	extraResult.manifests = map[string][]byte{"a.yaml": []byte("a")}
	extraResult.appValuesData = map[types.AppId]types.ValuesMap{"dev.demo": {"k": "v"}}
	extraResult.fallbackData = map[types.AppId]types.ValuesMap{"dev.demo": {"k": "v"}}

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Equal(t, 1, merged.entities.Len())
	assert.Len(t, merged.manifests, 1)
	assert.Len(t, merged.appValuesData, 1)
	assert.Len(t, merged.fallbackData, 1)
}

func TestMergeRenderResults_ExtraNilMaps(t *testing.T) {
	e := makeEntityWithAppIds("", "v1", "ConfigMap", "default", "cm1", "in-cluster.argocd")
	baseEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	baseResult := emptyRenderResult()
	baseResult.entities = baseEntities
	baseResult.manifests = map[string][]byte{"a.yaml": []byte("a")}

	extraResult := emptyRenderResult()

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Equal(t, 1, merged.entities.Len())
	assert.Len(t, merged.manifests, 1)
}

func TestMergeRenderResults_BothEmpty(t *testing.T) {
	baseResult := emptyRenderResult()
	extraResult := emptyRenderResult()

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Equal(t, 0, merged.entities.Len())
	assert.Nil(t, merged.chartModels)
	assert.Nil(t, merged.manifests)
	assert.Nil(t, merged.appValuesData)
	assert.Nil(t, merged.fallbackData)
}

func TestMergeRenderResults_MergesChartObjects(t *testing.T) {
	baseResult := emptyRenderResult()
	baseResult.chartObjects = map[string]*v2chart.Chart{
		"argo-cd": {Metadata: &v2chart.Metadata{Name: "argo-cd"}},
	}

	extraResult := emptyRenderResult()
	extraResult.chartObjects = map[string]*v2chart.Chart{
		"demo": {Metadata: &v2chart.Metadata{Name: "demo"}},
	}

	merged, err := mergeRenderResults(baseResult, extraResult)
	require.NoError(t, err)
	assert.Len(t, merged.chartObjects, 2)
	assert.Contains(t, merged.chartObjects, "argo-cd")
	assert.Contains(t, merged.chartObjects, "demo")
}
