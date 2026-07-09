package helm

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release"
	v1release "helm.sh/helm/v4/pkg/release/v1"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/utils"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// ChartDirectory is an interface for different types of chart directories
type ChartDirectory interface {
	// Path returns the absolute path to the chart directory
	Path() string
	// ChartYamlPath returns the path to Chart.yaml
	ChartYamlPath() string
	// ValuesYamlPath returns the path to values.yaml
	ValuesYamlPath() string
	// TemplatesDir returns the path to the templates directory
	TemplatesDir() string
	// ChartsDir returns the path to the charts directory
	ChartsDir() string
	// Cleanup removes the chart directory if needed
	Cleanup() error
	// Use helm to load the chart with caching based on path and mode
	LoadChart(cache *ChartCache, mode types.HelmNetworkMode) (chart.Charter, error)
}

// CreateTemporaryChartDirectory creates a temporary chart directory
func CreateTemporaryChartDirectory(l log.Logger) (ChartDirectory, error) {
	// Create temporary directory
	randomName := fmt.Sprintf("values.tmp-%d", rand.Intn(1000000))
	tmpDir := filepath.Join(".", ".hydra", randomName)

	err := os.MkdirAll(tmpDir, 0755)
	if err != nil {
		return nil, log.CreateError(errors.ErrCreateTempDirFailed, "error creating temporary directory",
			log.Err(err))
	}

	return NewTemporaryChartDirectory(l, tmpDir), nil
}

// TemporaryChartDirectory represents a temporary chart directory that can be cleaned up
type TemporaryChartDirectory struct {
	PersistentChartDirectory
}

// NewTemporaryChartDirectory creates a new temporary chart directory
func NewTemporaryChartDirectory(l log.Logger, path string) TemporaryChartDirectory {
	return TemporaryChartDirectory{
		PersistentChartDirectory: PersistentChartDirectory{l: l, path: path},
	}
}

// Cleanup removes the temporary chart directory
func (t TemporaryChartDirectory) Cleanup() error {
	if t.path == "" {
		return fmt.Errorf("Cleanup failed: missing path")
	}
	return os.RemoveAll(t.path)
}

// PersistentChartDirectory represents a persistent chart directory that should not be cleaned up
type PersistentChartDirectory struct {
	l    log.Logger
	path string
}

// NewChartDirectory creates a new persistent chart directory
func NewChartDirectory(l log.Logger, path string) PersistentChartDirectory {
	return PersistentChartDirectory{l: l, path: path}
}

// Path returns the absolute path to the persistent chart directory
func (p PersistentChartDirectory) Path() string {
	return p.path
}

// ChartYamlPath returns the path to Chart.yaml
func (p PersistentChartDirectory) ChartYamlPath() string {
	return filepath.Join(p.path, "Chart.yaml")
}

// ValuesYamlPath returns the path to values.yaml
func (p PersistentChartDirectory) ValuesYamlPath() string {
	return filepath.Join(p.path, "values.yaml")
}

// TemplatesDir returns the path to the templates directory
func (p PersistentChartDirectory) TemplatesDir() string {
	return filepath.Join(p.path, "templates")
}

// ChartsDir returns the path to the charts directory
func (p PersistentChartDirectory) ChartsDir() string {
	return filepath.Join(p.path, "charts")
}

// Cleanup is a no-op for persistent chart directories
func (p PersistentChartDirectory) Cleanup() error {
	return nil
}

func (p PersistentChartDirectory) LoadChart(
	cache *ChartCache,
	mode types.HelmNetworkMode,
) (chart.Charter, error) {
	if cache == nil {
		return nil, log.CreateError(errors.ErrLoadingHelmChartFailed, "LoadChart failed: missing cache")
	}

	return cache.GetOrLoad(p.path, mode, func() ChartCacheEntry {
		return p.loadChart(mode)
	})
}

func (p PersistentChartDirectory) loadChart(
	mode types.HelmNetworkMode,
) ChartCacheEntry {
	var ownChart chart.Charter
	var err error

	p.l.DebugLog(logIdChartDirectory, "loading chart in {mode} mode from path '{path}'",
		log.String("mode", mode.String()),
		log.String("path", p.path))

	ownChart, err = loader.Load(p.path)
	if err != nil {
		return ChartCacheEntry{
			Error: log.CreateError(errors.ErrLoadingHelmChartFailed, "failed to load chart from path '{path}' in '{mode}' mode",
				log.String("path", p.path),
				log.String("chartYamlPath", filepath.Join(p.path, "Chart.yaml")),
				log.String("mode", mode.String()),
				log.Err(err)),
		}
	}

	chrt, err := convertToV2Chart(ownChart)
	if err != nil {
		return ChartCacheEntry{
			Error: err,
		}
	}

	missingCharts := dumpDependencies(p.l, mode, "", chrt, p.Path(), map[string]*v2chart.Chart{})

	if mode == types.HelmNetworkModeOnline && len(missingCharts) > 0 {
		for path, missingChart := range missingCharts {
			err := DownloadChartDependencies(p.l, path, missingChart)
			if err != nil {
				return ChartCacheEntry{
					Error: log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed, "failed to download missing chart dependencies for chart in path '{path}'",
						log.String("path", path),
						log.Err(err)),
				}
			}
		}
		return p.loadChart(types.HelmNetworkModeOffline)
	}

	if mode == types.HelmNetworkModeOffline && len(missingCharts) > 0 {
		for dir, missingChart := range missingCharts {
			p.l.Error(logIdHelm, "unresolved missing dependencies for chart '{chart}' in directory '{dir}'",
				log.String("chart", missingChart.Name()),
				log.String("dir", dir),
			)
		}
	}

	return ChartCacheEntry{
		Charter: ownChart,
		Error:   err,
	}
}

