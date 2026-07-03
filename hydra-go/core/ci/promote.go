package ci

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/core/git"
)

const defaultCIPromoteNewChartVersion = "0.0.0"

// PromoteActions defines mode-dependent operations for the promote pipeline.
// Three implementations exist: dryRunPromoteActions, localPromoteActions, ciPromoteActions.
type PromoteActions interface {
	ExecutePromotion(repo *git.Repo, entry PromotionEntry, cfg *Config) error
}

// PromoteResult captures all promote operations performed (or planned in dry-run).
type PromoteResult struct {
	Promotions []PromotionEntry
}

// CollectErrors returns a combined error if any entries have HasError set.
// Returns nil if there are no errors.
func (r PromoteResult) CollectErrors() error {
	var msgs []string
	for _, p := range r.Promotions {
		if p.HasError {
			msgs = append(msgs, fmt.Sprintf("  %s/%s %s→%s: %s",
				p.Group, p.App, p.SourceEnv, p.TargetEnv, p.SkipReason))
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return fmt.Errorf("%d chart(s) with errors:\n%s", len(msgs), strings.Join(msgs, "\n"))
}

// PromotionEntry describes a single chart promotion between environments.
type PromotionEntry struct {
	Group      string
	App        string
	SourceEnv  string
	TargetEnv  string
	SourcePath string
	TargetPath string
	OldVersion string
	NewVersion string
	DepVersion string
	Branch     string
	Skipped    bool
	SkipReason string
	HasError   bool
}

func (e PromotionEntry) CommitMessage() string {
	if e.OldVersion == "" {
		if e.DepVersion != "" {
			return fmt.Sprintf("promote: %s %s → %s (new, version %s, dependency %s)",
				e.App, e.SourceEnv, e.TargetEnv, e.NewVersion, e.DepVersion)
		}
		return fmt.Sprintf("promote: %s %s → %s (new, version %s)",
			e.App, e.SourceEnv, e.TargetEnv, e.NewVersion)
	}
	if e.OldVersion == e.NewVersion {
		return fmt.Sprintf("promote: %s %s → %s (content changed)",
			e.App, e.SourceEnv, e.TargetEnv)
	}
	return fmt.Sprintf("promote: %s %s → %s (%s → %s)",
		e.App, e.SourceEnv, e.TargetEnv, e.OldVersion, e.NewVersion)
}

// NewPromoteActions returns the PromoteActions implementation for the given mode.
func NewPromoteActions(mode Mode, targetBranch string) PromoteActions {
	switch mode {
	case ModeDryRun:
		return &dryRunPromoteActions{targetBranch: targetBranch}
	case ModeLocal:
		return &localPromoteActions{targetBranch: targetBranch}
	case ModeCI:
		return &ciPromoteActions{}
	default:
		return &dryRunPromoteActions{targetBranch: targetBranch}
	}
}

// RunPromote detects charts that differ between environments and
// executes promotions according to the provided actions implementation.
// An optional onEntry callback is invoked for each entry immediately
// after it has been processed (promoted, skipped, or errored).
func RunPromote(configPath string, mode Mode, actions PromoteActions, targetBranch string, promoteTo string, onEntry ...func(PromotionEntry)) (PromoteResult, error) {
	dir := filepath.Dir(configPath)
	cfg, err := LoadConfig(dir)
	if err != nil {
		return PromoteResult{}, fmt.Errorf("load config: %w", err)
	}

	absChartsPath, err := filepath.Abs(filepath.Join(dir, cfg.CI.RootAppsPath))
	if err != nil {
		return PromoteResult{}, fmt.Errorf("resolve charts path: %w", err)
	}

	repo := git.Open(absChartsPath)
	if repo.Err != nil {
		return PromoteResult{}, fmt.Errorf("open repo: %w", repo.Err)
	}

	relChartsPath, err := filepath.Rel(repo.Path(), absChartsPath)
	if err != nil {
		return PromoteResult{}, fmt.Errorf("relativize charts path: %w", err)
	}
	cfg.CI.RootAppsPath = filepath.ToSlash(relChartsPath)

	if targetBranch != "" {
		if mode == ModeCI {
			return PromoteResult{}, fmt.Errorf("target branch is not supported in ci mode")
		}
		if !repo.BranchExists(targetBranch) {
			return PromoteResult{}, fmt.Errorf("target branch '%s' does not exist", targetBranch)
		}
		if setter, ok := actions.(interface{ setTargetBranch(string) }); ok {
			setter.setTargetBranch(targetBranch)
		}
		if mode != ModeDryRun {
			repo.Checkout(targetBranch)
			if repo.Err != nil {
				return PromoteResult{}, fmt.Errorf("checkout target branch '%s': %w", targetBranch, repo.Err)
			}
		}
	}

	var result PromoteResult

	for i := 0; i < len(cfg.CI.Environments)-1; i++ {
		sourceEnv := cfg.CI.Environments[i]
		targetEnv := cfg.CI.Environments[i+1]

		if promoteTo != "" && targetEnv != promoteTo {
			continue
		}

		entries, err := detectPromotions(repo, cfg, mode, sourceEnv, targetEnv)
		if err != nil {
			return result, fmt.Errorf("detect promotions %s→%s: %w", sourceEnv, targetEnv, err)
		}

		if targetBranch != "" {
			for i := range entries {
				if !entries[i].Skipped {
					entries[i].Branch = targetBranch
				}
			}
		}

		for _, entry := range entries {
			if !entry.Skipped {
				if err := actions.ExecutePromotion(repo, entry, cfg); err != nil {
					return result, fmt.Errorf("promote %s/%s %s→%s: %w",
						entry.Group, entry.App, entry.SourceEnv, entry.TargetEnv, err)
				}
			}
			result.Promotions = append(result.Promotions, entry)
			for _, fn := range onEntry {
				fn(entry)
			}
		}
	}

	if err := result.CollectErrors(); err != nil {
		return result, err
	}
	return result, nil
}

func detectPromotions(repo *git.Repo, cfg *Config, mode Mode, sourceEnv, targetEnv string) ([]PromotionEntry, error) {
	var entries []PromotionEntry

	pattern := filepath.Join(cfg.CI.RootAppsPath, "*", "*", "*")
	matches, err := filepath.Glob(filepath.Join(repo.Path(), pattern))
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}

	for _, absPath := range matches {
		relPath, _ := filepath.Rel(repo.Path(), absPath)
		relPath = filepath.ToSlash(relPath)

		group, app, env, err := ParseChartPath(relPath)
		if err != nil {
			continue
		}

		if env != sourceEnv {
			continue
		}

		sourcePath := relPath
		targetPath := filepath.ToSlash(filepath.Join(filepath.Dir(sourcePath), targetEnv))

		entry := PromotionEntry{
			Group:      group,
			App:        app,
			SourceEnv:  sourceEnv,
			TargetEnv:  targetEnv,
			SourcePath: sourcePath,
			TargetPath: targetPath,
			Branch:     PromoteBranch(targetEnv, group, app),
		}

		if app == "root" && !cfg.IsRootAppPromotable(group) {
			entry.Skipped = true
			entry.SkipReason = "root app not promotable"
			entries = append(entries, entry)
			continue
		}

		sourceChart, err := repo.LoadChart(sourcePath)
		if err != nil {
			entry.HasError = true
			entry.Skipped = true
			entry.SkipReason = fmt.Sprintf("load source chart %s: %v", sourcePath, err)
			entries = append(entries, entry)
			continue
		}
		entry.DepVersion = sourceChart.PrimaryDependencyVersion()

		targetAbs := filepath.Join(repo.Path(), targetPath)
		existingOnTarget := ""
		if _, statErr := os.Stat(targetAbs); statErr == nil {
			targetChart, err := repo.LoadChart(targetPath)
			if err != nil {
				entry.HasError = true
				entry.Skipped = true
				entry.SkipReason = fmt.Sprintf("load target chart %s: %v", targetPath, err)
				entries = append(entries, entry)
				continue
			}
			existingOnTarget = targetChart.GetVersion()
			entry.OldVersion = existingOnTarget
		} else if !os.IsNotExist(statErr) {
			entry.HasError = true
			entry.Skipped = true
			entry.SkipReason = fmt.Sprintf("stat target path %s: %v", targetPath, statErr)
			entries = append(entries, entry)
			continue
		}

		if existingOnTarget == "" {
			newVersion, err := computeNewTargetChartVersion(cfg, mode, sourceChart.GetVersion(), sourceEnv, targetEnv)
			if err != nil {
				entry.HasError = true
				entry.Skipped = true
				entry.SkipReason = fmt.Sprintf("rewrite version for %s: %v", sourcePath, err)
				entries = append(entries, entry)
				continue
			}
			entry.NewVersion = newVersion
			entry.OldVersion = ""
			entries = append(entries, entry)
			continue
		}

		if _, err := ComputePromoteTargetVersion(sourceChart.GetVersion(), sourceEnv, targetEnv, existingOnTarget); err != nil {
			entry.HasError = true
			entry.Skipped = true
			entry.SkipReason = fmt.Sprintf("rewrite version for %s: %v", sourcePath, err)
			entries = append(entries, entry)
			continue
		}
		entry.NewVersion = existingOnTarget

		renderedSourceChartYAML := sourceChart.RenderWithVersion(entry.NewVersion)

		equal, err := chartDirsEqual(repo.Path(), sourcePath, targetPath, renderedSourceChartYAML)
		if err != nil {
			entry.HasError = true
			entry.Skipped = true
			entry.SkipReason = fmt.Sprintf("compare directories: %v", err)
			entries = append(entries, entry)
			continue
		}
		if equal {
			entry.Skipped = true
			entry.SkipReason = "no differences"
			entries = append(entries, entry)
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func computeNewTargetChartVersion(cfg *Config, mode Mode, sourceVersion, sourceEnv, targetEnv string) (string, error) {
	if mode == ModeCI {
		if cfg.CI.Promote.NewChartVersion != "" {
			return cfg.CI.Promote.NewChartVersion, nil
		}
		return defaultCIPromoteNewChartVersion, nil
	}
	return ComputePromoteTargetVersion(sourceVersion, sourceEnv, targetEnv, "")
}

// versionContentEqual compares two versions ignoring the environment suffix.
func versionContentEqual(a, b ChartVersion) bool {
	return a.Major == b.Major && a.Minor == b.Minor && a.Patch == b.Patch && a.Extra == b.Extra
}

// chartDirsEqual compares two chart environment directories recursively.
// Chart.yaml is compared by matching the rendered source content (with target
// version already applied) against the target file on disk.
// Symlinks are compared by their target (os.Readlink).
// All other files are compared byte-for-byte.
func chartDirsEqual(repoPath, sourcePath, targetPath string, renderedSourceChartYAML string) (bool, error) {
	absSource := filepath.Join(repoPath, sourcePath)
	absTarget := filepath.Join(repoPath, targetPath)

	sourceFiles, err := walkRelFiles(absSource)
	if err != nil {
		return false, fmt.Errorf("walk source %s: %w", sourcePath, err)
	}
	targetFiles, err := walkRelFiles(absTarget)
	if err != nil {
		return false, fmt.Errorf("walk target %s: %w", targetPath, err)
	}

	sourceSet := make(map[string]struct{}, len(sourceFiles))
	for _, f := range sourceFiles {
		sourceSet[f] = struct{}{}
	}
	targetSet := make(map[string]struct{}, len(targetFiles))
	for _, f := range targetFiles {
		targetSet[f] = struct{}{}
	}

	if len(sourceSet) != len(targetSet) {
		return false, nil
	}
	for f := range sourceSet {
		if _, ok := targetSet[f]; !ok {
			return false, nil
		}
	}

	for _, f := range sourceFiles {
		if f == "Chart.yaml" {
			tgtData, err := os.ReadFile(filepath.Join(absTarget, "Chart.yaml"))
			if err != nil {
				return false, err
			}
			if renderedSourceChartYAML != string(tgtData) {
				return false, nil
			}
			continue
		}
		srcPath := filepath.Join(absSource, f)
		tgtPath := filepath.Join(absTarget, f)

		srcInfo, err := os.Lstat(srcPath)
		if err != nil {
			return false, err
		}
		tgtInfo, err := os.Lstat(tgtPath)
		if err != nil {
			return false, err
		}

		srcIsLink := srcInfo.Mode()&fs.ModeSymlink != 0
		tgtIsLink := tgtInfo.Mode()&fs.ModeSymlink != 0
		if srcIsLink != tgtIsLink {
			return false, nil
		}

		if srcIsLink {
			srcTarget, err := os.Readlink(srcPath)
			if err != nil {
				return false, err
			}
			tgtTarget, err := os.Readlink(tgtPath)
			if err != nil {
				return false, err
			}
			if srcTarget != tgtTarget {
				return false, nil
			}
			continue
		}

		srcData, err := os.ReadFile(srcPath)
		if err != nil {
			return false, err
		}
		tgtData, err := os.ReadFile(tgtPath)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(srcData, tgtData) {
			return false, nil
		}
	}

	return true, nil
}

func walkRelFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// --- dryRunPromoteActions ---

type dryRunPromoteActions struct {
	targetBranch string
}

func (a *dryRunPromoteActions) setTargetBranch(b string) { a.targetBranch = b }

func (a *dryRunPromoteActions) ExecutePromotion(_ *git.Repo, _ PromotionEntry, _ *Config) error {
	return nil
}

// --- localPromoteActions ---

type localPromoteActions struct {
	targetBranch string
}

func (a *localPromoteActions) setTargetBranch(b string) { a.targetBranch = b }

func (a *localPromoteActions) ExecutePromotion(repo *git.Repo, entry PromotionEntry, cfg *Config) error {
	fs, err := buildPromotionFS(repo, entry)
	if err != nil {
		return err
	}

	if a.targetBranch != "" {
		repo.CommitFS(entry.CommitMessage(), fs)
	} else {
		repo.CheckoutUpstreamBranch(cfg.CI.UpstreamBranch).
			Branch(entry.Branch).
			CommitFS(entry.CommitMessage(), fs)
		if repo.Err != nil {
			return repo.Err
		}
		repo.CheckoutUpstreamBranch(cfg.CI.UpstreamBranch)
	}
	return repo.Err
}

// --- ciPromoteActions ---

type ciPromoteActions struct{}

func (a *ciPromoteActions) ExecutePromotion(repo *git.Repo, entry PromotionEntry, cfg *Config) error {
	fs, err := buildPromotionFS(repo, entry)
	if err != nil {
		return err
	}

	repo.CheckoutUpstreamBranch(cfg.CI.UpstreamBranch).
		Branch(entry.Branch).
		CommitFS(entry.CommitMessage(), fs)
	if repo.Err != nil {
		return repo.Err
	}
	repo.PushSetUpstream("origin", entry.Branch)
	if repo.Err != nil {
		return repo.Err
	}
	repo.CheckoutUpstreamBranch(cfg.CI.UpstreamBranch)
	return repo.Err
}

func buildPromotionFS(repo *git.Repo, entry PromotionEntry) (*git.FS, error) {
	sourceChart, err := repo.LoadChart(entry.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("load chart %s: %w", entry.SourcePath, err)
	}

	sourceChart.Version(entry.NewVersion)

	absSource := filepath.Join(repo.Path(), entry.SourcePath)
	absTarget := filepath.Join(repo.Path(), entry.TargetPath)

	fs := git.NewFS()
	if err := fs.AddDir(entry.TargetPath, absSource, sourceChart); err != nil {
		return nil, fmt.Errorf("add source dir: %w", err)
	}

	if _, statErr := os.Stat(absTarget); statErr == nil {
		targetFiles, walkErr := walkRelFiles(absTarget)
		if walkErr != nil {
			return nil, fmt.Errorf("walk target dir: %w", walkErr)
		}
		sourceFiles, walkErr := walkRelFiles(absSource)
		if walkErr != nil {
			return nil, fmt.Errorf("walk source dir: %w", walkErr)
		}
		sourceSet := make(map[string]struct{}, len(sourceFiles))
		for _, f := range sourceFiles {
			sourceSet[f] = struct{}{}
		}
		for _, f := range targetFiles {
			if _, ok := sourceSet[f]; !ok {
				fs.Remove(filepath.ToSlash(filepath.Join(entry.TargetPath, f)))
			}
		}
	}

	return fs, nil
}
