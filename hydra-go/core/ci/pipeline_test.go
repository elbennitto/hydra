package ci

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

func TestRunTest_NoChanges_SkipsHelm(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev")),
		).
		Tag("build-001")
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("helm must not run when nothing changed: %v", args)
	}
	t.Cleanup(func() { helmRunHook = old })

	require.NoError(t, RunTest(configPath(repo), ModeLocal))
}

func TestRunTest_DryRun_DoesNotInvokeHelm(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev")),
		).
		Tag("build-001").
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("helm must not run in dry-run: %v", args)
	}
	t.Cleanup(func() { helmRunHook = old })

	require.NoError(t, RunTest(configPath(repo), ModeDryRun))
}

func TestRunDownload_DryRun_DoesNotInvokeDownloader(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev")),
		).
		Tag("build-001").
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := downloadChartDependencies
	downloadChartDependencies = func(_ log.Logger, _ string, _ *v2chart.Chart, _ string) error {
		return fmt.Errorf("downloader must not run in dry-run")
	}
	t.Cleanup(func() { downloadChartDependencies = old })

	require.NoError(t, RunDownload(configPath(repo), ModeDryRun))
}

func TestRunTest_Local_RunsLintAndTemplateForChangedCharts(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/dev/charts/service-ui", git.NewChart("service-ui").Version("1.0.0")).
			Add("apps/demo/service-auth/dev", git.NewChart("service-auth").Version("1.0.0-dev")),
		).
		Tag("build-001").
		Commit("change one chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := helmRunHook
	var helmCalls [][]string
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		row := append([]string{dir}, args...)
		helmCalls = append(helmCalls, row)
		switch {
		case len(args) == 2 && args[0] == "lint" && args[1] == ".":
			return []byte("lint ok"), nil
		case len(args) == 3 && args[0] == "template" && args[1] == "service-ui" && args[2] == ".":
			return []byte("template ok"), nil
		default:
			return nil, fmt.Errorf("unexpected helm invocation: %v", args)
		}
	}
	t.Cleanup(func() { helmRunHook = old })

	require.NoError(t, RunTest(configPath(repo), ModeLocal))
	require.Len(t, helmCalls, 2)
	assert.Equal(t, "lint", helmCalls[0][1])
	assert.Equal(t, "template", helmCalls[1][1])
	assert.Contains(t, helmCalls[0][0], "apps/demo/service-ui/dev")
	assert.Contains(t, helmCalls[1][0], "apps/demo/service-ui/dev")
}

func TestRunTest_Local_LogsSummaryForSuccessfulCharts(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/dev/charts/service-ui", git.NewChart("service-ui").Version("1.0.0")),
		).
		Tag("build-001").
		Commit("change one chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		switch {
		case len(args) == 2 && args[0] == "lint" && args[1] == ".":
			return []byte("lint ok"), nil
		case len(args) == 3 && args[0] == "template" && args[1] == "service-ui" && args[2] == ".":
			return []byte("template ok"), nil
		default:
			return nil, fmt.Errorf("unexpected helm invocation: %v", args)
		}
	}
	t.Cleanup(func() { helmRunHook = old })

	logs := captureCILogs(t, func() {
		require.NoError(t, RunTest(configPath(repo), ModeLocal))
	})
	assert.Contains(t, logs, "ci test summary")
	assert.Contains(t, logs, "1 chart(s) successful")
	assert.Contains(t, logs, "0 chart(s) failed")
}

