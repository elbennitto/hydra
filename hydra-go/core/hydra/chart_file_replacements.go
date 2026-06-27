package hydra

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	yqyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
)

func ChartFileOperationsForHydraApp(h HydraApp, networkMode types.HelmNetworkMode) (helm.ChartFileOperations, error) {
	valuesMap, err := h.LoadValuesMap(networkMode)
	if err != nil {
		return helm.ChartFileOperations{}, err
	}
	valuesMap, err = mergeRootAppHydraValuesIntoGlobal(valuesMap, h.AsRootApp())
	if err != nil {
		return helm.ChartFileOperations{}, err
	}
	valuesYaml, err := yqyaml.ToYaml(valuesMap)
	if err != nil {
		return helm.ChartFileOperations{}, err
	}
	hydraGlobal, err := yqyaml.FromYaml[types.HydraGlobal](valuesYaml)
	if err != nil {
		return helm.ChartFileOperations{}, err
	}
	if hydraGlobal.Global == nil || hydraGlobal.Global.Hydra == nil || hydraGlobal.Global.Hydra.TemplateFiles == nil {
		return helm.ChartFileOperations{}, nil
	}
	if err := types.ValidateHydraTemplateFiles(hydraGlobal.Global.Hydra.TemplateFiles); err != nil {
		return helm.ChartFileOperations{}, err
	}
	templateFiles := hydraGlobal.Global.Hydra.TemplateFiles
	ops := helm.ChartFileOperations{
		Moves:   make([]helm.ChartFileMove, 0, len(templateFiles.Move)),
		Deletes: make([]string, 0, len(templateFiles.Delete)),
	}
	for _, move := range templateFiles.Move {
		ops.Moves = append(ops.Moves, helm.ChartFileMove{
			From: helm.NormalizeChartFileReplacementPath(move.From),
			To:   helm.NormalizeChartFileReplacementPath(move.To),
		})
	}
	for _, path := range templateFiles.Delete {
		ops.Deletes = append(ops.Deletes, helm.NormalizeChartFileReplacementPath(path))
	}
	return ops, nil
}

func TemplateWithChartFileOperations(
	h HydraApp,
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	ops helm.ChartFileOperations,
) (types.YamlString, error) {
	if ops.Empty() {
		return h.Template(networkMode, kubernetesVersionOrFallback)
	}
	if c := h.AsChildApp(); c != nil {
		return c.templateWithChartFileOperations(networkMode, kubernetesVersionOrFallback, ops)
	}
	if r := h.AsRootApp(); r != nil {
		return r.templateWithChartFileOperations(networkMode, kubernetesVersionOrFallback, ops)
	}
	return "", log.CreateError(errors.ErrInvalidHydraStructure, "chart file operations require a root or child app")
}

func (a *RootApp) templateWithChartFileOperations(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	ops helm.ChartFileOperations,
) (types.YamlString, error) {
	return a.templateWithChartFileReplacements(networkMode, kubernetesVersionOrFallback, ops.Replacements, ops)
}

func (a *ChildApp) templateWithChartFileOperations(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	ops helm.ChartFileOperations,
) (types.YamlString, error) {
	return a.templateWithChartFileReplacements(networkMode, kubernetesVersionOrFallback, ops.Replacements, ops)
}

func (a *RootApp) templateWithChartFileReplacements(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	replacements []helm.ChartFileReplacement,
	ops helm.ChartFileOperations,
) (types.YamlString, error) {
	if len(replacements) == 0 {
		if ops.Empty() {
			return a.Template(networkMode, kubernetesVersionOrFallback)
		}
	}
	vals, err := helmChartValuesForTemplate(a.AsApp(), networkMode)
	if err != nil {
		return "", err
	}

	c, err := a.RootAppPath().LoadChart(a.caches.chartCache, networkMode)
	if err != nil {
		return "", err
	}
	if len(replacements) > 0 && len(ops.Replacements) == 0 {
		ops.Replacements = append(ops.Replacements, replacements...)
	}
	c, err = helm.ApplyChartFileOperations(c, ops)
	if err != nil {
		return "", err
	}

	manifest, err := helm.Template(a.l, c, helm.RenderChartParams{
		KubernetesVersionOrFallback: kubernetesVersionOrFallback,
		ReleaseName:                 string(a.RootAppId()),
		Namespace:                   string(argocdNamespace),
		ValuesMap:                   vals,
	})
	if err != nil {
		return "", log.CreateError(
			errors.ErrHydraConfigError,
			"failed to template root app '{app}': {err}",
			log.String("app", string(a.RootAppId())),
			log.Err(err))
	}

	rootPath := a.RootAppPath().Path()
	matches, err := filepath.Glob(filepath.Join(rootPath, "backup-*.sops.yaml"))
	if err != nil {
		return "", err
	}
	slices.Sort(matches)
	var staticSb strings.Builder
	for _, file := range matches {
		fileContent, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		relPath, err := filepath.Rel(rootPath, file)
		if err != nil {
			relPath = filepath.Base(file)
		}
		relPath = filepath.ToSlash(relPath)
		writeStaticManifestChunk(&staticSb, relPath, fileContent)
	}
	if staticSb.Len() > 0 {
		manifest = types.YamlString(string(manifest) + "\n" + staticSb.String())
	}

	return yq.YqPatchArgo(a.l, manifest, a.AppId(), argocdNamespace)
}

