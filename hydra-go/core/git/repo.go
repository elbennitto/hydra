package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

// ErrLinkedWorkTreeNotSupported is returned by Open when the directory is a Git
// linked worktree (the .git file points at the main repository). The embedded
// git engine (go-git) does not support that layout; use a normal single-tree clone.
var ErrLinkedWorkTreeNotSupported = errors.New("linked git worktree is not supported: check out a full repository clone, not a directory created with \"git worktree add\"")

// Repo represents a Git repository and provides chainable operations.
// Errors accumulate in Err; once set, subsequent chainable calls are no-ops.
type Repo struct {
	Err                     error
	path                    string
	repo                    *gogit.Repository
	upstreamResolutionCache map[string]resolvedUpstreamBranch
}

var commitSignature = object.Signature{
	Name:  "hydra",
	Email: "hydra@hydra.dev",
}

// Init creates a new Git repository at the given path and returns a Repo.
func Init(path string) *Repo {
	r := &Repo{path: path, upstreamResolutionCache: map[string]resolvedUpstreamBranch{}}
	repo, err := gogit.PlainInit(path, false)
	if err != nil {
		r.Err = fmt.Errorf("git init: %w", err)
		return r
	}
	r.repo = repo

	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	if err := repo.Storer.SetReference(head); err != nil {
		r.Err = fmt.Errorf("set default branch: %w", err)
	}
	return r
}

// Open opens an existing Git repository at the given path.
// Walks up the directory tree to find the repository root.
func Open(path string) *Repo {
	root, err := findRepoRoot(path)
	if err != nil {
		return &Repo{path: path, Err: err}
	}
	if st, lerr := os.Lstat(filepath.Join(root, ".git")); lerr == nil && !st.IsDir() {
		return &Repo{path: root, Err: ErrLinkedWorkTreeNotSupported}
	}
	r := &Repo{path: root, upstreamResolutionCache: map[string]resolvedUpstreamBranch{}}
	repo, err := gogit.PlainOpen(root)
	if err != nil {
		r.Err = fmt.Errorf("open git repository: %w", err)
		return r
	}
	r.repo = repo
	return r
}

// Path returns the repository root directory.
func (r *Repo) Path() string {
	return r.path
}

// Commit creates a single-file commit. Chainable.
func (r *Repo) Commit(msg, file, content string) *Repo {
	if r.Err != nil {
		return r
	}
	if err := r.writeFile(file, content); err != nil {
		r.Err = fmt.Errorf("commit write %s: %w", file, err)
		return r
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		r.Err = fmt.Errorf("worktree: %w", err)
		return r
	}
	if err := r.resetIndex(wt); err != nil {
		r.Err = fmt.Errorf("reset index: %w", err)
		return r
	}
	if _, err := wt.Add(file); err != nil {
		r.Err = fmt.Errorf("git add %s: %w", file, err)
		return r
	}
	if _, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: r.sig(),
	}); err != nil {
		r.Err = fmt.Errorf("git commit: %w", err)
	}
	return r
}

// CommitFiles creates a commit with multiple files. Chainable.
func (r *Repo) CommitFiles(msg string, files map[string]string) *Repo {
	if r.Err != nil {
		return r
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		r.Err = fmt.Errorf("worktree: %w", err)
		return r
	}
	if err := r.resetIndex(wt); err != nil {
		r.Err = fmt.Errorf("reset index: %w", err)
		return r
	}
	for file, content := range files {
		if err := r.writeFile(file, content); err != nil {
			r.Err = fmt.Errorf("commit write %s: %w", file, err)
			return r
		}
		if _, err := wt.Add(file); err != nil {
			r.Err = fmt.Errorf("git add %s: %w", file, err)
			return r
		}
	}
	if _, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: r.sig(),
	}); err != nil {
		r.Err = fmt.Errorf("git commit: %w", err)
	}
	return r
}

