package commands

import (
	"fmt"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

type appPatternCandidate struct {
	actual     string
	matchNames []string
}

// ResolveAppIdsFromConfig resolves wildcard patterns against config-based app IDs
// from the filesystem. Without wildcards, patterns are directly converted to AppIds.
// With wildcards, all clusters and their apps are enumerated to match against.
// Exclude patterns are resolved against the same app list and subtracted from the result.
// Every resolved app id must exist in the repository (root app and enabled child apps for
// the given Helm network mode); unknown ids fail with a clear error.
// When suppressGlobPatternSummary is true, the Info log that lists app IDs grouped by glob pattern
// (wildcard resolution path) is skipped; callers that print their own summary may set this.
func ResolveAppIdsFromConfig(
	l log.Logger,
	hydraContext types.HydraContext,
	config types.Config,
	patterns []types.AppIdPattern,
	excludePatterns []types.AppIdPattern,
	networkMode types.HelmNetworkMode,
	suppressGlobPatternSummary bool,
) (sets.Set[types.AppId], error) {
	if len(patterns) == 0 {
		return sets.New[types.AppId](), nil
	}

	allAppNames, patternCandidates, err := enumerateAllAppNames(l, hydraContext, config, networkMode)
	if err != nil {
		return nil, err
	}

	raw := make([]string, len(patterns))
	hasWildcard := false
	for i, p := range patterns {
		raw[i] = string(p)
		if types.IsGlobPattern(raw[i]) {
			hasWildcard = true
		}
	}

	rawExclude := make([]string, len(excludePatterns))
	for i, p := range excludePatterns {
		rawExclude[i] = string(p)
		if types.IsGlobPattern(rawExclude[i]) {
			hasWildcard = true
		}
	}

	if !hasWildcard && len(rawExclude) == 0 {
		result := sets.New[types.AppId]()
		for _, r := range raw {
			appId, err := types.NewAppId(r)
			if err != nil {
				return nil, err
			}
			result.Insert(appId)
		}
		if err := validateAppIdsAgainstEnumerated(result, allAppNames); err != nil {
			return nil, err
		}
		return result, nil
	}

	if !hasWildcard && len(rawExclude) > 0 {
		result := sets.New[types.AppId]()
		for _, r := range raw {
			appId, err := types.NewAppId(r)
			if err != nil {
				return nil, err
			}
			result.Insert(appId)
		}
		if err := validateAppIdsAgainstEnumerated(result, allAppNames); err != nil {
			return nil, err
		}
		result, err = applyExcludes(l, result, rawExclude, patternCandidates)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	resolved, warnings, err := resolveAppPatterns(raw, patternCandidates)
	if err != nil {
		return nil, err
	}

	for _, w := range warnings {
		l.Warn(logIdCommands, w)
	}

	result := sets.New[types.AppId]()
	for _, name := range resolved {
		result.Insert(types.AppId(name))
	}

	if len(rawExclude) > 0 {
		result, err = applyExcludes(l, result, rawExclude, patternCandidates)
		if err != nil {
			return nil, err
		}
	}

	if err := validateAppIdsAgainstEnumerated(result, allAppNames); err != nil {
		return nil, err
	}

	if !suppressGlobPatternSummary {
		l.Info(logIdCommands, "resolved {count} app IDs from {patterns} pattern(s), {excludePatterns} exclude pattern(s):\n{appIds}",
			log.Int("count", len(result)),
			log.Int("patterns", len(patterns)),
			log.Int("excludePatterns", len(excludePatterns)),
			log.String("appIds", formatResolvedByPattern(raw, result, patternCandidates)))
	}

	return result, nil
}

// ValidateAppIdInCluster returns an error if appId is not among the applications
// defined for the cluster (root apps and enabled child apps for the given Helm network mode).
func ValidateAppIdInCluster(cluster *hydra.Cluster, appId types.AppId, networkMode types.HelmNetworkMode) error {
	if appId.IsPresetApp() {
		return log.CreateError(errors.ErrHydraConfigError,
			"app id '{appId}' is a synthetic builtin preset owner and cannot be targeted as a Helm app",
			log.String("appId", string(appId)))
	}
	ids, err := cluster.AppIds(networkMode)
	if err != nil {
		return err
	}
	if ids.Has(appId) {
		return nil
	}
	return log.CreateError(errors.ErrHydraConfigError,
		"unknown app id '{appId}': not defined in this Hydra repository (check cluster layout and enabled child apps)",
		log.String("appId", string(appId)))
}

func validateAppIdsAgainstEnumerated(requested sets.Set[types.AppId], allAppNames []string) error {
	if len(requested) == 0 {
		return nil
	}
	known := sets.New[string]()
	for _, n := range allAppNames {
		known.Insert(n)
	}
	var unknown []string
	for id := range requested {
		if !known.Has(string(id)) {
			unknown = append(unknown, string(id))
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	return log.CreateError(errors.ErrHydraConfigError,
		"unknown app id(s): {ids} — not defined in this Hydra repository (check cluster layout and enabled child apps)",
		log.String("ids", strings.Join(unknown, ", ")))
}

func formatResolvedByPattern(patterns []string, result sets.Set[types.AppId], candidates []appPatternCandidate) string {
	var b strings.Builder
	for i, pattern := range patterns {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(pattern)
		b.WriteString(":")

		var matched []string
		for _, candidate := range candidates {
			if !result.Has(types.AppId(candidate.actual)) {
				continue
			}
			if appPatternMatchesCandidate(pattern, candidate) {
				matched = append(matched, candidate.actual)
			}
		}
		slices.Sort(matched)
		for _, m := range matched {
			b.WriteString("\n  * ")
			b.WriteString(m)
		}
	}
	return b.String()
}

func enumerateAllAppNames(
	l log.Logger,
	hydraContext types.HydraContext,
	config types.Config,
	networkMode types.HelmNetworkMode,
) ([]string, []appPatternCandidate, error) {
	offlineConfig := types.NewConfig(config.Color(), config.DryRun(), types.KubernetesConnectionAllowedNo, config.HelmTemplateCacheEnabled())
	h, err := hydra.ResolvePath(l, hydraContext, offlineConfig)
	if err != nil {
		return nil, nil, err
	}

	ctx := h.AsContext()
	if ctx == nil {
		return nil, nil, log.CreateError(errors.ErrInvalidHydraStructure,
			"hydra context path does not resolve to a context")
	}

	clusters, err := ctx.GetClusters()
	if err != nil {
		return nil, nil, err
	}

	var allAppNames []string
	seen := map[string]*appPatternCandidate{}
	for _, cluster := range clusters {
		appIds, err := cluster.AppIds(networkMode)
		if err != nil {
			return nil, nil, err
		}
		for appId := range appIds {
			actual := string(appId)
			allAppNames = append(allAppNames, actual)

			candidate := seen[actual]
			if candidate == nil {
				candidate = &appPatternCandidate{actual: actual}
				seen[actual] = candidate
			}

			if !appId.IsRootApp() {
				candidate.matchNames = appendIfMissing(candidate.matchNames, actual)
				continue
			}

			effectiveCluster, err := effectiveClusterNameForApp(ctx, appId)
			if err != nil {
				return nil, nil, err
			}
			clusterName, err := appId.ClusterName()
			if err != nil {
				return nil, nil, err
			}
			if effectiveCluster == clusterName {
				candidate.matchNames = appendIfMissing(candidate.matchNames, actual)
				continue
			}
			rootAppName, err := appId.RootAppName()
			if err != nil {
				return nil, nil, err
			}
			alias := string(types.NewRootAppId(effectiveCluster, rootAppName))
			candidate.matchNames = appendIfMissing(candidate.matchNames, alias)
		}
	}

	patternCandidates := make([]appPatternCandidate, 0, len(seen))
	for _, actual := range allAppNames {
		candidate := seen[actual]
		if candidate == nil {
			continue
		}
		patternCandidates = append(patternCandidates, *candidate)
		delete(seen, actual)
	}
	return allAppNames, patternCandidates, nil
}

func applyExcludes(
	l log.Logger,
	included sets.Set[types.AppId],
	rawExclude []string,
	candidates []appPatternCandidate,
) (sets.Set[types.AppId], error) {
	for _, pattern := range rawExclude {
		if !types.IsGlobPattern(pattern) {
			excluded := types.AppId(pattern)
			if included.Has(excluded) {
				included.Delete(excluded)
			} else {
				l.Warn(logIdCommands,
					fmt.Sprintf("exclude pattern '%s' did not match any included application", pattern))
			}
			continue
		}

		matched := 0
		for _, candidate := range candidates {
			if !appPatternMatchesCandidate(pattern, candidate) {
				continue
			}
			appId := types.AppId(candidate.actual)
			if included.Has(appId) {
				included.Delete(appId)
				matched++
			}
		}
		if matched == 0 {
			l.Warn(logIdCommands,
				fmt.Sprintf("exclude pattern '%s' did not match any included application", pattern))
		}
	}
	return included, nil
}

func resolveAppPatterns(patterns []string, candidates []appPatternCandidate) ([]string, []string, error) {
	resultSet := map[string]bool{}
	var warnings []string

	for _, pattern := range patterns {
		if !types.IsGlobPattern(pattern) {
			resultSet[pattern] = true
			continue
		}

		matched := 0
		var singleMatch string
		for _, candidate := range candidates {
			if !appPatternMatchesCandidate(pattern, candidate) {
				continue
			}
			resultSet[candidate.actual] = true
			matched++
			singleMatch = candidate.actual
		}

		if matched == 0 {
			return nil, nil, log.CreateError(errors.ErrAppPatternNoMatch,
				"pattern '{pattern}' matched no applications",
				log.String("pattern", pattern))
		}

		if matched == 1 {
			warnings = append(warnings,
				fmt.Sprintf("pattern '%s' matched only 1 application: '%s'", pattern, singleMatch))
		}
	}

	result := make([]string, 0, len(resultSet))
	for name := range resultSet {
		result = append(result, name)
	}
	slices.Sort(result)
	return result, warnings, nil
}

func appPatternMatchesCandidate(pattern string, candidate appPatternCandidate) bool {
	for _, matchName := range candidate.matchNames {
		if types.MatchAppIdGlob(pattern, matchName) {
			return true
		}
	}
	return false
}

func appendIfMissing(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func patternCandidatesFromAppNames(appNames []string) []appPatternCandidate {
	candidates := make([]appPatternCandidate, 0, len(appNames))
	for _, name := range appNames {
		candidates = append(candidates, appPatternCandidate{
			actual:     name,
			matchNames: []string{name},
		})
	}
	return candidates
}

// ResolveAppIdsInClusterWithExcludes returns all applications defined for the given cluster in the
// Hydra repository, minus any ids matched by excludePatterns (same glob rules as
// ResolveAppIdsFromConfig excludes).
func ResolveAppIdsInClusterWithExcludes(
	l log.Logger,
	hydraContext types.HydraContext,
	config types.Config,
	clusterName types.ClusterName,
	excludePatterns []types.AppIdPattern,
	networkMode types.HelmNetworkMode,
	limits hydra.RESTClientLimits,
) (sets.Set[types.AppId], error) {
	cluster, err := hydra.ResolvePathWithCluster(l, hydraContext, clusterName, config, limits)
	if err != nil {
		return nil, err
	}
	included, err := cluster.AppIds(networkMode)
	if err != nil {
		return nil, err
	}
	if len(included) == 0 {
		return nil, log.CreateError(errors.ErrNoAppsSpecified, "no applications defined for cluster")
	}
	if len(excludePatterns) == 0 {
		return included, nil
	}
	rawExclude := make([]string, len(excludePatterns))
	for i, p := range excludePatterns {
		rawExclude[i] = string(p)
	}
	appNames := make([]string, 0, len(included))
	for id := range included {
		appNames = append(appNames, string(id))
	}
	slices.Sort(appNames)
	return applyExcludes(l, included, rawExclude, patternCandidatesFromAppNames(appNames))
}
