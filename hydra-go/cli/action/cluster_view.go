package action

import (
	"path/filepath"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/export"
	"hydra-gitops.org/hydra/hydra-go/core/git"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/view"
	hyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
	"k8s.io/apimachinery/pkg/util/sets"
)

// ClusterViewFlags contains common configuration options for viewing cluster resources.
type ClusterViewFlags struct {
	flags.HelmNetworkModeFlag
	flags.ContextFlag
	flags.ColorFlag
	flags.KubernetesVersionFlag
	flags.CrdModeFlag
	flags.NoCacheFlag
}

func (f *ClusterViewFlags) Flags() flags.Flags {
	return f
}

// ClusterViewClusterFlags contains configuration options for viewing all apps from a cluster.
type ClusterViewClusterFlags struct {
	ClusterViewFlags
	flags.ClusterFlag
	OutputDir string
}

func (f *ClusterViewClusterFlags) Flags() flags.Flags {
	return f
}

// ClusterViewCluster renders templates for all apps from a cluster and exports to a directory.
func ClusterViewCluster(f ClusterViewClusterFlags) (hydra.Hydra, error) {
	l := log.Default()
	l.Info(logIdAction, "Exporting cluster dump for cluster '{cluster}' to '{dir}'",
		log.String("cluster", string(f.Cluster)),
		log.String("dir", f.OutputDir))

	if err := export.ValidateDir(f.OutputDir); err != nil {
		return nil, log.CreateError(errors.ErrWriteFailed, "invalid output directory: {err}", log.Err(err))
	}

	cluster, err := commands.ResolveCommandCluster(commands.ResolveCommandClusterOptions{
		Config:       flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo),
		HydraContext: f.HydraContext,
		Limits:       hydra.RESTClientLimits{},
		ClusterName:  f.Cluster,
	})
	if err != nil {
		return nil, err
	}

	appIds, err := cluster.AppIds(f.HelmNetworkMode)
	if err != nil {
		return nil, err
	}

	err = clusterViewAppsExport(f.ClusterViewFlags, appIds, f.OutputDir)
	if err != nil {
		return nil, err
	}

	return cluster, nil
}

// clusterViewAppsExport renders dependencies and exports everything to a directory.
func clusterViewAppsExport(f ClusterViewFlags, appIds sets.Set[types.AppId], outputDir string) error {
	if len(appIds) == 0 {
		return log.CreateError(errors.ErrNoAppsSpecified, "no apps specified for viewing")
	}

	cluster, err := commands.ResolveCommandCluster(commands.ResolveCommandClusterOptions{
		Config:       flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo),
		HydraContext: f.HydraContext,
		Limits:       hydra.RESTClientLimits{},
		AppIds:       appIds,
	})
	if err != nil {
		return err
	}

	l := cluster.L()

	skipRootApps := types.SkipRootApps(cluster.ClusterName != types.InCluster)
	renderedEntities, _, _, err := commands.RenderCluster(cluster, appIds, f.KubernetesVersion, f.CrdMode, skipRootApps, nil)
	if err != nil {
		return err
	}

	contextParentPath := filepath.Dir(string(cluster.ContextPath))

	var gitRepoRoot string
	if root, err := git.RepoRoot(contextParentPath); err == nil {
		gitRepoRoot = root
	}

	chartModels := commands.CollectChartInfo(cluster, appIds, f.HelmNetworkMode, gitRepoRoot)
	chartObjects := commands.CollectChartObjects(cluster, appIds, f.HelmNetworkMode)
	valueFiles := commands.CollectValueFiles(cluster, appIds, f.HelmNetworkMode)
	appValuesModels, appValuesData := commands.CollectAppValues(cluster, appIds, f.HelmNetworkMode)
	fallbackModels, fallbackData := commands.CollectFallbackValues(cluster, appIds, f.HelmNetworkMode)

	appRefParsers, err := hydra.HydraAppRefParsers(cluster, appIds, f.HelmNetworkMode, renderedEntities)
	if err != nil {
		return err
	}

	model, err := view.ToModelWithParsers(l, renderedEntities, appRefParsers, chartModels)
	if err != nil {
		return err
	}
	model.ValueFiles = valueFiles
	model.AppValues = appValuesModels
	model.FallbackValues = fallbackModels

	manifests := collectManifestYaml(renderedEntities, model)

	if remote, err := git.RemoteURL(contextParentPath); err == nil {
		model.GitRemote = remote
	}
	if gitRepoRoot != "" {
		if prefix, err := filepath.Rel(gitRepoRoot, contextParentPath); err == nil && prefix != "." {
			model.GitRepoPrefix = prefix + "/"
		}
	}
	if branch, err := git.Branch(contextParentPath); err == nil {
		model.GitBranch = branch
	}

	return export.WriteClusterDump(l, outputDir, model, chartObjects, manifests, valueFiles, contextParentPath, appValuesData, fallbackData)
}

