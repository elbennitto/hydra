package ci

import (
	"fmt"
	"strconv"
	"strings"
)

// envSuffixes maps environment names to their version suffixes.
var envSuffixes = map[string]string{
	"dev":   "-dev",
	"stage": "-stage",
	"prod":  "",
}

// ChartVersion represents a parsed Helm chart version with optional
// pre-release identifier, extra counter, and environment suffix.
type ChartVersion struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string // pre-release identifier from dependency, empty if none
	Extra      int    // -1 means no extra counter
	Env        string // empty string for prod
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// stripLeadingSemverV removes a leading "v" or "V" when the next byte is a
// digit (common upstream chart / git tag style, e.g. v4.11.0).
func stripLeadingSemverV(s string) string {
	if len(s) < 2 {
		return s
	}
	c0 := s[0]
	if (c0 != 'v' && c0 != 'V') || s[1] < '0' || s[1] > '9' {
		return s
	}
	return s[1:]
}

func ParseChartVersion(s string) (ChartVersion, error) {
	dotParts := strings.SplitN(s, ".", 3)
	if len(dotParts) != 3 {
		return ChartVersion{}, fmt.Errorf("invalid chart version: %q", s)
	}
	dotParts[0] = stripLeadingSemverV(dotParts[0])

	major, err := strconv.Atoi(dotParts[0])
	if err != nil || major < 0 {
		return ChartVersion{}, fmt.Errorf("invalid chart version: %q", s)
	}
	minor, err := strconv.Atoi(dotParts[1])
	if err != nil || minor < 0 {
		return ChartVersion{}, fmt.Errorf("invalid chart version: %q", s)
	}

	segments := strings.Split(dotParts[2], "-")
	if !isNumeric(segments[0]) {
		return ChartVersion{}, fmt.Errorf("invalid chart version: %q", s)
	}
	patch, _ := strconv.Atoi(segments[0])

	remaining := segments[1:]

	env := ""
	if len(remaining) > 0 {
		last := remaining[len(remaining)-1]
		if last == "dev" || last == "stage" {
			env = last
			remaining = remaining[:len(remaining)-1]
		}
	}

	extra := -1
	if len(remaining) > 0 {
		last := remaining[len(remaining)-1]
		if isNumeric(last) {
			extra, _ = strconv.Atoi(last)
			remaining = remaining[:len(remaining)-1]
		}
	}

	preRelease := strings.Join(remaining, "-")

	return ChartVersion{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		PreRelease: preRelease,
		Extra:      extra,
		Env:        env,
	}, nil
}

func (v ChartVersion) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		fmt.Fprintf(&b, "-%s", v.PreRelease)
	}
	if v.Extra >= 0 {
		fmt.Fprintf(&b, "-%d", v.Extra)
	}
	if v.Env != "" {
		fmt.Fprintf(&b, "-%s", v.Env)
	}
	return b.String()
}

// BaseVersion returns the version without extra counter and environment suffix.
func (v ChartVersion) BaseVersion() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		s += "-" + v.PreRelease
	}
	return s
}

// SameChildWrapperBaseVersion reports whether oldV and newV have the same
// semantic base: major.minor.patch plus any Helm pre-release segment, excluding
// the Hydra extra counter and environment suffix. When true, the only change
// is the extra counter (for example 1.0.0-dev → 1.0.0-1-dev).
func SameChildWrapperBaseVersion(oldV, newV string) (bool, error) {
	ov, err := ParseChartVersion(oldV)
	if err != nil {
		return false, err
	}
	nv, err := ParseChartVersion(newV)
	if err != nil {
		return false, err
	}
	return ov.BaseVersion() == nv.BaseVersion(), nil
}

