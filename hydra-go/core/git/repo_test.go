package git

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

func TestInit_CreatesGitRepo(t *testing.T) {
	repo := Init(t.TempDir())
	require.NoError(t, repo.Err)
	require.DirExists(t, filepath.Join(repo.Path(), ".git"))
}

func TestOpen_ExistingRepo(t *testing.T) {
	dir := t.TempDir()
	Init(dir)

	repo := Open(dir)
	require.NoError(t, repo.Err)
	require.Equal(t, dir, repo.Path())
}

func TestOpen_NotARepo(t *testing.T) {
	repo := Open(t.TempDir())
	require.Error(t, repo.Err)
}

// go-git v5 does not support linked worktrees; Open returns ErrLinkedWorkTreeNotSupported.
func TestOpen_LinkedWorktree_Rejected(t *testing.T) {
	_, err := exec.LookPath("git")
	require.NoError(t, err)

	mainDir := t.TempDir()
	wtDir := t.TempDir()

	for _, a := range [][]string{
		{"git", "init", "-b", "main", mainDir},
		{"git", "-C", mainDir, "config", "user.email", "t@e"},
		{"git", "-C", mainDir, "config", "user.name", "t"},
	} {
		out, err2 := exec.Command(a[0], a[1:]...).CombinedOutput()
		require.NoError(t, err2, "args=%v: %s", a, string(out))
	}
	f := filepath.Join(mainDir, "a.txt")
	require.NoError(t, os.WriteFile(f, []byte("x\n"), 0o644))
	out, err := exec.Command("git", "-C", mainDir, "add", "a.txt").CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("git", "-C", mainDir, "commit", "-m", "init").CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("git", "-C", mainDir, "branch", "other-branch").CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("git", "-C", mainDir, "worktree", "add", wtDir, "other-branch").CombinedOutput()
	require.NoError(t, err, string(out))

	repo := Open(wtDir)
	require.Error(t, repo.Err)
	assert.True(t, errors.Is(repo.Err, ErrLinkedWorkTreeNotSupported))
}

func TestCommit_SingleFile(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello")
	require.NoError(t, repo.Err)

	content, err := os.ReadFile(filepath.Join(repo.Path(), "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(content))
}

func TestCommit_ChainedCalls(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("first", "a.txt", "hello").
		Commit("second", "a.txt", "hello2")
	require.NoError(t, repo.Err)

	content, err := os.ReadFile(filepath.Join(repo.Path(), "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello2", string(content))
}

func TestCommit_SubDirectory(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "sub/dir/file.txt", "nested")
	require.NoError(t, repo.Err)
	require.FileExists(t, filepath.Join(repo.Path(), "sub/dir/file.txt"))
}

func TestCommitFiles_MultipleFiles(t *testing.T) {
	repo := Init(t.TempDir()).
		CommitFiles("init", map[string]string{
			"a.txt": "hello",
			"b.txt": "world",
		})
	require.NoError(t, repo.Err)

	a, _ := os.ReadFile(filepath.Join(repo.Path(), "a.txt"))
	b, _ := os.ReadFile(filepath.Join(repo.Path(), "b.txt"))
	require.Equal(t, "hello", string(a))
	require.Equal(t, "world", string(b))
}

func TestCommitFS_WithCharts(t *testing.T) {
	fs := NewFS().
		File("config.yaml", "key: value").
		Add("charts/app/dev",
			NewChart("app").
				Version("1.0.0-dev").
				Dep("app", "1.0.0", "oci://registry/charts"),
		)

	repo := Init(t.TempDir()).
		CommitFS("init", fs)
	require.NoError(t, repo.Err)

	require.FileExists(t, filepath.Join(repo.Path(), "config.yaml"))
	require.FileExists(t, filepath.Join(repo.Path(), "charts/app/dev/Chart.yaml"))
}

func TestTag_Create(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Tag("v1.0.0")
	require.NoError(t, repo.Err)

	tags, err := repo.Tags("v*")
	require.NoError(t, err)
	require.Equal(t, []string{"v1.0.0"}, tags)
}

func TestTag_Multiple(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Tag("build-202603051555").
		Commit("change", "a.txt", "hello2").
		Tag("demo-service-ui-1.200.9-dev")
	require.NoError(t, repo.Err)

	tags, err := repo.Tags("build-*")
	require.NoError(t, err)
	require.Equal(t, []string{"build-202603051555"}, tags)

	allTags, err := repo.Tags("*")
	require.NoError(t, err)
	require.Len(t, allTags, 2)
}

func TestTags_NoMatch(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Tag("v1.0.0")
	require.NoError(t, repo.Err)

	tags, err := repo.Tags("build-*")
	require.NoError(t, err)
	require.Nil(t, tags)
}

func TestBranch_CreateAndCheckout(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Branch("feature/test")
	require.NoError(t, repo.Err)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	require.Equal(t, "feature/test", branch)
}

func TestBranch_WithUnstagedChanges_ReportsGitDiff(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello\n")
	require.NoError(t, repo.Err)

	file := filepath.Join(repo.Path(), "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello\nchanged\n"), 0o644))

	repo.Branch("feature/test")
	require.Error(t, repo.Err)
	assert.ErrorContains(t, repo.Err, "worktree contains unstaged changes")
	assert.ErrorContains(t, repo.Err, "git diff:")
	assert.ErrorContains(t, repo.Err, "+changed")
}

