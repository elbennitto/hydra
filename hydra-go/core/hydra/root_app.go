package hydra

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/goccy/go-yaml"
	"hydra-gitops.org/hydra/hydra-go/base/cache"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/utils"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"k8s.io/apimachinery/pkg/util/sets"
)

// RootApp represents a root application within a cluster.
// It combines a Cluster with a root application name to provide access to
// the application's chart and configuration.
type RootApp struct {
	*Cluster
	RootAppName types.RootAppName
}

var _ Hydra = (*RootApp)(nil)
var _ HydraApp = (*RootApp)(nil)

var rootAppCache *cache.Cache[rootAppCacheKey, *RootApp]

func initRootAppCache(l log.Logger) {
	if rootAppCache == nil {
		rootAppCache = cache.NewCache[rootAppCacheKey, *RootApp](l, "rootapp-cache", false, nil)
	}
}

type rootAppCacheKey struct {
	clusterCacheKey
	types.RootAppName
}

func (key rootAppCacheKey) String() string {
	return fmt.Sprintf("%s:%s", key.clusterCacheKey.String(), string(key.RootAppName))
}

func (a *RootApp) cacheKey() rootAppCacheKey {
	return rootAppCacheKey{
		clusterCacheKey: a.Cluster.cacheKey(),
		RootAppName:     a.RootAppName,
	}
}

func (a *RootApp) AsApp() HydraApp {
	return a.AsRootApp()
}

// RootAppPath returns the path to the root app directory as a ChartDirectory.
func (a *RootApp) RootAppPath() helm.ChartDirectory {
	return helm.NewChartDirectory(a.l, filepath.Join(a.ClusterPath(), string(a.RootAppName)))
}

// RootAppDependencyPath returns the path to the root app's single dependency.
// Root apps are expected to have exactly one dependency.
func (a *RootApp) RootAppDependencyPath() (string, error) {
	// load root app chart metadata first
	chartYamlPath := a.RootAppPath().ChartYamlPath()
	data, err := os.ReadFile(chartYamlPath)
	if err != nil {
		return "", log.CreateError(
			errors.ErrHydraConfigError,
			"could not read root app Chart.yaml for '{app}' from '{path}': {err}",
			log.String("app", string(a.RootAppId())),
			log.String("path", chartYamlPath),
			log.Err(err))
	}

	var meta v2chart.Metadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return "", log.CreateError(
			errors.ErrHydraConfigError,
			"could not parse root app Chart.yaml for '{app}': {err}",
			log.String("app", string(a.RootAppId())),
			log.Err(err))
	}

	dependencies := meta.Dependencies
	if len(dependencies) != 1 {
		return "", log.CreateError(
			errors.ErrHydraConfigError,
			"root app '{app}' require exactly one dependency, found {count}",
			log.String("app", string(a.RootAppId())),
			log.Int("count", len(dependencies)))
	}

	// Get the repository path
	repository := dependencies[0].Repository
	if repository == "" {
		return "", log.CreateError(
			errors.ErrHydraConfigError,
			"root app '{app}' dependency has empty repository",
			log.String("app", string(a.RootAppId())))
	}

	// Resolve relative path from charts directory
	parent := filepath.Join(a.RootAppPath().Path(), utils.FileUriToPath(repository))

	return parent, nil
}

// RootAppId returns the release name for this RootApp.
// Format: <cluster>.<rootApp>
func (a *RootApp) RootAppId() types.AppId {
	return types.NewRootAppId(a.ClusterName, a.RootAppName)
}

func (a *RootApp) AppId() types.AppId {
	return a.RootAppId()
}

// NewRootApp creates a new RootApp instance for the given cluster and root application name.
func NewRootApp(cluster *Cluster, rootAppName types.RootAppName) (*RootApp, error) {
	if rootAppName == types.ReservedPresetRootAppName {
		return nil, log.CreateError(
			errors.ErrHydraConfigError,
			"root app name '{name}' is reserved for synthetic cluster-defaults preset ownership ids ({cluster}.preset.<presetId>)",
			log.String("name", string(rootAppName)))
	}
	initRootAppCache(cluster.l)
	rootPath := filepath.Join(cluster.ClusterPath(), string(rootAppName))
	if err := validateLevelType(cluster.l, rootPath, hydraTypeRootApp, hydraTypeRootApp); err != nil {
		return nil, err
	}
	key := rootAppCacheKey{
		clusterCacheKey: cluster.cacheKey(),
		RootAppName:     rootAppName,
	}

	return rootAppCache.GetOrLoad(key, func() (*RootApp, error) {
		return &RootApp{
			Cluster:     cluster,
			RootAppName: rootAppName,
		}, nil
	})
}