// CommitFS creates a commit from an FS builder. Chainable.
// Processes removals first, then symlinks, then regular files.
func (r *Repo) CommitFS(msg string, fs *FS) *Repo {
	if r.Err != nil {
		return r
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		r.Err = fmt.Errorf("worktree: %w", err)
		return r
	}
	if err := r.resetIndex(wt); err != nil {
		r.Err = fmt.Errorf("reset index: %w", err)
		return r
	}
	for _, file := range fs.removals() {
		absPath := filepath.Join(r.path, file)
		_ = os.Remove(absPath)
		if _, err := wt.Remove(file); err != nil {
			r.Err = fmt.Errorf("git rm %s: %w", file, err)
			return r
		}
	}
	for link, target := range fs.symlinks() {
		if err := r.writeSymlink(link, target); err != nil {
			r.Err = fmt.Errorf("commitFS symlink %s: %w", link, err)
			return r
		}
		if _, err := wt.Add(link); err != nil {
			r.Err = fmt.Errorf("git add %s: %w", link, err)
			return r
		}
	}
	for file, content := range fs.files() {
		if err := r.writeFile(file, content); err != nil {
			r.Err = fmt.Errorf("commitFS write %s: %w", file, err)
			return r
		}
		if _, err := wt.Add(file); err != nil {
			r.Err = fmt.Errorf("git add %s: %w", file, err)
			return r
		}
	}
	if _, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: r.sig(),
	}); err != nil {
		r.Err = fmt.Errorf("git commit: %w", err)
	}
	return r
}

// Tag creates a lightweight tag at HEAD. Chainable.
func (r *Repo) Tag(name string) *Repo {
	if r.Err != nil {
		return r
	}
	head, err := r.repo.Head()
	if err != nil {
		r.Err = fmt.Errorf("get HEAD: %w", err)
		return r
	}
	ref := plumbing.NewHashReference(plumbing.NewTagReferenceName(name), head.Hash())
	if err := r.repo.Storer.SetReference(ref); err != nil {
		r.Err = fmt.Errorf("git tag %s: %w", name, err)
	}
	return r
}

// Tags returns all tags matching a glob pattern, sorted alphabetically.
func (r *Repo) Tags(pattern string) ([]string, error) {
	iter, err := r.repo.Tags()
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	var tags []string
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		if matched, _ := filepath.Match(pattern, name); matched {
			tags = append(tags, name)
		}
		return nil
	})
	if len(tags) == 0 {
		return nil, nil
	}
	sort.Strings(tags)
	return tags, nil
}

// Branch creates and checks out a new branch. Chainable.
func (r *Repo) Branch(name string) *Repo {
	if r.Err != nil {
		return r
	}
	wt, err := r.repo.Worktree()
	if err != nil {
		r.Err = fmt.Errorf("worktree: %w", err)
		return r
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
		Create: true,
	}); err != nil {
		if diff, ok := r.unstagedDiffOnCheckoutError(err); ok {
			r.Err = fmt.Errorf("git checkout -b %s: %w\n\ngit diff:\n%s", name, err, diff)
			return r
		}
		r.Err = fmt.Errorf("git checkout -b %s: %w", name, err)
	}
	return r
}

// Checkout switches to an existing branch. No-op when already on that branch. Chainable.
func (r *Repo) Checkout(name string) *Repo {
	if r.Err != nil {
		return r
	}
	current, err := r.CurrentBranch()
	if err == nil && current == name {
		log.Default().DebugLog(logIdGitRepo, "skipping checkout; already on target branch",
			log.String("branch", name))
		return r
	}

	originHead, originHeadErr := r.resolveOriginHead()
	if originHeadErr != nil {
		log.Default().DebugLog(logIdGitRepo, "origin/HEAD could not be resolved before checkout",
			log.String("branch", name),
			log.String("reason", originHeadErr.Error()))
	} else {
		log.Default().DebugLog(logIdGitRepo, "resolved origin/HEAD before checkout",
			log.String("branch", originHead.localBranch),
			log.String("remoteRef", originHead.remoteRef.String()))
	}

	if r.BranchExists(name) {
		log.Default().DebugLog(logIdGitRepo, "checking out existing local branch",
			log.String("branch", name))
		return r.checkoutBranch(name)
	}

	if originHeadErr == nil {
		log.Default().DebugLog(logIdGitRepo, "local branch missing; creating it from origin/HEAD",
			log.String("branch", name),
			log.String("originHead", originHead.localBranch),
			log.String("remoteRef", originHead.remoteRef.String()))
		return r.checkoutNewBranchFromOriginHead(name, originHead)
	}

	log.Default().Info(logIdGitRepo, "local branch missing and origin/HEAD unavailable; attempting regular checkout",
		log.String("branch", name))

	return r.checkoutBranch(name)
}

type originHeadRef struct {
	localBranch string
	remoteRef   plumbing.ReferenceName
	hash        plumbing.Hash
}

type resolvedUpstreamBranch struct {
	originHeadRef
	requestedUpstream string
}

