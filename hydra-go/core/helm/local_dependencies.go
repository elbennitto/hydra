package helm

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// EnsureLocalChartDependencies verifies that a chart can resolve all declared
// dependencies from local chart content only. It does not download anything.
func EnsureLocalChartDependencies(l log.Logger, chartPath string) error {
	chrt, err := log.WithoutDebug2(func() (chart.Charter, error) {
		return loader.Load(chartPath)
	})
	if err != nil {
		return fmt.Errorf("load chart: %w", err)
	}

	v2, err := convertToV2Chart(chrt)
	if err != nil {
		return err
	}
	if v2.Metadata == nil || len(v2.Metadata.Dependencies) == 0 {
		return nil
	}

	missingCharts := dumpDependencies(l, types.HelmNetworkModeOffline, "", v2, chartPath, map[string]*v2chart.Chart{})
	if len(missingCharts) == 0 {
		return nil
	}

	missingPaths := make([]string, 0, len(missingCharts))
	for path, ch := range missingCharts {
		names := make([]string, 0, len(ch.Metadata.Dependencies))
		for _, dep := range ch.Metadata.Dependencies {
			names = append(names, dep.Name)
		}
		sort.Strings(names)
		missingPaths = append(missingPaths, fmt.Sprintf("%s (chart=%s, deps=%s)", filepath.Clean(path), ch.Name(), strings.Join(names, ",")))
	}
	sort.Strings(missingPaths)

	return fmt.Errorf("missing local chart dependencies: %s; run `hydra ci run download` first", strings.Join(missingPaths, "; "))
}
