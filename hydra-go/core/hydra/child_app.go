package hydra

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/base/cache"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	hydrayaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
)

// ChildApp represents a child application within a root application.
// It extends RootApp with a specific child application name.
type ChildApp struct {
	*RootApp
	ChildAppName types.ChildAppName
}

var _ Hydra = (*ChildApp)(nil)
var _ HydraApp = (*ChildApp)(nil)

var childAppCache *cache.Cache[childAppCacheKey, *ChildApp]

func initChildAppCache(l log.Logger) {
	if childAppCache == nil {
		childAppCache = cache.NewCache[childAppCacheKey, *ChildApp](l, "childapp-cache", false, nil)
	}
}

type childAppCacheKey struct {
	rootAppCacheKey
	types.ChildAppName
}

func (key childAppCacheKey) String() string {
	return fmt.Sprintf("%s:%s", key.rootAppCacheKey.String(), string(key.ChildAppName))
}

func (a *ChildApp) AsApp() HydraApp {
	return a.AsChildApp()
}

// ChildAppPath returns the path to the child app's chart directory.
// It resolves the path based on the root app's dependency structure.
func (a *ChildApp) ChildAppPath() (helm.ChartDirectory, error) {
	parent, err := a.RootApp.RootAppDependencyPath()
	if err != nil {
		return nil, err
	}
	stage := filepath.Base(parent)
	dir := filepath.Dir(parent)
	root := filepath.Base(dir)
	if root != types.RootDir {
		return nil, log.CreateError(errors.ErrInvalidHydraStructure,
			"unexpected root app dependency structure, expected '.../<root>/<stage>', got '{path}'",
			log.String("path", parent))
	}
	base := filepath.Dir(dir)
	path := filepath.Join(base, string(a.ChildAppName), stage)

	return helm.NewChartDirectory(a.l, path), nil
}

// ChildAppId returns the release name for this ChildApp.
// Format: <cluster>.<rootApp>.<childApp>
func (a *ChildApp) ChildAppId() types.AppId {
	return types.NewChildAppId(a.ClusterName, a.RootAppName, a.ChildAppName)
}

func (a *ChildApp) AppId() types.AppId {
	return a.ChildAppId()
}

// NewChildApp creates a new ChildApp instance for the given root app and child application name.
func NewChildApp(rootApp *RootApp, childAppName types.ChildAppName) (*ChildApp, error) {
	initChildAppCache(rootApp.l)
	key := childAppCacheKey{
		rootAppCacheKey: rootApp.cacheKey(),
		ChildAppName:    childAppName,
	}
	return childAppCache.GetOrLoad(key, func() (*ChildApp, error) {
		return &ChildApp{
			RootApp:      rootApp,
			ChildAppName: childAppName,
		}, nil
	})
}

// AsChildApp returns this ChildApp.
func (a *ChildApp) AsChildApp() *ChildApp {
	return a
}

// WithApp processes the ChildApp with the given app reference.
// It validates that the app matches this child app's identity.
func (a *ChildApp) WithApp(appId types.AppId) (HydraApp, error) {
	if err := validateChildApp(appId, a.ClusterName, a.RootAppName, a.ChildAppName); err != nil {
		return nil, err
	}

	return a, nil
}

// LoadValuesMap loads and processes values for this ChildApp.
// It inherits values from its RootApp and merges them with child app specific values.
func (a *ChildApp) LoadValuesMap(networkMode types.HelmNetworkMode) (types.ValuesMap, error) {
	cacheKey := newValuesCacheKey(a.AppId(), networkMode)
	return a.caches.getOrLoadValues(cacheKey, func() (types.ValuesMap, error) {
		return a.loadValuesMap(networkMode)
	})
}