func dumpDependencies(
	l log.Logger,
	mode types.HelmNetworkMode,
	indent string,
	c *v2chart.Chart,
	chartsDir string,
	missingCharts map[string]*v2chart.Chart,
) map[string]*v2chart.Chart {
	if mode == types.HelmNetworkModeLocal && !isLocalChart(chartsDir) {
		l.DebugLog(logIdHelm, "{mode} mode: skipping remote chart dependency '{dep}' with repository '{repo}'",
			log.String("mode", mode.String()),
			log.String("repo", chartsDir),
			log.String("dep", c.Name()),
		)
		return missingCharts
	}

	l.DebugLog(logIdHelm, "Found Chart-Dependency{indent} '{name}' with version {version} from {path}",
		log.String("indent", indent),
		log.String("name", c.Name()),
		log.String("version", c.Metadata.Version),
		log.String("path", chartsDir),
	)
	missing := false
	dependencies := []*v2chart.Chart{}
	for _, d := range c.Metadata.Dependencies {
		keep := true
		dir := d.Repository

		var dep *v2chart.Chart = nil
		for _, chartDep := range c.Dependencies() {
			if chartDep.Metadata.Name == d.Name {
				dep = chartDep
				break
			}
		}

		if dep == nil {
			switch mode {
			case types.HelmNetworkModeLocal:
				// just ignore missing dependencies in local mode

			case types.HelmNetworkModeOnline:
				missing = true

			case types.HelmNetworkModeOffline:
				missing = true
				l.DebugLog(logIdHelm, "missing dependency '{dep}' with repository '{repo}' of chart '{chart}' in directory '{dir}'",
					log.String("dep", d.Name),
					log.String("repo", dir),
					log.String("chart", c.Name()),
					log.String("dir", chartsDir),
				)
			}
		} else if !isLocalChart(dir) {
			if mode == types.HelmNetworkModeLocal {
				keep = false
				l.DebugLog(logIdHelm, "{mode} mode: skipping remote chart dependency '{dep}' with repository '{repo}' of chart '{chart}'",
					log.String("mode", mode.String()),
					log.String("repo", dir),
					log.String("dep", d.Name),
					log.String("chart", c.Name()),
				)
			} else {
				missingCharts = dumpDependencies(l, mode, indent+"  ", dep, dir, missingCharts)
			}
		} else {
			dir = filepath.Join(chartsDir, utils.FileUriToPath(dir))
			missingCharts = dumpDependencies(l, mode, indent+"  ", dep, dir, missingCharts)
		}
		if keep && dep != nil {
			dependencies = append(dependencies, dep)
		}
	}
	if missing {
		missingCharts[chartsDir] = c
	}
	c.SetDependencies(dependencies...)
	return missingCharts
}

func isLocalChart(chartsDir string) bool {
	return strings.HasPrefix(chartsDir, "file:") || strings.HasPrefix(chartsDir, "/")
}

// convertToV2Chart converts a chart to v2 Chart type for dependency checking
func convertToV2Chart(ownChart chart.Charter) (*v2chart.Chart, error) {
	switch c := ownChart.(type) {
	case *v2chart.Chart:
		return c, nil
	case v2chart.Chart:
		return &c, nil
	default:
		return nil, log.CreateError(errors.ErrLoadingHelmChartFailed, "unsupported chart type '{type}'",
			log.String("type", fmt.Sprintf("%T", ownChart)))
	}
}

// convertToV1Release converts a chart to v2 Chart type for release operations
func convertToV1Release(ownChart release.Releaser) (*v1release.Release, error) {
	switch c := ownChart.(type) {
	case *v1release.Release:
		return c, nil
	case v1release.Release:
		return &c, nil
	default:
		return nil, log.CreateError(errors.ErrLoadingHelmChartFailed, "unsupported release type '{type}'",
			log.String("type", fmt.Sprintf("%T", ownChart)))
	}
}