// LoadValues loads and merges global values from all levels (context, cluster, and rootapp).
// Returns the fully resolved values for this root application.
func (a *RootApp) LoadValuesMap(networkMode types.HelmNetworkMode) (types.ValuesMap, error) {
	cacheKey := newValuesCacheKey(a.AppId(), networkMode)
	return a.caches.getOrLoadValues(cacheKey, func() (types.ValuesMap, error) {
		return a.loadValuesMap(networkMode)
	})
}

func (a *RootApp) loadValuesMap(networkMode types.HelmNetworkMode) (types.ValuesMap, error) {
	// load the root app charter
	charter, err := a.RootAppPath().LoadChart(a.caches.chartCache, networkMode)
	if err != nil {
		return nil, err
	}

	// load values from cluster level (which includes context level)
	clusterVals, err := a.Cluster.LoadValuesMap(networkMode)
	if err != nil {
		return nil, err
	}

	valuesMap, err := helm.LoadValuesMap(a.l, charter, clusterVals)
	if err != nil {
		return nil, err
	}
	valuesMap, err = ensureHydraTypeInValues(valuesMap, hydraTypeRootApp, hydraTypeRootApp)
	if err != nil {
		return nil, err
	}
	return valuesMap, nil
}

// coalescedHelmValuesMap returns umbrella values after chart Coalesce/dependencies/hydra-fallback,
// before ToRenderValues. Used when extracting child-app subtrees; must match the single coalesce
// Helm performs before subchart values are built.
func (a *RootApp) coalescedHelmValuesMap(networkMode types.HelmNetworkMode) (types.ValuesMap, error) {
	charter, err := a.RootAppPath().LoadChart(a.caches.chartCache, networkMode)
	if err != nil {
		return nil, err
	}
	clusterVals, err := a.Cluster.LoadValuesMap(networkMode)
	if err != nil {
		return nil, err
	}
	return helm.CoalescedValuesMapBeforeRender(a.l, charter, clusterVals)
}

// AsRootApp returns this RootApp.
func (a *RootApp) AsRootApp() *RootApp {
	return a
}

// WithApp processes the RootApp with the given app reference.
// If the app specifies a child app, it delegates to that ChildApp.
// Otherwise, it returns this RootApp after validation.
func (a *RootApp) WithApp(appId types.AppId) (HydraApp, error) {
	if err := validateRootApp(appId, a.ClusterName, a.RootAppName); err != nil {
		return nil, err
	}

	childAppName, err := appId.ChildAppName()
	if err != nil {
		return nil, err
	}
	if childAppName == nil {
		return a, nil
	}

	childApp, err := NewChildApp(a, *childAppName)
	if err != nil {
		return nil, err
	}
	return childApp.WithApp(appId)
}

const argocdNamespace = types.Namespace("argocd")

func (a *RootApp) Namespace(types.HelmNetworkMode) (types.Namespace, error) {
	return argocdNamespace, nil
}

// Template renders and returns the templates for this RootApp.
func (a *RootApp) Template(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
) (types.YamlString, error) {
	if _, ok, _ := a.Cluster.helmChartInputValues(a.AppId()); ok {
		return a.template(networkMode, kubernetesVersionOrFallback)
	}
	digest, err := helmChartTemplateValuesDigest(a.AsApp(), networkMode)
	if err != nil {
		return "", err
	}
	cacheKey := newTemplateCacheKey(a.AppId(), networkMode, kubernetesVersionOrFallback, false, digest)
	return a.caches.getOrLoadTemplates(cacheKey, func() (types.YamlString, error) {
		if a.Config().HelmTemplateCacheEnabled() {
			keyYAML, err := a.rootHelmTemplateDiskCacheKeyYAML(networkMode, kubernetesVersionOrFallback)
			if err != nil {
				return "", err
			}
			if cached, hit, err := tryReadHelmTemplateDiskCache(a.RootAppPath().Path(), "", keyYAML); err != nil || hit {
				return cached, err
			}
		}
		out, err := a.template(networkMode, kubernetesVersionOrFallback)
		if err != nil {
			return "", err
		}
		if a.Config().HelmTemplateCacheEnabled() {
			keyYAML, kerr := a.rootHelmTemplateDiskCacheKeyYAML(networkMode, kubernetesVersionOrFallback)
			if kerr == nil {
				_ = writeHelmTemplateDiskCacheFiles(a.RootAppPath().Path(), "", keyYAML, out)
			}
		}
		return out, nil
	})
}