func (a *ChildApp) loadValuesMap(networkMode types.HelmNetworkMode) (types.ValuesMap, error) {
	path, err := a.ChildAppPath()
	if err != nil {
		return nil, err
	}
	a.l.DebugLog(logIdChildApp, "child app path: {path}", log.String("path", path.Path()))

	chart, err := path.LoadChart(a.caches.chartCache, networkMode)
	if err != nil {
		return nil, err
	}

	vals, err := a.MergedChildValuesForHelmInstall(networkMode)
	if err != nil {
		return nil, err
	}
	vals, err = ensureHydraTypeInValues(vals, hydraTypeChildApp, hydraTypeChildApp)
	if err != nil {
		return nil, err
	}

	return helm.LoadValuesMap(a.l, chart, vals)
}

// MergedChildValuesForHelmInstall returns the raw Helm user values passed to the child chart's
// CoalesceValues (umbrella-derived subtree + extraValueFiles). Same stage [helm.Template] must use;
// see [helm.CoalescedValuesMapBeforeRender].
func (a *ChildApp) MergedChildValuesForHelmInstall(networkMode types.HelmNetworkMode) (types.ValuesMap, error) {
	path, err := a.ChildAppPath()
	if err != nil {
		return nil, err
	}

	vals, extraValuesFiles, err := a.LoadValuesFromRootApp(networkMode)
	if err != nil {
		return nil, err
	}

	for _, extraValuesFile := range extraValuesFiles {
		vals, err = values.LoadAndMergeValuesFile(a.l, filepath.Join(path.Path(), extraValuesFile), vals)
		if err != nil {
			return nil, err
		}
	}
	return vals, nil
}

// LoadValuesFromRootApp loads values from the parent RootApp.
// It extracts global values and app-specific values for this child app.
func (a *ChildApp) LoadValuesFromRootApp(networkMode types.HelmNetworkMode) (types.ValuesMap, []string, error) {
	// Child-specific subtree must come from one umbrella Coalesce pass (pre–ToRenderValues) so it
	// matches Helm’s dependency values (e.g. external-dns provider + extraValueFiles).
	coalesced, err := a.RootApp.coalescedHelmValuesMap(networkMode)
	if err != nil {
		return nil, nil, err
	}
	// global.* keys that subcharts read (e.g. baseUrl) are only fully present after
	// chartutil.ToRenderValues / MergeGlobalValues on the umbrella — use that for global only.
	full, err := a.RootApp.LoadValuesMap(networkMode)
	if err != nil {
		return nil, nil, err
	}
	// YAML round-trip so nested maps are consistently map[string]any — LookupMap and
	// MergeValues require typed subtrees (Helm/yaml can produce map[string]interface{}).
	coalesced, err = valuesMapYAMLNormalize(coalesced)
	if err != nil {
		return nil, nil, err
	}
	full, err = valuesMapYAMLNormalize(full)
	if err != nil {
		return nil, nil, err
	}
	result := map[string]any{}
	// Merge global from Coalesce-only vs post-ToRenderValues umbrella values. Full merge can
	// surface keys (e.g. global.baseUrl from chart defaults + GitOps values) that belong in the
	// child’s .Values.global; coalesced alone can be structurally narrower. Order: coalesced
	// first, then full — [values.MergeValues] gives the right map precedence on key conflicts.
	gCoa, err := values.LookupMap(coalesced, "global")
	if err != nil {
		return nil, nil, err
	}
	gFull, err := values.LookupMap(full, "global")
	if err != nil {
		return nil, nil, err
	}
	switch {
	case gCoa != nil && gFull != nil:
		result["global"] = values.MergeValues(gCoa, gFull)
	case gFull != nil:
		result["global"] = gFull
	case gCoa != nil:
		result["global"] = gCoa
	}
	if app, err := values.LookupMap(coalesced, string(a.ChildAppName)); err != nil || app != nil {
		if err != nil {
			return nil, nil, err
		}
		result = values.MergeValues(result, app)
	}
	var extraValuesFiles []string
	if appConfig, err := values.LookupMap(coalesced, string(a.RootAppName), "apps", string(a.ChildAppName)); err != nil || appConfig != nil {
		if err != nil {
			return nil, nil, err
		}
		if evf, ok := appConfig["extraValueFiles"].(map[string]any); ok {
			for k, v := range evf {
				if b, ok := v.(bool); ok && b {
					extraValuesFiles = append(extraValuesFiles, k)
				}
			}
			slices.Sort(extraValuesFiles)
		}
	}
	replicateParentGlobalIntoChildDependencyValues(result, string(a.ChildAppName))
	return result, extraValuesFiles, nil
}

