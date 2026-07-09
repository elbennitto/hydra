package ci

import (
	"fmt"
	"path/filepath"

	"hydra-gitops.org/hydra/hydra-go/core/git"
)

type chartReleaseState struct {
	BaselineRef         string
	ChangedSinceBaseline bool
	FirstRelease         bool
}

// chartDirChangedSinceLastRelease reports whether files under relChartDir
// changed since the last build-* tag that included this directory, or since
// the repository root commit when no such tag exists.
func chartDirChangedSinceLastRelease(r *git.Repo, relChartDir string) (bool, error) {
	state, err := chartDirReleaseState(r, relChartDir)
	if err != nil {
		return false, err
	}
	if !state.ChangedSinceBaseline {
		return false, nil
	}
	versionOnly, err := chartDirOnlyVersionChanged(r, state.BaselineRef, relChartDir)
	if err != nil {
		return false, err
	}
	if versionOnly {
		return false, nil
	}
	return true, nil
}

// chartDirReleaseState returns the release baseline for a chart directory.
// When the chart has no prior build tag, the baseline becomes the commit that
// introduced the chart path instead of the repository root, so the chart's
// initial checked-in version is not treated as an unreleased extra bump.
func chartDirReleaseState(r *git.Repo, relChartDir string) (chartReleaseState, error) {
	tag, err := r.LastBuildTag(relChartDir)
	if err == nil {
		changed, err := r.HasChanges(tag, "HEAD", relChartDir)
		return chartReleaseState{BaselineRef: tag, ChangedSinceBaseline: changed}, err
	}
	introHash, errIntro := r.FirstCommitAffectingPath(relChartDir)
	if errIntro != nil {
		return chartReleaseState{}, fmt.Errorf("change detection for %s: last build tag: %w; first path commit: %w", relChartDir, err, errIntro)
	}
	changed, errChanged := r.HasChanges(introHash, "HEAD", relChartDir)
	if errChanged != nil {
		return chartReleaseState{}, errChanged
	}
	return chartReleaseState{BaselineRef: introHash, ChangedSinceBaseline: changed, FirstRelease: true}, nil
}

// chartDirOnlyVersionChanged reports whether the only change under relChartDir
// is a Chart.yaml version bump. This keeps release from re-bumping a chart
// when the previous commit only updated the chart version field.
func chartDirOnlyVersionChanged(r *git.Repo, baselineRef, relChartDir string) (bool, error) {
	paths, err := r.ChangedPaths(baselineRef, "HEAD")
	if err != nil {
		return false, err
	}
	expectedChartPath := filepath.ToSlash(filepath.Join(relChartDir, "Chart.yaml"))
	if len(paths) != 1 || paths[0] != expectedChartPath {
		return false, nil
	}

	baseChart, err := r.ChartYAMLAt(baselineRef, relChartDir)
	if err != nil {
		return false, err
	}
	headChart, err := r.ChartYAMLAt("HEAD", relChartDir)
	if err != nil {
		return false, err
	}

	baseNorm, err := normalizeChartYAMLVersion(baseChart)
	if err != nil {
		return false, err
	}
	headNorm, err := normalizeChartYAMLVersion(headChart)
	if err != nil {
		return false, err
	}
	return baseNorm == headNorm, nil
}

func normalizeChartYAMLVersion(raw string) (string, error) {
	return git.RewriteChartYAMLVersion([]byte(raw), "__hydra_chart_version__")
}

func envAllowed(env string, allowed []string) bool {
	for _, e := range allowed {
		if e == env {
			return true
		}
	}
	return false
}
