package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hydra-gitops.org/hydra/hydra-go/core/git"
)

// --- Mock PromoteActions ---

type mockPromoteActions struct {
	executions []PromotionEntry
	err        error
}

func (m *mockPromoteActions) ExecutePromotion(_ *git.Repo, entry PromotionEntry, _ *Config) error {
	m.executions = append(m.executions, entry)
	return m.err
}

// --- Helpers ---

func configYAML(envs, promotableRootApps string) string {
	return "ci:\n" +
		"  rootAppsPath: apps\n" +
		"  environments: [" + envs + "]\n" +
		"  appGroups:\n" +
		"    - name: demo\n" +
		"      path: apps/demo\n" +
		"    - name: cluster-infra\n" +
		"      path: apps/cluster-infra\n" +
		"  registry: \"oci://registry/helm\"\n" +
		"  promote:\n" +
		"    promotableRootApps: [" + promotableRootApps + "]\n"
}

func configPath(repo *git.Repo) string {
	return filepath.Join(repo.Path(), ".hydra-ci.yaml")
}

func addOriginBareRemote(t *testing.T, repo *git.Repo) string {
	t.Helper()
	remoteDir := t.TempDir()
	out, err := exec.Command("git", "init", "--bare", remoteDir).CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("git", "-C", repo.Path(), "remote", "add", "origin", remoteDir).CombinedOutput()
	require.NoError(t, err, string(out))
	return remoteDir
}

// --- Detection Tests (using mock) ---

func TestPromote_DevToStage(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.Equal(t, "demo", p.Group)
	assert.Equal(t, "service-ui", p.App)
	assert.Equal(t, "dev", p.SourceEnv)
	assert.Equal(t, "stage", p.TargetEnv)
	assert.Equal(t, "1.198.3-stage", p.OldVersion)
	assert.Equal(t, "1.198.3-stage", p.NewVersion)
	assert.Equal(t, "hydra/promote/to-stage/demo/service-ui", p.Branch)
	assert.False(t, p.Skipped)

	require.Len(t, mock.executions, 1)
	assert.Equal(t, "service-ui", mock.executions[0].App)
}

func TestPromote_NoDifferences(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.True(t, result.Promotions[0].Skipped)
	assert.Equal(t, "no differences", result.Promotions[0].SkipReason)
	assert.Empty(t, mock.executions)
}

func TestRunPromote_OnEntryCallbackMatchesPromotions(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			).
			Add("apps/demo/service-auth/dev",
				git.NewChart("service-auth").
					Version("18.33.28-dev").
					Dep("service-auth", "18.33.28", "oci://registry/helm"),
			).
			Add("apps/demo/service-auth/stage",
				git.NewChart("service-auth").
					Version("18.30.0-stage").
					Dep("service-auth", "18.30.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	var seen []PromotionEntry
	result, err := RunPromote(configPath(repo), ModeLocal, &localPromoteActions{}, "", "", func(e PromotionEntry) {
		seen = append(seen, e)
	})
	require.NoError(t, err)
	require.Len(t, result.Promotions, 2)
	require.Len(t, seen, len(result.Promotions))
	for i := range seen {
		assert.Equal(t, result.Promotions[i].Group, seen[i].Group)
		assert.Equal(t, result.Promotions[i].App, seen[i].App)
		assert.Equal(t, result.Promotions[i].Skipped, seen[i].Skipped)
	}
}

func TestRunPromote_OnEntryCalledWhenSkipped(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	var n int
	result, err := RunPromote(configPath(repo), ModeDryRun, &dryRunPromoteActions{}, "", "", func(PromotionEntry) {
		n++
	})
	require.NoError(t, err)
	require.Len(t, result.Promotions, 1)
	assert.Equal(t, 1, n)
	assert.True(t, result.Promotions[0].Skipped)
}

func TestPromote_NewApp_NoTargetDir(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped)
	assert.Equal(t, "", p.OldVersion)
	assert.Equal(t, "1.200.9-stage", p.NewVersion)
	assert.Equal(t, "1.200.9", p.DepVersion)

	require.Len(t, mock.executions, 1)
}