func valuesMapYAMLNormalize(m types.ValuesMap) (types.ValuesMap, error) {
	if m == nil {
		return types.ValuesMap{}, nil
	}
	// Deep-clone before yaml.v3 Encode: root/coalesced values maps are cached; Node.Encode can
	// mutate shared nested tables in place, corrupting cached subtrees across child-app renders
	// (symptoms: later helm.Template sees only top-level global, missing dependency-key nest).
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var clone types.ValuesMap
	if err := json.Unmarshal(b, &clone); err != nil {
		return nil, err
	}
	ys, err := hydrayaml.ToYaml(clone)
	if err != nil {
		return nil, err
	}
	return hydrayaml.FromYaml[types.ValuesMap](ys)
}

// replicateParentGlobalIntoChildDependencyValues nests umbrella global under the chart's
// dependency key matching ChildAppName (e.g. service-auth). Helm v4 coalesceGlobals skips
// merging top-level global into a subchart when the subchart's existing `global` value is
// not a table (chart defaults can ship a scalar/placeholder), leaving .Values.global empty
// in dependency templates. Matching ArgoCD-passed values shape fixes hydra helm template.
func replicateParentGlobalIntoChildDependencyValues(result types.ValuesMap, depName string) {
	if result == nil || depName == "" {
		return
	}
	gAny, ok := result["global"]
	if !ok || gAny == nil {
		return
	}
	g, ok := globalValueAsStringMap(gAny)
	if !ok || g == nil {
		return
	}
	dep, ok := result[depName].(map[string]any)
	if !ok || dep == nil {
		dep = map[string]any{}
		result[depName] = dep
	}
	var existing map[string]any
	if eg, ok := dep["global"].(map[string]any); ok && eg != nil {
		existing = eg
	}
	// Dependency-local global (e.g. hydra.refs) overrides umbrella global on conflicts.
	dep["global"] = values.MergeValues(g, existing)
}

// globalValueAsStringMap accepts map-shaped global values from Helm/yaml that may not assert
// directly to map[string]any (e.g. chartutil.Values, JSON decode types).
func globalValueAsStringMap(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	// Fallback: normalize via JSON (same approach as valuesMapYAMLNormalize edge cases).
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, false
	}
	return out, true
}

func (a *ChildApp) Namespace(networkMode types.HelmNetworkMode) (types.Namespace, error) {
	// load values of the root app
	rootAppValues, err := a.RootApp.LoadValuesMap(networkMode)
	if err != nil {
		return "", err
	}

	namespaceAny := values.Lookup(
		rootAppValues,
		string(a.RootAppName),
		"apps",
		string(a.ChildAppName),
		"namespace")

	if namespaceAny == nil {
		return "", log.CreateError(errors.ErrHydraConfigError,
			"namespace not defined for child app '{app}' in root app values",
			log.String("app", string(a.ChildAppId())))
	}
	if namespaceStr, ok := namespaceAny.(string); ok && namespaceStr != "" {
		return types.Namespace(namespaceStr), nil
	}

	return "", log.CreateError(errors.ErrHydraConfigError,
		"namespace for child app '{app}' is not a valid string",
		log.String("app", string(a.ChildAppId())))
}

func (a *ChildApp) AppIds() ([]types.AppId, error) {
	return []types.AppId{a.AppId()}, nil
}