func (r *Repo) CheckoutUpstreamBranch(upstream string) *Repo {
	if r.Err != nil {
		return r
	}
	cacheKey := upstreamCacheKey(upstream)
	_, cacheHit := r.upstreamResolutionCache[cacheKey]
	resolved, err := r.resolveUpstreamBranch(upstream)
	if err != nil {
		fallbackBranch := fallbackLocalBranchForUpstream(upstream)
		if fallbackBranch != "" && r.BranchExists(fallbackBranch) {
			if current, cerr := r.CurrentBranch(); cerr == nil && current == fallbackBranch {
				log.Default().DebugLog(logIdGitRepo, "skipping checkout; already on target branch",
					log.String("branch", fallbackBranch))
				return r
			}
			log.Default().DebugLog(logIdGitRepo, "configured upstream unavailable; falling back to existing local branch",
				log.String("upstream", upstream),
				log.String("branch", fallbackBranch),
				log.String("reason", err.Error()))
			return r.checkoutBranch(fallbackBranch)
		}
		r.Err = fmt.Errorf("resolve upstream branch %q: %w", upstream, err)
		return r
	}

	if current, cerr := r.CurrentBranch(); cerr == nil && current == resolved.localBranch {
		log.Default().DebugLog(logIdGitRepo, "skipping checkout; already on target branch",
			log.String("branch", resolved.localBranch))
		return r
	}

	if cacheHit {
		log.Default().DebugLog(logIdGitRepo, "using cached upstream branch before checkout",
			log.String("upstream", resolved.requestedUpstream),
			log.String("branch", resolved.localBranch),
			log.String("remoteRef", resolved.remoteRef.String()))
	} else {
		log.Default().DebugLog(logIdGitRepo, "resolved upstream branch before checkout",
			log.String("upstream", resolved.requestedUpstream),
			log.String("branch", resolved.localBranch),
			log.String("remoteRef", resolved.remoteRef.String()))
	}

	if r.BranchExists(resolved.localBranch) {
		log.Default().DebugLog(logIdGitRepo, "checking out existing local branch",
			log.String("branch", resolved.localBranch))
		return r.checkoutBranch(resolved.localBranch)
	}

	log.Default().DebugLog(logIdGitRepo, "local branch missing; creating it from configured upstream",
		log.String("branch", resolved.localBranch),
		log.String("upstream", resolved.requestedUpstream),
		log.String("remoteRef", resolved.remoteRef.String()))
	return r.checkoutNewBranchFromOriginHead(resolved.localBranch, resolved.originHeadRef)
}

func (r *Repo) checkoutBranch(name string) *Repo {
	current, err := r.CurrentBranch()
	if err == nil && current == name {
		log.Default().DebugLog(logIdGitRepo, "skipping checkout; already on target branch",
			log.String("branch", name))
		return r
	}

	wt, err := r.repo.Worktree()
	if err != nil {
		r.Err = fmt.Errorf("worktree: %w", err)
		return r
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
	}); err != nil {
		r.Err = fmt.Errorf("git checkout %s: %w", name, err)
	}
	return r
}

func (r *Repo) checkoutNewBranchFromOriginHead(name string, originHead originHeadRef) *Repo {
	wt, err := r.repo.Worktree()
	if err != nil {
		r.Err = fmt.Errorf("worktree: %w", err)
		return r
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
		Create: true,
		Hash:   originHead.hash,
	}); err != nil {
		r.Err = fmt.Errorf("git checkout -b %s %s: %w", name, originHead.remoteRef.Short(), err)
	}
	return r
}

// PushSetUpstream pushes the given local branch to the given remote and sets upstream.
// Equivalent to: git push --set-upstream <remote> <branch>
func (r *Repo) PushSetUpstream(remote, branch string) *Repo {
	if r.Err != nil {
		return r
	}
	if strings.TrimSpace(remote) == "" {
		r.Err = fmt.Errorf("git push: remote must not be empty")
		return r
	}
	if strings.TrimSpace(branch) == "" {
		r.Err = fmt.Errorf("git push: branch must not be empty")
		return r
	}
	out, err := exec.Command("git", "-C", r.path, "push", "--set-upstream", remote, branch).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			r.Err = fmt.Errorf("git push --set-upstream %s %s: %w", remote, branch, err)
			return r
		}
		r.Err = fmt.Errorf("git push --set-upstream %s %s: %w\n%s", remote, branch, err, msg)
	}
	return r
}