func TestCheckout_ExistingBranch(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Branch("feature/test").
		Commit("on branch", "b.txt", "branch content").
		Checkout("main")
	require.NoError(t, repo.Err)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	require.Equal(t, "main", branch)

	require.NoFileExists(t, filepath.Join(repo.Path(), "b.txt"))
}

func TestCheckout_SameBranchIsNoop(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello")
	require.NoError(t, repo.Err)

	branch, _ := repo.CurrentBranch()
	require.Equal(t, "main", branch)

	repo.Checkout("main")
	require.NoError(t, repo.Err)

	branch, _ = repo.CurrentBranch()
	require.Equal(t, "main", branch)
}

func TestCheckout_MissingLocalBranch_CreatesFromOriginHead(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello")
	require.NoError(t, repo.Err)

	head, err := repo.repo.Head()
	require.NoError(t, err)
	require.NoError(t, repo.repo.Storer.SetReference(plumbing.NewHashReference(
		plumbing.ReferenceName("refs/remotes/origin/main"), head.Hash(),
	)))
	require.NoError(t, repo.repo.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.ReferenceName("refs/remotes/origin/HEAD"),
		plumbing.ReferenceName("refs/remotes/origin/main"),
	)))

	out, err := exec.Command("git", "-C", repo.Path(), "checkout", "--detach", "HEAD").CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("git", "-C", repo.Path(), "branch", "-D", "main").CombinedOutput()
	require.NoError(t, err, string(out))

	repo = Open(repo.Path())
	require.NoError(t, repo.Err)

	var buf bytes.Buffer
	oldLogger := log.Default()
	log.SetDefault(log.NewLoggerWithHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer log.SetDefault(oldLogger)

	repo.Checkout("main")
	require.NoError(t, repo.Err)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	require.Equal(t, "main", branch)
	require.True(t, repo.BranchExists("main"))
	assert.Contains(t, buf.String(), "resolved origin/HEAD before checkout")
	assert.Contains(t, buf.String(), "branch=main")
	assert.Contains(t, buf.String(), "local branch missing; creating it from origin/HEAD")
}

