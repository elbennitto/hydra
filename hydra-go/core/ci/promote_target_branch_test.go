package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hydra-gitops.org/hydra/hydra-go/core/git"
)

func TestPromote_TargetBranch_Local_AllCommitsOnBranch(t *testing.T) {
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

	repo.Branch("my-release").Checkout("main")
	require.NoError(t, repo.Err)

	actions := &localPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, actions, "my-release", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 2)
	for _, p := range result.Promotions {
		assert.Equal(t, "my-release", p.Branch)
	}

	repo.Checkout("my-release")
	require.NoError(t, repo.Err, "target branch should exist")

	uiChart, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.198.3-stage", uiChart.GetVersion())

	authChart, err := repo.LoadChart("apps/demo/service-auth/stage")
	require.NoError(t, err)
	assert.Equal(t, "18.30.0-stage", authChart.GetVersion())

	repo.Checkout("hydra/promote/to-stage/demo/service-ui")
	assert.Error(t, repo.Err, "auto-generated branch should not exist")
	repo.Err = nil

	repo.Checkout("hydra/promote/to-stage/demo/service-auth")
	assert.Error(t, repo.Err, "auto-generated branch should not exist")
}

func TestPromote_TargetBranch_DryRun(t *testing.T) {
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

	repo.Branch("my-release").Checkout("main")
	require.NoError(t, repo.Err)

	actions := &dryRunPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeDryRun, actions, "my-release", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	p := result.Promotions[0]
	assert.Equal(t, "my-release", p.Branch)
	assert.False(t, p.Skipped)

	stageChart, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.198.3-stage", stageChart.GetVersion(), "dry-run should not modify files")
}

func TestPromote_TargetBranch_DoesNotExist(t *testing.T) {
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

	actions := &localPromoteActions{}
	_, err := RunPromote(configPath(repo), ModeLocal, actions, "nonexistent-branch", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent-branch")
	assert.Contains(t, err.Error(), "does not exist")
}

func TestPromote_TargetBranch_SingleChart(t *testing.T) {
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

	repo.Branch("feature/promote").Checkout("main")
	require.NoError(t, repo.Err)

	actions := &localPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, actions, "feature/promote", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.Equal(t, "feature/promote", result.Promotions[0].Branch)

	repo.Checkout("feature/promote")
	require.NoError(t, repo.Err)

	promoted, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.198.3-stage", promoted.GetVersion())
}

func TestPromote_TargetBranch_NoDiffs(t *testing.T) {
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

	repo.Branch("my-release").Checkout("main")
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "my-release", "")
	require.NoError(t, err)

	require.Len(t, result.Promotions, 1)
	assert.True(t, result.Promotions[0].Skipped)
	assert.Equal(t, "no differences", result.Promotions[0].SkipReason)
	assert.Empty(t, mock.executions)
}

func TestPromote_TargetBranch_PartialFailure(t *testing.T) {
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
					Version("2.0.0").
					Dep("service-auth", "2.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/service-config/dev",
				git.NewChart("service-config").
					Version("3.0.0").
					Dep("service-config", "3.0.0", "oci://registry/helm"),
			),
		)
	require.NoError(t, repo.Err)

	repo.Branch("my-release").Checkout("main")
	require.NoError(t, repo.Err)

	mock := &mockPromoteActions{}
	result, err := RunPromote(configPath(repo), ModeLocal, mock, "my-release", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service-auth")
	assert.Contains(t, err.Error(), "service-config")

	require.Len(t, mock.executions, 1)
	assert.Equal(t, "service-ui", mock.executions[0].App)
	assert.Equal(t, "my-release", mock.executions[0].Branch)

	var errorEntries []PromotionEntry
	for _, p := range result.Promotions {
		if p.HasError {
			errorEntries = append(errorEntries, p)
		}
	}
	assert.Len(t, errorEntries, 2)
}

func TestPromote_TargetBranch_EmptyString_DefaultBehavior(t *testing.T) {
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
	assert.Equal(t, "hydra/promote/to-stage/demo/service-ui", result.Promotions[0].Branch)
}
