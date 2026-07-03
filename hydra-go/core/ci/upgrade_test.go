package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

func TestRunUpgrade_Local_UpdatesDependencyVersion(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.2-dev").
					Dep("service-ui", "1.2.2", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)
	versionsFile := filepath.Join(repo.Path(), "versions.yaml")
	require.NoError(t, os.WriteFile(versionsFile, []byte(`versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
`), 0o644))

	err := RunUpgrade(configPath(repo), ModeLocal, versionsFile, false)
	require.NoError(t, err)

	chart, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", chart.GetDepVersion("service-ui"))
	assert.Equal(t, "1.2.2-dev", chart.GetVersion())
}

func TestRunUpgrade_DryRun_DoesNotWrite(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.2-dev").
					Dep("service-ui", "1.2.2", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)
	versionsFile := filepath.Join(repo.Path(), "versions.yaml")
	require.NoError(t, os.WriteFile(versionsFile, []byte(`versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
`), 0o644))

	err := RunUpgrade(configPath(repo), ModeDryRun, versionsFile, false)
	require.NoError(t, err)

	chart, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.2", chart.GetDepVersion("service-ui"))
	assert.Equal(t, "1.2.2-dev", chart.GetVersion())
}

func TestRunUpgrade_Local_DoesNotChangeChartVersionWhenDependencyAlreadyCurrent(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.2-dev").
					Dep("service-ui", "1.2.3", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)
	versionsFile := filepath.Join(repo.Path(), "versions.yaml")
	require.NoError(t, os.WriteFile(versionsFile, []byte(`versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
`), 0o644))

	err := RunUpgrade(configPath(repo), ModeLocal, versionsFile, false)
	require.NoError(t, err)

	chart, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", chart.GetDepVersion("service-ui"))
	assert.Equal(t, "1.2.2-dev", chart.GetVersion())
}

func TestRunUpgrade_Local_DoesNotChangeExistingWrapperExtraForNewDependencyBase(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.3-1-dev").
					Dep("service-ui", "1.2.2", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)
	versionsFile := filepath.Join(repo.Path(), "versions.yaml")
	require.NoError(t, os.WriteFile(versionsFile, []byte(`versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
`), 0o644))

	err := RunUpgrade(configPath(repo), ModeLocal, versionsFile, false)
	require.NoError(t, err)

	chart, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", chart.GetDepVersion("service-ui"))
	assert.Equal(t, "1.2.3-1-dev", chart.GetVersion())
}

func TestRunUpgrade_WithoutVersionsFileReadsStdin(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.2-dev").
					Dep("service-ui", "1.2.2", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	err := runUpgrade(configPath(repo), ModeLocal, "", false, strings.NewReader(`versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
`))
	require.NoError(t, err)

	chart, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", chart.GetDepVersion("service-ui"))
	assert.Equal(t, "1.2.2-dev", chart.GetVersion())
}

func TestRunUpgrade_MissingDependencyReturnsError(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.2-dev").
					Dep("other", "1.2.2", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)
	versionsFile := filepath.Join(repo.Path(), "versions.yaml")
	require.NoError(t, os.WriteFile(versionsFile, []byte(`versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
`), 0o644))

	err := RunUpgrade(configPath(repo), ModeLocal, versionsFile, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `dependencies must contain an entry named "service-ui"`)
}

func TestRunUpgrade_SkipMissingDependencyContinues(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.2-dev").
					Dep("other", "1.2.2", "oci://registry/helm"),
			).
			Add("apps/demo/service-api/dev",
				git.NewChart("service-api").
					Version("1.2.2-dev").
					Dep("service-api", "1.2.2", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)
	versionsFile := filepath.Join(repo.Path(), "versions.yaml")
	require.NoError(t, os.WriteFile(versionsFile, []byte(`versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
  - rootApp: demo
    app: service-api
    env: dev
    version: 1.2.3
`), 0o644))

	err := RunUpgrade(configPath(repo), ModeLocal, versionsFile, true)
	require.NoError(t, err)

	missingChart, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.2", missingChart.GetDepVersion("other"))
	updatedChart, err := repo.LoadChart("apps/demo/service-api/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", updatedChart.GetDepVersion("service-api"))
}

func TestLoadUpgradeVersionsFile_ValidatesRequiredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "versions.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`versions:
  - rootApp: demo
    env: dev
    version: 1.2.3
`), 0o644))

	_, err := LoadUpgradeVersionsFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "versions[0].app must not be empty")
}
