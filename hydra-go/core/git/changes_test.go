package git

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChangedPaths_SingleFile(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Tag("v1").
		Commit("change", "a.txt", "hello2")
	require.NoError(t, repo.Err)

	paths, err := repo.ChangedPaths("v1", "HEAD")
	require.NoError(t, err)
	require.Equal(t, []string{"a.txt"}, paths)
}

func TestChangedPaths_NoChanges(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Tag("v1")
	require.NoError(t, repo.Err)

	paths, err := repo.ChangedPaths("v1", "HEAD")
	require.NoError(t, err)
	require.Nil(t, paths)
}

func TestChangedPaths_MultipleFiles(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Tag("v1").
		CommitFiles("changes", map[string]string{
			"a.txt": "changed",
			"b.txt": "new",
		})
	require.NoError(t, repo.Err)

	paths, err := repo.ChangedPaths("v1", "HEAD")
	require.NoError(t, err)
	require.Len(t, paths, 2)
	require.Contains(t, paths, "a.txt")
	require.Contains(t, paths, "b.txt")
}

func TestHasChanges_WithChanges(t *testing.T) {
	repo := Init(t.TempDir()).
		CommitFS("init", NewFS().
			Add("apps/demo/service-ui/dev",
				NewChart("service-ui").Version("1.0.0-dev"),
			),
		).
		Tag("build-001").
		Commit("update", "apps/demo/service-ui/dev/values.yaml", "new: value")
	require.NoError(t, repo.Err)

	changed, err := repo.HasChanges("build-001", "HEAD", "apps/demo/service-ui/dev")
	require.NoError(t, err)
	require.True(t, changed)
}

func TestHasChanges_NoChanges(t *testing.T) {
	repo := Init(t.TempDir()).
		CommitFS("init", NewFS().
			Add("apps/demo/service-ui/dev",
				NewChart("service-ui").Version("1.0.0-dev"),
			),
		).
		Tag("build-001")
	require.NoError(t, repo.Err)

	changed, err := repo.HasChanges("build-001", "HEAD", "apps/demo/service-ui/dev")
	require.NoError(t, err)
	require.False(t, changed)
}

func TestHasChanges_UnrelatedDirectory(t *testing.T) {
	repo := Init(t.TempDir()).
		CommitFS("init", NewFS().
			Add("apps/demo/service-ui/dev",
				NewChart("service-ui").Version("1.0.0-dev"),
			).
			Add("apps/demo/service-auth/dev",
				NewChart("service-auth").Version("2.0.0-dev"),
			),
		).
		Tag("build-001").
		Commit("change auth", "apps/demo/service-auth/dev/values.yaml", "new: value")
	require.NoError(t, repo.Err)

	changed, err := repo.HasChanges("build-001", "HEAD", "apps/demo/service-ui/dev")
	require.NoError(t, err)
	require.False(t, changed, "service-ui should not be detected as changed")

	changed, err = repo.HasChanges("build-001", "HEAD", "apps/demo/service-auth/dev")
	require.NoError(t, err)
	require.True(t, changed, "service-auth should be detected as changed")
}

func TestHasChanges_DevOnlyNotStage(t *testing.T) {
	repo := Init(t.TempDir()).
		CommitFS("init", NewFS().
			Add("apps/demo/service-ui/dev",
				NewChart("service-ui").Version("1.0.0-dev"),
			).
			Add("apps/demo/service-ui/stage",
				NewChart("service-ui").Version("1.0.0-stage"),
			),
		).
		Tag("build-001").
		Commit("change dev", "apps/demo/service-ui/dev/values.yaml", "new: value")
	require.NoError(t, repo.Err)

	devChanged, err := repo.HasChanges("build-001", "HEAD", "apps/demo/service-ui/dev")
	require.NoError(t, err)
	require.True(t, devChanged)

	stageChanged, err := repo.HasChanges("build-001", "HEAD", "apps/demo/service-ui/stage")
	require.NoError(t, err)
	require.False(t, stageChanged)
}

func TestLastBuildTag_FindsTag(t *testing.T) {
	repo := Init(t.TempDir()).
		CommitFS("init", NewFS().
			Add("apps/demo/service-ui/dev",
				NewChart("service-ui").Version("1.0.0-dev"),
			),
		).
		Tag("build-001").
		Commit("unrelated", "other.txt", "x").
		Tag("build-002").
		Commit("change ui", "apps/demo/service-ui/dev/values.yaml", "new: value").
		Tag("build-003")
	require.NoError(t, repo.Err)

	tag, err := repo.LastBuildTag("apps/demo/service-ui/dev")
	require.NoError(t, err)
	require.Equal(t, "build-003", tag)
}

func TestLastBuildTag_NoTags(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello")
	require.NoError(t, repo.Err)

	_, err := repo.LastBuildTag("apps/demo/service-ui/dev")
	require.Error(t, err)
}

func TestFirstCommitAffectingPath_FindsIntroductionCommit(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		CommitFS("add chart", NewFS().
			Add("apps/demo/service-ui/dev",
				NewChart("service-ui").Version("1.0.0-dev"),
			),
		).
		Commit("change chart", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)

	h, err := repo.FirstCommitAffectingPath("apps/demo/service-ui/dev")
	require.NoError(t, err)

	commit, err := repo.resolveCommit(h)
	require.NoError(t, err)
	require.Equal(t, "add chart", commit.Message)
}

func TestInitialCommitHash(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Commit("second", "b.txt", "world")
	require.NoError(t, repo.Err)

	h, err := repo.InitialCommitHash()
	require.NoError(t, err)
	require.NotEmpty(t, h)

	c, err := repo.resolveCommit(h)
	require.NoError(t, err)
	require.Equal(t, 0, c.NumParents())
}
