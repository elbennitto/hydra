package ci

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	herrors "hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
)

const testValuesFileName = "values.test.yaml"

var (
	downloadChartDependencies = helm.DownloadChartDependencies
	ensureLocalDependencies   = helm.EnsureLocalChartDependencies
)

// Mode represents the execution mode for a CI pipeline step.
type Mode string

const (
	ModeDryRun Mode = "dry-run"
	ModeLocal  Mode = "local"
	ModeCI     Mode = "ci"
)

func RunTest(configPath string, mode Mode) error {
	l := log.Default()
	return runChangedChartPipeline(configPath, mode, "test", herrors.ErrCiTest, func(rel string) {
		l.Info(logIdCI, "ci test dry-run: would verify local dependencies, then run helm lint and helm template for {path}",
			log.String("path", rel))
	}, func(repo *git.Repo, rel, absDir string) error {
		if err := validateChartVersionForTest(repo, rel); err != nil {
			return err
		}

		if err := ensureLocalDependencies(l, absDir); err != nil {
			return fmt.Errorf("chart %s: dependency check: %w", rel, err)
		}

		testValuesArgs, err := optionalTestValuesArgs(absDir)
		if err != nil {
			return fmt.Errorf("chart %s: %w", rel, err)
		}

		chartName, err := chartNameForTest(repo, rel)
		if err != nil {
			return err
		}

		ctx := context.Background()
		lintArgs := append([]string{"lint", "."}, testValuesArgs...)
		out, err := helmRun(ctx, absDir, lintArgs...)
		if err != nil {
			return fmt.Errorf("chart %s: helm lint: %w\n%s", rel, err, string(out))
		}

		templateArgs := append([]string{"template", chartName, "."}, testValuesArgs...)
		out, err = helmRun(ctx, absDir, templateArgs...)
		if err != nil {
			return fmt.Errorf("chart %s: helm template: %w\n%s", rel, err, string(out))
		}

		l.Info(logIdCI, "ci test validated chart {path}", log.String("path", rel))
		return nil
	}, true, func(successCount, failureCount int) {
		l.Info(logIdCI, "ci test summary: {successful} chart(s) successful, {failed} chart(s) failed",
			log.Int("successful", successCount),
			log.Int("failed", failureCount),
		)
	})
}

func RunDownload(configPath string, mode Mode) error {
	l := log.Default()
	registryConfigPath := ""
	cleanup := func() {}
	if mode != ModeDryRun {
		var err error
		registryConfigPath, cleanup, err = prepareRegistryAuth(configPath)
		if err != nil {
			return log.CreateError(herrors.ErrCiDownload, "ci download: prepare OCI registry auth: {err}",
				log.Err(err),
			)
		}
	}
	defer cleanup()

	return runChangedChartPipeline(configPath, mode, "download", herrors.ErrCiDownload, func(rel string) {
		l.Info(logIdCI, "ci download dry-run: would fetch dependencies for chart {path}",
			log.String("path", rel))
	}, func(_ *git.Repo, rel, absDir string) error {
		if err := downloadChartDependencies(l, absDir, nil, registryConfigPath); err != nil {
			return fmt.Errorf("chart %s: dependency download: %w", rel, extendDownloadAuthError(configPath, err, registryConfigPath))
		}
		l.Info(logIdCI, "ci download fetched dependencies for chart {path}", log.String("path", rel))
		return nil
	}, false, nil)
}

