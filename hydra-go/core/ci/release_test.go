package ci

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

func TestRunRelease_NoChanges(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		).
		Tag("build-001")
	require.NoError(t, repo.Err)

	res, err := RunRelease(configPath(repo), ModeLocal, "")
	require.NoError(t, err)
	assert.Empty(t, res.Children)
}

func TestRunRelease_Local_ExtraVersionAndRoot(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
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

	res, err := RunRelease(configPath(repo), ModeLocal, "")
	require.NoError(t, err)
	require.Len(t, res.Children, 1)
	assert.Equal(t, "1.200.9-1-dev", res.Children[0].NewVersion)

	child, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-1-dev", child.GetVersion())

	root, err := repo.LoadChart("apps/demo/root/dev")
	require.NoError(t, err)
	assert.Equal(t, "200.22.1-dev", root.GetVersion())
	ver, err := root.GetValue("apps.service-ui.version")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-1-dev", ver)

	tags, err := repo.Tags("demo-*")
	require.NoError(t, err)
	assert.Contains(t, tags, "demo-service-ui-1.200.9-1-dev")
	assert.Contains(t, tags, "demo-root-200.22.1-dev")

	buildTags, err := repo.Tags("build-*")
	require.NoError(t, err)
	assert.Contains(t, buildTags, "build-202603051555")
}

// When the upstream dependency semver changes, the child wrapper’s base
// changes (1.0.0-dev → 1.0.1-dev) and the root app must get a middle-component
// bump, not a patch-only bump.
func TestRunRelease_Local_DependencyBumpsRootMinor(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.0", "oci://registry/helm"),
			).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.0-dev").
					Values("apps:\n  service-ui:\n    enabled: true\n    version: \"1.0.0-dev\"\n"),
			),
		).
		Tag("build-001").
		CommitFS("bump upstream", git.NewFS().
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.0.0-dev").
					Dep("service-ui", "1.0.1", "oci://registry/helm").
					Values("n: 1\n"),
			),
		)
	require.NoError(t, repo.Err)

	res, err := RunRelease(configPath(repo), ModeLocal, "")
	require.NoError(t, err)
	require.Len(t, res.Children, 1)
	assert.Equal(t, "1.0.1-dev", res.Children[0].NewVersion)

	root, err := repo.LoadChart("apps/demo/root/dev")
	require.NoError(t, err)
	assert.Equal(t, "200.23.0-dev", root.GetVersion())
	ver, err := root.GetValue("apps.service-ui.version")
	require.NoError(t, err)
	assert.Equal(t, "1.0.1-dev", ver)
}

func TestRunRelease_Local_NormalizesRootEnvSuffixBeforeTagging(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.2.0-dev").
					Dep("service-ui", "1.2.0", "oci://registry/helm"),
			).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("1.2.0").
					Values("apps:\n  service-ui:\n    enabled: true\n    version: \"1.2.0-dev\"\n"),
			),
		).
		Tag("build-001").
		Commit("tweak values", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	_, err := RunRelease(configPath(repo), ModeLocal, "")
	require.NoError(t, err)

	root, err := repo.LoadChart("apps/demo/root/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.2.1-dev", root.GetVersion())

	tags, err := repo.Tags("demo-root-*")
	require.NoError(t, err)
	assert.Contains(t, tags, "demo-root-1.2.1-dev")
	assert.NotContains(t, tags, "demo-root-1.2.1")
}

