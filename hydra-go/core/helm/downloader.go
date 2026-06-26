package helm

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/base/utils"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/downloader"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/registry"
)

var downloadManagerUpdateHook func(*downloader.Manager) error
var downloadChartDependenciesSleep = time.Sleep

const dependencyDownloadRetryAttempts = 3
const dependencyDownloadRetryDelay = 3 * time.Second

func DownloadChartDependencies(l log.Logger, chartPath string, c *v2chart.Chart, registryConfigPath string) (err error) {
	l.Info(logIdHelm, "fetching missing dependencies of chart with path '{path}'",
		log.String("path", chartPath))

	if c == nil {
		// Load the chart to read its requirements
		chrt, err := log.WithoutDebug2(func() (chart.Charter, error) {
			return loader.Load(chartPath)
		})
		if err != nil {
			return log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
				"failed to load chart", log.String("path", chartPath), log.Err(err))
		}
		// Convert Charter interface to concrete v2 Chart
		switch ch := chrt.(type) {
		case *v2chart.Chart:
			c = ch
		case v2chart.Chart:
			c = &ch
		default:
			return log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
				"unsupported chart type", log.String("type", string(rune(0))))
		}
	}

	// Skip if no dependencies
	if c.Metadata == nil || len(c.Metadata.Dependencies) == 0 {
		l.Info(logIdHelm, "no dependencies found, skipping download")
		return nil
	}

	restoreChartYAML, err := resolveWildcardFileDependencyVersions(chartPath)
	if err != nil {
		return err
	}
	defer func() {
		if restoreChartYAML == nil {
			return
		}
		if restoreErr := restoreChartYAML(); restoreErr != nil {
			if err == nil {
				err = restoreErr
				return
			}
			l.Warn(logIdHelm, "failed to restore Chart.yaml after dependency download", log.String("path", chartPath), log.Err(restoreErr))
		}
	}()

	// Create a downloader manager
	settings := cli.New()
	if strings.TrimSpace(registryConfigPath) != "" {
		settings.RegistryConfig = registryConfigPath
	}

	// Initialize OCI registry client
	registryClient, err := registry.NewClient(
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
		registry.ClientOptDebug(false),
		registry.ClientOptEnableCache(true),
	)
	if err != nil {
		l.Error(logIdHelm, "failed to create registry client", log.Err(err))
		return err
	}

	man := &downloader.Manager{
		Out:       log.NewSlogWriter("helm: ", log.LevelDebug),
		ChartPath: chartPath,
		// Always refresh repositories before dependency download.
		SkipUpdate:       false,
		Getters:          getter.All(settings),
		RepositoryConfig: settings.RepositoryConfig,
		RepositoryCache:  settings.RepositoryCache,
		ContentCache:     settings.ContentCache,
		Debug:            false,
		Verify:           downloader.VerifyNever,
		RegistryClient:   registryClient,
	}

	// Use Update() to download dependencies based on Chart.yaml
	// This will create .tgz files in charts/ subdirectory
	err = updateChartDependenciesWithRetry(l, man, chartPath)
	if err != nil {
		l.Error(logIdHelm, "failed to download chart dependencies", log.String("path", chartPath), log.Err(err))
		return err
	}

	// Remove Chart.lock file created by Update()
	lockFilePath := filepath.Join(chartPath, "Chart.lock")
	if _, err := os.Stat(lockFilePath); err == nil {
		if err := os.Remove(lockFilePath); err != nil {
			l.Warn(logIdHelm, "failed to remove Chart.lock file", log.String("path", lockFilePath), log.Err(err))
		} else {
			l.DebugLog(logIdHelm, "removed Chart.lock file", log.String("path", lockFilePath))
		}
	}

	l.DebugLog(logIdHelm, "successfully downloaded chart dependencies", log.String("path", chartPath))
	return nil
}