func TestPromote_NewApp_NoTargetDir_CI_DefaultResetVersion(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeCI, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped)
	assert.Equal(t, "", p.OldVersion)
	assert.Equal(t, "0.0.0", p.NewVersion)
	assert.Equal(t, "1.200.9", p.DepVersion)
}

func TestPromote_NewApp_NoTargetDir_CI_ConfiguredResetVersion(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", "ci:\n"+
				"  rootAppsPath: apps\n"+
				"  environments: [dev, stage]\n"+
				"  appGroups:\n"+
				"    - name: demo\n"+
				"      path: apps/demo\n"+
				"  registry: \"oci://registry/helm\"\n"+
				"  promote:\n"+
				"    promotableRootApps: []\n"+
				"    newChartVersion: 0.1.0-stage\n").
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeCI, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped)
	assert.Equal(t, "", p.OldVersion)
	assert.Equal(t, "0.1.0-stage", p.NewVersion)
	assert.Equal(t, "1.200.9", p.DepVersion)
}

func TestPromote_StageToProd(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("stage, prod", "")).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/prod",
				git.NewChart("service-ui").
					Version("1.198.3").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.Equal(t, "stage", p.SourceEnv)
	assert.Equal(t, "prod", p.TargetEnv)
	assert.Equal(t, "1.198.3", p.NewVersion)
	assert.False(t, p.Skipped)
}

func TestPromote_ExtraVersionDiffers(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-2-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-1-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.True(t, p.Skipped)
	assert.Equal(t, "no differences", p.SkipReason)
}

func TestPromote_ExtraDevResetsCounterWhenStageUsesExtraLine(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-1-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-1-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.True(t, p.Skipped)
	assert.Equal(t, "no differences", p.SkipReason)
	assert.Empty(t, mock.executions)
}

func TestPromote_RootAppBlocked(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.0-dev").
					Dep("infra_library", "1.0.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.True(t, p.Skipped)
	assert.Equal(t, "root app not promotable", p.SkipReason)
	assert.Equal(t, "root", p.App)
	assert.Empty(t, mock.executions)
}

func TestPromote_RootAppAllowed(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "demo")).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.0-dev").
					Dep("infra_library", "1.0.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped)
	assert.Equal(t, "root", p.App)
	require.Len(t, mock.executions, 1)
}