func runChangedChartPipeline(
	configPath string,
	mode Mode,
	pipelineName string,
	pipelineErrId herrors.ErrorId,
	logDryRun func(rel string),
	runChart func(repo *git.Repo, rel, absDir string) error,
	collectErrors bool,
	logSummary func(successCount, failureCount int),
) error {
	l := log.Default()
	dir := filepath.Dir(configPath)
	cfg, err := LoadConfig(dir)
	if err != nil {
		return log.CreateError(pipelineErrId, "ci {pipeline}: load config: {err}",
			log.String("pipeline", pipelineName),
			log.Err(err),
		)
	}

	absChartsPath, err := filepath.Abs(filepath.Join(dir, cfg.CI.RootAppsPath))
	if err != nil {
		return log.CreateError(pipelineErrId, "ci {pipeline}: resolve charts path: {err}",
			log.String("pipeline", pipelineName),
			log.Err(err),
		)
	}

	repo := git.Open(absChartsPath)
	if repo.Err != nil {
		return log.CreateError(pipelineErrId, "ci {pipeline}: open repo: {err}",
			log.String("pipeline", pipelineName),
			log.Err(repo.Err),
		)
	}

	relChartsPath, err := filepath.Rel(repo.Path(), absChartsPath)
	if err != nil {
		return log.CreateError(pipelineErrId, "ci {pipeline}: relativize charts path: {err}",
			log.String("pipeline", pipelineName),
			log.Err(err),
		)
	}
	cfg.CI.RootAppsPath = filepath.ToSlash(relChartsPath)

	chartRelPaths, err := changedChartPathsForTest(repo, cfg)
	if err != nil {
		return log.CreateError(pipelineErrId, "ci {pipeline}: detect changed charts: {err}",
			log.String("pipeline", pipelineName),
			log.Err(err),
		)
	}
	if len(chartRelPaths) == 0 {
		l.Info(logIdCI, "ci {pipeline}: no changed charts detected", log.String("pipeline", pipelineName))
		return nil
	}

	var chartErrors []error
	successCount := 0
	for _, rel := range chartRelPaths {
		absDir := filepath.Join(repo.Path(), filepath.FromSlash(rel))
		if mode == ModeDryRun {
			logDryRun(rel)
			continue
		}
		if err := runChart(repo, rel, absDir); err != nil {
			pipelineErr := log.CreateError(pipelineErrId, "ci {pipeline} failed for chart {path}: {err}",
				log.String("pipeline", pipelineName),
				log.String("path", rel),
				log.Err(err),
			)
			if !collectErrors {
				return pipelineErr
			}
			l.Error(logIdCI, "ci {pipeline} failed for chart {path}: {err}",
				log.String("pipeline", pipelineName),
				log.String("path", rel),
				log.String("err", pipelineErr.Error()),
			)
			chartErrors = append(chartErrors, pipelineErr)
			continue
		}
		successCount++
	}

	if mode != ModeDryRun && logSummary != nil {
		logSummary(successCount, len(chartErrors))
	}

	if len(chartErrors) == 1 {
		return chartErrors[0]
	}
	if len(chartErrors) > 1 {
		return log.CreateError(pipelineErrId, "ci {pipeline} failed with {count} error(s):\n{err}",
			log.String("pipeline", pipelineName),
			log.Int("count", len(chartErrors)),
			log.Err(errors.Join(chartErrors...)),
		)
	}

	return nil
}

func RunSprint(mode Mode) error {
	return log.CreateError(herrors.ErrCiSprint, "ci sprint [{mode}]: not yet implemented",
		log.String("mode", string(mode)),
	)
}

func RunSync(mode Mode) error {
	return log.CreateError(herrors.ErrCiSync, "ci sync [{mode}]: not yet implemented",
		log.String("mode", string(mode)),
	)
}

func RunUpdate(mode Mode) error {
	return log.CreateError(herrors.ErrCiUpdate, "ci update [{mode}]: not yet implemented",
		log.String("mode", string(mode)),
	)
}

func extendDownloadAuthError(configPath string, err error, registryConfigPath string) error {
	if err == nil || strings.TrimSpace(registryConfigPath) != "" || !looksLikeMissingRegistryCredentials(err) {
		return err
	}
	registryHost := registryHostFromError(err)
	if registryHost == "" {
		registryHost = "<registry-host>"
	}
	return fmt.Errorf("%w\n%s", err, missingDownloadTokenError(configPath, registryHost))
}

func looksLikeMissingRegistryCredentials(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "basic credential not found") ||
		strings.Contains(msg, "401") ||
		strings.Contains(msg, "unauthorized")
}

