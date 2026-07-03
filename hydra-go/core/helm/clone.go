package helm

import (
	"slices"

	"helm.sh/helm/v4/pkg/chart/common"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/base/utils"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yaml"
)

func cloneFile(f *common.File) *common.File {
	return &common.File{
		Name:    f.Name,
		Data:    slices.Clone(f.Data),
		ModTime: f.ModTime,
	}
}

func cloneFileSlice(f []*common.File) []*common.File {
	cloned := make([]*common.File, len(f))
	for i, file := range f {
		cloned[i] = cloneFile(file)
	}
	return cloned
}

func cloneValues(v types.ValuesMap) (types.ValuesMap, error) {
	if v == nil {
		return nil, nil
	}

	yamlString, err := yaml.ToYaml(v)
	if err != nil {
		return nil, err
	}

	v, err = yaml.FromYaml[types.ValuesMap](yamlString)
	if err != nil {
		return nil, err
	}

	return v, nil
}

func CloneChart(c *v2chart.Chart) (*v2chart.Chart, error) {
	values, err := cloneValues(c.Values)
	if err != nil {
		return nil, err
	}

	result := &v2chart.Chart{
		Raw:           cloneFileSlice(c.Raw),
		Metadata:      utils.Clone(c.Metadata),
		Lock:          utils.Clone(c.Lock),
		Templates:     cloneFileSlice(c.Templates),
		Values:        values,
		Schema:        slices.Clone(c.Schema),
		SchemaModTime: c.SchemaModTime,
		Files:         cloneFileSlice(c.Files),
		ModTime:       c.ModTime,
	}

	for _, dep := range c.Dependencies() {
		clonedDep, err := CloneChart(dep)
		if err != nil {
			return nil, err
		}
		result.AddDependency(clonedDep)
	}

	return result, nil
}
