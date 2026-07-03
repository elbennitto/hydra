package export

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	v2chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/view"
	hyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
)

var logIdExport = log.Hydra().Child("core").Child("export")

// ValidateDir checks that the output directory is usable, without modifying anything.
// Call this early (before expensive rendering) to fail fast on invalid paths.
// Rules:
//   - If dir does not exist: parent must exist.
//   - If dir exists: must be a directory containing hydra.yaml, manifests/, and charts/.
//   - Otherwise: returns an error.
func ValidateDir(dir string) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		parentDir := filepath.Dir(dir)
		parentInfo, parentErr := os.Stat(parentDir)
		if parentErr != nil || !parentInfo.IsDir() {
			return fmt.Errorf("parent directory %q does not exist", parentDir)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot stat %q: %w", dir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%q exists but is not a directory", dir)
	}

	for _, required := range []string{"hydra.yaml", "manifests", "charts", "values"} {
		p := filepath.Join(dir, required)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return fmt.Errorf("%q is a directory but does not contain %q", dir, required)
		}
	}

	return nil
}

// PrepareDir creates or clears the output directory so it is ready for writing.
// If the directory already exists, only hydra.yaml, manifests/ and charts/ are removed;
// all other contents are left untouched.
// Must only be called after ValidateDir succeeded.
func PrepareDir(dir string) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	if err != nil {
		return fmt.Errorf("cannot stat %q: %w", dir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%q exists but is not a directory", dir)
	}

	for _, name := range []string{"hydra.yaml", "manifests", "charts", "values"} {
		p := filepath.Join(dir, name)
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("failed to remove %q: %w", p, err)
		}
	}

	return nil
}

// ValidateContextDir checks that a context-level output directory is usable.
// This is the top-level directory that will contain one subdirectory per cluster.
// Rules:
//   - If dir does not exist: parent must exist.
//   - If dir exists: must be a directory.
//   - If dir exists and has subdirectories with hydra.yaml: validate each as a cluster dump via ValidateDir.
//   - Empty dir or dir with non-dump subdirs is valid.
func ValidateContextDir(dir string) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		parentDir := filepath.Dir(dir)
		parentInfo, parentErr := os.Stat(parentDir)
		if parentErr != nil || !parentInfo.IsDir() {
			return fmt.Errorf("parent directory %q does not exist", parentDir)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot stat %q: %w", dir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%q exists but is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subdir := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(subdir, "hydra.yaml")); os.IsNotExist(err) {
			continue
		}
		if err := ValidateDir(subdir); err != nil {
			return err
		}
	}

	return nil
}

// ValidateAndPrepareDir validates and then prepares the output directory in one call.
func ValidateAndPrepareDir(dir string) error {
	if err := ValidateDir(dir); err != nil {
		return err
	}
	return PrepareDir(dir)
}

// WriteHydraYaml marshals the DependenciesModel to YAML and writes it to <dir>/hydra.yaml.
func WriteHydraYaml(dir string, model view.DependenciesModel) error {
	data, err := yaml.Marshal(model)
	if err != nil {
		return fmt.Errorf("failed to marshal hydra.yaml: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "hydra.yaml"), data, 0644)
}

// WriteManifests writes rendered manifest YAML files to <dir>/manifests/<manifestPath>.
func WriteManifests(l log.Logger, dir string, manifests map[string][]byte) error {
	manifestsDir := filepath.Join(dir, "manifests")
	written := 0
	for manifestPath, content := range manifests {
		if manifestPath == "" {
			continue
		}
		fullPath := filepath.Join(manifestsDir, manifestPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %q: %w", fullPath, err)
		}
		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write manifest %q: %w", fullPath, err)
		}
		written++
	}
	l.Info(logIdExport, "wrote {count} manifest files", log.Int("count", written))
	return nil
}

// WriteCharts saves chart archives as <dir>/charts/<name>.tgz.
// Uses Helm's built-in Save and renames to <name>.tgz.
func WriteCharts(l log.Logger, dir string, charts map[string]*v2chart.Chart) error {
	chartsDir := filepath.Join(dir, "charts")
	if err := os.MkdirAll(chartsDir, 0755); err != nil {
		return fmt.Errorf("failed to create charts directory: %w", err)
	}

	written := 0
	for name, chrt := range charts {
		savedPath, err := v2chartutil.Save(chrt, chartsDir)
		if err != nil {
			l.Warn(logIdExport, "failed to save chart '{name}': {err}",
				log.String("name", name), log.Err(err))
			continue
		}

		targetPath := filepath.Join(chartsDir, name+".tgz")
		if savedPath != targetPath {
			if err := os.Rename(savedPath, targetPath); err != nil {
				return fmt.Errorf("failed to rename chart %q to %q: %w", savedPath, targetPath, err)
			}
		}

		l.DebugLog(logIdExport, "wrote chart '{name}' to '{path}'",
			log.String("name", name), log.String("path", targetPath))
		written++
	}
	l.Info(logIdExport, "wrote {count} chart archives", log.Int("count", written))
	return nil
}

// WriteValueFiles copies values files from the GitOps repository into
// <dir>/values/files/<relative-path>. The source paths are resolved via
// contextParentPath + valueFile.Path.
func WriteValueFiles(l log.Logger, dir string, valueFiles []view.ValueFileModel, contextParentPath string) error {
	filesDir := filepath.Join(dir, "values", "files")
	written := 0

	for _, vf := range valueFiles {
		srcPath := filepath.Join(contextParentPath, vf.Path)
		data, err := os.ReadFile(srcPath)
		if err != nil {
			l.Warn(logIdExport, "skipping value file '{path}': {err}",
				log.String("path", srcPath), log.Err(err))
			continue
		}

		destPath := filepath.Join(filesDir, vf.Path)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for value file %q: %w", destPath, err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write value file %q: %w", destPath, err)
		}
		written++
	}

	l.Info(logIdExport, "wrote {count} value files", log.Int("count", written))
	return nil
}