func TestPromote_MultipleCharts(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			).
			Add("apps/demo/service-auth/dev",
				git.NewChart("service-auth").
					Version("18.33.28-dev").
					Dep("service-auth", "18.33.28", "oci://registry/helm"),
			).
			Add("apps/demo/service-auth/stage",
				git.NewChart("service-auth").
					Version("18.30.0-stage").
					Dep("service-auth", "18.30.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-config/dev",
				git.NewChart("service-config").
					Version("5.2.0-dev").
					Dep("service-config", "5.2.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-config/stage",
				git.NewChart("service-config").
					Version("5.1.0-stage").
					Dep("service-config", "5.1.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	assert.Len(t, result.Promotions, 3)
	assert.Len(t, mock.executions, 3)

	for _, p := range result.Promotions {
		assert.False(t, p.Skipped)
		assert.Equal(t, "dev", p.SourceEnv)
		assert.Equal(t, "stage", p.TargetEnv)
	}
}

func TestPromote_ThreeEnvs_BothPairs(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage, prod", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/prod",
				git.NewChart("service-ui").
					Version("1.195.0").
					Dep("service-ui", "1.195.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 2)

	devToStage := result.Promotions[0]
	assert.Equal(t, "dev", devToStage.SourceEnv)
	assert.Equal(t, "stage", devToStage.TargetEnv)
	assert.Equal(t, "1.198.3-stage", devToStage.NewVersion)

	stageToProd := result.Promotions[1]
	assert.Equal(t, "stage", stageToProd.SourceEnv)
	assert.Equal(t, "prod", stageToProd.TargetEnv)
	assert.Equal(t, "1.195.0", stageToProd.NewVersion)
}

func TestPromote_PromoteTo_FiltersTargetEnv(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage, prod", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/prod",
				git.NewChart("service-ui").
					Version("1.195.0").
					Dep("service-ui", "1.195.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "stage")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.Equal(t, "dev", result.Promotions[0].SourceEnv)
	assert.Equal(t, "stage", result.Promotions[0].TargetEnv)
}

func TestPromote_PromoteTo_ProdOnly(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage, prod", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/prod",
				git.NewChart("service-ui").
					Version("1.195.0").
					Dep("service-ui", "1.195.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "prod")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.Equal(t, "stage", result.Promotions[0].SourceEnv)
	assert.Equal(t, "prod", result.Promotions[0].TargetEnv)
}

// --- Mode Tests ---

func TestPromote_DryRun_NoGitChanges(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	actions := &dryRunPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeDryRun, actions, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped)

	tags, _ := repo.Tags("*")
	assert.Nil(t, tags, "dry-run should not create tags")

	_, err = repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	stageChart, _ := repo.LoadChart("apps/demo/service-ui/stage")
	assert.Equal(t, "1.198.3-stage", stageChart.GetVersion(), "dry-run should not modify files")
}

func TestPromote_Local_CreatesBranchAndCommit(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm").
					Values("replicaCount: 2\n"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm").
					Values("replicaCount: 1\n"),
			),
		)
	require.NoError(t, repo.Err)

	actions := &localPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, actions, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped)

	branch := "hydra/promote/to-stage/demo/service-ui"
	repo.Checkout(branch)
	require.NoError(t, repo.Err, "promote branch should exist")

	promoted, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.198.3-stage", promoted.GetVersion())
	assert.Equal(t, "1.200.9", promoted.GetDepVersion("service-ui"))

	val, err := promoted.GetValue("replicaCount")
	require.NoError(t, err)
	assert.Equal(t, "2", val, "values should come from source (dev)")
}

func TestPromote_Local_MultipleBranches(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			).
			Add("apps/demo/service-auth/dev",
				git.NewChart("service-auth").
					Version("18.33.28-dev").
					Dep("service-auth", "18.33.28", "oci://registry/helm"),
			).
			Add("apps/demo/service-auth/stage",
				git.NewChart("service-auth").
					Version("18.30.0-stage").
					Dep("service-auth", "18.30.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	actions := &localPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, actions, "", "")
	require.NoError(t, err)
	require.Len(t, result.Promotions, 2)

	repo.Checkout("hydra/promote/to-stage/demo/service-ui")
	require.NoError(t, repo.Err)
	uiChart, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.198.3-stage", uiChart.GetVersion())

	repo.Checkout("hydra/promote/to-stage/demo/service-auth")
	require.NoError(t, repo.Err)
	authChart, err := repo.LoadChart("apps/demo/service-auth/stage")
	require.NoError(t, err)
	assert.Equal(t, "18.30.0-stage", authChart.GetVersion())
}

func TestPromote_CI_CreatesBranchAndCommit(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm").
					Values("replicaCount: 2\n"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm").
					Values("replicaCount: 1\n"),
			),
		)
	require.NoError(t, repo.Err)
		remoteDir := addOriginBareRemote(t, repo)

	actions := &ciPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeCI, actions, "", "")
	require.NoError(t, err)
	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped)

	branch := "hydra/promote/to-stage/demo/service-ui"
	repo.Checkout(branch)
	require.NoError(t, repo.Err, "promote branch should exist")

	promoted, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.198.3-stage", promoted.GetVersion())
	assert.Equal(t, "1.200.9", promoted.GetDepVersion("service-ui"))

	val, err := promoted.GetValue("replicaCount")
	require.NoError(t, err)
	assert.Equal(t, "2", val, "values should come from source (dev)")

	out, err := exec.Command("git", "--git-dir", remoteDir, "show-ref", "--verify", "refs/heads/"+branch).CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestPromote_CI_TargetBranchNotSupported(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			),
		).
		Branch("custom-target")
	require.NoError(t, repo.Err)

	actions := &ciPromoteActions{}
	_, err := RunPromote(configPath(repo), ModeCI, actions, "custom-target", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target branch is not supported in ci mode")
}

// --- Helper Tests ---

func TestParseChartPath(t *testing.T) {
	group, app, env, err := ParseChartPath("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "demo", group)
	assert.Equal(t, "service-ui", app)
	assert.Equal(t, "dev", env)
}

func TestParseChartPath_TooShort(t *testing.T) {
	_, _, _, err := ParseChartPath("apps/demo")
	require.Error(t, err)
}

func TestVersionContentEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     ChartVersion
		expected bool
	}{
		{
			name:     "same base version different env",
			a:        ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: "dev"},
			b:        ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: "stage"},
			expected: true,
		},
		{
			name:     "different minor version",
			a:        ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: "dev"},
			b:        ChartVersion{Major: 1, Minor: 198, Patch: 3, Extra: -1, Env: "stage"},
			expected: false,
		},
		{
			name:     "same base different extra",
			a:        ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 2, Env: "dev"},
			b:        ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 1, Env: "stage"},
			expected: false,
		},
		{
			name:     "same base same extra",
			a:        ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 1, Env: "dev"},
			b:        ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 1, Env: "stage"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, versionContentEqual(tt.a, tt.b))
		})
	}
}