// Fetch updates remote-tracking refs for the given remote.
func (r *Repo) Fetch(remote string) *Repo {
	if r.Err != nil {
		return r
	}
	if strings.TrimSpace(remote) == "" {
		r.Err = fmt.Errorf("git fetch: remote must not be empty")
		return r
	}
	out, err := exec.Command("git", "-C", r.path, "fetch", "--prune", remote).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			r.Err = fmt.Errorf("git fetch --prune %s: %w", remote, err)
			return r
		}
		r.Err = fmt.Errorf("git fetch --prune %s: %w\n%s", remote, err, msg)
		return r
	}

	// Drop cached upstream resolutions because remote refs may have changed.
	r.upstreamResolutionCache = map[string]resolvedUpstreamBranch{}
	return r
}

// BranchExists checks if a branch with the given name exists in the repository.
func (r *Repo) BranchExists(name string) bool {
	if r.Err != nil {
		return false
	}
	ref := plumbing.NewBranchReferenceName(name)
	_, err := r.repo.Reference(ref, false)
	return err == nil
}

// RemoteBranchExists checks if a branch exists for the given remote.
func (r *Repo) RemoteBranchExists(remote, branch string) bool {
	if r.Err != nil {
		return false
	}
	remote = strings.TrimSpace(remote)
	branch = strings.TrimSpace(branch)
	if remote == "" || branch == "" {
		return false
	}
	ref := normalizeRemoteRefName(remote + "/" + strings.TrimPrefix(branch, "refs/heads/"))
	_, err := r.repo.Reference(ref, false)
	return err == nil
}

func (r *Repo) unstagedDiffOnCheckoutError(err error) (string, bool) {
	if err == nil || !strings.Contains(err.Error(), "worktree contains unstaged changes") {
		return "", false
	}
	out, diffErr := exec.Command("git", "-C", r.path, "diff").CombinedOutput()
	diff := strings.TrimSpace(string(out))
	if diffErr != nil {
		if diff == "" {
			return fmt.Sprintf("failed to run git diff: %v", diffErr), true
		}
		return fmt.Sprintf("%s\n\nfailed to run git diff cleanly: %v", diff, diffErr), true
	}
	if diff == "" {
		return "(no unstaged diff output)", true
	}
	return diff, true
}

// LoadChart reads a Helm chart from a directory relative to the repo root.
func (r *Repo) LoadChart(dir string) (*Chart, error) {
	absDir := filepath.Join(r.path, dir)
	return loadChartFromDir(absDir, r.path, dir)
}

// ChartYAMLAt returns the raw Chart.yaml contents at the given ref and chart
// directory.
func (r *Repo) ChartYAMLAt(ref, dir string) (string, error) {
	commit, err := r.resolveCommit(ref)
	if err != nil {
		return "", err
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", err
	}
	file, err := tree.File(filepath.ToSlash(filepath.Join(dir, "Chart.yaml")))
	if err != nil {
		return "", err
	}
	contents, err := file.Contents()
	if err != nil {
		return "", err
	}
	return contents, nil
}

// RemoteURL returns the URL of the "origin" remote.
func (r *Repo) RemoteURL() (string, error) {
	remote, err := r.repo.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("get remote origin: %w", err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("remote origin has no URLs")
	}
	return urls[0], nil
}

// ResolveUpstreamBranch returns the local branch name for the configured upstream ref.
// Examples: "origin/main" -> "main", "origin/HEAD" -> repository default branch.
func (r *Repo) ResolveUpstreamBranch(upstream string) (string, error) {
	if r.Err != nil {
		return "", r.Err
	}
	resolved, err := r.resolveUpstreamBranch(upstream)
	if err != nil {
		return "", err
	}
	return resolved.localBranch, nil
}

// CurrentBranch returns the current branch name.
// Returns "HEAD" when in detached-HEAD state.
func (r *Repo) CurrentBranch() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}
	if head.Name().IsBranch() {
		return head.Name().Short(), nil
	}
	return "HEAD", nil
}

func (r *Repo) resolveOriginHead() (originHeadRef, error) {
	resolved, err := r.resolveUpstreamBranch("origin/HEAD")
	if err != nil {
		return originHeadRef{}, err
	}
	return resolved.originHeadRef, nil
}

