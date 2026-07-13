package helm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/downloader"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

func TestDownloadChartDependencies_ResolvesWildcardVersionForLocalFileDependency(t *testing.T) {
	rootDir := t.TempDir()

	depDir := filepath.Join(rootDir, "shared", "infra_library", "dev")
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(depDir, "Chart.yaml"), []byte(`apiVersion: v2
name: infra_library
version: 1.2.3
type: library
`), 0o644))

	chartDir := filepath.Join(rootDir, "apps", "argocd", "root", "dev")
	require.NoError(t, os.MkdirAll(chartDir, 0o755))
	originalChart := []byte(`apiVersion: v2
name: root
version: 0.1.0
type: application
dependencies:
  - name: infra_library
    version: "*"
    repository: file://../../../../shared/infra_library/dev
`)
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), originalChart, 0o644))

	require.NoError(t, DownloadChartDependencies(log.Default(), chartDir, nil, ""))
	require.FileExists(t, filepath.Join(chartDir, "charts", "infra_library-1.2.3.tgz"))

	restoredChart, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	require.NoError(t, err)
	require.Equal(t, string(originalChart), string(restoredChart))
}

func TestUpdateChartDependenciesWithRetry_RetriesUnauthorizedFailure(t *testing.T) {
	originalSleep := downloadChartDependenciesSleep
	t.Cleanup(func() {
		downloadManagerUpdateHook = nil
		downloadChartDependenciesSleep = originalSleep
	})

	attempts := 0
	downloadManagerUpdateHook = func(*downloader.Manager) error {
		attempts++
		if attempts < 3 {
			return errors.New(`dependency download: could not download oci://harbor.example.test/team/chart: failed to perform "FetchReference" on source: GET "https://harbor.example.test/v2/team/chart/manifests/1.2.3": response status code 401: unauthorized`)
		}
		return nil
	}

	sleepCalls := 0
	downloadChartDependenciesSleep = func(time.Duration) {
		sleepCalls++
	}

	err := updateChartDependenciesWithRetry(log.Default(), &downloader.Manager{}, "/tmp/chart")
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	require.Equal(t, 2, sleepCalls)
}

func TestShouldRetryDependencyDownload_TransientErrors(t *testing.T) {
	require.True(t, shouldRetryDependencyDownload(errors.New("response status code 401: unauthorized")))
	require.True(t, shouldRetryDependencyDownload(errors.New("UNAUTHORIZED to access repository")))
	require.True(t, shouldRetryDependencyDownload(errors.New("Get \"https://github.com\": read tcp 10.0.0.5:39246->140.82.121.4:443: read: connection reset by peer")))
	require.True(t, shouldRetryDependencyDownload(errors.New("Get \"https://github.com\": dial tcp: i/o timeout")))
	require.False(t, shouldRetryDependencyDownload(errors.New("invalid chart metadata")))
	require.False(t, shouldRetryDependencyDownload(nil))
}