func TestPromote_CollectsAllVersionErrors(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-auth/dev",
				git.NewChart("service-auth").
					Version("2.0.0").
					Dep("service-auth", "2.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-config/dev",
				git.NewChart("service-config").
					Version("3.0.0").
					Dep("service-config", "3.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.Error(t, err)

	var errorEntries []PromotionEntry
	for _, p := range result.Promotions {
		if p.HasError {
			errorEntries = append(errorEntries, p)
		}
	}
	assert.Len(t, errorEntries, 2, "both charts with missing suffix should be reported")
	assert.Contains(t, err.Error(), "service-auth")
	assert.Contains(t, err.Error(), "service-config")

	var okEntries []PromotionEntry
	for _, p := range result.Promotions {
		if !p.Skipped && !p.HasError {
			okEntries = append(okEntries, p)
		}
	}
	assert.Len(t, okEntries, 1, "valid chart should still be processed")
	assert.Equal(t, "service-ui", okEntries[0].App)
	assert.Len(t, mock.executions, 1, "valid chart should be executed")
}

func TestPromote_ConfigInNestedRepo_ChartsInParent(t *testing.T) {
	parentDir := t.TempDir()

	parentRepo := git.Init(parentDir).
		CommitFS("init", git.NewFS().
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			),
		)
	require.NoError(t, parentRepo.Err)

	subDir := filepath.Join(parentDir, "hydra")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	nestedRepo := git.Init(subDir).
		Commit("init config", ".hydra-ci.yaml",
			"ci:\n"+
				"  rootAppsPath: ../apps\n"+
				"  environments: [dev, stage]\n"+
				"  appGroups: []\n"+
				"  registry: \"\"\n"+
				"  promote:\n"+
				"    promotableRootApps: []\n",
		)
	require.NoError(t, nestedRepo.Err)

	cfgPath := filepath.Join(subDir, ".hydra-ci.yaml")
	actions := &localPromoteActions{}
	result, err := RunPromote(cfgPath, ModeLocal, actions, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped)
	assert.Equal(t, "demo", p.Group)
	assert.Equal(t, "service-ui", p.App)
	assert.Equal(t, "1.198.3-stage", p.NewVersion)

	parentRepo.Checkout("hydra/promote/to-stage/demo/service-ui")
	require.NoError(t, parentRepo.Err)
	promoted, err := parentRepo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.198.3-stage", promoted.GetVersion())
}

func TestPromote_ConfigInNestedRepo_PreservesNestedRepoContents(t *testing.T) {
	parentDir := t.TempDir()

	parentRepo := git.Init(parentDir).
		CommitFS("init", git.NewFS().
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.198.3-stage").
					Dep("service-ui", "1.198.3", "oci://registry/helm"),
			),
		)
	require.NoError(t, parentRepo.Err)

	subDir := filepath.Join(parentDir, "hydra")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	nestedRepo := git.Init(subDir).
		Commit("init config", ".hydra-ci.yaml",
			"ci:\n"+
				"  rootAppsPath: ../apps\n"+
				"  environments: [dev, stage]\n"+
				"  appGroups: []\n"+
				"  registry: \"\"\n"+
				"  promote:\n"+
				"    promotableRootApps: []\n",
		).
		Commit("add readme", "README.md", "# Hydra\n")
	require.NoError(t, nestedRepo.Err)

	cfgPath := filepath.Join(subDir, ".hydra-ci.yaml")
	actions := &localPromoteActions{}
	result, err := RunPromote(cfgPath, ModeLocal, actions, "", "")
	require.NoError(t, err)
	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped)

	require.DirExists(t, subDir, "nested repo directory must survive promote")
	require.FileExists(t, filepath.Join(subDir, ".hydra-ci.yaml"), "config must survive promote")
	require.FileExists(t, filepath.Join(subDir, "README.md"), "nested repo files must survive promote")

	content, err := os.ReadFile(filepath.Join(subDir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Hydra\n", string(content))
}

func TestPromotionEntry_CommitMessage(t *testing.T) {
	entry := PromotionEntry{
		App:        "service-ui",
		SourceEnv:  "dev",
		TargetEnv:  "stage",
		OldVersion: "1.198.3-stage",
		NewVersion: "1.198.3-stage",
	}
	assert.Equal(t, "promote: service-ui dev → stage (content changed)", entry.CommitMessage())

	newApp := PromotionEntry{
		App:        "service-ui",
		SourceEnv:  "dev",
		TargetEnv:  "stage",
		OldVersion: "",
		NewVersion: "1.200.9-stage",
		DepVersion: "1.200.9",
	}
	assert.Equal(t, "promote: service-ui dev → stage (new, version 1.200.9-stage, dependency 1.200.9)", newApp.CommitMessage())

	contentChanged := PromotionEntry{
		App:        "service-auth",
		SourceEnv:  "dev",
		TargetEnv:  "stage",
		OldVersion: "18.33.28-stage",
		NewVersion: "18.33.28-stage",
	}
	assert.Equal(t, "promote: service-auth dev → stage (content changed)", contentChanged.CommitMessage())
}

// --- Directory-level chart comparison tests ---

func TestPromote_ValuesChangedVersionSame(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-auth/dev",
				git.NewChart("service-auth").
					Version("18.33.28-dev").
					Dep("service-auth", "18.33.28", "oci://registry/helm").
					Values("service-auth:\n  test: true\n"),
			).
			Add("apps/demo/service-auth/stage",
				git.NewChart("service-auth").
					Version("18.33.28-stage").
					Dep("service-auth", "18.33.28", "oci://registry/helm").
					Values("service-auth:\n  test: false\n"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped, "should detect values.yaml difference")
	assert.Equal(t, "18.33.28-stage", p.NewVersion)
	require.Len(t, mock.executions, 1)
}

func TestPromote_ExtraFileInSource(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm").
					Values("replicaCount: 2\n"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped, "should detect missing values.yaml in target")
	require.Len(t, mock.executions, 1)
}

func TestPromote_NoDifferences_WithValues(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm").
					Values("replicaCount: 2\n"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm").
					Values("replicaCount: 2\n"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.True(t, result.Promotions[0].Skipped)
	assert.Equal(t, "no differences", result.Promotions[0].SkipReason)
	assert.Empty(t, mock.executions)
}

func TestPromote_Local_TemplateFileCopied(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/cluster-infra/cert-mgr/dev",
				git.NewChart("cert-mgr").Version("0.1.0-dev"),
			).
			File("apps/cluster-infra/cert-mgr/dev/templates/issuer.yaml",
				"apiVersion: cert-manager.io/v1\nkind: ClusterIssuer\n").
			Add("apps/cluster-infra/cert-mgr/stage",
				git.NewChart("cert-mgr").Version("0.1.0-stage"),
			),
		)
	require.NoError(t, repo.Err)

	actions := &localPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, actions, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped, "template file difference must trigger promotion")

	repo.Checkout("hydra/promote/to-stage/cluster-infra/cert-mgr")
	require.NoError(t, repo.Err)

	tmpl, readErr := os.ReadFile(filepath.Join(repo.Path(),
		"apps/cluster-infra/cert-mgr/stage/templates/issuer.yaml"))
	require.NoError(t, readErr, "template must be copied to target")
	assert.Equal(t, "apiVersion: cert-manager.io/v1\nkind: ClusterIssuer\n", string(tmpl))

	promoted, err := repo.LoadChart("apps/cluster-infra/cert-mgr/stage")
	require.NoError(t, err)
	assert.Equal(t, "0.1.0-stage", promoted.GetVersion())
}