// ClusterViewContextFlags contains configuration options for exporting all clusters in a context.
type ClusterViewContextFlags struct {
	ClusterViewFlags
	OutputDir string
}

func (f *ClusterViewContextFlags) Flags() flags.Flags {
	return f
}

// clusterRenderResult holds the complete render output for a cluster.
type clusterRenderResult struct {
	entities          entity.Entities
	chartModels       []view.ChartModel
	chartObjects      map[string]*v2chart.Chart
	manifests         map[string][]byte
	valueFiles        []view.ValueFileModel
	appValuesModels   []view.AppValuesModel
	appValuesData     map[types.AppId]types.ValuesMap
	fallbackModels    []view.AppValuesModel
	fallbackData      map[types.AppId]types.ValuesMap
	appRefParsers     []types.RefParser
	contextParentPath string
	gitRemote         string
	gitRepoPrefix     string
	gitBranch         string
}

// ClusterViewContext renders templates for all clusters in a context and exports to a directory.
func ClusterViewContext(f ClusterViewContextFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Exporting context dump to '{dir}'", log.String("dir", f.OutputDir))

	if err := export.ValidateContextDir(f.OutputDir); err != nil {
		return log.CreateError(errors.ErrWriteFailed, "invalid output directory: {err}", log.Err(err))
	}

	resolved, err := hydra.ResolvePath(l, f.HydraContext, flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo))
	if err != nil {
		return err
	}

	context := resolved.AsContext()
	if context == nil {
		return log.CreateError(errors.ErrInvalidHydraStructure, "resolved path is not a context")
	}

	clusters, err := context.GetClusters()
	if err != nil {
		return err
	}

	inCluster, err := context.WithCluster(types.InCluster, hydra.RESTClientLimits{})
	if err != nil {
		return err
	}

	inClusterAppIds, err := inCluster.AppIds(f.HelmNetworkMode)
	if err != nil {
		return err
	}

	l.Info(logIdAction, "Exporting cluster '{cluster}'", log.String("cluster", string(types.InCluster)))

	inClusterResult, err := renderClusterApps(f.ClusterViewFlags, inCluster, inClusterAppIds, false)
	if err != nil {
		return err
	}

	inClusterScopeInfo, err := inClusterResult.entities.ScopeInfoMapFromCrds(types.KeyTemplateEntity)
	if err != nil {
		return err
	}

	for _, cluster := range clusters {
		if cluster.ClusterName == types.InCluster {
			continue
		}

		l.Info(logIdAction, "Exporting cluster '{cluster}'", log.String("cluster", string(cluster.ClusterName)))

		appIds, err := cluster.AppIds(f.HelmNetworkMode)
		if err != nil {
			return err
		}

		result, err := renderClusterApps(f.ClusterViewFlags, cluster, appIds, false, inClusterScopeInfo)
		if err != nil {
			return err
		}

		rootResult, childResult, err := splitRenderResult(*result, appIds)
		if err != nil {
			return err
		}

		merged, err := mergeRenderResults(*inClusterResult, rootResult)
		if err != nil {
			return err
		}
		*inClusterResult = merged

		clusterDir := filepath.Join(f.OutputDir, string(cluster.ClusterName))
		if err := writeClusterExport(&childResult, clusterDir); err != nil {
			return err
		}
	}

	inClusterDir := filepath.Join(f.OutputDir, string(types.InCluster))
	if err := writeClusterExport(inClusterResult, inClusterDir); err != nil {
		return err
	}

	return nil
}

