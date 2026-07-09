package ci

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

// releaseTagTime is the clock used for build-* tags (overridable in tests).
var releaseTagTime = time.Now

type releaseExecutor interface {
	run(repo *git.Repo, cfg *Config, plan []ReleaseChildEntry, roots map[groupEnvKey]rootChartUpdate, tags []string) (ReleaseResult, error)
}

// NewReleaseExecutor returns the mode-specific release executor.
func NewReleaseExecutor(mode Mode, targetBranch string) releaseExecutor {
	switch mode {
	case ModeDryRun:
		return dryRunReleaseExecutor{targetBranch: targetBranch}
	case ModeLocal:
		return localReleaseExecutor{targetBranch: targetBranch}
	default:
		return ciReleaseExecutor{targetBranch: targetBranch}
	}
}

type dryRunReleaseExecutor struct {
	targetBranch string
}

func (a dryRunReleaseExecutor) run(_ *git.Repo, _ *Config, plan []ReleaseChildEntry, roots map[groupEnvKey]rootChartUpdate, tags []string) (ReleaseResult, error) {
	l := log.Default()
	if a.targetBranch != "" {
		l.Info(logIdCI, "release dry-run: target branch {branch}", log.String("branch", a.targetBranch))
	}
	if len(tags) == 0 {
		tags = releaseTags(plan, roots)
	}
	for _, e := range plan {
		l.Info(logIdCI, "release dry-run: {path} {old} → {new}",
			log.String("path", e.Path),
			log.String("old", e.OldVersion),
			log.String("new", e.NewVersion),
		)
	}
	for _, ru := range roots {
		l.Info(logIdCI, "release dry-run: root {path} version {version}",
			log.String("path", ru.relPath),
			log.String("version", ru.chart.GetVersion()),
		)
	}
	for _, tag := range tags {
		l.Info(logIdCI, "release dry-run: create tag {tag}", log.String("tag", tag))
	}
	return releaseResultFromPlan(plan), nil
}

type localReleaseExecutor struct {
	targetBranch string
}

func (a localReleaseExecutor) run(repo *git.Repo, cfg *Config, plan []ReleaseChildEntry, roots map[groupEnvKey]rootChartUpdate, tags []string) (ReleaseResult, error) {
	l := log.Default()
	if a.targetBranch != "" {
		if !repo.BranchExists(a.targetBranch) {
			return ReleaseResult{}, fmt.Errorf("target branch '%s' does not exist", a.targetBranch)
		}
		l.Info(logIdCI, "release local: checkout target branch {branch}", log.String("branch", a.targetBranch))
		repo.Checkout(a.targetBranch)
	} else {
		l.Info(logIdCI, "release local: checkout default branch from upstream {upstream}",
			log.String("upstream", cfg.CI.UpstreamBranch))
		repo.CheckoutUpstreamBranch(cfg.CI.UpstreamBranch)
	}
	if repo.Err != nil {
		return ReleaseResult{}, repo.Err
	}
	if len(tags) == 0 {
		tags = releaseTags(plan, roots)
	}
	if len(plan) == 0 && len(roots) == 0 {
		for _, tag := range tags {
			repo.Tag(tag)
			if repo.Err != nil {
				return ReleaseResult{}, repo.Err
			}
			l.Info(logIdCI, "release local: created tag {tag}", log.String("tag", tag))
		}
		return releaseResultFromPlan(plan), nil
	}

	for _, e := range plan {
		l.Info(logIdCI, "release local: update {path} {old} → {new}",
			log.String("path", e.Path),
			log.String("old", e.OldVersion),
			log.String("new", e.NewVersion),
		)
		ch, err := repo.LoadChart(e.Path)
		if err != nil {
			return ReleaseResult{}, fmt.Errorf("load %s: %w", e.Path, err)
		}
		ch.Version(e.NewVersion)
		if err := ch.Save(); err != nil {
			return ReleaseResult{}, fmt.Errorf("save child %s: %w", e.Path, err)
		}
	}
	for _, ru := range roots {
		l.Info(logIdCI, "release local: update root {path} version {version}",
			log.String("path", ru.relPath),
			log.String("version", ru.chart.GetVersion()),
		)
		if err := ru.chart.Save(); err != nil {
			return ReleaseResult{}, fmt.Errorf("save root %s: %w", ru.relPath, err)
		}
	}

	files := map[string]string{}
	for _, e := range plan {
		if err := mergeChartDirFiles(repo, e.Path, files); err != nil {
			return ReleaseResult{}, err
		}
	}
	for _, ru := range roots {
		if err := mergeChartDirFiles(repo, ru.relPath, files); err != nil {
			return ReleaseResult{}, err
		}
	}

	msg := releaseCommitMessage(plan)
	l.Info(logIdCI, "release local: create commit {message}", log.String("message", msg))
	repo.CommitFiles(msg, files)
	if repo.Err != nil {
		return ReleaseResult{}, repo.Err
	}

	for _, tag := range tags {
		repo.Tag(tag)
		if repo.Err != nil {
			return ReleaseResult{}, repo.Err
		}
		l.Info(logIdCI, "release local: created tag {tag}", log.String("tag", tag))
	}
	return releaseResultFromPlan(plan), nil
}