func (a *RootApp) rootHelmTemplateDiskCacheKeyYAML(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
) ([]byte, error) {
	vals, err := helmChartValuesForTemplate(a.AsApp(), networkMode)
	if err != nil {
		return nil, err
	}
	staticDigest, err := rootAppStaticBackupManifestDigest(a.RootAppPath().Path())
	if err != nil {
		return nil, err
	}
	return rootHelmTemplateDiskCacheKeyYAML(
		a.AppId(),
		networkMode,
		kubernetesVersionOrFallback,
		string(a.RootAppId()),
		string(argocdNamespace),
		vals,
		staticDigest,
	)
}

func (a *RootApp) template(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
) (types.YamlString, error) {
	vals, err := helmChartValuesForTemplate(a.AsApp(), networkMode)
	if err != nil {
		return "", err
	}

	// load the root app chart
	c, err := a.RootAppPath().LoadChart(a.caches.chartCache, networkMode)
	if err != nil {
		return "", err
	}

	params := helm.RenderChartParams{
		KubernetesVersionOrFallback: kubernetesVersionOrFallback,
		ReleaseName:                 string(a.RootAppId()),
		Namespace:                   string(argocdNamespace),
		ValuesMap:                   vals,
	}

	manifest, err := helm.Template(a.l, c, params)
	if err != nil {
		return "", log.CreateError(
			errors.ErrHydraConfigError,
			"failed to template root app '{app}': {err}",
			log.String("app", string(a.RootAppId())),
			log.Err(err))
	}

	rootPath := a.RootAppPath().Path()
	matches, err := filepath.Glob(filepath.Join(rootPath, "backup-*.sops.yaml"))
	if err != nil {
		return "", err
	}
	slices.Sort(matches)
	var staticSb strings.Builder
	for _, file := range matches {
		fileContent, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		relPath, err := filepath.Rel(rootPath, file)
		if err != nil {
			relPath = filepath.Base(file)
		}
		relPath = filepath.ToSlash(relPath)
		writeStaticManifestChunk(&staticSb, relPath, fileContent)
	}
	if staticSb.Len() > 0 {
		manifest = types.YamlString(string(manifest) + "\n" + staticSb.String())
	}

	return yq.YqPatchArgo(a.l, manifest, a.AppId(), argocdNamespace)
}

func (a *RootApp) Description() string {
	return "RootApp " + string(a.RootAppName) +
		" in Cluster " + string(a.ClusterName) +
		" at " + string(a.Context.ContextPath)
}

func (a *RootApp) Uninstall() error {
	a.l.Info(logIdRootApp, "uninstalling root app {app}", log.String("app", string(a.RootAppId())))

	return nil
}

func (a *RootApp) AppIds(networkMode types.HelmNetworkMode) (sets.Set[types.AppId], error) {
	childAppIds, err := a.GetChildAppIds(networkMode)
	if err != nil {
		return nil, err
	}

	appIds := sets.New(a.AppId())
	appIds = appIds.Union(childAppIds)

	return appIds, nil
}

func (a *RootApp) GetChildAppIds(networkMode types.HelmNetworkMode) (sets.Set[types.AppId], error) {
	vals, err := a.LoadValuesMap(networkMode)
	if err != nil {
		return nil, err
	}

	name := string(a.RootAppName)

	childAppValues, err := values.LookupMap(vals, name, "apps")
	if err != nil {
		return nil, err
	}

	if childAppValues == nil {
		a.l.DebugLog(logIdRootApp, "no child app definition found for root app '{app}'",
			log.String("app", string(a.RootAppId())))
		return nil, nil
	}

	childAppIds := sets.New[types.AppId]()
	for childAppName, appValues := range childAppValues {
		childAppValues, ok := appValues.(map[string]any)
		if !ok {
			a.l.Warn(logIdRootApp, "missing child app settings for app {app}", log.String("app", childAppName))
			continue
		}
		enabled, ok := childAppValues["enabled"]
		if ok && enabled == false {
			continue
		}
		if ok && enabled != true {
			a.l.Warn(logIdRootApp, "invalid enabled value of child app {app}", log.String("app", childAppName))
			continue
		}
		childAppIds = childAppIds.Insert(types.NewChildAppId(a.ClusterName, a.RootAppName, types.ChildAppName(childAppName)))
	}

	return childAppIds, nil
}