// renderClusterApps renders all apps for a cluster and returns the full result.
func renderClusterApps(f ClusterViewFlags, cluster *hydra.Cluster, appIds sets.Set[types.AppId], skipRootApps bool, additionalScopeInfo ...types.ScopeInfoMap) (*clusterRenderResult, error) {
	if len(appIds) == 0 {
		return &clusterRenderResult{
			entities: entity.Entities{},
		}, nil
	}

	l := cluster.L()

	renderedEntities, _, _, err := commands.RenderCluster(cluster, appIds, f.KubernetesVersion, f.CrdMode, types.SkipRootApps(skipRootApps), nil, additionalScopeInfo...)
	if err != nil {
		return nil, err
	}

	contextParentPath := filepath.Dir(string(cluster.ContextPath))

	var gitRepoRoot string
	if root, err := git.RepoRoot(contextParentPath); err == nil {
		gitRepoRoot = root
	}

	chartModels := commands.CollectChartInfo(cluster, appIds, f.HelmNetworkMode, gitRepoRoot)
	chartObjects := commands.CollectChartObjects(cluster, appIds, f.HelmNetworkMode)
	valueFiles := commands.CollectValueFiles(cluster, appIds, f.HelmNetworkMode)
	appValuesModels, appValuesData := commands.CollectAppValues(cluster, appIds, f.HelmNetworkMode)
	fallbackModels, fallbackData := commands.CollectFallbackValues(cluster, appIds, f.HelmNetworkMode)

	var gitRemote, gitRepoPrefix, gitBranch string
	if remote, err := git.RemoteURL(contextParentPath); err == nil {
		gitRemote = remote
	}
	if gitRepoRoot != "" {
		if prefix, err := filepath.Rel(gitRepoRoot, contextParentPath); err == nil && prefix != "." {
			gitRepoPrefix = prefix + "/"
		}
	}
	if branch, err := git.Branch(contextParentPath); err == nil {
		gitBranch = branch
	}

	appRefParsers, err := hydra.HydraAppRefParsers(cluster, appIds, f.HelmNetworkMode, renderedEntities)
	if err != nil {
		return nil, err
	}

	l.Info(logIdAction, "rendered {count} entities for cluster '{cluster}'",
		log.Int("count", renderedEntities.Len()),
		log.String("cluster", string(cluster.ClusterName)))

	return &clusterRenderResult{
		entities:          renderedEntities,
		chartModels:       chartModels,
		chartObjects:      chartObjects,
		manifests:         nil,
		valueFiles:        valueFiles,
		appValuesModels:   appValuesModels,
		appValuesData:     appValuesData,
		fallbackModels:    fallbackModels,
		fallbackData:      fallbackData,
		appRefParsers:     appRefParsers,
		contextParentPath: contextParentPath,
		gitRemote:         gitRemote,
		gitRepoPrefix:     gitRepoPrefix,
		gitBranch:         gitBranch,
	}, nil
}