type ciReleaseExecutor struct {
	targetBranch string
}

func (a ciReleaseExecutor) run(repo *git.Repo, cfg *Config, plan []ReleaseChildEntry, roots map[groupEnvKey]rootChartUpdate, tags []string) (ReleaseResult, error) {
	if a.targetBranch != "" {
		return ReleaseResult{}, fmt.Errorf("target branch is not supported in ci mode")
	}

	l := log.Default()
	if err := stageTrackedChangesBeforeBranchCheckout(repo, "release"); err != nil {
		return ReleaseResult{}, err
	}
	l.Info(logIdCI, "release ci: checkout default branch from upstream {upstream}",
		log.String("upstream", cfg.CI.UpstreamBranch))
	repo.CheckoutUpstreamBranch(cfg.CI.UpstreamBranch)
	if repo.Err != nil {
		return ReleaseResult{}, repo.Err
	}
	if len(tags) == 0 {
		tags = releaseTags(plan, roots)
	}
	if len(plan) == 0 && len(roots) == 0 {
		for _, tag := range tags {
			repo.Tag(tag)
			if repo.Err != nil {
				return ReleaseResult{}, repo.Err
			}
			l.Info(logIdCI, "release ci: created tag {tag}", log.String("tag", tag))
		}
		if err := pushReleaseTags(repo, "origin", tags); err != nil {
			return ReleaseResult{}, err
		}
		return releaseResultFromPlan(plan), nil
	}

	for _, e := range plan {
		l.Info(logIdCI, "release ci: update {path} {old} -> {new}",
			log.String("path", e.Path),
			log.String("old", e.OldVersion),
			log.String("new", e.NewVersion),
		)
		ch, err := repo.LoadChart(e.Path)
		if err != nil {
			return ReleaseResult{}, fmt.Errorf("load %s: %w", e.Path, err)
		}
		ch.Version(e.NewVersion)
		if err := ch.Save(); err != nil {
			return ReleaseResult{}, fmt.Errorf("save child %s: %w", e.Path, err)
		}
	}
	for _, ru := range roots {
		l.Info(logIdCI, "release ci: update root {path} version {version}",
			log.String("path", ru.relPath),
			log.String("version", ru.chart.GetVersion()),
		)
		if err := ru.chart.Save(); err != nil {
			return ReleaseResult{}, fmt.Errorf("save root %s: %w", ru.relPath, err)
		}
	}

	files := map[string]string{}
	for _, e := range plan {
		if err := mergeChartDirFiles(repo, e.Path, files); err != nil {
			return ReleaseResult{}, err
		}
	}
	for _, ru := range roots {
		if err := mergeChartDirFiles(repo, ru.relPath, files); err != nil {
			return ReleaseResult{}, err
		}
	}

	msg := releaseCommitMessage(plan)
	l.Info(logIdCI, "release ci: create commit {message}", log.String("message", msg))
	repo.CommitFiles(msg, files)
	if repo.Err != nil {
		return ReleaseResult{}, repo.Err
	}

	for _, tag := range tags {
		repo.Tag(tag)
		if repo.Err != nil {
			return ReleaseResult{}, repo.Err
		}
		l.Info(logIdCI, "release ci: created tag {tag}", log.String("tag", tag))
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		return ReleaseResult{}, err
	}
	l.Info(logIdCI, "release ci: push branch {branch}", log.String("branch", branch))
	repo.PushSetUpstream("origin", branch)
	if repo.Err != nil {
		return ReleaseResult{}, repo.Err
	}
	if err := pushReleaseTags(repo, "origin", tags); err != nil {
		return ReleaseResult{}, err
	}

	return releaseResultFromPlan(plan), nil
}

