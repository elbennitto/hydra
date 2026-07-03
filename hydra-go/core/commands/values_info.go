package commands

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"

	helmchart "helm.sh/helm/v4/pkg/chart"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/view"
	"k8s.io/apimachinery/pkg/util/sets"
)

// CollectValueFiles discovers all values files from the GitOps repository that
// are used by the given cluster and apps but are NOT bundled in chart .tgz
// archives. Paths in the returned models are relative to the context parent
// directory (the "group" level).
func CollectValueFiles(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
) []view.ValueFileModel {
	l := cluster.L()
	contextPath := string(cluster.ContextPath)
	basePath := filepath.Dir(contextPath)

	seen := make(map[string]bool)
	var files []view.ValueFileModel

	addFile := func(absPath, fileType string, appId types.AppId) {
		if seen[absPath] {
			return
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return
		}
		relPath, err := filepath.Rel(basePath, absPath)
		if err != nil {
			l.Warn(logIdCommands, "cannot compute relative path for '{path}': {err}",
				log.String("path", absPath), log.Err(err))
			return
		}
		seen[absPath] = true
		files = append(files, view.ValueFileModel{
			Path:  relPath,
			Type:  fileType,
			AppId: appId,
		})
	}

	// Group values (parent of context)
	groupValuesPath := filepath.Join(contextPath, "..", "values.yaml")
	groupValuesPath, _ = filepath.Abs(groupValuesPath)
	addFile(groupValuesPath, "group", "")

	// Context values
	contextValuesPath := filepath.Join(contextPath, "values.yaml")
	addFile(contextValuesPath, "context", "")

	// Cluster values
	clusterValuesPath := cluster.ClusterValuesFile()
	addFile(clusterValuesPath, "cluster", "")

	// Per-app: root app chart values.yaml is inside the tgz, so we skip it.
	// But child app extra value files are on disk.
	for appId := range appIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			continue
		}

		childApp := h.(hydra.Hydra).AsChildApp()
		if childApp == nil {
			continue
		}

		childPath, err := childApp.ChildAppPath()
		if err != nil {
			continue
		}

		_, extraFiles, err := childApp.LoadValuesFromRootApp(networkMode)
		if err != nil {
			continue
		}

		for _, ef := range extraFiles {
			absPath := filepath.Join(childPath.Path(), ef)
			addFile(absPath, "app", appId)
		}
	}

	// Root app values from the gitops repository (e.g. gitops-repo/.../demo/values.yaml).
	// These are root-app-specific overrides, distinct from child app values.
	for appId := range appIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			continue
		}
		rootApp := h.(hydra.Hydra).AsRootApp()
		if rootApp == nil {
			continue
		}
		rootAppValuesPath := filepath.Join(rootApp.RootAppPath().Path(), "values.yaml")
		if _, err := os.Stat(rootAppValuesPath); err == nil {
			relPath, err := filepath.Rel(basePath, rootAppValuesPath)
			if err == nil && !seen[rootAppValuesPath] {
				seen[rootAppValuesPath] = true
				files = append(files, view.ValueFileModel{
					Path:  relPath,
					Type:  "rootApp",
					AppId: types.NewRootAppId(cluster.ClusterName, rootApp.RootAppName),
				})
			}
		}
	}

	slices.SortFunc(files, func(a, b view.ValueFileModel) int {
		return cmp.Compare(a.Path, b.Path)
	})

	l.Info(logIdCommands, "collected {count} value files", log.Int("count", len(files)))
	return files
}

// CollectAppValues loads the fully merged values for each app and returns
// metadata models plus the actual values keyed by AppId.
func CollectAppValues(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
) ([]view.AppValuesModel, map[types.AppId]types.ValuesMap) {
	l := cluster.L()
	var models []view.AppValuesModel
	valuesMap := make(map[types.AppId]types.ValuesMap)

	for appId := range appIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			l.Warn(logIdCommands, "skipping values for app '{app}': {err}",
				log.String("app", string(appId)), log.Err(err))
			continue
		}

		vals, err := h.(hydra.Hydra).LoadValuesMap(networkMode)
		if err != nil {
			l.Warn(logIdCommands, "failed to load values for app '{app}': {err}",
				log.String("app", string(appId)), log.Err(err))
			continue
		}

		models = append(models, view.AppValuesModel{AppId: appId})
		valuesMap[appId] = vals
	}

	slices.SortFunc(models, func(a, b view.AppValuesModel) int {
		return cmp.Compare(a.AppId, b.AppId)
	})

	l.Info(logIdCommands, "collected merged values for {count} apps", log.Int("count", len(models)))
	return models, valuesMap
}

// CollectFallbackValues extracts the hydra fallback values (from
// infra_library.fn.hydra.fallback) for each app. Unlike full chart defaults,
// this only returns the hydra-specific fallback values (e.g. global.hydra.*).
func CollectFallbackValues(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
) ([]view.AppValuesModel, map[types.AppId]types.ValuesMap) {
	l := cluster.L()
	var models []view.AppValuesModel
	valuesMap := make(map[types.AppId]types.ValuesMap)

	for appId := range appIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			l.Warn(logIdCommands, "skipping fallback values for app '{app}': {err}",
				log.String("app", string(appId)), log.Err(err))
			continue
		}

		hh := h.(hydra.Hydra)
		var charter helmchart.Charter

		if childApp := hh.AsChildApp(); childApp != nil {
			chartDir, err := childApp.ChildAppPath()
			if err != nil {
				continue
			}
			charter, err = chartDir.LoadChart(cluster.ChartCache(), networkMode)
			if err != nil {
				continue
			}
		} else if rootApp := hh.AsRootApp(); rootApp != nil {
			charter, err = rootApp.RootAppPath().LoadChart(cluster.ChartCache(), networkMode)
			if err != nil {
				continue
			}
		} else {
			continue
		}

		fallback, err := helm.ExtractFallbackValues(l, charter)
		if err != nil {
			l.Warn(logIdCommands, "failed to extract fallback values for app '{app}': {err}",
				log.String("app", string(appId)), log.Err(err))
			continue
		}
		if fallback == nil {
			continue
		}

		models = append(models, view.AppValuesModel{AppId: appId})
		valuesMap[appId] = fallback
	}

	slices.SortFunc(models, func(a, b view.AppValuesModel) int {
		return cmp.Compare(a.AppId, b.AppId)
	})

	l.Info(logIdCommands, "collected fallback values for {count} apps", log.Int("count", len(models)))
	return models, valuesMap
}
