package ci

import (
	"fmt"
	"path/filepath"

	"hydra-gitops.org/hydra/hydra-go/core/git"
)

// ReleaseChildEntry describes one child chart updated by the release pipeline.
type ReleaseChildEntry struct {
	Group, App, Env, Path  string
	OldVersion, NewVersion string
}

// ReleaseResult summarizes a release run.
type ReleaseResult struct {
	Children []ReleaseChildEntry
}

// RunRelease detects changed child charts, bumps wrapper and root versions,
// and runs mode-specific actions (dry-run log, local commit+tags, or CI stub).
func RunRelease(configPath string, mode Mode, targetBranch string) (ReleaseResult, error) {
	dir := filepath.Dir(configPath)
	cfg, err := LoadConfig(dir)
	if err != nil {
		return ReleaseResult{}, fmt.Errorf("load config: %w", err)
	}

	absChartsPath, err := filepath.Abs(filepath.Join(dir, cfg.CI.RootAppsPath))
	if err != nil {
		return ReleaseResult{}, fmt.Errorf("resolve charts path: %w", err)
	}

	repo := git.Open(absChartsPath)
	if repo.Err != nil {
		return ReleaseResult{}, fmt.Errorf("open repo: %w", repo.Err)
	}

	buildTags, err := repo.Tags(BuildTagPrefix + "*")
	if err != nil {
		return ReleaseResult{}, fmt.Errorf("list build tags: %w", err)
	}
	firstReleaseRepo := len(buildTags) == 0

	relChartsPath, err := filepath.Rel(repo.Path(), absChartsPath)
	if err != nil {
		return ReleaseResult{}, fmt.Errorf("relativize charts path: %w", err)
	}
	cfg.CI.RootAppsPath = filepath.ToSlash(relChartsPath)

	pattern := filepath.Join(cfg.CI.RootAppsPath, "*", "*", "*")
	matches, err := filepath.Glob(filepath.Join(repo.Path(), pattern))
	if err != nil {
		return ReleaseResult{}, fmt.Errorf("glob charts: %w", err)
	}

	var plan []ReleaseChildEntry
	allChartTags, err := currentReleaseTagsForAllCharts(repo, cfg, matches)
	if err != nil {
		return ReleaseResult{}, err
	}
	for _, absPath := range matches {
		relPath, errRel := filepath.Rel(repo.Path(), absPath)
		if errRel != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)
		group, app, env, errP := ParseChartPath(relPath)
		if errP != nil {
			continue
		}
		if !envAllowed(env, cfg.CI.Environments) {
			continue
		}
		if app == "root" {
			continue
		}

		releaseState, errCh := chartDirReleaseState(repo, relPath)
		if errCh != nil {
			return ReleaseResult{}, errCh
		}
		if !releaseState.ChangedSinceBaseline && !releaseState.FirstRelease {
			continue
		}

		ch, errL := repo.LoadChart(relPath)
		if errL != nil {
			return ReleaseResult{}, fmt.Errorf("load chart %s: %w", relPath, errL)
		}
		oldVer := ch.GetVersion()
		dep, errDep := dependencyVersionForWrapperRelease(ch, oldVer, relPath)
		if errDep != nil {
			return ReleaseResult{}, errDep
		}
		if releaseState.FirstRelease && !releaseState.ChangedSinceBaseline {
			newVer, errN := ComputeWrapperVersion(dep, env, -1)
			if errN != nil {
				return ReleaseResult{}, fmt.Errorf("chart %s: %w", relPath, errN)
			}
			sameBase, errSame := SameChildWrapperBaseVersion(oldVer, newVer)
			if errSame == nil && sameBase {
				continue
			}
		}
		if releaseState.FirstRelease {
			newVer, errN := ComputeWrapperVersion(dep, env, -1)
			if errN != nil {
				return ReleaseResult{}, fmt.Errorf("chart %s: %w", relPath, errN)
			}
			if oldVer == newVer || sameVersionIgnoringEnv(oldVer, newVer) {
				continue
			}
			plan = append(plan, ReleaseChildEntry{
				Group:      group,
				App:        app,
				Env:        env,
				Path:       relPath,
				OldVersion: oldVer,
				NewVersion: newVer,
			})
			continue
		} else {
			newVer, errN := NextChildChartWrapperVersion(dep, env, oldVer)
			if errN != nil {
				return ReleaseResult{}, fmt.Errorf("chart %s: %w", relPath, errN)
			}
			plan = append(plan, ReleaseChildEntry{
				Group:      group,
				App:        app,
				Env:        env,
				Path:       relPath,
				OldVersion: oldVer,
				NewVersion: newVer,
			})
		}
	}

	if len(plan) == 0 && !firstReleaseRepo {
		return ReleaseResult{}, nil
	}

	roots, errR := buildRootChartUpdates(repo, cfg, plan)
	if errR != nil {
		return ReleaseResult{}, errR
	}

	var tags []string
	if firstReleaseRepo {
		tags = applyFirstReleaseTagOverrides(allChartTags, plan, roots)
		tags = append(tags, BuildTagPrefix+releaseTagTime().UTC().Format("200601021504"))
	}

	exec := NewReleaseExecutor(mode, targetBranch)
	return exec.run(repo, cfg, plan, roots, tags)
}

