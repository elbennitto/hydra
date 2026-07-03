package commands

import (
	"cmp"
	"path/filepath"
	"slices"

	"helm.sh/helm/v4/pkg/chart"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/view"
	"k8s.io/apimachinery/pkg/util/sets"
)

// CollectChartInfo loads the Helm chart for each app and extracts metadata
// (name, version, appVersion, dependencies) into a sorted slice of ChartModels.
// Sub-chart dependencies are recursively collected and added as separate entries
// (with empty AppId) so the UI can resolve transitive dependency trees.
// When gitRepoRoot is non-empty, ChartPath is set to the chart directory
// relative to the git repository root.
func CollectChartInfo(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	gitRepoRoot string,
) []view.ChartModel {
	l := cluster.L()
	var charts []view.ChartModel
	seen := make(map[string]bool)

	for appId := range appIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			l.Warn(logIdCommands, "skipping chart info for app '{app}': {err}",
				log.String("app", string(appId)), log.Err(err))
			continue
		}

		var charter chart.Charter
		var chartDirPath string

		if childApp := h.(hydra.Hydra).AsChildApp(); childApp != nil {
			chartDir, err := childApp.ChildAppPath()
			if err != nil {
				l.Warn(logIdCommands, "skipping chart info for child app '{app}': {err}",
					log.String("app", string(appId)), log.Err(err))
				continue
			}
			chartDirPath = chartDir.Path()
			charter, err = chartDir.LoadChart(cluster.ChartCache(), networkMode)
			if err != nil {
				l.Warn(logIdCommands, "skipping chart info for child app '{app}': {err}",
					log.String("app", string(appId)), log.Err(err))
				continue
			}
		} else if rootApp := h.(hydra.Hydra).AsRootApp(); rootApp != nil {
			chartDirPath = rootApp.RootAppPath().Path()
			charter, err = rootApp.RootAppPath().LoadChart(cluster.ChartCache(), networkMode)
			if err != nil {
				l.Warn(logIdCommands, "skipping chart info for root app '{app}': {err}",
					log.String("app", string(appId)), log.Err(err))
				continue
			}
		}

		chrt := toV2Chart(charter)
		if chrt == nil || chrt.Metadata == nil {
			continue
		}

		model := view.ChartModel{
			AppId:      appId,
			Name:       chrt.Metadata.Name,
			Version:    chrt.Metadata.Version,
			AppVersion: chrt.Metadata.AppVersion,
		}

		if gitRepoRoot != "" && chartDirPath != "" {
			if rel, err := filepath.Rel(gitRepoRoot, chartDirPath); err == nil {
				model.ChartPath = filepath.ToSlash(rel)
			}
		}

		for _, dep := range chrt.Metadata.Dependencies {
			model.Dependencies = append(model.Dependencies, view.ChartDependencyModel{
				Name:       dep.Name,
				Version:    dep.Version,
				Repository: dep.Repository,
			})
		}
		slices.SortFunc(model.Dependencies, func(a, b view.ChartDependencyModel) int {
			return cmp.Or(
				cmp.Compare(a.Name, b.Name),
				cmp.Compare(a.Version, b.Version),
			)
		})

		charts = append(charts, model)
		seen[chrt.Metadata.Name+"@"+chrt.Metadata.Version] = true

		collectSubCharts(chrt, &charts, seen)
	}

	slices.SortFunc(charts, func(a, b view.ChartModel) int {
		return cmp.Or(
			cmp.Compare(a.AppId, b.AppId),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Version, b.Version),
		)
	})

	return charts
}

// collectSubCharts recursively walks loaded sub-charts and adds them as
// ChartModel entries (with empty AppId). Prevents infinite loops via the seen set.
func collectSubCharts(chrt *v2chart.Chart, charts *[]view.ChartModel, seen map[string]bool) {
	for _, dep := range chrt.Dependencies() {
		if dep.Metadata == nil {
			continue
		}
		key := dep.Metadata.Name + "@" + dep.Metadata.Version
		if seen[key] {
			continue
		}
		seen[key] = true

		model := view.ChartModel{
			Name:       dep.Metadata.Name,
			Version:    dep.Metadata.Version,
			AppVersion: dep.Metadata.AppVersion,
		}
		for _, d := range dep.Metadata.Dependencies {
			model.Dependencies = append(model.Dependencies, view.ChartDependencyModel{
				Name:       d.Name,
				Version:    d.Version,
				Repository: d.Repository,
			})
		}
		slices.SortFunc(model.Dependencies, func(a, b view.ChartDependencyModel) int {
			return cmp.Or(
				cmp.Compare(a.Name, b.Name),
				cmp.Compare(a.Version, b.Version),
			)
		})

		*charts = append(*charts, model)
		collectSubCharts(dep, charts, seen)
	}
}

// CollectChartObjects loads and returns chart objects keyed by chart name.
// Used to save charts as .tgz archives in the cluster dump.
func CollectChartObjects(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
) map[string]*v2chart.Chart {
	l := cluster.L()
	result := make(map[string]*v2chart.Chart)

	for appId := range appIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			continue
		}

		var charter chart.Charter

		if childApp := h.(hydra.Hydra).AsChildApp(); childApp != nil {
			chartDir, err := childApp.ChildAppPath()
			if err != nil {
				continue
			}
			charter, err = chartDir.LoadChart(cluster.ChartCache(), networkMode)
			if err != nil {
				continue
			}
		} else if rootApp := h.(hydra.Hydra).AsRootApp(); rootApp != nil {
			charter, err = rootApp.RootAppPath().LoadChart(cluster.ChartCache(), networkMode)
			if err != nil {
				continue
			}
		}

		chrt := toV2Chart(charter)
		if chrt == nil || chrt.Metadata == nil {
			continue
		}

		if _, exists := result[chrt.Metadata.Name]; !exists {
			result[chrt.Metadata.Name] = chrt
			l.DebugLog(logIdCommands, "collected chart object '{name}' v{version}",
				log.String("name", chrt.Metadata.Name),
				log.String("version", chrt.Metadata.Version))
		}
	}

	return result
}

func toV2Chart(charter chart.Charter) *v2chart.Chart {
	if charter == nil {
		return nil
	}
	switch c := charter.(type) {
	case *v2chart.Chart:
		return c
	case v2chart.Chart:
		return &c
	default:
		return nil
	}
}