func registryHostFromError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	start := strings.Index(msg, "https://")
	if start < 0 {
		start = strings.Index(msg, "http://")
	}
	if start < 0 {
		return ""
	}
	rest := msg[start:]
	end := strings.Index(rest, "\"")
	if end < 0 {
		end = len(rest)
	}
	rawURL := rest[:end]
	parsed, parseErr := url.Parse(rawURL)
	if parseErr == nil && parsed.Host != "" {
		return normalizeRegistryHost(parsed.Host)
	}
	return normalizeRegistryHost(rawURL)
}

func changedChartPathsForTest(repo *git.Repo, cfg *Config) ([]string, error) {
	pattern := filepath.Join(cfg.CI.RootAppsPath, "*", "*", "*")
	matches, err := filepath.Glob(filepath.Join(repo.Path(), pattern))
	if err != nil {
		return nil, fmt.Errorf("glob charts: %w", err)
	}

	chartRelPaths := make([]string, 0, len(matches))
	for _, absPath := range matches {
		relPath, errRel := filepath.Rel(repo.Path(), absPath)
		if errRel != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)
		_, _, env, errParse := ParseChartPath(relPath)
		if errParse != nil {
			continue
		}
		if !envAllowed(env, cfg.CI.Environments) {
			continue
		}

		changed, errChanged := chartDirChangedSinceLastRelease(repo, relPath)
		if errChanged != nil {
			return nil, errChanged
		}
		if changed {
			chartRelPaths = append(chartRelPaths, relPath)
		}
	}

	sort.Strings(chartRelPaths)
	return chartRelPaths, nil
}

func chartNameForTest(repo *git.Repo, relPath string) (string, error) {
	ch, err := repo.LoadChart(relPath)
	if err != nil {
		return "", fmt.Errorf("load chart %s: %w", relPath, err)
	}
	return ch.GetName(), nil
}

func optionalTestValuesArgs(chartDir string) ([]string, error) {
	path := filepath.Join(chartDir, testValuesFileName)
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("optional test values file %s is a directory", path)
		}
		return []string{"-f", testValuesFileName}, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, fmt.Errorf("stat optional test values file %s: %w", path, err)
}

func validateChartVersionForTest(repo *git.Repo, relPath string) error {
	group, app, env, err := ParseChartPath(relPath)
	if err != nil {
		return fmt.Errorf("chart %s: parse chart path: %w", relPath, err)
	}

	ch, err := repo.LoadChart(relPath)
	if err != nil {
		return fmt.Errorf("load chart %s: %w", relPath, err)
	}

	version := ch.GetVersion()
	parsed, err := ParseChartVersion(version)
	if err != nil {
		return fmt.Errorf("chart %s: invalid Chart.yaml version %q: %w", relPath, version, err)
	}

	expectedEnv := ""
	if env != "prod" {
		expectedEnv = env
	}
	if parsed.Env != expectedEnv {
		return fmt.Errorf("chart %s: version %q does not match directory environment %q", relPath, version, env)
	}

	if app == "root" {
		return nil
	}

	depVer, err := dependencyVersionForWrapperRelease(ch, version, relPath)
	if err != nil {
		return err
	}
	expectedBase, err := ComputeWrapperVersion(depVer, env, -1)
	if err != nil {
		return fmt.Errorf("chart %s: compute expected wrapper version: %w", relPath, err)
	}
	want, err := ParseChartVersion(expectedBase)
	if err != nil {
		return fmt.Errorf("chart %s: parse expected wrapper version %q: %w", relPath, expectedBase, err)
	}

	if parsed.Major != want.Major || parsed.Minor != want.Minor || parsed.Patch != want.Patch ||
		parsed.PreRelease != want.PreRelease || parsed.Env != want.Env {
		return fmt.Errorf("chart %s: version %q does not match dependency/version line for %s/%s@%s; expected %q or %q with extra counter",
			relPath, version, group, app, env, expectedBase, expectedBase)
	}

	return nil
}