func TestRunTest_Local_UsesOptionalTestValuesFileWhenPresent(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-auth/dev",
				git.NewChart("service-auth").
					Version("1.0.0-dev").
					Dep("service-auth", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-auth/dev/charts/service-auth", git.NewChart("service-auth").Version("1.0.0")).
			File("apps/demo/service-auth/dev/values.test.yaml", "global:\n  baseUrl: https://dummy.invalid\n"),
		).
		Tag("build-001").
		Commit("change one chart", "apps/demo/service-auth/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := helmRunHook
	var helmCalls [][]string
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		row := append([]string{dir}, args...)
		helmCalls = append(helmCalls, row)
		switch {
		case len(args) == 4 && args[0] == "lint" && args[1] == "." && args[2] == "-f" && args[3] == testValuesFileName:
			return []byte("lint ok"), nil
		case len(args) == 5 && args[0] == "template" && args[1] == "service-auth" && args[2] == "." && args[3] == "-f" && args[4] == testValuesFileName:
			return []byte("template ok"), nil
		default:
			return nil, fmt.Errorf("unexpected helm invocation: %v", args)
		}
	}
	t.Cleanup(func() { helmRunHook = old })

	require.NoError(t, RunTest(configPath(repo), ModeLocal))
	require.Len(t, helmCalls, 2)
	assert.Equal(t, []string{"lint", ".", "-f", testValuesFileName}, helmCalls[0][1:])
	assert.Equal(t, []string{"template", "service-auth", ".", "-f", testValuesFileName}, helmCalls[1][1:])
}

func TestRunTest_Local_FailsWhenDependencyMissing(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			),
		).
		Tag("build-001").
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("helm must not run when local dependency verification fails: %v", args)
	}
	t.Cleanup(func() { helmRunHook = old })

	err := RunTest(configPath(repo), ModeLocal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run `hydra ci run download` first")
}

func TestRunTest_Local_FailsWhenChildVersionDoesNotMatchDependencyAndEnv(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("9.9.9-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/dev/charts/service-ui", git.NewChart("service-ui").Version("1.0.0")),
		).
		Tag("build-001").
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("helm must not run when version validation fails: %v", args)
	}
	t.Cleanup(func() { helmRunHook = old })

	err := RunTest(configPath(repo), ModeLocal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `version "9.9.9-dev" does not match`)
	assert.Contains(t, err.Error(), `expected "1.0.0-dev"`)
}

func TestRunTest_Local_FailsWhenRootVersionEnvSuffixIsWrong(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.1"),
			),
		).
		Tag("build-001").
		Commit("change root", "apps/demo/root/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("helm must not run when root version validation fails: %v", args)
	}
	t.Cleanup(func() { helmRunHook = old })

	err := RunTest(configPath(repo), ModeLocal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `chart apps/demo/root/dev: version "200.22.1" does not match directory environment "dev"`)
}

func TestRunTest_Local_CollectsMultipleChartErrorsBeforeFailing(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.1"),
			).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("9.9.9-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/dev/charts/service-ui", git.NewChart("service-ui").Version("1.0.0")),
		).
		Tag("build-001").
		CommitFS("change charts", git.NewFS().
			File("apps/demo/root/dev/values.yaml", "a: 1\n").
			File("apps/demo/service-ui/dev/values.yaml", "b: 2\n"),
		)
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("helm must not run when chart version validation fails: %v", args)
	}
	t.Cleanup(func() { helmRunHook = old })

	err := RunTest(configPath(repo), ModeLocal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci test failed with 2 error(s)")
	assert.Contains(t, err.Error(), `chart apps/demo/root/dev: version "200.22.1" does not match directory environment "dev"`)
	assert.Contains(t, err.Error(), `chart apps/demo/service-ui/dev: version "9.9.9-dev" does not match`)
}

func TestRunTest_Local_LogsSummaryWhenSomeChartsFail(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.1"),
			).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/dev/charts/service-ui", git.NewChart("service-ui").Version("1.0.0")),
		).
		Tag("build-001").
		CommitFS("change charts", git.NewFS().
			File("apps/demo/root/dev/values.yaml", "a: 1\n").
			File("apps/demo/service-ui/dev/values.yaml", "b: 2\n"),
		)
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		switch {
		case len(args) == 2 && args[0] == "lint" && args[1] == ".":
			return []byte("lint ok"), nil
		case len(args) == 3 && args[0] == "template" && args[1] == "service-ui" && args[2] == ".":
			return []byte("template ok"), nil
		default:
			return nil, fmt.Errorf("unexpected helm invocation: %v", args)
		}
	}
	t.Cleanup(func() { helmRunHook = old })

	logs := captureCILogs(t, func() {
		err := RunTest(configPath(repo), ModeLocal)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ci test failed for chart apps/demo/root/dev")
	})
	assert.Contains(t, logs, "ci test summary")
	assert.Contains(t, logs, "1 chart(s) successful")
	assert.Contains(t, logs, "1 chart(s) failed")
}