func currentReleaseTagsForAllCharts(repo *git.Repo, cfg *Config, matches []string) ([]string, error) {
	seen := make(map[string]struct{})
	var tags []string
	for _, absPath := range matches {
		relPath, errRel := filepath.Rel(repo.Path(), absPath)
		if errRel != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)
		group, app, env, errP := ParseChartPath(relPath)
		if errP != nil {
			continue
		}
		if !envAllowed(env, cfg.CI.Environments) {
			continue
		}

		ch, err := repo.LoadChart(relPath)
		if err != nil {
			return nil, fmt.Errorf("load chart %s: %w", relPath, err)
		}
		version := ch.GetVersion()
		if version == "" {
			continue
		}

		var tag string
		if app == "root" {
			tag = RootAppTag(group, version)
		} else {
			tag = AppTag(group, app, version)
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags, nil
}

func applyFirstReleaseTagOverrides(tags []string, plan []ReleaseChildEntry, roots map[groupEnvKey]rootChartUpdate) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags)+len(plan)+len(roots))

	for _, t := range tags {
		seen[t] = struct{}{}
		out = append(out, t)
	}

	replace := func(oldTag, newTag string) {
		if oldTag == newTag {
			return
		}
		for i := 0; i < len(out); i++ {
			if out[i] == oldTag {
				out = append(out[:i], out[i+1:]...)
				delete(seen, oldTag)
				break
			}
		}
		if _, ok := seen[newTag]; !ok {
			seen[newTag] = struct{}{}
			out = append(out, newTag)
		}
	}

	for _, e := range plan {
		replace(AppTag(e.Group, e.App, e.OldVersion), AppTag(e.Group, e.App, e.NewVersion))
	}
	for k, ru := range roots {
		replace(RootAppTag(k.group, ru.oldVersion), RootAppTag(k.group, ru.chart.GetVersion()))
	}

	return out
}

func sameVersionIgnoringEnv(a, b string) bool {
	av, errA := ParseChartVersion(a)
	bv, errB := ParseChartVersion(b)
	if errA != nil || errB != nil {
		return false
	}
	return av.Major == bv.Major &&
		av.Minor == bv.Minor &&
		av.Patch == bv.Patch &&
		av.PreRelease == bv.PreRelease &&
		av.Extra == bv.Extra
}

// dependencyVersionForWrapperRelease selects the semver used to derive the next
// child wrapper version. When Chart.yaml lists dependencies, the version of
// the matching (or first) dependency is used. Otherwise the semver base is
// taken from the chart's own version field (major.minor.patch plus any Helm
// pre-release segment, without Hydra's environment suffix or extra counter) so
// standalone charts still get correct -dev / -stage / prod suffixes from
// NextChildChartWrapperVersion.
func dependencyVersionForWrapperRelease(ch *git.Chart, oldVer, relPath string) (string, error) {
	if v := ch.PrimaryDependencyVersion(); v != "" {
		return v, nil
	}
	parsed, err := ParseChartVersion(oldVer)
	if err != nil {
		return "", fmt.Errorf("chart %s: Chart.yaml has no dependencies; chart version must be a Hydra wrapper semver (e.g. 1.2.3-dev): %w", relPath, err)
	}
	return parsed.BaseVersion(), nil
}
