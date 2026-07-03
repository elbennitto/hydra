package helm

import (
	"fmt"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/common"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

func extractHydraFallbackValues(l log.Logger, chrt *v2chart.Chart, valuesMap types.ValuesMap) (types.ValuesMap, error) {
	fallback, err := renderFallbackValues(l, chrt)
	if err != nil {
		return nil, err
	}
	if fallback == nil {
		return valuesMap, nil
	}
	return values.MergeValues(fallback, valuesMap), nil
}

// ExtractFallbackValues returns only the hydra fallback values (from
// infra_library.fn.hydra.fallback) without merging them into anything.
// Returns nil when the chart has no infra_library dependency.
func ExtractFallbackValues(l log.Logger, ch chart.Charter) (types.ValuesMap, error) {
	var chrt *v2chart.Chart
	switch c := ch.(type) {
	case *v2chart.Chart:
		chrt = c
	case v2chart.Chart:
		chrt = &c
	default:
		return nil, log.CreateError(errors.ErrLoadingHelmChartFailed, "unsupported chart type '{type}'",
			log.String("type", fmt.Sprintf("%T", ch)))
	}
	return renderFallbackValues(l, chrt)
}

func renderFallbackValues(l log.Logger, chrt *v2chart.Chart) (types.ValuesMap, error) {
	l.DebugLog(logIdHelm, "extracting hydra fallback values for chart '{chart}'", log.String("chart", chrt.Metadata.Name))

	infraLibraryChart := FindDependencyOptional(l, "infra_library", chrt)
	if infraLibraryChart == nil {
		return nil, nil
	}

	infraLibraryChart, err := CloneChart(infraLibraryChart)
	if err != nil {
		return nil, err
	}
	infraLibraryChart.Metadata.Type = "application"

	infraLibraryChart.Templates = append(infraLibraryChart.Templates, &common.File{
		Name: "templates/template-generated-by-hydra-to-extract-fallback-values.yaml",
		Data: []byte(`{{"global:\n  hydra:"}}{{- include "infra_library.fn.hydra.fallback" . | nindent 4 -}}`),
	})

	yamlString, err := TemplateV2Chart(l, infraLibraryChart, RenderChartParams{
		ReleaseName: "hydra-infra-library",
	})
	if err != nil {
		return nil, err
	}

	return values.ParseValuesString(types.YamlString(yamlString))
}

func FindDependencyRequired(
	l log.Logger,
	name string,
	c *v2chart.Chart,
) (*v2chart.Chart, error) {
	result := FindDependencyOptional(l, name, c)
	if result == nil {
		return nil, log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
			"Chart-Dependency '{name}' not found", log.String("name", name))
	}
	return result, nil
}

func FindDependencyOptional(
	l log.Logger,
	name string,
	c *v2chart.Chart,
) *v2chart.Chart {
	result := findDependency(name, c)
	if result == nil {
		l.DebugLog(logIdHelm, "Chart-Dependency '{name}' not found.", log.String("name", name))
	} else {
		l.DebugLog(logIdHelm, "Found Chart-Dependency '{name}' with version {version}",
			log.String("name", result.Name()),
			log.String("version", result.Metadata.Version),
		)
	}
	return result
}

func findDependency(
	name string,
	c *v2chart.Chart,
) *v2chart.Chart {
	for _, d := range c.Metadata.Dependencies {
		for _, chartDep := range c.Dependencies() {
			if chartDep.Metadata.Name == d.Name {
				if d.Name == name {
					return chartDep
				}
				result := findDependency(name, chartDep)
				if result != nil {
					return result
				}
			}
		}
	}
	return nil
}
