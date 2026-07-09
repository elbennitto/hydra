package git

import (
	"fmt"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ChangedPaths returns all file paths that changed between two refs.
func (r *Repo) ChangedPaths(from, to string) ([]string, error) {
	fromCommit, err := r.resolveCommit(from)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", from, err)
	}
	toCommit, err := r.resolveCommit(to)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", to, err)
	}

	fromTree, err := fromCommit.Tree()
	if err != nil {
		return nil, err
	}
	toTree, err := toCommit.Tree()
	if err != nil {
		return nil, err
	}

	changes, err := fromTree.Diff(toTree)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	for _, c := range changes {
		name := c.To.Name
		if name == "" {
			name = c.From.Name
		}
		seen[name] = true
	}

	if len(seen) == 0 {
		return nil, nil
	}

	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// HasChanges checks if any files under the given directory path changed
// between two refs. Uses subtree hash comparison for efficiency.
func (r *Repo) HasChanges(from, to, path string) (bool, error) {
	fromCommit, err := r.resolveCommit(from)
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", from, err)
	}
	toCommit, err := r.resolveCommit(to)
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", to, err)
	}

	fromTree, err := fromCommit.Tree()
	if err != nil {
		return false, err
	}
	toTree, err := toCommit.Tree()
	if err != nil {
		return false, err
	}

	cleanPath := strings.TrimSuffix(path, "/")

	fromSub, fromErr := fromTree.Tree(cleanPath)
	toSub, toErr := toTree.Tree(cleanPath)

	if fromErr != nil && toErr != nil {
		return false, nil
	}
	if fromErr != nil || toErr != nil {
		return true, nil
	}

	return fromSub.Hash != toSub.Hash, nil
}

// LastBuildTag returns the most recent build-* tag that included changes
// in the given path. Returns ("", error) if no matching tag is found.
func (r *Repo) LastBuildTag(path string) (string, error) {
	tags, err := r.Tags("build-*")
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no build tags found")
	}

	sort.Sort(sort.Reverse(sort.StringSlice(tags)))

	dir := strings.TrimSuffix(path, "/")

	for _, tag := range tags {
		commit, err := r.resolveCommit(tag)
		if err != nil {
			continue
		}

		paths, err := commitChangedPaths(commit)
		if err != nil {
			continue
		}
		for _, p := range paths {
			if strings.HasPrefix(p, dir+"/") || p == dir {
				return tag, nil
			}
		}
	}

	return "", fmt.Errorf("no build tag found that includes %s", path)
}

// FirstCommitAffectingPath returns the oldest commit reachable from HEAD that
// changed the given path. This is used as the baseline for unreleased, newly
// introduced charts so their initial checked-in version does not immediately
// trigger an extra wrapper bump on the first release.
func (r *Repo) FirstCommitAffectingPath(path string) (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}

	iter, err := r.repo.Log(&gogit.LogOptions{From: head.Hash()})
	if err != nil {
		return "", fmt.Errorf("commit log: %w", err)
	}

	var commits []*object.Commit
	if err := iter.ForEach(func(commit *object.Commit) error {
		commits = append(commits, commit)
		return nil
	}); err != nil {
		return "", fmt.Errorf("iterate commits: %w", err)
	}

	dir := strings.TrimSuffix(path, "/")
	for i := len(commits) - 1; i >= 0; i-- {
		paths, err := commitChangedPaths(commits[i])
		if err != nil {
			return "", err
		}
		for _, p := range paths {
			if strings.HasPrefix(p, dir+"/") || p == dir {
				return commits[i].Hash.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no commit found that includes %s", path)
}

// commitChangedPaths returns the file paths changed in a single commit
// (compared to its first parent, or all files for the initial commit).
func commitChangedPaths(commit *object.Commit) ([]string, error) {
	commitTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	if commit.NumParents() == 0 {
		var paths []string
		_ = commitTree.Files().ForEach(func(f *object.File) error {
			paths = append(paths, f.Name)
			return nil
		})
		return paths, nil
	}

	parent, err := commit.Parent(0)
	if err != nil {
		return nil, err
	}
	parentTree, err := parent.Tree()
	if err != nil {
		return nil, err
	}

	changes, err := parentTree.Diff(commitTree)
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, c := range changes {
		name := c.To.Name
		if name == "" {
			name = c.From.Name
		}
		paths = append(paths, name)
	}
	return paths, nil
}