func TestRunRelease_DryRun_NoWrites(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
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
		Commit("tweak", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	res, err := RunRelease(configPath(repo), ModeDryRun, "")
	require.NoError(t, err)
	require.Len(t, res.Children, 1)

	ch, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-dev", ch.GetVersion(), "dry-run must not write Chart.yaml")
}

func TestRunRelease_CI_NotImplemented(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
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
		Commit("tweak", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	_, err := RunRelease(configPath(repo), ModeCI, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestDependencyVersionForWrapperRelease(t *testing.T) {
	tests := []struct {
		name    string
		chart   *git.Chart
		oldVer  string
		want    string
		wantErr bool
	}{
		{
			name:   "no deps uses chart base dev",
			chart:  git.NewChart("standalone").Version("0.1.0-dev"),
			oldVer: "0.1.0-dev",
			want:   "0.1.0",
		},
		{
			name:   "no deps prerelease segment preserved",
			chart:  git.NewChart("backend").Version("1.5.1-2b866935-dev"),
			oldVer: "1.5.1-2b866935-dev",
			want:   "1.5.1-2b866935",
		},
		{
			name:   "dependency wins when present",
			chart:  git.NewChart("service-ui").Version("1.0.0-dev").Dep("service-ui", "9.9.9", "oci://registry/helm"),
			oldVer: "1.0.0-dev",
			want:   "9.9.9",
		},
		{
			name: "only wildcard file deps uses chart base",
			chart: git.NewChart("unit-test-app").Version("1.0.0-dev").
				Dep("upstream-lib", "*", "file://charts/upstream").
				Dep("infra_library", "*", "file://shared/infra_library/dev"),
			oldVer: "1.0.0-dev",
			want:   "1.0.0",
		},
		{
			name:    "no deps invalid chart version",
			chart:   git.NewChart("bad").Version("not-semver"),
			oldVer:  "not-semver",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dependencyVersionForWrapperRelease(tt.chart, tt.oldVer, "apps/g/example/dev")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunRelease_Local_NoChartYAMLDependencies(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/cluster-infra/standalone-wrapper/dev",
				git.NewChart("standalone-wrapper").
					Version("0.1.0-dev"),
			).
			Add("apps/cluster-infra/root/dev",
				git.NewChart("cluster-infra").
					Version("100.0.0-dev").
					Values("apps:\n  standalone-wrapper:\n    enabled: true\n    version: \"0.1.0-dev\"\n"),
			),
		).
		Tag("build-001").
		Commit("tweak values", "apps/cluster-infra/standalone-wrapper/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	res, err := RunRelease(configPath(repo), ModeLocal, "")
	require.NoError(t, err)
	require.Len(t, res.Children, 1)
	assert.Equal(t, "0.1.0-1-dev", res.Children[0].NewVersion)

	child, err := repo.LoadChart("apps/cluster-infra/standalone-wrapper/dev")
	require.NoError(t, err)
	assert.Equal(t, "0.1.0-1-dev", child.GetVersion())
}

// When child charts exist for stage/prod but the umbrella root chart exists only
// for some environments (e.g. only apps/demo/root/dev), release must still bump
// children and not fail loading a non-existent root path.
func TestRunRelease_Local_SkipsMissingRootChartForEnv(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage, prod", "")).
			Add("apps/demo/service-ui/stage",
				git.NewChart("service-ui").
					Version("1.200.9-stage").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			).
			Add("apps/demo/root/dev",
				git.NewChart("demo").
					Version("200.22.0-dev").
					Values("apps:\n  service-ui:\n    enabled: true\n    version: \"1.200.9-dev\"\n"),
			),
		).
		Tag("build-001").
		Commit("tweak stage", "apps/demo/service-ui/stage/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	res, err := RunRelease(configPath(repo), ModeLocal, "")
	require.NoError(t, err)
	require.Len(t, res.Children, 1)
	assert.Equal(t, "stage", res.Children[0].Env)

	stageChild, err := repo.LoadChart("apps/demo/service-ui/stage")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-1-stage", stageChild.GetVersion())

	_, statErr := os.Stat(filepath.Join(repo.Path(), "apps/demo/root/stage"))
	require.Error(t, statErr)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRunRelease_Local_TargetBranch(t *testing.T) {
	oldClock := releaseTagTime
	releaseTagTime = func() time.Time { return time.Date(2026, 3, 5, 15, 55, 0, 0, time.UTC) }
	t.Cleanup(func() { releaseTagTime = oldClock })

	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
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
		Branch("work").
		Commit("tweak values", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	br, err := repo.CurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "work", br)

	res, err := RunRelease(configPath(repo), ModeLocal, "work")
	require.NoError(t, err)
	require.Len(t, res.Children, 1)

	brAfter, err := repo.CurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "work", brAfter)

	child, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-1-dev", child.GetVersion())

	subjects, err := repo.RecentCommitSubjects(2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(subjects), 1)
	assert.Contains(t, subjects[0], "release:")
}

func TestRunRelease_Local_TargetBranchMissing(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		).
		Tag("build-001").
		Commit("tweak", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	_, err := RunRelease(configPath(repo), ModeLocal, "no-such-branch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-branch")
}

func TestRunRelease_DryRun_WithTargetBranch(t *testing.T) {
	repo := git.Init(t.TempDir()).
		CommitFS("init", git.NewFS().
			File(".hydra-ci.yaml", configYAML("dev, stage", "")).
			Add("apps/demo/service-ui/dev",
				git.NewChart("service-ui").
					Version("1.200.9-dev").
					Dep("service-ui", "1.200.9", "oci://registry/helm"),
			),
		).
		Tag("build-001").
		Branch("work").
		Commit("tweak", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	res, err := RunRelease(configPath(repo), ModeDryRun, "work")
	require.NoError(t, err)
	require.Len(t, res.Children, 1)

	ch, err := repo.LoadChart("apps/demo/service-ui/dev")
	require.NoError(t, err)
	assert.Equal(t, "1.200.9-dev", ch.GetVersion(), "dry-run must not write Chart.yaml")
}