func TestRunDownload_Local_DownloadsChangedChartsEvenWhenDependenciesExist(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/dev/charts/service-ui", git.NewChart("service-ui").Version("1.0.0")),
		).
		Tag("build-001").
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	old := downloadChartDependencies
	var downloadPaths []string
	downloadChartDependencies = func(_ log.Logger, chartPath string, _ *v2chart.Chart, _ string) error {
		downloadPaths = append(downloadPaths, chartPath)
		return nil
	}
	t.Cleanup(func() { downloadChartDependencies = old })

	require.NoError(t, RunDownload(configPath(repo), ModeLocal))
	require.Len(t, downloadPaths, 1)
	assert.Contains(t, downloadPaths[0], "apps/demo/service-ui/dev")
}

func TestRunDownload_Local_LogsIntoConfiguredDownloadRegistries(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			),
		).
		Tag("build-001").
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	oldLogin := prepareRegistryAuthHook
	oldDownload := downloadChartDependencies
	var usedConfigPath string
	prepareRegistryAuthHook = func(gotConfigPath string) (string, func(), error) {
		assert.Equal(t, configPath(repo), gotConfigPath)
		return "/tmp/hydra-test-registry-config.json", func() {}, nil
	}
	downloadChartDependencies = func(_ log.Logger, chartPath string, _ *v2chart.Chart, registryConfigPath string) error {
		assert.Contains(t, chartPath, "apps/demo/service-ui/dev")
		usedConfigPath = registryConfigPath
		return nil
	}
	t.Cleanup(func() {
		prepareRegistryAuthHook = oldLogin
		downloadChartDependencies = oldDownload
	})

	require.NoError(t, RunDownload(configPath(repo), ModeLocal))
	assert.Equal(t, "/tmp/hydra-test-registry-config.json", usedConfigPath)
}

func TestRunDownload_Local_ExtendsMissingTokenErrorReport(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://harbor.example.test/team/chart"),
			),
		).
		Tag("build-001").
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	oldAuth := prepareRegistryAuthHook
	oldDownload := downloadChartDependencies
	prepareRegistryAuthHook = func(gotConfigPath string) (string, func(), error) {
		assert.Equal(t, configPath(repo), gotConfigPath)
		return "", func() {}, nil
	}
	downloadChartDependencies = func(_ log.Logger, chartPath string, _ *v2chart.Chart, registryConfigPath string) error {
		return fmt.Errorf(`could not download oci://harbor.example.test/team/chart: failed to perform "FetchReference" on source: GET "https://harbor.example.test/v2/team/chart/manifests/1.2.3": basic credential not found`)
	}
	t.Cleanup(func() {
		prepareRegistryAuthHook = oldAuth
		downloadChartDependencies = oldDownload
	})

	err := RunDownload(configPath(repo), ModeLocal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing OCI registry credentials for dependency download")
	assert.Contains(t, err.Error(), "Registry host: harbor.example.test")
	assert.Contains(t, err.Error(), "Expected secret entry: secrets.registryTokens[]")
}

func captureCILogs(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	formattedHandler := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})

	oldSlog := slog.Default()
	oldLogger := log.Default()
	slog.SetDefault(slog.New(formattedHandler))
	log.SetDefault(log.NewLoggerWithHandler(formattedHandler))
	t.Cleanup(func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldSlog)
	})

	fn()
	return buf.String()
}