// ComputeWrapperVersion produces the chart wrapper version from a dependency
// version and target environment. If the base version already exists (indicated
// by existingExtra), an extra counter is appended.
//
// Examples:
//
//	ComputeWrapperVersion("1.200.9", "dev", -1)  → "1.200.9-dev"
//	ComputeWrapperVersion("1.200.9", "prod", -1) → "1.200.9"
//	ComputeWrapperVersion("1.200.9", "dev", 0)   → "1.200.9-1-dev"
//	ComputeWrapperVersion("1.200.9", "dev", 2)   → "1.200.9-3-dev"
func ComputeWrapperVersion(dependencyVersion string, env string, existingExtra int) (string, error) {
	dependencyVersion = stripLeadingSemverV(dependencyVersion)
	base := dependencyVersion
	preRelease := ""
	if idx := strings.Index(dependencyVersion, "-"); idx >= 0 {
		base = dependencyVersion[:idx]
		preRelease = dependencyVersion[idx+1:]
	}

	parts := strings.SplitN(base, ".", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid dependency version: %q", dependencyVersion)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid major version in %q: %w", dependencyVersion, err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid minor version in %q: %w", dependencyVersion, err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", fmt.Errorf("invalid patch version in %q: %w", dependencyVersion, err)
	}

	extra := -1
	if existingExtra >= 0 {
		extra = existingExtra + 1
	}

	suffix, ok := envSuffixes[env]
	if !ok {
		return "", fmt.Errorf("unknown environment: %q", env)
	}

	v := ChartVersion{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		PreRelease: preRelease,
		Extra:      extra,
		Env:        strings.TrimPrefix(suffix, "-"),
	}
	return v.String(), nil
}

// promoteBaseSlotOccupied reports whether the target chart already occupies the
// base version line (major.minor.patch + Helm prerelease, no Hydra extra
// counter) for the target environment, so the promoted chart must use the
// first extra counter (-1-<env> or -1 for prod).
func promoteBaseSlotOccupied(existing ChartVersion, candidate ChartVersion) bool {
	if existing.Major != candidate.Major || existing.Minor != candidate.Minor || existing.Patch != candidate.Patch {
		return false
	}
	if existing.PreRelease != candidate.PreRelease {
		return false
	}
	if existing.Env != candidate.Env {
		return false
	}
	return existing.Extra < 0
}

// ComputePromoteTargetVersion derives the chart version for the target
// environment when promoting. When the source used a Hydra extra counter
// (e.g. -2- in 1.0.0-2-dev), that counter is not copied to the target: promote
// targets the base line first (1.0.0-stage). If the target chart already uses
// that base line (same semver + prerelease, env suffix, no extra counter), the
// version becomes 1.0.0-1-stage (or 1.0.0-1 for prod). When the source has no
// extra counter, the candidate stays the base line even if the target already
// shows it (same version for content-only updates).
//
// existingTargetVersion is the current Chart.yaml version on the target
// directory, or empty when the target directory does not exist yet.
func ComputePromoteTargetVersion(sourceVersion, sourceEnv, targetEnv, existingTargetVersion string) (string, error) {
	v, err := ParseChartVersion(sourceVersion)
	if err != nil {
		return "", err
	}

	// CI promote may use a reset/default chart version (0.0.0) without an
	// environment suffix. Treat it as a valid source exception for all
	// source environments.
	if !isDefaultPromoteResetVersion(v) && v.Env != sourceEnv && !(v.Env == "" && sourceEnv == "prod") {
		return "", fmt.Errorf("version %q does not have expected suffix for %q", sourceVersion, sourceEnv)
	}

	sourceHadExtraCounter := v.Extra >= 0
	v.Extra = -1
	targetSuffix := ""
	if targetEnv != "prod" {
		targetSuffix = targetEnv
	}
	v.Env = targetSuffix
	candidate := v.String()

	if existingTargetVersion == "" {
		return candidate, nil
	}

	ev, err := ParseChartVersion(existingTargetVersion)
	if err != nil {
		return "", fmt.Errorf("parse existing target version %q: %w", existingTargetVersion, err)
	}

	if sourceHadExtraCounter && promoteBaseSlotOccupied(ev, v) {
		v.Extra = 1
		return v.String(), nil
	}
	return candidate, nil
}

func isDefaultPromoteResetVersion(v ChartVersion) bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0 && v.PreRelease == "" && v.Extra < 0 && v.Env == ""
}