func TestPromote_Local_ExtraTargetFileRemoved(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/svc/dev",
				git.NewChart("svc").Version("2.0.0-dev"),
			).
			Add("apps/demo/svc/stage",
				git.NewChart("svc").Version("1.0.0-stage"),
			).
			File("apps/demo/svc/stage/obsolete.yaml", "old content\n"),
		)
	require.NoError(t, repo.Err)

	actions := &localPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, actions, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped)

	repo.Checkout("hydra/promote/to-stage/demo/svc")
	require.NoError(t, repo.Err)

	_, readErr := os.ReadFile(filepath.Join(repo.Path(), "apps/demo/svc/stage/obsolete.yaml"))
	assert.True(t, os.IsNotExist(readErr), "obsolete file must be removed from target")
}

func TestPromote_Local_PreservesSourceFormatting(t *testing.T) {
	sourceChartYAML := "apiVersion: v2\nname: fluent-bit\ndescription: Fluent Bit DaemonSet for Kubernetes logging\ntype: application\nversion: 0.55.0-dev\n\ndependencies:\n  - name: fluent-bit\n    repository: oci://ghcr.io/fluent/helm-charts\n    version: 0.55.0\n"

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			File("apps/cluster-infra/fluent-bit/dev/Chart.yaml", sourceChartYAML).
			Add("apps/cluster-infra/fluent-bit/stage",
				git.NewChart("fluent-bit").Version("0.50.0-stage"),
			),
		)
	require.NoError(t, repo.Err)

	actions := &localPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, actions, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped)

	repo.Checkout("hydra/promote/to-stage/cluster-infra/fluent-bit")
	require.NoError(t, repo.Err)

	promotedRaw, readErr := os.ReadFile(filepath.Join(repo.Path(),
		"apps/cluster-infra/fluent-bit/stage/Chart.yaml"))
	require.NoError(t, readErr)

	expected := "apiVersion: v2\nname: fluent-bit\ndescription: Fluent Bit DaemonSet for Kubernetes logging\ntype: application\nversion: 0.50.0-stage\n\ndependencies:\n  - name: fluent-bit\n    repository: oci://ghcr.io/fluent/helm-charts\n    version: 0.55.0\n"
	assert.Equal(t, expected, string(promotedRaw),
		"promoted Chart.yaml must preserve source formatting (description, blank lines)")
}

func TestPromote_CommentInChartYAML_Detected(t *testing.T) {
	sourceChart := "apiVersion: v2\nname: service-auth\n# managed by hydra\ntype: application\nversion: 18.33.28-dev\ndependencies:\n  - name: service-auth\n    version: \"18.33.28\"\n    repository: oci://registry/helm\n"
	targetChart := "apiVersion: v2\nname: service-auth\ntype: application\nversion: 18.33.28-stage\ndependencies:\n  - name: service-auth\n    version: \"18.33.28\"\n    repository: oci://registry/helm\n"

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			File("apps/demo/service-auth/dev/Chart.yaml", sourceChart).
			File("apps/demo/service-auth/stage/Chart.yaml", targetChart),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.False(t, p.Skipped, "comment difference in Chart.yaml must trigger promotion")
	assert.Equal(t, "18.33.28-stage", p.NewVersion)
	require.Len(t, mock.executions, 1)
}

func TestPromote_DepsChangedVersionSame_Detected(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-stage").
					Dep("service-ui", "1.198.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.False(t, result.Promotions[0].Skipped,
		"dependency changes in Chart.yaml must trigger promotion")
	require.Len(t, mock.executions, 1)
}