func TestCheckoutUpstreamBranch_UsesConfiguredRemoteBranch(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello")
	require.NoError(t, repo.Err)

	head, err := repo.repo.Head()
	require.NoError(t, err)
	require.NoError(t, repo.repo.Storer.SetReference(plumbing.NewHashReference(
		plumbing.ReferenceName("refs/remotes/origin/stable"), head.Hash(),
	)))

	out, err := exec.Command("git", "-C", repo.Path(), "checkout", "--detach", "HEAD").CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("git", "-C", repo.Path(), "branch", "-D", "main").CombinedOutput()
	require.NoError(t, err, string(out))

	repo = Open(repo.Path())
	require.NoError(t, repo.Err)

	var buf bytes.Buffer
	oldLogger := log.Default()
	log.SetDefault(log.NewLoggerWithHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer log.SetDefault(oldLogger)

	repo.CheckoutUpstreamBranch("origin/stable")
	require.NoError(t, repo.Err)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	require.Equal(t, "stable", branch)
	require.True(t, repo.BranchExists("stable"))
	assert.Contains(t, buf.String(), "resolved upstream branch before checkout")
	assert.Contains(t, buf.String(), "upstream=origin/stable")
	assert.Contains(t, buf.String(), "branch=stable")
}

func TestCheckout_PreservesUntrackedFiles(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "a.txt", "hello").
		Branch("feature/test").
		Commit("on branch", "b.txt", "branch content").
		Checkout("main")
	require.NoError(t, repo.Err)

	untrackedDir := filepath.Join(repo.Path(), "untracked-nested")
	require.NoError(t, os.MkdirAll(untrackedDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(untrackedDir, "file.txt"), []byte("keep me"), 0o644))

	repo.Branch("another-branch").
		Commit("more work", "c.txt", "content").
		Checkout("main")
	require.NoError(t, repo.Err)

	require.DirExists(t, untrackedDir, "untracked directory must survive checkout")
	content, err := os.ReadFile(filepath.Join(untrackedDir, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "keep me", string(content))
}

func TestCheckout_PreservesNestedGitRepo(t *testing.T) {
	parentDir := t.TempDir()
	parentRepo := Init(parentDir).
		Commit("init", "a.txt", "hello")
	require.NoError(t, parentRepo.Err)

	nestedDir := filepath.Join(parentDir, "nested-repo")
	nestedRepo := Init(nestedDir).
		Commit("nested init", "nested.txt", "nested content")
	require.NoError(t, nestedRepo.Err)

	parentRepo.Branch("feature").
		Commit("on feature", "b.txt", "feature content").
		Checkout("main")
	require.NoError(t, parentRepo.Err)

	require.DirExists(t, nestedDir, "nested git repo must survive checkout")
	require.FileExists(t, filepath.Join(nestedDir, "nested.txt"))
	content, err := os.ReadFile(filepath.Join(nestedDir, "nested.txt"))
	require.NoError(t, err)
	require.Equal(t, "nested content", string(content))
}

func TestCommitFS_IgnoresPreviouslyStagedFiles(t *testing.T) {
	repo := Init(t.TempDir()).
		CommitFS("init", NewFS().File("existing.txt", "hello\n"))
	require.NoError(t, repo.Err)

	stagedPath := filepath.Join(repo.Path(), "staged.txt")
	require.NoError(t, os.WriteFile(stagedPath, []byte("should not be committed\n"), 0o644))
	cmd := exec.Command("git", "-C", repo.Path(), "add", "staged.txt")
	require.NoError(t, cmd.Run())

	repo.CommitFS("promote", NewFS().File("promoted.txt", "promoted\n"))
	require.NoError(t, repo.Err)

	out, err := exec.Command("git", "-C", repo.Path(),
		"diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	require.NoError(t, err)
	names := strings.TrimSpace(string(out))
	assert.Contains(t, names, "promoted.txt")
	assert.NotContains(t, names, "staged.txt")

	assert.FileExists(t, stagedPath)
}

func TestCommit_IgnoresPreviouslyStagedFiles(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("init", "existing.txt", "hello\n")
	require.NoError(t, repo.Err)

	stagedPath := filepath.Join(repo.Path(), "staged.txt")
	require.NoError(t, os.WriteFile(stagedPath, []byte("should not be committed\n"), 0o644))
	cmd := exec.Command("git", "-C", repo.Path(), "add", "staged.txt")
	require.NoError(t, cmd.Run())

	repo.Commit("second", "new.txt", "new content\n")
	require.NoError(t, repo.Err)

	out, err := exec.Command("git", "-C", repo.Path(),
		"diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	require.NoError(t, err)
	names := strings.TrimSpace(string(out))
	assert.Contains(t, names, "new.txt")
	assert.NotContains(t, names, "staged.txt")
}

func TestRecentCommitSubjects(t *testing.T) {
	repo := Init(t.TempDir()).
		Commit("first", "a.txt", "1").
		Commit("second line\nbody", "b.txt", "2")
	require.NoError(t, repo.Err)

	subjects, err := repo.RecentCommitSubjects(5)
	require.NoError(t, err)
	require.Len(t, subjects, 2)
	assert.Equal(t, "second line", subjects[0])
	assert.Equal(t, "first", subjects[1])
}

func TestErrorAccumulation(t *testing.T) {
	repo := Init(t.TempDir())
	repo.Err = os.ErrPermission

	repo.Commit("should not run", "a.txt", "hello")
	require.ErrorIs(t, repo.Err, os.ErrPermission)
	require.NoFileExists(t, filepath.Join(repo.Path(), "a.txt"))
}