func (a *ChildApp) templateWithChartFileReplacements(
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	replacements []helm.ChartFileReplacement,
	ops helm.ChartFileOperations,
) (types.YamlString, error) {
	if len(replacements) == 0 {
		if ops.Empty() {
			return a.Template(networkMode, kubernetesVersionOrFallback)
		}
	}
	vals, err := helmChartValuesForTemplate(a.AsApp(), networkMode)
	if err != nil {
		return "", err
	}

	staticManifests := filepath.Join(a.RootAppPath().Path(), "apps", string(a.ChildAppName))
	files, err := recursiveGlob(staticManifests, "*.yaml")
	if err != nil {
		return "", err
	}

	type staticChunk struct {
		relPath     string
		content     []byte
		k8sDefaults bool
	}
	chunks := make([]staticChunk, 0, len(files))
	for _, file := range files {
		fileContent, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		relPath, err := filepath.Rel(a.RootAppPath().Path(), file)
		if err != nil {
			relPath = filepath.Base(file)
		}
		relPath = filepath.ToSlash(relPath)
		base := path.Base(file)
		chunks = append(chunks, staticChunk{
			relPath:     relPath,
			content:     fileContent,
			k8sDefaults: entity.IsKubernetesDefaultsStaticFilename(base),
		})
	}

	chartPath, err := a.ChildAppPath()
	if err != nil {
		return "", err
	}
	c, err := chartPath.LoadChart(a.caches.chartCache, networkMode)
	if err != nil {
		return "", err
	}
	if len(replacements) > 0 && len(ops.Replacements) == 0 {
		ops.Replacements = append(ops.Replacements, replacements...)
	}
	c, err = helm.ApplyChartFileOperations(c, ops)
	if err != nil {
		return "", err
	}

	namespace, err := a.Namespace(networkMode)
	if err != nil {
		return "", err
	}
	skipCrds, err := a.skipCrdsFromRootAppConfig(networkMode)
	if err != nil {
		return "", err
	}

	manifest, err := helm.Template(a.l, c, helm.RenderChartParams{
		KubernetesVersionOrFallback: kubernetesVersionOrFallback,
		ReleaseName:                 string(a.ChildAppName),
		Namespace:                   string(namespace),
		ValuesMap:                   vals,
		SkipCrds:                    skipCrds,
	})
	if err != nil {
		return "", log.CreateError(
			errors.ErrHelmTemplateFailed,
			"failed to template child app '{app}': {err}",
			log.String("app", string(a.ChildAppId())),
			log.Err(err))
	}

	var nonDefaultsStatic strings.Builder
	for _, ch := range chunks {
		if ch.k8sDefaults {
			continue
		}
		writeStaticManifestChunk(&nonDefaultsStatic, ch.relPath, ch.content)
	}
	combinedForIds := types.YamlString(string(manifest) + "\n" + nonDefaultsStatic.String())
	existingIds, err := entity.IdsFromManifest(a.l, combinedForIds, types.KeyTemplateEntity)
	if err != nil {
		return "", err
	}

	var staticSb strings.Builder
	for _, ch := range chunks {
		body := ch.content
		if ch.k8sDefaults {
			filtered, ferr := entity.FilterKubernetesDefaultsBody(a.l, ch.relPath, ch.content, existingIds, types.KeyTemplateEntity)
			if ferr != nil {
				return "", ferr
			}
			if len(filtered) == 0 {
				continue
			}
			body = filtered
		}
		writeStaticManifestChunk(&staticSb, ch.relPath, body)
	}
	if staticSb.Len() > 0 {
		manifest = types.YamlString(string(manifest) + "\n" + staticSb.String())
	}

	return yq.YqPatchArgo(a.l, manifest, a.AppId(), namespace)
}