// splitRenderResult separates a render result into root-app and child-app portions.
func splitRenderResult(result clusterRenderResult, allAppIds sets.Set[types.AppId]) (rootResult clusterRenderResult, childResult clusterRenderResult, err error) {
	rootAppIds := sets.New[types.AppId]()
	childAppIds := sets.New[types.AppId]()
	for appId := range allAppIds {
		if appId.IsRootApp() {
			rootAppIds.Insert(appId)
		} else {
			childAppIds.Insert(appId)
		}
	}

	rootResult = clusterRenderResult{
		entities:          entity.Entities{},
		contextParentPath: result.contextParentPath,
		gitRemote:         result.gitRemote,
		gitRepoPrefix:     result.gitRepoPrefix,
		gitBranch:         result.gitBranch,
	}
	childResult = clusterRenderResult{
		entities:          entity.Entities{},
		contextParentPath: result.contextParentPath,
		gitRemote:         result.gitRemote,
		gitRepoPrefix:     result.gitRepoPrefix,
		gitBranch:         result.gitBranch,
	}

	if result.entities.Len() > 0 {
		_, rootEntities, err := result.entities.SelectByAppIds(rootAppIds)
		if err != nil {
			return rootResult, childResult, err
		}
		_, childEntities, err := result.entities.SelectByAppIds(childAppIds)
		if err != nil {
			return rootResult, childResult, err
		}
		rootResult.entities = rootEntities
		childResult.entities = childEntities
	}

	for _, cm := range result.chartModels {
		if cm.AppId.IsRootApp() {
			rootResult.chartModels = append(rootResult.chartModels, cm)
		} else {
			childResult.chartModels = append(childResult.chartModels, cm)
		}
	}

	rootChartNames := sets.New[string]()
	for _, cm := range rootResult.chartModels {
		rootChartNames.Insert(cm.Name)
	}
	childChartNames := sets.New[string]()
	for _, cm := range childResult.chartModels {
		childChartNames.Insert(cm.Name)
	}

	if result.chartObjects != nil {
		rootResult.chartObjects = make(map[string]*v2chart.Chart)
		childResult.chartObjects = make(map[string]*v2chart.Chart)
		for name, chrt := range result.chartObjects {
			if rootChartNames.Has(name) {
				rootResult.chartObjects[name] = chrt
			}
			if childChartNames.Has(name) {
				childResult.chartObjects[name] = chrt
			}
		}
	}

	if result.manifests != nil {
		rootResult.manifests = make(map[string][]byte)
		childResult.manifests = make(map[string][]byte)
		for manifestPath, data := range result.manifests {
			appId := manifestAppId(manifestPath)
			if rootAppIds.Has(appId) {
				rootResult.manifests[manifestPath] = data
			} else {
				childResult.manifests[manifestPath] = data
			}
		}
	}

	for _, vf := range result.valueFiles {
		rootResult.valueFiles = append(rootResult.valueFiles, vf)
		childResult.valueFiles = append(childResult.valueFiles, vf)
	}

	for _, avm := range result.appValuesModels {
		if avm.AppId.IsRootApp() {
			rootResult.appValuesModels = append(rootResult.appValuesModels, avm)
		} else {
			childResult.appValuesModels = append(childResult.appValuesModels, avm)
		}
	}

	if result.appValuesData != nil {
		rootResult.appValuesData = make(map[types.AppId]types.ValuesMap)
		childResult.appValuesData = make(map[types.AppId]types.ValuesMap)
		for appId, vals := range result.appValuesData {
			if rootAppIds.Has(appId) {
				rootResult.appValuesData[appId] = vals
			} else {
				childResult.appValuesData[appId] = vals
			}
		}
	}

	for _, fbm := range result.fallbackModels {
		if fbm.AppId.IsRootApp() {
			rootResult.fallbackModels = append(rootResult.fallbackModels, fbm)
		} else {
			childResult.fallbackModels = append(childResult.fallbackModels, fbm)
		}
	}

	if result.fallbackData != nil {
		rootResult.fallbackData = make(map[types.AppId]types.ValuesMap)
		childResult.fallbackData = make(map[types.AppId]types.ValuesMap)
		for appId, vals := range result.fallbackData {
			if rootAppIds.Has(appId) {
				rootResult.fallbackData[appId] = vals
			} else {
				childResult.fallbackData[appId] = vals
			}
		}
	}

	return rootResult, childResult, nil
}