func recursiveGlob(root string, pattern string) ([]string, error) {
	var matches []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if !d.IsDir() {
			if matched, _ := filepath.Match(pattern, d.Name()); matched {
				matches = append(matches, path)
			}
		}

		return nil
	})

	return matches, err
}

// Template renders and returns the templates for this ChildApp.
func (a *ChildApp) Template(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
) (types.YamlString, error) {
	skipCrds, err := a.skipCrdsFromRootAppConfig(networkMode)
	if err != nil {
		return "", err
	}
	if _, ok, _ := a.Cluster.helmChartInputValues(a.AppId()); ok {
		return a.template(networkMode, kubernetesVersionOrFallback, skipCrds)
	}
	digest, err := helmChartTemplateValuesDigest(a.AsApp(), networkMode)
	if err != nil {
		return "", err
	}
	cacheKey := newTemplateCacheKey(a.AppId(), networkMode, kubernetesVersionOrFallback, skipCrds, digest)
	childSuffix := string(a.ChildAppName)
	return a.caches.getOrLoadTemplates(cacheKey, func() (types.YamlString, error) {
		if a.Config().HelmTemplateCacheEnabled() {
			keyYAML, err := a.childHelmTemplateDiskCacheKeyYAML(networkMode, kubernetesVersionOrFallback, skipCrds)
			if err != nil {
				return "", err
			}
			if cached, hit, err := tryReadHelmTemplateDiskCache(a.RootAppPath().Path(), childSuffix, keyYAML); err != nil || hit {
				return cached, err
			}
		}
		out, err := a.template(networkMode, kubernetesVersionOrFallback, skipCrds)
		if err != nil {
			return "", err
		}
		if a.Config().HelmTemplateCacheEnabled() {
			keyYAML, kerr := a.childHelmTemplateDiskCacheKeyYAML(networkMode, kubernetesVersionOrFallback, skipCrds)
			if kerr == nil {
				_ = writeHelmTemplateDiskCacheFiles(a.RootAppPath().Path(), childSuffix, keyYAML, out)
			}
		}
		return out, nil
	})
}

func (a *ChildApp) childHelmTemplateDiskCacheKeyYAML(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	skipCrds bool,
) ([]byte, error) {
	vals, err := helmChartValuesForTemplate(a.AsApp(), networkMode)
	if err != nil {
		return nil, err
	}
	digest, err := childAppStaticManifestDigest(a.RootAppPath().Path(), a.ChildAppName)
	if err != nil {
		return nil, err
	}
	namespace, err := a.Namespace(networkMode)
	if err != nil {
		return nil, err
	}
	return childHelmTemplateDiskCacheKeyYAML(
		a.AppId(),
		networkMode,
		kubernetesVersionOrFallback,
		string(a.ChildAppName),
		string(namespace),
		skipCrds,
		vals,
		digest,
	)
}

// skipCrdsFromRootAppConfig returns root values path <rootApp>.apps.<childApp>.skipCrds (ArgoCD helm.skipCrds).
func (a *ChildApp) skipCrdsFromRootAppConfig(networkMode types.HelmNetworkMode) (bool, error) {
	vals, err := a.RootApp.LoadValuesMap(networkMode)
	if err != nil {
		return false, err
	}
	v := values.Lookup(vals, string(a.RootAppName), "apps", string(a.ChildAppName), "skipCrds")
	if v == nil {
		return false, nil
	}
	if b, ok := v.(bool); ok {
		return b, nil
	}
	return false, nil
}