func pushReleaseTags(repo *git.Repo, remote string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	pushRemote := remote
	pushToken := ""
	if token, header, err := gitLabToken(repo); err == nil && header == "PRIVATE-TOKEN" {
		if remoteURL, remoteErr := repo.RemoteURL(); remoteErr == nil {
			authRemote, authErr := gitLabTokenPushRemote(remoteURL, token)
			if authErr == nil {
				pushRemote = authRemote
				pushToken = token
			}
		}
	}

	args := []string{"-C", repo.Path(), "push", pushRemote}
	args = append(args, tags...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if pushToken != "" {
			msg = strings.ReplaceAll(msg, pushToken, "***")
			escapedToken := url.QueryEscape(pushToken)
			if escapedToken != pushToken {
				msg = strings.ReplaceAll(msg, escapedToken, "***")
			}
		}
		if msg == "" {
			return fmt.Errorf("git push %s <tags>: %w", remote, err)
		}
		return fmt.Errorf("git push %s <tags>: %w\n%s", remote, err, msg)
	}
	return nil
}

func gitLabTokenPushRemote(remoteURL, token string) (string, error) {
	host, err := parseGitRemoteHost(remoteURL)
	if err != nil {
		return "", err
	}
	projectPath, err := parseGitRemoteProjectPath(remoteURL)
	if err != nil {
		return "", err
	}

	scheme := "https"
	if parsed, err := url.Parse(remoteURL); err == nil {
		if parsed.Scheme == "http" {
			scheme = "http"
		}
	}

	authURL := &url.URL{
		Scheme: scheme,
		User:   url.UserPassword("oauth2", token),
		Host:   host,
		Path:   "/" + strings.TrimPrefix(projectPath, "/") + ".git",
	}
	return authURL.String(), nil
}

func mergeChartDirFiles(repo *git.Repo, rel string, dest map[string]string) error {
	m, err := readChartDirFiles(repo, rel)
	if err != nil {
		return err
	}
	for k, v := range m {
		dest[k] = v
	}
	return nil
}

func readChartDirFiles(repo *git.Repo, rel string) (map[string]string, error) {
	out := make(map[string]string)
	base := filepath.Join(repo.Path(), rel)
	cy, err := os.ReadFile(filepath.Join(base, "Chart.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read %s/Chart.yaml: %w", rel, err)
	}
	relSlash := filepath.ToSlash(rel)
	out[relSlash+"/Chart.yaml"] = string(cy)
	if b, err := os.ReadFile(filepath.Join(base, "values.yaml")); err == nil {
		out[relSlash+"/values.yaml"] = string(b)
	}
	return out, nil
}

func releaseCommitMessage(plan []ReleaseChildEntry) string {
	var parts []string
	for _, e := range plan {
		parts = append(parts, fmt.Sprintf("%s/%s@%s %s", e.Group, e.App, e.Env, e.NewVersion))
	}
	return "release: " + strings.Join(parts, ", ")
}

func releaseTags(plan []ReleaseChildEntry, roots map[groupEnvKey]rootChartUpdate) []string {
	seen := make(map[string]struct{})
	var tags []string
	for _, e := range plan {
		t := AppTag(e.Group, e.App, e.NewVersion)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		tags = append(tags, t)
	}
	for k, ru := range roots {
		version, err := NormalizeChartVersionEnv(ru.chart.GetVersion(), k.env)
		if err != nil {
			version = ru.chart.GetVersion()
		}
		t := RootAppTag(k.group, version)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		tags = append(tags, t)
	}
	ts := releaseTagTime().UTC().Format("200601021504")
	tags = append(tags, BuildTagPrefix+ts)
	return tags
}

func releaseResultFromPlan(plan []ReleaseChildEntry) ReleaseResult {
	out := make([]ReleaseChildEntry, len(plan))
	copy(out, plan)
	return ReleaseResult{Children: out}
}
