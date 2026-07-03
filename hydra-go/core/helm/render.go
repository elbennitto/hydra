package helm

import (
	"regexp"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/common"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release"
	v1release "helm.sh/helm/v4/pkg/release/v1"
	herrors "hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// RenderChartParams contains the parameters for rendering a Helm chart
type RenderChartParams struct {
	KubernetesVersionOrFallback types.KubernetesVersionOrFallback
	// ReleaseName is the Helm release name
	ReleaseName string
	// Namespace is the Kubernetes namespace for the release
	Namespace string
	// ValuesMap are the Helm values to use for rendering
	ValuesMap types.ValuesMap
	// SkipCrds mirrors Helm's --skip-crds / ArgoCD's helm.skipCrds: omit chart packaged CRDs
	// (crds/ and CRDs included by the install action) from the rendered manifest.
	SkipCrds bool
}

// ExtractTemplateFromResult extracts a single template from a RenderResult by name
func ExtractTemplateFromResult(l log.Logger, manifest types.YamlString, templateName string) (types.YamlString, error) {
	// Split manifest and find the requested template
	manifestMap := SplitManifestMap(manifest)

	if contents, ok := manifestMap[templateName]; ok {
		if len(contents) > 0 {
			return contents[0], nil
		}
	}

	for template, contents := range manifestMap {
		l.DebugLog(logIdHelm, "found template '{template}' with {count} entries",
			log.String("template", template),
			log.Int("count", len(contents)))
	}

	return "", log.CreateError(herrors.ErrTemplateNotFound, "template '{templateName}' not found in manifest",
		log.String("templateName", templateName))
}

// Template renders a chart and returns a manifest
func Template(l log.Logger, chart chart.Charter, params RenderChartParams) (types.YamlString, error) {
	v2chrt, err := convertToV2Chart(chart)
	if err != nil {
		return "", err
	}

	return TemplateV2Chart(l, v2chrt, params)
}

func parseKubeVersion(kubernetesVersionOrFallback types.KubernetesVersionOrFallback) (*common.KubeVersion, error) {
	if kubernetesVersionOrFallback == "" {
		return nil, nil
	}
	ver, err := common.ParseKubeVersion(string(kubernetesVersionOrFallback))
	if err != nil {
		return nil, err
	}
	return ver, nil
}

// TemplateV2Chart renders a v2 chart and returns a manifest
func TemplateV2Chart(l log.Logger, chart *v2chart.Chart, params RenderChartParams) (types.YamlString, error) {
	if chart == nil {
		return "", log.CreateError(herrors.ErrHelmTemplateFailed, "Chart cannot be nil")
	}

	return templateV2Chart(l, chart, params)
}

func templateV2Chart(l log.Logger, chart *v2chart.Chart, params RenderChartParams) (types.YamlString, error) {
	if chart == nil {
		return "", log.CreateError(herrors.ErrLoadingHelmChartFailed, "Chart cannot be nil")
	}

	l.DebugLog(logIdHelm, "running helm template...")

	ver, err := parseKubeVersion(params.KubernetesVersionOrFallback)
	if err != nil {
		return "", err
	}

	cfg := &action.Configuration{}
	client := action.NewInstall(cfg)
	if ver == nil {
		l.DebugLog(logIdHelm, "using Kubernetes version provided by helm...")
	} else {
		l.DebugLog(logIdHelm, "using Kubernetes version {version} provided by user...",
			log.String("version", string(params.KubernetesVersionOrFallback)))
		client.KubeVersion = ver
	}
	client.Namespace = params.Namespace
	client.ReleaseName = params.ReleaseName
	client.SkipCRDs = params.SkipCrds
	// Use DryRunStrategy to skip actual cluster interaction and avoid nil KubeClient
	client.DryRunStrategy = action.DryRunClient

	rel, err := log.WithoutDebug2(func() (release.Releaser, error) {
		return client.Run(chart, params.ValuesMap)
	})
	if err != nil {
		return "", log.CreateError(herrors.ErrHelmTemplateFailed, "Rendering Chart failed: '{err}'", log.Err(err))
	}

	v1rel, err := convertToV1Release(rel)
	if err != nil {
		return "", err
	}

	var manifest strings.Builder
	manifest.WriteString(v1rel.Manifest)

	for _, hook := range v1rel.Hooks {
		if !hookSupportedByArgoCd(hook) {
			continue
		}
		manifest.WriteString("\n---\n# Source: ")
		manifest.WriteString(hook.Path)
		manifest.WriteString("\n")
		manifest.WriteString(hook.Manifest)
	}

	if !params.SkipCrds {
		for _, file := range chart.CRDs() {
			for _, doc := range splitCRDDocuments(file.Data) {
				manifest.WriteString("\n---\n# Source: ")
				manifest.WriteString(file.Name)
				manifest.WriteString("\n")
				manifest.WriteString(doc)
			}
		}
	}

	return types.YamlString(manifest.String()), nil
}

var crdDocSep = regexp.MustCompile(`(?m)^---\s*$`)

// splitCRDDocuments splits a CRD file's raw data into individual YAML documents,
// stripping leading document separators (---) that would break # Source: annotations.
func splitCRDDocuments(data []byte) []string {
	docs := crdDocSep.Split(string(data), -1)
	var result []string
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc != "" {
			result = append(result, doc)
		}
	}
	return result
}

func hookSupportedByArgoCd(hook *v1release.Hook) bool {
	for _, event := range hook.Events {
		if event == v1release.HookPreInstall {
			return true
		}
		if event == v1release.HookPostInstall {
			return true
		}
	}
	return false
}