// WriteMergedValues writes the fully merged values for each app as
// <dir>/values/merged/<appId>.yaml.
func WriteMergedValues(l log.Logger, dir string, appValues map[types.AppId]types.ValuesMap) error {
	mergedDir := filepath.Join(dir, "values", "merged")
	if err := os.MkdirAll(mergedDir, 0755); err != nil {
		return fmt.Errorf("failed to create merged values directory: %w", err)
	}

	written := 0
	for appId, vals := range appValues {
		yamlStr, err := hyaml.ToYaml(vals)
		data := []byte(yamlStr)
		if err != nil {
			l.Warn(logIdExport, "failed to marshal values for app '{app}': {err}",
				log.String("app", string(appId)), log.Err(err))
			continue
		}

		filePath := filepath.Join(mergedDir, string(appId)+".yaml")
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return fmt.Errorf("failed to write merged values %q: %w", filePath, err)
		}
		written++
	}

	l.Info(logIdExport, "wrote {count} merged values files", log.Int("count", written))
	return nil
}

// WriteFallbackValues writes the hydra fallback values for each app as
// <dir>/values/fallback/<appId>.yaml. These contain only the hydra-specific
// fallback values (e.g. global.hydra.*) from infra_library.fn.hydra.fallback.
func WriteFallbackValues(l log.Logger, dir string, fallbackValues map[types.AppId]types.ValuesMap) error {
	fallbackDir := filepath.Join(dir, "values", "fallback")
	if err := os.MkdirAll(fallbackDir, 0755); err != nil {
		return fmt.Errorf("failed to create fallback values directory: %w", err)
	}

	written := 0
	for appId, vals := range fallbackValues {
		yamlStr, err := hyaml.ToYaml(vals)
		data := []byte(yamlStr)
		if err != nil {
			l.Warn(logIdExport, "failed to marshal fallback values for app '{app}': {err}",
				log.String("app", string(appId)), log.Err(err))
			continue
		}

		filePath := filepath.Join(fallbackDir, string(appId)+".yaml")
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return fmt.Errorf("failed to write fallback values %q: %w", filePath, err)
		}
		written++
	}

	l.Info(logIdExport, "wrote {count} fallback values files", log.Int("count", written))
	return nil
}

// WriteClusterDump exports the full cluster dump to a directory.
// The caller must have called ValidateDir beforehand to fail fast;
// this function calls PrepareDir (clear/create) right before writing.
func WriteClusterDump(
	l log.Logger,
	dir string,
	model view.DependenciesModel,
	charts map[string]*v2chart.Chart,
	manifests map[string][]byte,
	valueFiles []view.ValueFileModel,
	contextParentPath string,
	appValues map[types.AppId]types.ValuesMap,
	fallbackValues map[types.AppId]types.ValuesMap,
) error {
	if err := PrepareDir(dir); err != nil {
		return log.CreateError(errors.ErrWriteFailed, "failed to prepare output directory: {err}", log.Err(err))
	}

	if err := WriteHydraYaml(dir, model); err != nil {
		return log.CreateError(errors.ErrWriteFailed, "failed to write hydra.yaml: {err}", log.Err(err))
	}
	l.Info(logIdExport, "wrote hydra.yaml to '{dir}'", log.String("dir", dir))

	if err := WriteCharts(l, dir, charts); err != nil {
		return log.CreateError(errors.ErrWriteFailed, "failed to write charts: {err}", log.Err(err))
	}

	if err := WriteManifests(l, dir, manifests); err != nil {
		return log.CreateError(errors.ErrWriteFailed, "failed to write manifests: {err}", log.Err(err))
	}

	if err := WriteValueFiles(l, dir, valueFiles, contextParentPath); err != nil {
		return log.CreateError(errors.ErrWriteFailed, "failed to write value files: {err}", log.Err(err))
	}

	if err := WriteMergedValues(l, dir, appValues); err != nil {
		return log.CreateError(errors.ErrWriteFailed, "failed to write merged values: {err}", log.Err(err))
	}

	if fallbackValues != nil {
		if err := WriteFallbackValues(l, dir, fallbackValues); err != nil {
			return log.CreateError(errors.ErrWriteFailed, "failed to write fallback values: {err}", log.Err(err))
		}
	}

	return nil
}