func (r *Repo) resolveUpstreamBranch(upstream string) (resolvedUpstreamBranch, error) {
	upstream = upstreamCacheKey(upstream)
	if r.upstreamResolutionCache == nil {
		r.upstreamResolutionCache = map[string]resolvedUpstreamBranch{}
	}
	if cached, ok := r.upstreamResolutionCache[upstream]; ok {
		return cached, nil
	}
	refName := normalizeRemoteRefName(upstream)
	ref, err := r.repo.Reference(refName, false)
	if err != nil {
		return resolvedUpstreamBranch{}, fmt.Errorf("read %s: %w", refName, err)
	}

	target := refName
	if ref.Type() == plumbing.SymbolicReference {
		target = ref.Target()
	}
	if !target.IsRemote() {
		return resolvedUpstreamBranch{}, fmt.Errorf("%s points to non-remote ref %s", refName, target)
	}

	resolved, err := r.repo.Reference(target, true)
	if err != nil {
		return resolvedUpstreamBranch{}, fmt.Errorf("resolve %s: %w", target, err)
	}

	resolvedUpstream := resolvedUpstreamBranch{
		originHeadRef: originHeadRef{
			localBranch: localBranchFromRemoteRef(target),
			remoteRef:   target,
			hash:        resolved.Hash(),
		},
		requestedUpstream: upstream,
	}
	r.upstreamResolutionCache[upstream] = resolvedUpstream
	return resolvedUpstream, nil
}

func upstreamCacheKey(upstream string) string {
	if upstream == "" {
		return "origin/HEAD"
	}
	return upstream
}

func normalizeRemoteRefName(upstream string) plumbing.ReferenceName {
	switch {
	case strings.HasPrefix(upstream, "refs/"):
		return plumbing.ReferenceName(upstream)
	case strings.Contains(upstream, "/"):
		return plumbing.ReferenceName("refs/remotes/" + upstream)
	default:
		return plumbing.ReferenceName("refs/remotes/origin/" + upstream)
	}
}

func localBranchFromRemoteRef(ref plumbing.ReferenceName) string {
	short := ref.Short()
	parts := strings.SplitN(short, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return short
}

func fallbackLocalBranchForUpstream(upstream string) string {
	if upstream == "" || upstream == "origin/HEAD" || upstream == "refs/remotes/origin/HEAD" {
		return "main"
	}
	return localBranchFromRemoteRef(normalizeRemoteRefName(upstream))
}

func (r *Repo) resolveCommit(ref string) (*object.Commit, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", ref, err)
	}
	return r.repo.CommitObject(*hash)
}

// InitialCommitHash returns the hash of the repository's root (first) commit.
func (r *Repo) InitialCommitHash() (string, error) {
	if r.Err != nil {
		return "", r.Err
	}
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}
	c, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return "", fmt.Errorf("commit object: %w", err)
	}
	for {
		if c.NumParents() == 0 {
			return c.Hash.String(), nil
		}
		c, err = c.Parent(0)
		if err != nil {
			return "", fmt.Errorf("walk parents: %w", err)
		}
	}
}

// RecentCommitSubjects returns the first line of each commit message walking
// from HEAD along the first-parent chain, for at most max commits.
func (r *Repo) RecentCommitSubjects(max int) ([]string, error) {
	if r.Err != nil {
		return nil, r.Err
	}
	if max <= 0 {
		return nil, nil
	}
	head, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("get HEAD: %w", err)
	}
	c, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("commit object: %w", err)
	}
	var out []string
	for len(out) < max {
		line := strings.SplitN(c.Message, "\n", 2)[0]
		out = append(out, line)
		if c.NumParents() == 0 {
			break
		}
		c, err = c.Parent(0)
		if err != nil {
			return nil, fmt.Errorf("parent: %w", err)
		}
	}
	return out, nil
}

// resetIndex resets the staging area to match HEAD so that only explicitly
// added files will be included in the next commit. No-op on an empty repo
// (no HEAD yet, i.e. initial commit).
func (r *Repo) resetIndex(wt *gogit.Worktree) error {
	head, err := r.repo.Head()
	if err != nil {
		return nil
	}
	return wt.Reset(&gogit.ResetOptions{
		Commit: head.Hash(),
		Mode:   gogit.MixedReset,
	})
}

func (r *Repo) sig() *object.Signature {
	return &object.Signature{
		Name:  commitSignature.Name,
		Email: commitSignature.Email,
		When:  time.Now(),
	}
}

func (r *Repo) writeFile(relPath, content string) error {
	absPath := filepath.Join(r.path, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(absPath, []byte(content), 0o644)
}

func (r *Repo) writeSymlink(relPath, target string) error {
	absPath := filepath.Join(r.path, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(absPath)
	return os.Symlink(target, absPath)
}

func findRepoRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("not a git repository: %s", path)
		}
		abs = parent
	}
}
