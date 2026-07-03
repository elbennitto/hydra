package ci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

func TestRunAuto_TestStepDryRun_NoChangedCharts(t *testing.T) {
	dir := t.TempDir()
	appsDir := filepath.Join(dir, "apps")
	require.NoError(t, os.MkdirAll(appsDir, 0755))

	r := git.Init(appsDir)
	r.Commit("init", "README.md", "x")
	require.NoError(t, r.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	const hydraCI = "ci:\n" +
		"  rootAppsPath: apps\n" +
		"  environments:\n" +
		"    - dev\n" +
		"    - stage\n" +
		"    - prod\n" +
		"  autoSteps:\n" +
		"    - test\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(hydraCI), 0644))

	require.NoError(t, RunAuto(cfgPath, ModeDryRun, "", "", nil))
}

func TestRunAuto_CustomStepsReleaseFirst(t *testing.T) {
	dir := t.TempDir()
	appsDir := filepath.Join(dir, "apps")
	require.NoError(t, os.MkdirAll(appsDir, 0755))

	r := git.Init(appsDir)
	r.Commit("init", "README.md", "x")
	require.NoError(t, r.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	const hydraCI = `ci:
  rootAppsPath: apps
  environments:
    - dev
    - stage
    - prod
  autoSteps:
    - release
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(hydraCI), 0644))

	err := RunAuto(cfgPath, ModeDryRun, "", "", nil)
	// Release runs change detection; empty repo layout yields no chart dirs or other errors.
	require.NoError(t, err)
}

func TestRunAuto_PackageDryRun(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage, prod", []string{"publish"})).
			Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev")),
		).
		Tag("build-202601011200").
		Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	old := helmRunHook
	helmRunHook = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("helm must not run in dry-run")
	}
	t.Cleanup(func() { helmRunHook = old })

	require.NoError(t, RunAuto(configPath(repo), ModeDryRun, "", "", nil))
}

// autoConfigYAML returns a full .hydra-ci.yaml body like configYAML plus autoSteps.
// Use with git.Init(repoRoot) and chart paths apps/<group>/<app>/<env> (same layout as release_test).
func autoConfigYAML(envs string, steps []string) string {
	var b string
	for _, s := range steps {
		b += "    - " + s + "\n"
	}
	return configYAML(envs, "") + "  autoSteps:\n" + b
}

func TestRunAuto_Local_ReleaseOnlyValuesBump(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage", []string{"release"})).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.0-dev").
					Values("apps:\n  service-ui:\n    enabled: true\n    version: \"1.200.9-dev\"\n"),
			),
		).
		Tag("build-001").
		Commit("tweak values", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	require.NoError(t, RunAuto(configPath(repo), ModeLocal, "", "", nil))

	ch, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-1-dev", ch.GetVersion())
	subjects, err := repo.RecentCommitSubjects(1)
	require.NoError(t, err)
	require.NotEmpty(t, subjects)
	assert.Contains(t, subjects[0], "release:")
}

func TestRunAuto_Local_ValuesChangeReleaseThenPromoteGitOrder(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage", []string{"release", "promote"})).
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
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.0-dev").
					Values("apps:\n  service-ui:\n    enabled: true\n    version: \"1.200.9-dev\"\n"),
			),
		).
		Tag("build-001").
		Commit("tweak values", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	require.NoError(t, RunAuto(configPath(repo), ModeLocal, "", "", nil))

	repo.Checkout("main")
	require.NoError(t, repo.Err)
	mainSubjects, err := repo.RecentCommitSubjects(2)
	require.NoError(t, err)
	require.NotEmpty(t, mainSubjects)
	assert.Contains(t, mainSubjects[0], "release:")

	promoteBranch := PromoteBranch("stage", "demo", "service-ui")
	repo.Checkout(promoteBranch)
	require.NoError(t, repo.Err)
	pbSubjects, err := repo.RecentCommitSubjects(3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(pbSubjects), 2)
	assert.Contains(t, pbSubjects[0], "promote:")
	assert.Contains(t, pbSubjects[1], "release:")

	stageChart, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-stage", stageChart.GetVersion())
}

func TestRunAuto_Local_PromoteOnlyNoReleaseCommitOnMain(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage", []string{"promote"})).
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
		Tag("build-001")
	require.NoError(t, repo.Err)

	mainBefore, err := repo.RecentCommitSubjects(3)
	require.NoError(t, err)
	require.NotEmpty(t, mainBefore)

	require.NoError(t, RunAuto(configPath(repo), ModeLocal, "", "", nil))

	repo.Checkout("main")
	require.NoError(t, repo.Err)
	mainAfter, err := repo.RecentCommitSubjects(3)
	require.NoError(t, err)
	assert.Equal(t, mainBefore, mainAfter, "promote must not add commits on main when using promote branches")

	repo.Checkout(PromoteBranch("stage", "demo", "service-ui"))
	require.NoError(t, repo.Err)
	stageChart, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-stage", stageChart.GetVersion())
}

func TestRunAuto_DryRun_ReleasePromoteUnchangedCharts(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage", []string{"release", "promote"})).
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
		Tag("build-001").
		Commit("tweak values", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	require.NoError(t, RunAuto(configPath(repo), ModeDryRun, "", "", nil))

	ch, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-dev", ch.GetVersion())
}

func TestRunAuto_PromoteToFiltersTargetEnv(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage, prod", []string{"promote"})).
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

	require.NoError(t, RunAuto(configPath(repo), ModeLocal, "", "stage", nil))

	assert.False(t, repo.BranchExists("hydra/promote/to-prod/demo/service-ui"))
	assert.True(t, repo.BranchExists("hydra/promote/to-stage/demo/service-ui"))
}

func TestRunAuto_TargetBranchPromoteNoHydraBranch(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage", []string{"promote"})).
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

	repo.Branch("my-auto").Checkout("main")
	require.NoError(t, repo.Err)

	require.NoError(t, RunAuto(configPath(repo), ModeLocal, "my-auto", "", nil))

	assert.False(t, repo.BranchExists("hydra/promote/to-stage/demo/service-ui"))

	repo.Checkout("my-auto")
	require.NoError(t, repo.Err)
	ui, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-stage", ui.GetVersion())
}

func TestRunAuto_OnPromoteEntryRecordsEachPromotion(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage", []string{"promote"})).
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

	var appsSeen []string
	require.NoError(t, RunAuto(configPath(repo), ModeLocal, "", "", func(e PromotionEntry) {
		if !e.Skipped {
			appsSeen = append(appsSeen, e.App)
		}
	}))
	require.Len(t, appsSeen, 2)
	sort.Strings(appsSeen)
	assert.Equal(t, []string{"service-auth", "service-ui"}, appsSeen)
}

func TestRunAuto_EmptyReleasePlanStillRunsPromote(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", autoConfigYAML("dev, stage", []string{"release", "promote"})).
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
		Tag("build-001")
	require.NoError(t, repo.Err)

	require.NoError(t, RunAuto(configPath(repo), ModeLocal, "", "", nil))

	repo.Checkout("main")
	require.NoError(t, repo.Err)
	mainSubjects, err := repo.RecentCommitSubjects(2)
	require.NoError(t, err)
	require.Len(t, mainSubjects, 1)
	assert.Equal(t, "init", mainSubjects[0])

	promoteBranch := PromoteBranch("stage", "demo", "service-ui")
	repo.Checkout(promoteBranch)
	require.NoError(t, repo.Err)
	pbSubjects, err := repo.RecentCommitSubjects(1)
	require.NoError(t, err)
	require.NotEmpty(t, pbSubjects)
	assert.Contains(t, pbSubjects[0], "promote:")
}
