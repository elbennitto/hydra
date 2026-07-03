package helm

import (
	"fmt"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	v2chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	"hydra-gitops.org/hydra/hydra-go/core/yaml"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
)

// LoadValues loads and processes values for a chart with given base values
func LoadValuesYaml(l log.Logger, ch chart.Charter, given types.ValuesMap) (types.YamlString, error) {
	valuesMap, err := LoadValuesMap(l, ch, given)
	if err != nil {
		return "", err
	}
	return yaml.ToYaml(valuesMap)
}

// CoalescedValuesMapBeforeRender runs the same CoalesceValues, ProcessDependencies, and
// extractHydraFallbackValues steps as [LoadValuesMap], but stops before chartutil.ToRenderValues.
// This matches the value map Helm's install action expects as the user-supplied "raw" values
// merged with chart defaults (coalesced once). Passing [LoadValuesMap] output into [helm.Template]
// instead would run Coalesce/toRenderValues twice and can drop nested overrides (e.g. webhook DNS).
func CoalescedValuesMapBeforeRender(l log.Logger, ch chart.Charter, given types.ValuesMap) (types.ValuesMap, error) {
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

	valuesMap, err := chartutil.CoalesceValues(ch, given)
	if err != nil {
		return nil, err
	}

	err = v2chartutil.ProcessDependencies(chrt, valuesMap)
	if err != nil {
		return nil, err
	}

	return extractHydraFallbackValues(l, chrt, valuesMap)
}

func LoadValuesMap(l log.Logger, ch chart.Charter, given types.ValuesMap) (types.ValuesMap, error) {
	valuesMap, err := CoalescedValuesMapBeforeRender(l, ch, given)
	if err != nil {
		return nil, err
	}

	options := common.ReleaseOptions{}

	rendered, err := log.WithoutDebug2(func() (map[string]any, error) {
		return chartutil.ToRenderValues(ch, valuesMap, options, nil)
	})

	if err != nil {
		return nil, err
	}

	renderedValues, ok := rendered["Values"]
	if !ok {
		return nil, log.CreateError(errors.ErrValuesFailed, "failed to extract final values from rendered chart")
	}

	renderedValuesMap, ok := renderedValues.(common.Values)
	if !ok {
		return nil, log.CreateError(errors.ErrValuesFailed, "final values have unexpected type '{type}'",
			log.String("type", fmt.Sprintf("%T", renderedValues)))
	}

	cleanup := values.Lookup(renderedValuesMap, "root", "hydra", "values-cleanup")

	if c, ok := cleanup.(string); ok {
		renderedValuesYaml, err := yaml.ToYaml(renderedValuesMap)
		if err != nil {
			return nil, err
		}

		cleaned, err := yq.Yq(renderedValuesYaml, c)
		if err != nil {
			return nil, err
		}
		cleanedValues, err := yaml.FromYaml[types.ValuesMap](cleaned)
		if err != nil {
			return nil, err
		}
		return values.MergeGlobalValues(cleanedValues, "root")
	} else if cleanup != nil {
		return nil, log.CreateError(errors.ErrValuesCleanupFailed, "values-cleanup has unexpected type '{type}'",
			log.String("type", fmt.Sprintf("%T", cleanup)))
	}
	return values.MergeGlobalValues(renderedValuesMap, "root")
}
