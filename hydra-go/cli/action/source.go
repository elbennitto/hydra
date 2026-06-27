package action

import (
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/highlight"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

const (
	sourceBannerUnrendered = "# hydra local source: Helm chart template sources (unrendered; not valid Kubernetes YAML).\n"
	sourceNoTemplates      = "# (no chart template files match the selection)\n"
)

// SourceFlags holds flags for hydra local source.
type SourceFlags struct {
	flags.ColorFlag
	flags.HelmNetworkModeFlag
	flags.ContextFlag
	flags.ExcludeAppFlag
	flags.NoCacheFlag
	flags.PredicatesFlag
	flags.IncludePathFlag
	AppId types.AppId
}

var _ flags.Flags = (*SourceFlags)(nil)
var _ flags.WithColorFlag = (*SourceFlags)(nil)
var _ flags.WithContextFlag = (*SourceFlags)(nil)
var _ flags.WithExcludeAppFlag = (*SourceFlags)(nil)
var _ flags.WithHelmNetworkModeFlag = (*SourceFlags)(nil)
var _ flags.WithIncludePathFlag = (*SourceFlags)(nil)
var _ flags.WithNoCacheFlag = (*SourceFlags)(nil)
var _ flags.WithPredicatesFlag = (*SourceFlags)(nil)

func (f *SourceFlags) Flags() flags.Flags {
	return f
}

func (f *SourceFlags) WithColorFlag() *flags.ColorFlag {
	return &f.ColorFlag
}

func (f *SourceFlags) WithContextFlag() *flags.ContextFlag {
	return &f.ContextFlag
}

func (f *SourceFlags) WithExcludeAppFlag() *flags.ExcludeAppFlag {
	return &f.ExcludeAppFlag
}

func (f *SourceFlags) WithHelmNetworkModeFlag() *flags.HelmNetworkModeFlag {
	return &f.HelmNetworkModeFlag
}

func (f *SourceFlags) WithIncludePathFlag() *flags.IncludePathFlag {
	return &f.IncludePathFlag
}

func (f *SourceFlags) WithNoCacheFlag() *flags.NoCacheFlag {
	return &f.NoCacheFlag
}

func (f *SourceFlags) WithPredicatesFlag() *flags.PredicatesFlag {
	return &f.PredicatesFlag
}

// Source prints unrendered Helm chart template bodies for the resolved app. Templates are taken
// from the loaded chart (including packaged charts/*.tgz dependencies), not only from loose
// templates/ directories on disk.
// Optional --include-path prefixes filter by Helm template path (OR semantics). When --include or
// --exclude are set, Hydra first applies the same rendered-manifest CEL filtering as
// hydra local template, then prints only the source template files backing the matched manifests.
func Source(f SourceFlags) (hydra.Hydra, string, error) {
	l := log.Default()
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo)
	h, err := resolveHydraAppForLocalPrint(l, f.HydraContext, f.AppId, config, f.HelmNetworkMode)
	if err != nil {
		return nil, "", err
	}
	app := h.AsApp()
	chartDir, err := hydra.ChartDirectoryForHydraApp(app)
	if err != nil {
		return nil, "", err
	}
	prefixes := normalizedSourcePrefixes(f.IncludePathPrefixes)
	noTemplates := false
	if len(f.Predicates) > 0 {
		selected, err := sourceTemplatePathsFromPredicates(h, f)
		if err != nil {
			return nil, "", err
		}
		selected = normalizedSourcePrefixes(selected)
		if len(prefixes) > 0 {
			filtered := selected[:0]
			for _, path := range selected {
				if helm.TemplateSourcePathMatchesAnyPrefix(path, prefixes) {
					filtered = append(filtered, path)
				}
			}
			selected = filtered
		}
		if len(selected) == 0 {
			noTemplates = true
		}
		prefixes = selected
	}
	srcBlock := ""
	if !noTemplates {
		charter, err := chartDir.LoadChart(hydra.ChartCacheForHydraApp(app), f.HelmNetworkMode)
		if err != nil {
			return nil, "", err
		}
		ops, err := hydra.ChartFileOperationsForHydraApp(app, f.HelmNetworkMode)
		if err != nil {
			return nil, "", err
		}
		charter, err = helm.ApplyChartFileOperations(charter, ops)
		if err != nil {
			return nil, "", err
		}
		srcBlock, err = helm.ChartSourceTemplatesMultidoc(charter, prefixes)
		if err != nil {
			return nil, "", err
		}
	}
	if strings.TrimSpace(srcBlock) == "" {
		srcBlock = sourceNoTemplates
	}
	body := srcBlock
	if f.Color {
		colored, err := highlight.GoTemplateTerminal256(f.Color, srcBlock)
		if err != nil {
			return nil, "", err
		}
		body = colored
	}
	out := sourceBannerUnrendered + body
	return h, out, nil
}

func normalizedSourcePrefixes(paths []string) []string {
	prefixes := make([]string, 0, len(paths))
	for _, p := range paths {
		n := helm.NormalizeTemplateSourcePathPrefix(p)
		if idx := strings.Index(n, "/templates/"); idx >= 0 {
			n = n[idx+1:]
		} else if idx := strings.Index(n, "/charts/"); idx >= 0 {
			n = n[idx+1:]
		}
		if n != "" {
			prefixes = append(prefixes, n)
		}
	}
	return prefixes
}

func sourceTemplatePathsFromPredicates(h hydra.Hydra, f SourceFlags) ([]string, error) {
	entities, err := templateSortedEntitiesFromResolved(h, TemplateFlags{
		HelmNetworkModeFlag: f.HelmNetworkModeFlag,
		ContextFlag:         f.ContextFlag,
		ExcludeAppFlag:      f.ExcludeAppFlag,
		PredicatesFlag:      f.PredicatesFlag,
		NoCacheFlag:         f.NoCacheFlag,
		AppId:               f.AppId,
	})
	if err != nil {
		return nil, err
	}
	return uniqueTemplatePaths(entities), nil
}

func uniqueTemplatePaths(entities entity.Entities) []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0, len(entities.Items))
	for _, e := range entities.Items {
		path, err := e.TemplatePath()
		if err != nil || path == "" {
			continue
		}
		p := string(path)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths
}