func (a *ChildApp) template(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	skipCrds bool,
) (types.YamlString, error) {
	vals, err := helmChartValuesForTemplate(a.AsApp(), networkMode)
	if err != nil {
		return "", err
	}

	staticManifests := filepath.Join(a.RootAppPath().Path(), "apps", string(a.ChildAppName))
	a.l.DebugLog(logIdChildApp, "checking for static manifests at '{path}'", log.String("path", staticManifests))

	files, err := recursiveGlob(staticManifests, "*.yaml")
	if err != nil {
		return "", err
	}

	type staticChunk struct {
		relPath     string
		content     []byte
		k8sDefaults bool
	}
	chunks := make([]staticChunk, 0, len(files))
	for _, file := range files {
		a.l.DebugLog(logIdChildApp, "including static manifest '{file}' for child app '{app}'",
			log.String("file", path.Base(file)),
			log.String("app", string(a.ChildAppId())))
		fileContent, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		relPath, err := filepath.Rel(a.RootAppPath().Path(), file)
		if err != nil {
			relPath = filepath.Base(file)
		}
		relPath = filepath.ToSlash(relPath)
		base := path.Base(file)
		chunks = append(chunks, staticChunk{
			relPath:     relPath,
			content:     fileContent,
			k8sDefaults: entity.IsKubernetesDefaultsStaticFilename(base),
		})
	}

	// load the child app chart
	path, err := a.ChildAppPath()
	if err != nil {
		return "", err
	}
	c, err := path.LoadChart(a.caches.chartCache, networkMode)
	if err != nil {
		return "", err
	}

	namespace, err := a.Namespace(networkMode)
	if err != nil {
		return "", err
	}

	if skipCrds {
		a.l.DebugLog(logIdChildApp, "helm skipCrds for child app {app} (aligned with ArgoCD helm.skipCrds)",
			log.String("app", string(a.ChildAppId())))
	}

	params := helm.RenderChartParams{
		KubernetesVersionOrFallback: kubernetesVersionOrFallback,
		ReleaseName:                 string(a.ChildAppName),
		Namespace:                   string(namespace),
		ValuesMap:                   vals,
		SkipCrds:                    skipCrds,
	}

	manifest, err := helm.Template(a.l, c, params)
	if err != nil {
		return "", log.CreateError(
			errors.ErrHelmTemplateFailed,
			"failed to template child app '{app}': {err}",
			log.String("app", string(a.ChildAppId())),
			log.Err(err))
	}

	var nonDefaultsStatic strings.Builder
	for _, ch := range chunks {
		if ch.k8sDefaults {
			continue
		}
		writeStaticManifestChunk(&nonDefaultsStatic, ch.relPath, ch.content)
	}
	combinedForIds := types.YamlString(string(manifest) + "\n" + nonDefaultsStatic.String())
	existingIds, err := entity.IdsFromManifest(a.l, combinedForIds, types.KeyTemplateEntity)
	if err != nil {
		return "", err
	}

	var staticSb strings.Builder
	for _, ch := range chunks {
		body := ch.content
		if ch.k8sDefaults {
			filtered, ferr := entity.FilterKubernetesDefaultsBody(a.l, ch.relPath, ch.content, existingIds, types.KeyTemplateEntity)
			if ferr != nil {
				return "", ferr
			}
			if len(filtered) == 0 {
				a.l.DebugLog(logIdChildApp, "skipped kubernetes-defaults file '{path}' (all documents duplicate chart/other static)",
					log.String("path", ch.relPath))
				continue
			}
			body = filtered
		}
		writeStaticManifestChunk(&staticSb, ch.relPath, body)
	}

	manifest = types.YamlString(string(manifest) + "\n" + staticSb.String())

	return yq.YqPatchArgo(a.l, manifest, a.AppId(), namespace)
}

func writeStaticManifestChunk(sb *strings.Builder, relPath string, content []byte) {
	sb.WriteString("---\n# Source: ")
	sb.WriteString(relPath)
	sb.WriteString("\n")
	sb.Write(content)
}

func (a *ChildApp) Description() string {
	return "ChildApp " + string(a.ChildAppName) +
		" of RootApp " + string(a.RootAppName) +
		" in Cluster " + string(a.ClusterName) +
		" at " + string(a.Context.ContextPath)
}

func (a *ChildApp) Uninstall() error {
	a.l.Info(logIdChildApp, "uninstalling child app {appId}", log.String("appId", string(a.ChildAppId())))

	return nil
}