func updateChartDependenciesWithRetry(l log.Logger, man *downloader.Manager, chartPath string) error {
	var lastErr error
	for attempt := 1; attempt <= dependencyDownloadRetryAttempts; attempt++ {
		err := runDownloadManagerUpdate(man)
		if err == nil {
			if attempt > 1 {
				l.Info(logIdHelm, "dependency download retry succeeded for chart at {path} on attempt {attempt}",
					log.String("path", chartPath),
					log.Int("attempt", attempt))
			}
			return nil
		}

		lastErr = err
		if attempt == dependencyDownloadRetryAttempts || !shouldRetryDependencyDownload(err) {
			break
		}

		l.Warn(logIdHelm, "dependency download attempt {attempt}/{maxAttempts} failed for chart at {path}; retrying in {delay}: {error}",
			log.Int("attempt", attempt),
			log.Int("maxAttempts", dependencyDownloadRetryAttempts),
			log.String("path", chartPath),
			log.String("delay", dependencyDownloadRetryDelay.String()),
			log.String("error", err.Error()))
		downloadChartDependenciesSleep(dependencyDownloadRetryDelay)
	}
	return lastErr
}

func runDownloadManagerUpdate(man *downloader.Manager) error {
	if downloadManagerUpdateHook != nil {
		return downloadManagerUpdateHook(man)
	}
	return man.Update()
}

func shouldRetryDependencyDownload(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized")
}

func resolveWildcardFileDependencyVersions(chartPath string) (func() error, error) {
	chartYAMLPath := filepath.Join(chartPath, "Chart.yaml")
	original, err := os.ReadFile(chartYAMLPath)
	if err != nil {
		return nil, log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
			"failed to read chart metadata", log.String("path", chartYAMLPath), log.Err(err))
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(original, &doc); err != nil {
		return nil, log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
			"failed to parse chart metadata", log.String("path", chartYAMLPath), log.Err(err))
	}

	depsNode := chartDependenciesNode(&doc)
	if depsNode == nil {
		return nil, nil
	}

	changed := false
	for _, depNode := range depsNode.Content {
		repoNode := chartDependencyField(depNode, "repository")
		versionNode := chartDependencyField(depNode, "version")
		if repoNode == nil || versionNode == nil {
			continue
		}
		repository := strings.TrimSpace(repoNode.Value)
		if versionNode.Value != "*" || !strings.HasPrefix(repository, "file://") {
			continue
		}

		resolvedVersion, err := localDependencyVersion(chartPath, repository)
		if err != nil {
			return nil, err
		}
		versionNode.Value = resolvedVersion
		changed = true
	}

	if !changed {
		return nil, nil
	}

	updated, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
			"failed to render patched chart metadata", log.String("path", chartYAMLPath), log.Err(err))
	}
	if err := os.WriteFile(chartYAMLPath, updated, 0o644); err != nil {
		return nil, log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
			"failed to write patched chart metadata", log.String("path", chartYAMLPath), log.Err(err))
	}

	return func() error {
		return os.WriteFile(chartYAMLPath, original, 0o644)
	}, nil
}

func localDependencyVersion(chartPath, repository string) (string, error) {
	depPath := filepath.Clean(filepath.Join(chartPath, filepath.FromSlash(utils.FileUriToPath(repository))))
	loaded, err := log.WithoutDebug2(func() (chart.Charter, error) {
		return loader.Load(depPath)
	})
	if err != nil {
		return "", log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
			"failed to load local dependency chart", log.String("path", depPath), log.Err(err))
	}

	depChart, err := convertToV2Chart(loaded)
	if err != nil {
		return "", err
	}
	if depChart.Metadata == nil || strings.TrimSpace(depChart.Metadata.Version) == "" {
		return "", log.CreateError(errors.ErrLoadingHelmChartDependenciesFailed,
			"local dependency chart has no version", log.String("path", depPath))
	}
	return depChart.Metadata.Version, nil
}

func chartDependenciesNode(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "dependencies" {
			return root.Content[i+1]
		}
	}
	return nil
}

func chartDependencyField(depNode *yaml.Node, field string) *yaml.Node {
	if depNode == nil || depNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(depNode.Content); i += 2 {
		if depNode.Content[i].Value == field {
			return depNode.Content[i+1]
		}
	}
	return nil
}