// manifestAppId extracts the appId from a manifest path (first path segment).
func manifestAppId(manifestPath string) types.AppId {
	for i, c := range manifestPath {
		if c == '/' {
			return types.AppId(manifestPath[:i])
		}
	}
	return types.AppId(manifestPath)
}

// mergeRenderResults merges extra render data into a base result.
func mergeRenderResults(base, extra clusterRenderResult) (clusterRenderResult, error) {
	merged := base

	if base.entities.Len() == 0 && extra.entities.Len() == 0 {
		merged.entities = entity.Entities{}
	} else if base.entities.Len() == 0 {
		merged.entities = extra.entities
	} else if extra.entities.Len() > 0 {
		var err error
		merged.entities, err = base.entities.Add(extra.entities)
		if err != nil {
			return clusterRenderResult{}, err
		}
	}

	merged.chartModels = append(base.chartModels, extra.chartModels...)
	if len(merged.chartModels) == 0 {
		merged.chartModels = nil
	}

	merged.chartObjects = mergeMaps(base.chartObjects, extra.chartObjects)
	merged.manifests = mergeBytesMaps(base.manifests, extra.manifests)

	merged.valueFiles = append(base.valueFiles, extra.valueFiles...)
	if len(merged.valueFiles) == 0 {
		merged.valueFiles = nil
	}

	merged.appValuesModels = append(base.appValuesModels, extra.appValuesModels...)
	if len(merged.appValuesModels) == 0 {
		merged.appValuesModels = nil
	}
	merged.appValuesData = mergeValuesDataMaps(base.appValuesData, extra.appValuesData)

	merged.fallbackModels = append(base.fallbackModels, extra.fallbackModels...)
	if len(merged.fallbackModels) == 0 {
		merged.fallbackModels = nil
	}
	merged.fallbackData = mergeValuesDataMaps(base.fallbackData, extra.fallbackData)

	merged.appRefParsers = append(base.appRefParsers, extra.appRefParsers...)

	return merged, nil
}

func mergeMaps[K comparable, V any](base, extra map[K]V) map[K]V {
	if base == nil && extra == nil {
		return nil
	}
	result := make(map[K]V)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

func mergeBytesMaps(base, extra map[string][]byte) map[string][]byte {
	if base == nil && extra == nil {
		return nil
	}
	result := make(map[string][]byte)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

func mergeValuesDataMaps(base, extra map[types.AppId]types.ValuesMap) map[types.AppId]types.ValuesMap {
	if base == nil && extra == nil {
		return nil
	}
	result := make(map[types.AppId]types.ValuesMap)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

// writeClusterExport writes a render result to a cluster output directory.
func writeClusterExport(data *clusterRenderResult, outputDir string) error {
	l := log.Default()

	model, err := view.ToModelWithParsers(l, data.entities, data.appRefParsers, data.chartModels)
	if err != nil {
		return err
	}

	model.ValueFiles = data.valueFiles
	model.AppValues = data.appValuesModels
	model.FallbackValues = data.fallbackModels
	model.GitRemote = data.gitRemote
	model.GitRepoPrefix = data.gitRepoPrefix
	model.GitBranch = data.gitBranch

	manifests := collectManifestYaml(data.entities, model)

	return export.WriteClusterDump(l, outputDir, model, data.chartObjects, manifests, data.valueFiles, data.contextParentPath, data.appValuesData, data.fallbackData)
}

// collectManifestYaml extracts rendered YAML for each entity that has a manifestPath.
func collectManifestYaml(entities entity.Entities, model view.DependenciesModel) map[string][]byte {
	result := make(map[string][]byte)
	for _, idModel := range model.Entities {
		if idModel.ManifestPath == "" {
			continue
		}
		e, ok := entities.EntityMap[idModel.Id]
		if !ok {
			continue
		}
		u, ok := e.Unstructured(types.KeyTemplateEntity)
		if !ok {
			continue
		}
		yamlStr, err := hyaml.ToYaml(u.Object)
		if err != nil {
			continue
		}
		result[idModel.ManifestPath] = []byte(yamlStr)
	}
	return result
}
