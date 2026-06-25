package hydra

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"slices"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	"hydra-gitops.org/hydra/hydra-go/core/yaml"
	"k8s.io/apimachinery/pkg/util/sets"
)

type HydraPredicateContext struct {
	AppId       types.AppId
	Tag         string
	GroupName   string
	ParserIndex int
	BlockPath   string
	Predicate   string
	Sources     []string
}

func (c HydraPredicateContext) SourceSummary() string {
	if len(c.Sources) == 0 {
		return ""
	}
	return strings.Join(c.Sources, ", ")
}

func (c HydraPredicateContext) Summary() string {
	parts := []string{fmt.Sprintf("app=%s", c.AppId)}
	if c.Tag != "" {
		parts = append(parts, fmt.Sprintf("tag=%s", c.Tag))
	}
	if c.BlockPath != "" {
		parts = append(parts, fmt.Sprintf("block=%s", c.BlockPath))
	}
	if src := c.SourceSummary(); src != "" {
		parts = append(parts, fmt.Sprintf("source=%s", src))
	}
	return strings.Join(parts, " ")
}

// orderedUnionPerAppHydraConfigMapDocuments returns every chart-scoped Hydra ConfigMap document from
// perApp buckets exactly once, in stable Id order (cluster-wide catalog for merging into each app).
func orderedUnionPerAppHydraConfigMapDocuments(
	perApp map[types.AppId][]HydraConfigMapDocument,
	appIds sets.Set[types.AppId],
) []HydraConfigMapDocument {
	appList := appIds.UnsortedList()
	slices.SortFunc(appList, func(a, b types.AppId) int {
		return cmp.Compare(string(a), string(b))
	})
	seen := sets.New[types.Id]()
	var out []HydraConfigMapDocument
	for _, aid := range appList {
		for _, d := range perApp[aid] {
			if seen.Has(d.Id) {
				continue
			}
			seen.Insert(d.Id)
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b HydraConfigMapDocument) int {
		return cmp.Compare(string(a.Id), string(b.Id))
	})
	return out
}

// HelmHydraMapFromValues converts validated Helm HydraValues to a generic map for merging with ConfigMap data.hydra.
func HelmHydraMapFromValues(hv *types.HydraValues) (map[string]any, error) {
	if hv == nil {
		return nil, nil
	}
	ys, err := yaml.ToYaml(hv)
	if err != nil {
		return nil, err
	}
	return yaml.FromYaml[map[string]any](ys)
}

// helmChartBackedAppIds drops synthetic builtin preset owner ids ({cluster}.preset.<presetId>);
// they have no on-disk Helm chart and cannot be passed to [Cluster.WithApp].
func helmChartBackedAppIds(appIds sets.Set[types.AppId]) sets.Set[types.AppId] {
	out := sets.New[types.AppId]()
	for id := range appIds {
		if !id.IsPresetApp() {
			out.Insert(id)
		}
	}
	return out
}

func hydraAppMergedValuesMap(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) (map[types.AppId]types.ValuesMap, error) {
	helmIds := helmChartBackedAppIds(appIds)
	if helmIds.Len() == 0 {
		return map[types.AppId]types.ValuesMap{}, nil
	}
	perApp, global, err := PartitionHydraConfigDocumentsByApp(rendered, types.KeyTemplateEntity, helmIds)
	if err != nil {
		return nil, err
	}
	out := make(map[types.AppId]types.ValuesMap)
	for appId := range helmIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			return nil, err
		}
		hv, err := HydraValues(h, networkMode)
		if err != nil {
			return nil, err
		}
		helmMap, err := HelmHydraMapFromValues(hv)
		if err != nil {
			return nil, err
		}
		docsForApp := HydraConfigMapDocumentsForApp(perApp, global, appIds, appId)
		out[appId] = MergeHelmHydraWithConfigMapDocuments(helmMap, docsForApp)
	}
	return out, nil
}

// HydraConfigMapDocumentsForApp returns the ordered union of Hydra ConfigMap documents that apply to
// targetApp after scope filtering (same catalog as cluster-wide merge).
func HydraConfigMapDocumentsForApp(
	perApp map[types.AppId][]HydraConfigMapDocument,
	global []HydraConfigMapDocument,
	clusterAppIds sets.Set[types.AppId],
	targetApp types.AppId,
) []HydraConfigMapDocument {
	unionPerApp := orderedUnionPerAppHydraConfigMapDocuments(perApp, clusterAppIds)
	docs := make([]HydraConfigMapDocument, 0, len(unionPerApp)+len(global))
	docs = append(docs, unionPerApp...)
	docs = append(docs, global...)
	slices.SortFunc(docs, func(a, b HydraConfigMapDocument) int {
		return cmp.Compare(string(a.Id), string(b.Id))
	})
	out := make([]HydraConfigMapDocument, 0, len(docs))
	for i := range docs {
		if EvaluateHydraScope(docs[i].Scope, clusterAppIds, targetApp) {
			out = append(out, docs[i])
		}
	}
	return out
}

// HydraAppMergedGlobalHydraMaps returns Helm-derived global.hydra deep-merged with every Hydra ConfigMap
// data.hydra document from the partition (all chart-scoped carriers across apps, deduped, plus global
// fragments), merged into each app’s map on top of that app’s own Helm global.hydra. The rendered catalog
// must include every ConfigMap carrier needed for partitioning (typically a full-cluster template render).
func HydraAppMergedGlobalHydraMaps(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) (map[types.AppId]types.ValuesMap, error) {
	return hydraAppMergedValuesMap(cluster, appIds, networkMode, rendered)
}

// hydraValuesFromMergedMapLoose unmarshals merged Helm + ConfigMap hydra YAML into HydraValues without Validate()
// (ConfigMap-only fragments may omit fields required for full Helm validation).
func hydraValuesFromMergedMapLoose(merged types.ValuesMap) (*types.HydraValues, error) {
	if len(merged) == 0 {
		return nil, nil
	}
	ys, err := yaml.ToYaml(merged)
	if err != nil {
		return nil, err
	}
	hv, err := yaml.FromYaml[types.HydraValues](ys)
	if err != nil {
		return nil, err
	}
	return &hv, nil
}

func extractRefsFromMergedMap(merged types.ValuesMap) map[string]types.HydraRefGroup {
	if len(merged) == 0 {
		return nil
	}
	refsVal, ok := merged["refs"]
	if !ok || refsVal == nil {
		return nil
	}
	ys, err := yaml.ToYaml(refsVal)
	if err != nil {
		return nil
	}
	m, err := yaml.FromYaml[map[string]types.HydraRefGroup](ys)
	if err != nil {
		return nil
	}
	return m
}

func extractClonesFromMergedMap(merged types.ValuesMap) map[string]types.HydraCloneRule {
	if len(merged) == 0 {
		return nil
	}
	v, ok := merged["clones"]
	if !ok || v == nil {
		return nil
	}
	ys, err := yaml.ToYaml(v)
	if err != nil {
		return nil
	}
	m, err := yaml.FromYaml[map[string]types.HydraCloneRule](ys)
	if err != nil {
		return nil
	}
	return m
}

func warnUnknownCloneTag(name string, tag string) {
	if tag == "" || tag == "bootstrap" {
		return
	}
	l := log.Default()
	l.Warn(logIdHydra, "clone rule {name} has unknown tag {tag} — ignored (treated as always active)",
		log.String("name", name), log.String("tag", tag))
}

// cloneRulesFromHydraCloneMap validates and returns named clone rules from a global.hydra.clones map.
func cloneRulesFromHydraCloneMap(clones map[string]types.HydraCloneRule, declaringApp types.AppId) ([]types.HydraCloneRuleEntry, error) {
	if len(clones) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(clones))
	for n := range clones {
		names = append(names, n)
	}
	slices.Sort(names)
	var out []types.HydraCloneRuleEntry
	for _, name := range names {
		rule := clones[name]
		if strings.TrimSpace(rule.Predicate) == "" {
			return nil, fmt.Errorf("clones[%s]: predicate is required", name)
		}
		if rule.Targets.IsEmpty() {
			return nil, fmt.Errorf("clones[%s]: targets is required", name)
		}
		warnUnknownCloneTag(name, strings.TrimSpace(rule.Tag))
		out = append(out, types.HydraCloneRuleEntry{
			Name:         name,
			Rule:         rule,
			DeclaringApp: declaringApp,
		})
	}
	return out, nil
}

// cloneRulesFromMergedHydraMap validates and returns named clone rules from a merged global.hydra map.
func cloneRulesFromMergedHydraMap(merged types.ValuesMap, declaringApp types.AppId) ([]types.HydraCloneRuleEntry, error) {
	return cloneRulesFromHydraCloneMap(extractClonesFromMergedMap(merged), declaringApp)
}

// HydraAppCloneRules collects clone rules from Helm global.hydra + Hydra ConfigMaps using the same
// catalog and scope semantics as [HydraAppRefParsers].
func HydraAppCloneRules(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]types.HydraCloneRuleEntry, error) {
	helmIds := helmChartBackedAppIds(appIds)
	if helmIds.Len() == 0 {
		return nil, nil
	}
	perApp, global, err := PartitionHydraConfigDocumentsByApp(rendered, types.KeyTemplateEntity, helmIds)
	if err != nil {
		return nil, err
	}
	var result []types.HydraCloneRuleEntry
	for appId := range helmIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			return nil, err
		}
		hv, err := HydraValues(h, networkMode)
		if err != nil {
			return nil, err
		}
		helmMap, err := HelmHydraMapFromValues(hv)
		if err != nil {
			return nil, err
		}
		docs := HydraConfigMapDocumentsForApp(perApp, global, appIds, appId)
		merged := MergeHelmHydraWithConfigMapDocuments(helmMap, docs)
		entries, err := cloneRulesFromMergedHydraMap(merged, appId)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	slices.SortFunc(result, func(a, b types.HydraCloneRuleEntry) int {
		if c := cmp.Compare(string(a.DeclaringApp), string(b.DeclaringApp)); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return result, nil
}

// HydraAppNamespaceOwners collects declared namespace ownership from global.hydra.ownerNamespaces
// across all apps. Returns a map from namespace to the single declaring app. Duplicate declarations
// (two apps claiming the same namespace) are a hard error.
func HydraAppNamespaceOwners(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
) (map[types.Namespace]types.AppId, error) {
	result := map[types.Namespace]types.AppId{}
	for appId := range appIds {
		if appId.IsPresetApp() {
			continue
		}
		h, err := cluster.WithApp(appId)
		if err != nil {
			return nil, err
		}
		hv, err := HydraValues(h, networkMode)
		if err != nil {
			return nil, err
		}
		if hv == nil {
			continue
		}
		for _, ns := range hv.OwnerNamespaces {
			key := types.Namespace(ns)
			if existing, ok := result[key]; ok && existing != appId {
				return nil, log.CreateError(errors.ErrHydraConfigError,
					"namespace {ns} is declared as ownerNamespace by multiple apps: {a} and {b}",
					log.String("ns", ns), log.String("a", string(existing)), log.String("b", string(appId)))
			}
			result[key] = appId
		}
	}
	return result, nil
}

// RefParsersFromMergedHydraMap builds ref parsers from a merged global.hydra map (Helm + ConfigMap data.hydra).
func RefParsersFromMergedHydraMap(merged types.ValuesMap) ([]types.RefParser, error) {
	refs := extractRefsFromMergedMap(merged)
	if refs == nil {
		return nil, nil
	}
	return RefParsersFromHydraRefGroups(refs)
}

func HydraAppUninstallPredicates(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]string, error) {
	contexts, err := HydraAppUninstallPredicateContexts(cluster, appIds, networkMode, rendered)
	if err != nil {
		return nil, err
	}
	return predicateStringsFromContexts(contexts), nil
}

func HydraAppUninstallSafePredicates(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]string, error) {
	_, negativePriority, err := HydraAppRefOwnershipUninstallPredicateLinePriorityBands(
		cluster,
		appIds,
		networkMode,
		rendered,
		true,
	)
	if err != nil {
		return nil, err
	}
	appList := appIds.UnsortedList()
	slices.SortFunc(appList, func(a, b types.AppId) int {
		return cmp.Compare(string(a), string(b))
	})
	seen := sets.New[string]()
	out := make([]string, 0)
	for _, appId := range appList {
		for _, line := range negativePriority[appId] {
			predicate := strings.TrimSpace(line.Cel)
			if predicate == "" || seen.Has(predicate) {
				continue
			}
			seen.Insert(predicate)
			out = append(out, predicate)
		}
	}
	return out, nil
}

func HydraAppBackupPredicates(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]string, error) {
	contexts, err := HydraAppBackupPredicateContexts(cluster, appIds, networkMode, rendered)
	if err != nil {
		return nil, err
	}
	return predicateStringsFromContexts(contexts), nil
}

func HydraAppUninstallPredicateContexts(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]HydraPredicateContext, error) {
	return collectPredicateContextsByTag(cluster, appIds, networkMode, rendered, "uninstall")
}

func HydraAppBackupPredicateContexts(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]HydraPredicateContext, error) {
	return collectPredicateContextsByTag(cluster, appIds, networkMode, rendered, "backup")
}

// HydraAppBootstrapGuardPredicates returns CEL predicate strings from enabled
// global.hydra.refs groups tagged with RefTagBootstrapGuard (bootstrap-guard).
func HydraAppBootstrapGuardPredicates(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]string, error) {
	return collectPredicatesByTag(cluster, appIds, networkMode, rendered, types.RefTagBootstrapGuard)
}

// WarnDuplicateBackupUninstallTags checks if any ref group has both a backup
// tag and an uninstall/uninstall-safe/uninstall-force tag, and logs a warning
// for each occurrence. The backup tag already implies uninstall.
func WarnDuplicateBackupUninstallTags(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) {
	l := cluster.L()
	m, err := HydraAppValues(cluster, appIds, networkMode, rendered)
	if err != nil {
		return
	}
	uninstallTags := []string{"uninstall", "uninstall-safe", "uninstall-force"}
	for _, hv := range m {
		if hv == nil {
			continue
		}
		for name, group := range hv.Refs {
			if !group.IsEnabled() || !group.HasTag("backup") {
				continue
			}
			for _, ut := range uninstallTags {
				if group.HasTag(ut) {
					l.Warn(logIdHydra, "ref group \"{name}\" has both [backup] and [{tag}] tags -- the {tag} tag should be removed (backup implies uninstall)",
						log.String("name", name), log.String("tag", ut))
				}
			}
		}
	}
}

func HydraAppUninstallForcePredicates(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]string, error) {
	return collectPredicatesByTag(cluster, appIds, networkMode, rendered, "uninstall-force")
}

// HydraAppUninstallForcePredicateLines collects uninstall-force ownership predicate lines per app,
// including pick CEL entries, plus synthetic lines from
// global.hydra.clones (same CEL as strong-tier clone ownership) so runtime clone targets such as
// Kyverno-mirrored image-pull Secrets participate in uninstall-force leftover classification.
func HydraAppUninstallForcePredicateLines(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
	includeRuntimeTaggedGroups bool,
) (map[types.AppId][]types.RefOwnershipPredicateLine, error) {
	m, err := HydraAppValues(cluster, appIds, networkMode, rendered)
	if err != nil {
		return nil, err
	}
	out := make(map[types.AppId][]types.RefOwnershipPredicateLine)
	for appId, hv := range m {
		if hv == nil {
			continue
		}
		var lines []types.RefOwnershipPredicateLine
		groupNames := make([]string, 0, len(hv.Refs))
		for groupName := range hv.Refs {
			groupNames = append(groupNames, groupName)
		}
		slices.Sort(groupNames)
		for _, groupName := range groupNames {
			group := hv.Refs[groupName]
			if !group.IsEnabled() || !group.HasTag("uninstall-force") {
				continue
			}
			if !includeRuntimeTaggedGroups && group.HasTag(types.RefTagRuntime) {
				continue
			}
			for parserIndex, rp := range group.RefParsers {
				appendRefOwnershipLinesFromRefParser(&lines, groupName, parserIndex, nil, group, rp)
			}
		}
		for _, cel := range CloneOwnershipPredicatesFromHydraValues(hv) {
			cel = strings.TrimSpace(cel)
			if cel == "" {
				continue
			}
			lines = append(lines, types.RefOwnershipPredicateLine{Cel: cel})
		}
		if len(lines) > 0 {
			out[appId] = lines
		}
	}
	return out, nil
}

// HydraAppAllRefOwnershipPredicates returns non-empty ref-parser predicate and pick-CEL strings per
// app from enabled ref groups whose tags include **uninstall**, **uninstall-safe**, **uninstall-force**,
// and/or **backup** (not untagged groups), plus synthetic predicates derived from
// **global.hydra.clones** (same shape as in Helm / Hydra ConfigMaps) so materialized or operator-synced
// resources that mirror a clone source id are owned by the app that declares the clone rule. Each
// ref-parser’s **pick** entries participate in ownership so objects reachable only via pick in the
// ref graph still match uninstall ownership when the CEL is non-empty. When includeRuntimeTaggedGroups
// is false, groups tagged [RefTagRuntime] are skipped (hydra local review; template-mapped ids in
// cluster review and cluster uninstall). When true, runtime-tagged groups are included for
// cluster-only ownership matching (ids absent from every standalone template render).
func HydraAppAllRefOwnershipPredicates(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
	includeRuntimeTaggedGroups bool,
) (map[types.AppId][]string, error) {
	nonNegativePriority, negativePriority, err := HydraAppRefOwnershipUninstallPredicatePriorityBands(
		cluster, appIds, networkMode, rendered, includeRuntimeTaggedGroups)
	if err != nil {
		return nil, err
	}
	out := make(map[types.AppId][]string)
	for appId := range appIds {
		var preds []string
		if s := nonNegativePriority[appId]; len(s) > 0 {
			preds = append(preds, s...)
		}
		if w := negativePriority[appId]; len(w) > 0 {
			preds = append(preds, w...)
		}
		if len(preds) > 0 {
			out[appId] = preds
		}
	}
	return out, nil
}

// refGroupHasDirectUninstallOwnershipTag reports whether a ref group’s tags allow direct runtime
// ownership assignment in the central cluster inventory pass.
func refGroupHasDirectUninstallOwnershipTag(group types.HydraRefGroup) bool {
	return group.HasTag("uninstall") || group.HasTag("uninstall-force") || group.HasTag("backup")
}

// HydraAppRefOwnershipUninstallPredicatePriorityBands partitions uninstall ref-ownership predicates
// into non-negative-priority rules and negative-priority rules.
func HydraAppRefOwnershipUninstallPredicatePriorityBands(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
	includeRuntimeTaggedGroups bool,
) (nonNegativePriority map[types.AppId][]string, negativePriority map[types.AppId][]string, err error) {
	nonNegativeLines, negativeLines, err := HydraAppRefOwnershipUninstallPredicateLinePriorityBands(cluster, appIds, networkMode, rendered, includeRuntimeTaggedGroups)
	if err != nil {
		return nil, nil, err
	}
	return refOwnershipLinePriorityBandMapToStrings(nonNegativeLines), refOwnershipLinePriorityBandMapToStrings(negativeLines), nil
}

func refOwnershipLinePriorityBandMapToStrings(m map[types.AppId][]types.RefOwnershipPredicateLine) map[types.AppId][]string {
	out := make(map[types.AppId][]string)
	for id, lines := range m {
		ss := refOwnershipLinesToCelStrings(lines)
		if len(ss) > 0 {
			out[id] = ss
		}
	}
	return out
}

// HydraAppRefOwnershipUninstallPredicateLinePriorityBands is like
// HydraAppRefOwnershipUninstallPredicatePriorityBands but keeps predicates as structured lines for
// shared compile paths.
func HydraAppRefOwnershipUninstallPredicateLinePriorityBands(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
	includeRuntimeTaggedGroups bool,
) (nonNegativePriority map[types.AppId][]types.RefOwnershipPredicateLine, negativePriority map[types.AppId][]types.RefOwnershipPredicateLine, err error) {
	m := map[types.AppId]*types.HydraValues{}
	sourcesByAppAndGroup := map[types.AppId]map[string][]string{}
	helmSourceByApp := map[types.AppId]string{}
	if cluster != nil {
		m, err = HydraAppValues(cluster, appIds, networkMode, rendered)
		if err != nil {
			return nil, nil, err
		}
		helmIds := helmChartBackedAppIds(appIds)
		perApp, global, err := PartitionHydraConfigDocumentsByApp(rendered, types.KeyTemplateEntity, helmIds)
		if err != nil {
			return nil, nil, err
		}
		for appId := range helmIds {
			h, herr := cluster.WithApp(appId)
			if herr != nil {
				return nil, nil, herr
			}
			hv, herr := HydraValues(h, networkMode)
			if herr != nil {
				return nil, nil, herr
			}
			helmMap, herr := HelmHydraMapFromValues(hv)
			if herr != nil {
				return nil, nil, herr
			}
			docs := HydraConfigMapDocumentsForApp(perApp, global, appIds, appId)
			sourcesByAppAndGroup[appId] = predicateSourcesByGroup(h, helmMap, docs)
			helmSource := "helm values"
			if chartDir, cerr := ChartDirectoryForHydraApp(h); cerr == nil {
				helmSource = filepath.Join(chartDir.Path(), "values.yaml")
			}
			helmSourceByApp[appId] = helmSource
		}
	} else {
		for appId := range appIds {
			m[appId] = nil
		}
	}
	nonNegativePriority = make(map[types.AppId][]types.RefOwnershipPredicateLine)
	negativePriority = make(map[types.AppId][]types.RefOwnershipPredicateLine)
	defaultNonNegative, defaultNegative := defaultRefOwnershipPredicateLinePriorityBands(includeRuntimeTaggedGroups)
	for appId, hv := range m {
		var nonNegative []types.RefOwnershipPredicateLine
		var negative []types.RefOwnershipPredicateLine
		if hv != nil {
			nonNegative = refOwnershipLinesFromHydraValuesNonNegativePriority(hv, includeRuntimeTaggedGroups, sourcesByAppAndGroup[appId])
			negative = refOwnershipLinesFromHydraValuesNegativePriority(hv, includeRuntimeTaggedGroups, sourcesByAppAndGroup[appId])
			nonNegativeClone, negativeClone := refOwnershipPredicateLinesFromCloneRulesPriorityBands(hv, helmSourceByApp[appId])
			nonNegative = append(nonNegative, nonNegativeClone...)
			negative = append(negative, negativeClone...)
		}
		if len(defaultNonNegative) > 0 {
			nonNegative = append(nonNegative, defaultNonNegative...)
		}
		if len(defaultNegative) > 0 {
			negative = append(negative, defaultNegative...)
		}
		if len(nonNegative) > 0 {
			nonNegativePriority[appId] = append([]types.RefOwnershipPredicateLine{}, nonNegative...)
		}
		if len(negative) > 0 {
			negativePriority[appId] = append([]types.RefOwnershipPredicateLine{}, negative...)
		}
	}
	return nonNegativePriority, negativePriority, nil
}

func defaultRefOwnershipPredicateLinePriorityBands(includeRuntimeTaggedGroups bool) (nonNegativePriority, negativePriority []types.RefOwnershipPredicateLine) {
	for parserIndex, entry := range references.DefaultRefParserEntries() {
		rp := entry.Parser
		if !includeRuntimeTaggedGroups && slices.Contains(rp.Tags, types.RefTagRuntime) {
			continue
		}
		sourceList := []string{"embedded default ref-parsers"}
		if strings.TrimSpace(entry.SourcePath) != "" {
			sourceList = []string{entry.SourcePath}
		}
		switch {
		case slices.Contains(rp.Tags, "uninstall") || slices.Contains(rp.Tags, "uninstall-force") || slices.Contains(rp.Tags, "backup"):
			appendRefOwnershipLinesFromDefaultRefParser(&nonNegativePriority, parserIndex, sourceList, rp)
		case slices.Contains(rp.Tags, "uninstall-safe"):
			appendRefOwnershipLinesFromDefaultRefParser(&negativePriority, parserIndex, sourceList, rp)
		}
	}
	return nonNegativePriority, negativePriority
}

func refOwnershipPredicateLinesFromCloneRulesPriorityBands(hv *types.HydraValues, helmSource string) (nonNegativePriority, negativePriority []types.RefOwnershipPredicateLine) {
	nonNegativeStrings, negativeStrings := refOwnershipPredicatesFromCloneRulesPriorityBands(hv)
	ruleNames := make([]string, 0, len(hv.Clones))
	for name := range hv.Clones {
		ruleNames = append(ruleNames, name)
	}
	slices.Sort(ruleNames)
	ruleNameByPredicate := make(map[string]string)
	for _, ruleName := range ruleNames {
		rule := hv.Clones[ruleName]
		if strings.TrimSpace(rule.Predicate) == "" || rule.Targets.IsEmpty() {
			continue
		}
		p, err := ownershipPredicateCelFromCloneRule(rule)
		if err != nil || p == "" {
			continue
		}
		if _, exists := ruleNameByPredicate[p]; !exists {
			ruleNameByPredicate[p] = ruleName
		}
	}
	for _, x := range nonNegativeStrings {
		nonNegativePriority = append(nonNegativePriority, types.RefOwnershipPredicateLine{
			Cel: x,
			Source: &types.RefOwnershipRuleSource{
				Kind:      types.RefOwnershipRuleSourceKindCloneRule,
				BlockPath: cloneRuleBlockPath(ruleNameByPredicate[x]),
				Sources:   optionalSourceList(helmSource),
			},
		})
	}
	for _, x := range negativeStrings {
		negativePriority = append(negativePriority, types.RefOwnershipPredicateLine{
			Cel: x,
			Source: &types.RefOwnershipRuleSource{
				Kind:      types.RefOwnershipRuleSourceKindCloneRule,
				BlockPath: cloneRuleBlockPath(ruleNameByPredicate[x]),
				Sources:   optionalSourceList(helmSource),
			},
		})
	}
	return nonNegativePriority, negativePriority
}

func refOwnershipPredicatesFromHydraValues(hv *types.HydraValues, includeRuntimeTaggedGroups bool) []string {
	return append(
		refOwnershipPredicatesFromHydraValuesNonNegativePriority(hv, includeRuntimeTaggedGroups),
		refOwnershipPredicatesFromHydraValuesNegativePriority(hv, includeRuntimeTaggedGroups)...,
	)
}

func refOwnershipPredicatesFromHydraValuesNonNegativePriority(hv *types.HydraValues, includeRuntimeTaggedGroups bool) []string {
	return refOwnershipLinesToCelStrings(refOwnershipLinesFromHydraValuesNonNegativePriority(hv, includeRuntimeTaggedGroups, nil))
}

func refOwnershipParserPriority(group types.HydraRefGroup, rp types.HydraRefParser) int {
	if rp.Priority != 0 {
		return rp.Priority
	}
	if group.Priority != 0 {
		return group.Priority
	}
	return rp.Priority
}

// refOwnershipLinesFromHydraValuesNonNegativePriority collects uninstall ownership predicate lines
// from ref groups that participate with non-negative priority.
func refOwnershipLinesFromHydraValuesNonNegativePriority(hv *types.HydraValues, includeRuntimeTaggedGroups bool, sourcesByGroup map[string][]string) []types.RefOwnershipPredicateLine {
	var out []types.RefOwnershipPredicateLine
	groupNames := make([]string, 0, len(hv.Refs))
	for groupName := range hv.Refs {
		groupNames = append(groupNames, groupName)
	}
	slices.Sort(groupNames)
	for _, groupName := range groupNames {
		group := hv.Refs[groupName]
		if !group.IsEnabled() {
			continue
		}
		if !group.HasTag("uninstall") && !group.HasTag("uninstall-safe") && !group.HasTag("uninstall-force") && !group.HasTag("backup") {
			continue
		}
		if !refGroupHasDirectUninstallOwnershipTag(group) {
			continue
		}
		if !includeRuntimeTaggedGroups && group.HasTag(types.RefTagRuntime) {
			continue
		}
		for parserIndex, rp := range group.RefParsers {
			if refOwnershipParserPriority(group, rp) < 0 {
				continue
			}
			appendRefOwnershipLinesFromRefParser(&out, groupName, parserIndex, sourcesByGroup[groupName], group, rp)
		}
	}
	return out
}

// refOwnershipPredicatesFromHydraValuesNegativePriority collects predicates from ref groups that
// carry uninstall-safe semantics or explicit negative parser priorities.
func refOwnershipPredicatesFromHydraValuesNegativePriority(hv *types.HydraValues, includeRuntimeTaggedGroups bool) []string {
	return refOwnershipLinesToCelStrings(refOwnershipLinesFromHydraValuesNegativePriority(hv, includeRuntimeTaggedGroups, nil))
}

func refOwnershipLinesFromHydraValuesNegativePriority(hv *types.HydraValues, includeRuntimeTaggedGroups bool, sourcesByGroup map[string][]string) []types.RefOwnershipPredicateLine {
	var out []types.RefOwnershipPredicateLine
	groupNames := make([]string, 0, len(hv.Refs))
	for groupName := range hv.Refs {
		groupNames = append(groupNames, groupName)
	}
	slices.Sort(groupNames)
	for _, groupName := range groupNames {
		group := hv.Refs[groupName]
		if !group.IsEnabled() {
			continue
		}
		groupIsNegativeOnly := group.HasTag("uninstall-safe") && !refGroupHasDirectUninstallOwnershipTag(group)
		if !groupIsNegativeOnly && !refGroupHasDirectUninstallOwnershipTag(group) {
			continue
		}
		if !includeRuntimeTaggedGroups && group.HasTag(types.RefTagRuntime) {
			continue
		}
		for parserIndex, rp := range group.RefParsers {
			if !groupIsNegativeOnly && refOwnershipParserPriority(group, rp) >= 0 {
				continue
			}
			appendRefOwnershipLinesFromRefParser(&out, groupName, parserIndex, sourcesByGroup[groupName], group, rp)
		}
	}
	return out
}

func appendRefOwnershipLinesFromRefParser(out *[]types.RefOwnershipPredicateLine, groupName string, parserIndex int, sources []string, group types.HydraRefGroup, rp types.HydraRefParser) {
	combined, err := rp.MatchPredicate()
	matchPredicate := ""
	priority := refOwnershipParserPriority(group, rp)
	matchSource := &types.RefOwnershipRuleSource{
		Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
		GroupName: groupName,
		BlockPath: fmt.Sprintf("global.hydra.refs.%s.ref-parsers[%d]", groupName, parserIndex),
		Sources:   append([]string{}, sources...),
	}
	if err == nil {
		if p := strings.TrimSpace(string(combined)); p != "" {
			matchPredicate = p
			*out = append(*out, types.RefOwnershipPredicateLine{
				Cel:      p,
				Source:   matchSource,
				Priority: priority,
			})
		}
	}
	for pickIndex, pick := range rp.Pick {
		if c := strings.TrimSpace(pick.Cel); c != "" {
			*out = append(*out, types.RefOwnershipPredicateLine{
				Cel:      pickExpressionTargetPredicateWithScope(c, matchPredicate),
				Priority: priority,
				Source: &types.RefOwnershipRuleSource{
					Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
					GroupName: groupName,
					BlockPath: fmt.Sprintf("global.hydra.refs.%s.ref-parsers[%d].pick[%d]", groupName, parserIndex, pickIndex),
					Sources:   append([]string{}, sources...),
				},
			})
		}
	}
}

func appendRefOwnershipLinesFromDefaultRefParser(out *[]types.RefOwnershipPredicateLine, parserIndex int, sources []string, rp types.RefParser) {
	matchPredicate := ""
	matchSource := &types.RefOwnershipRuleSource{
		Kind:      types.RefOwnershipRuleSourceKindEmbeddedDefaultRefParser,
		GroupName: "embedded-default",
		Sources:   append([]string{}, sources...),
	}
	if p := strings.TrimSpace(string(rp.MatchPredicate())); p != "" {
		matchPredicate = p
		*out = append(*out, types.RefOwnershipPredicateLine{
			Cel: templateScopedOwnershipPredicate(p),
			Source: func() *types.RefOwnershipRuleSource {
				src := *matchSource
				src.BlockPath = fmt.Sprintf("embedded ref-parsers[%d]", parserIndex)
				return &src
			}(),
		})
	}
	for pickIndex, pick := range rp.Pick {
		if c := strings.TrimSpace(string(pick.Cel)); c != "" {
			*out = append(*out, types.RefOwnershipPredicateLine{
				Cel: templateScopedOwnershipPredicate(pickExpressionTargetPredicateWithScope(c, matchPredicate)),
				Source: &types.RefOwnershipRuleSource{
					Kind:      types.RefOwnershipRuleSourceKindEmbeddedDefaultRefParser,
					GroupName: "embedded-default",
					BlockPath: fmt.Sprintf("embedded ref-parsers[%d].pick[%d]", parserIndex, pickIndex),
					Sources:   append([]string{}, sources...),
				},
			})
		}
	}
}

func cloneRuleBlockPath(name string) string {
	if strings.TrimSpace(name) == "" {
		return "global.hydra.clones"
	}
	return fmt.Sprintf("global.hydra.clones.%s", name)
}

func optionalSourceList(source string) []string {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	return []string{source}
}

func refOwnershipLinesToCelStrings(lines []types.RefOwnershipPredicateLine) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln.Cel != "" {
			out = append(out, ln.Cel)
		}
	}
	return out
}

// cloneRuleSourceIdPattern matches a clone rule predicate of the form id == "group/ver/kind/ns/name" (or
// core 4-segment id) as produced by hand-written Hydra config.
var cloneRuleSourceIdPattern = regexp.MustCompile(`\bid\s*==\s*"([^"]+)"`)

// refOwnershipPredicatesFromCloneRulesPriorityBands returns clone-derived uninstall ownership
// predicates. All clone rules contribute to the non-negative-priority band so cluster mirrors are
// first-class uninstall ownership and are deleted when the declaring app is removed.
func refOwnershipPredicatesFromCloneRulesPriorityBands(hv *types.HydraValues) (nonNegativePriority, negativePriority []string) {
	if hv == nil || len(hv.Clones) == 0 {
		return nil, nil
	}
	ruleNames := make([]string, 0, len(hv.Clones))
	for n := range hv.Clones {
		ruleNames = append(ruleNames, n)
	}
	slices.Sort(ruleNames)
	seenStrong := sets.New[string]()
	for _, ruleName := range ruleNames {
		rule := hv.Clones[ruleName]
		if strings.TrimSpace(rule.Predicate) == "" || rule.Targets.IsEmpty() {
			continue
		}
		p, err := ownershipPredicateCelFromCloneRule(rule)
		if err != nil || p == "" {
			continue
		}
		if seenStrong.Has(p) {
			continue
		}
		seenStrong.Insert(p)
		nonNegativePriority = append(nonNegativePriority, p)
	}
	return nonNegativePriority, nil
}

// refOwnershipPredicatesFromCloneRules returns CEL predicates for uninstall ownership from
// global.hydra.clones: a namespaced object matches when it has the same GVK and name as the clone
// source, its namespace is not the source and not in the rule’s exclude list, and it is in
// managedNamespaces() (the same set clone targets are derived from).
func refOwnershipPredicatesFromCloneRules(hv *types.HydraValues) []string {
	s, w := refOwnershipPredicatesFromCloneRulesPriorityBands(hv)
	return append(s, w...)
}

// CloneOwnershipPredicatesFromHydraValues returns synthetic uninstall ownership CEL strings from
// global.hydra.clones for merged Hydra values (same strings as non-negative-priority clone ownership and as
// appended to HydraAppUninstallForcePredicateLines).
func CloneOwnershipPredicatesFromHydraValues(hv *types.HydraValues) []string {
	return refOwnershipPredicatesFromCloneRules(hv)
}

func ownershipPredicateCelFromCloneRule(rule types.HydraCloneRule) (string, error) {
	pred := strings.TrimSpace(rule.Predicate)
	if pred == "" {
		return "", nil
	}
	m := cloneRuleSourceIdPattern.FindStringSubmatch(pred)
	if m == nil {
		return "", nil
	}
	group, version, kind, sourceNs, resName, err := types.Id(m[1]).Components()
	if err != nil || sourceNs == "" {
		return "", nil
	}
	gvkStr := gvkStringForCel(group, version, kind)
	exc := sets.New[string]()
	exc.Insert(string(sourceNs))
	for _, e := range rule.Exclude {
		e = strings.TrimSpace(e)
		if e != "" {
			exc.Insert(e)
		}
	}
	excList := exc.UnsortedList()
	slices.Sort(excList)
	var b strings.Builder
	b.WriteString(`gvk == "`)
	b.WriteString(gvkStr)
	b.WriteString(`" && name == "`)
	b.WriteString(string(resName))
	b.WriteString(`"`)
	for _, ns := range excList {
		b.WriteString(` && string(ns) != "`)
		b.WriteString(ns)
		b.WriteString(`"`)
	}
	b.WriteString(` && size(managedNamespaces().filter(n, n == string(ns))) > 0`)
	return b.String(), nil
}

func gvkStringForCel(group types.Group, version types.Version, kind types.Kind) string {
	if group == "" {
		return string(version) + "/" + string(kind)
	}
	return string(group) + "/" + string(version) + "/" + string(kind)
}

func collectPredicatesByTag(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
	tag string,
) ([]string, error) {
	contexts, err := collectPredicateContextsByTag(cluster, appIds, networkMode, rendered, tag)
	if err != nil {
		return nil, err
	}
	return predicateStringsFromContexts(contexts), nil
}

func predicateStringsFromContexts(contexts []HydraPredicateContext) []string {
	result := make([]string, 0, len(contexts))
	for _, ctx := range contexts {
		if p := strings.TrimSpace(ctx.Predicate); p != "" {
			result = append(result, p)
		}
	}
	return result
}

func collectPredicateContextsByTag(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
	tag string,
) ([]HydraPredicateContext, error) {
	helmIds := helmChartBackedAppIds(appIds)
	if helmIds.Len() == 0 {
		return nil, nil
	}
	perApp, global, err := PartitionHydraConfigDocumentsByApp(rendered, types.KeyTemplateEntity, helmIds)
	if err != nil {
		return nil, err
	}
	appList := helmIds.UnsortedList()
	slices.SortFunc(appList, func(a, b types.AppId) int {
		return cmp.Compare(string(a), string(b))
	})
	var result []HydraPredicateContext
	for _, appId := range appList {
		h, err := cluster.WithApp(appId)
		if err != nil {
			return nil, err
		}
		hv, err := HydraValues(h, networkMode)
		if err != nil {
			return nil, err
		}
		helmMap, err := HelmHydraMapFromValues(hv)
		if err != nil {
			return nil, err
		}
		docs := HydraConfigMapDocumentsForApp(perApp, global, appIds, appId)
		merged := MergeHelmHydraWithConfigMapDocuments(helmMap, docs)
		refs := extractRefsFromMergedMap(merged)
		if refs == nil {
			continue
		}
		sourcesByGroup := predicateSourcesByGroup(h, helmMap, docs)
		groupNames := make([]string, 0, len(refs))
		for name := range refs {
			groupNames = append(groupNames, name)
		}
		slices.Sort(groupNames)
		for _, groupName := range groupNames {
			group := refs[groupName]
			if !group.IsEnabled() || !group.HasTag(tag) {
				continue
			}
			for parserIndex, rp := range group.RefParsers {
				pred, err := rp.MatchPredicate()
				if err != nil {
					return nil, fmt.Errorf("refs.%s.ref-parsers[%d]: %w", groupName, parserIndex, err)
				}
				if p := strings.TrimSpace(string(pred)); p != "" {
					result = append(result, HydraPredicateContext{
						AppId:       appId,
						Tag:         tag,
						GroupName:   groupName,
						ParserIndex: parserIndex,
						BlockPath:   fmt.Sprintf("global.hydra.refs.%s.ref-parsers[%d]", groupName, parserIndex),
						Predicate:   p,
						Sources:     slices.Clone(sourcesByGroup[groupName]),
					})
				}
				for pickIndex, pick := range rp.Pick {
					if p := strings.TrimSpace(pick.Cel); p != "" {
						result = append(result, HydraPredicateContext{
							AppId:       appId,
							Tag:         tag,
							GroupName:   groupName,
							ParserIndex: parserIndex,
							BlockPath:   fmt.Sprintf("global.hydra.refs.%s.ref-parsers[%d].pick[%d]", groupName, parserIndex, pickIndex),
							Predicate:   pickExpressionPredicate(p),
							Sources:     slices.Clone(sourcesByGroup[groupName]),
						})
					}
				}
			}
		}
	}
	if tag == "uninstall-safe" {
		defaultContexts, err := defaultPredicateContextsByTag(tag)
		if err != nil {
			return nil, err
		}
		result = append(result, defaultContexts...)
	}
	return result, nil
}

func defaultPredicateContextsByTag(tag string) ([]HydraPredicateContext, error) {
	parsers := references.DefaultRefParsers()
	result := make([]HydraPredicateContext, 0, len(parsers))
	for parserIndex, rp := range parsers {
		if !slices.Contains(rp.Tags, tag) {
			continue
		}
		pred := rp.MatchPredicate()
		if p := strings.TrimSpace(string(pred)); p != "" {
			result = append(result, HydraPredicateContext{
				Tag:         tag,
				GroupName:   "embedded-default",
				ParserIndex: parserIndex,
				BlockPath:   fmt.Sprintf("embedded ref-parsers[%d]", parserIndex),
				Predicate:   p,
				Sources:     []string{"embedded default ref-parsers"},
			})
		}
		for pickIndex, pick := range rp.Pick {
			if p := strings.TrimSpace(string(pick.Cel)); p != "" {
				result = append(result, HydraPredicateContext{
					Tag:         tag,
					GroupName:   "embedded-default",
					ParserIndex: parserIndex,
					BlockPath:   fmt.Sprintf("embedded ref-parsers[%d].pick[%d]", parserIndex, pickIndex),
					Predicate:   pickExpressionPredicate(p),
					Sources:     []string{"embedded default ref-parsers"},
				})
			}
		}
	}
	return result, nil
}

func pickExpressionPredicate(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("size((%s)) > 0", trimmed)
}

func pickExpressionTargetPredicate(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf(`size((%s).filter(ref, ref.hasEndpoint(string(id)))) > 0`, trimmed)
}

func pickExpressionTargetPredicateWithScope(expr string, matchPredicate string) string {
	predicate := pickExpressionTargetPredicate(expr)
	matchPredicate = strings.TrimSpace(matchPredicate)
	if predicate == "" || matchPredicate == "" {
		return predicate
	}
	return fmt.Sprintf("(%s) && (%s)", matchPredicate, predicate)
}

func templateScopedOwnershipPredicate(predicate string) string {
	predicate = strings.TrimSpace(predicate)
	if predicate == "" {
		return ""
	}
	return fmt.Sprintf(`templateEntity != null && (%s)`, predicate)
}

func predicateSourcesByGroup(h HydraApp, helmMap map[string]any, docs []HydraConfigMapDocument) map[string][]string {
	out := map[string][]string{}
	if helmRefs := extractRefsFromMergedMap(helmMap); helmRefs != nil {
		chartDir, err := ChartDirectoryForHydraApp(h)
		helmSource := "helm values"
		if err == nil {
			helmSource = filepath.Join(chartDir.Path(), "values.yaml")
		}
		for groupName := range helmRefs {
			out[groupName] = appendUniqueString(out[groupName], helmSource)
		}
	}
	for _, doc := range docs {
		docRefs := extractRefsFromMergedMap(doc.Hydra)
		if docRefs == nil {
			continue
		}
		source := fmt.Sprintf("ConfigMap %s data.hydra", doc.Id)
		for groupName := range docRefs {
			out[groupName] = appendUniqueString(out[groupName], source)
		}
	}
	return out
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// RefParsersFromHydraRefGroups converts enabled global.hydra-style ref groups to ref parsers.
func RefParsersFromHydraRefGroups(refs map[string]types.HydraRefGroup) ([]types.RefParser, error) {
	return refParsersFromHydraRefGroups(refs, "")
}

func refParsersFromHydraRefGroups(refs map[string]types.HydraRefGroup, parserApp types.AppId) ([]types.RefParser, error) {
	var result []types.RefParser
	for _, group := range refs {
		if !group.IsEnabled() {
			continue
		}
		for _, rp := range group.RefParsers {
			groupAttrs, err := types.RefAttributesFromParserAttributes(group.Attributes)
			if err != nil {
				return nil, err
			}
			attributes, err := hydraRefParserAttributes(rp, parserApp)
			if err != nil {
				return nil, err
			}
			attributes = types.MergeRefAttributes(groupAttrs, attributes)
			selector, cel, err := rp.SelectorAndCel()
			if err != nil {
				return nil, fmt.Errorf("refs: %w", err)
			}
			parser := types.RefParser{
				Selector:   selector,
				Cel:        cel,
				Tags:       mergeSortedStringSlices(group.Tag, rp.Tag),
				Desc:       group.Desc,
				Label:      firstNonEmptyString(rp.Label, group.Label),
				Attributes: attributes,
				Reverse:    group.Reverse || rp.Reverse,
			}
			for _, p := range rp.Pick {
				if strings.TrimSpace(p.Cel) == "" {
					return nil, fmt.Errorf("refs: empty pick.cel")
				}
				pickAttrs, err := types.RefAttributesFromParserAttributes(p.Attributes)
				if err != nil {
					return nil, err
				}
				parser.Pick = append(parser.Pick, types.RefPicker{
					Cel:        types.CelExpression(p.Cel),
					Tag:        p.Tag,
					Label:      p.Label,
					Attributes: pickAttrs,
					Reverse:    p.Reverse,
				})
			}
			result = append(result, parser)
		}
	}
	return result, nil
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func mergeSortedStringSlices(a, b []string) []string {
	if len(a) == 0 {
		return slices.Clone(b)
	}
	if len(b) == 0 {
		return slices.Clone(a)
	}
	merged := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		merged[s] = struct{}{}
	}
	for _, s := range b {
		merged[s] = struct{}{}
	}
	out := make([]string, 0, len(merged))
	for s := range merged {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// HydraAppRefParsers collects all ref-parsers from all enabled ref groups across the given apps:
// Helm global.hydra merged with every Hydra ConfigMap data.hydra fragment in the rendered catalog that
// applies to that app (cluster-wide carrier union + scope).
func HydraAppRefParsers(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]types.RefParser, error) {
	helmIds := helmChartBackedAppIds(appIds)
	if helmIds.Len() == 0 {
		return nil, nil
	}
	perApp, global, err := PartitionHydraConfigDocumentsByApp(rendered, types.KeyTemplateEntity, helmIds)
	if err != nil {
		return nil, err
	}
	var result []types.RefParser
	for appId := range helmIds {
		h, err := cluster.WithApp(appId)
		if err != nil {
			return nil, err
		}
		hv, err := HydraValues(h, networkMode)
		if err != nil {
			return nil, err
		}
		helmMap, err := HelmHydraMapFromValues(hv)
		if err != nil {
			return nil, err
		}
		docs := HydraConfigMapDocumentsForApp(perApp, global, appIds, appId)
		merged := MergeHelmHydraWithConfigMapDocuments(helmMap, docs)
		parsers, err := refParsersFromHydraRefGroups(extractRefsFromMergedMap(merged), appId)
		if err != nil {
			return nil, err
		}
		result = append(result, parsers...)
	}
	return result, nil
}

func hydraRefParserAttributes(rp types.HydraRefParser, parserApp types.AppId) ([]types.RefAttribute, error) {
	attributes, err := types.RefAttributesFromParserAttributes(rp.Attributes)
	if err != nil {
		return nil, err
	}
	if parserApp != "" && !hasRefAttributeType(attributes, types.RefAttributeOriginApp) {
		attributes = types.MergeRefAttributes(attributes, []types.RefAttribute{{
			Type:  types.RefAttributeOriginApp,
			Value: string(parserApp),
		}})
	}
	return attributes, nil
}

func hasRefAttributeType(attrs []types.RefAttribute, attrType string) bool {
	for _, attr := range attrs {
		if attr.Type == attrType {
			return true
		}
	}
	return false
}

func HydraAppRefGroups(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) (map[types.AppId]map[string]types.HydraRefGroup, error) {
	mergedMaps, err := hydraAppMergedValuesMap(cluster, appIds, networkMode, rendered)
	if err != nil {
		return nil, err
	}

	result := make(map[types.AppId]map[string]types.HydraRefGroup)
	for appId := range appIds {
		refs := extractRefsFromMergedMap(mergedMaps[appId])
		if len(refs) > 0 {
			result[appId] = refs
		}
	}
	return result, nil
}

func HydraAppUninstallFinalizers(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) ([]string, error) {
	m, err := HydraAppValues(cluster, appIds, networkMode, rendered)
	if err != nil {
		return nil, err
	}

	seen := sets.New[string]()
	var result []string
	for _, v := range m {
		if v == nil {
			continue
		}
		for _, f := range v.UninstallFinalizer {
			if !seen.Has(f) {
				seen.Insert(f)
				result = append(result, f)
			}
		}
	}

	return result, nil
}

// HydraAppValues returns merged effective Hydra configuration per app (Helm global.hydra plus
// data.hydra from rendered Hydra ConfigMaps). Values are unmarshalled without Validate().
func HydraAppValues(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) (map[types.AppId]*types.HydraValues, error) {
	mergedMaps, err := hydraAppMergedValuesMap(cluster, appIds, networkMode, rendered)
	if err != nil {
		return nil, err
	}
	result := map[types.AppId]*types.HydraValues{}
	for appId := range appIds {
		hv, err := hydraValuesFromMergedMapLoose(mergedMaps[appId])
		if err != nil {
			return nil, err
		}
		result[appId] = hv
	}
	return result, nil
}

func LoadValuesYaml(h Hydra, mode types.HelmNetworkMode) (types.YamlString, error) {
	valuesMap, err := h.LoadValuesMap(mode)
	if err != nil {
		return "", err
	}

	return yaml.ToYaml(valuesMap)
}

func rootAppHydraValuesMap(rootApp *RootApp, valuesMap types.ValuesMap) (types.ValuesMap, error) {
	topLevelHydra, err := yaml.LookupMap(valuesMap, "global", "hydra")
	if err != nil {
		return nil, err
	}
	if rootApp == nil {
		return topLevelHydra, nil
	}

	dependencyHydra, err := yaml.LookupMap(valuesMap, string(rootApp.RootAppName), "global", "hydra")
	if err != nil {
		return nil, err
	}
	switch {
	case dependencyHydra == nil:
		return topLevelHydra, nil
	case topLevelHydra == nil:
		return dependencyHydra, nil
	default:
		return values.MergeValues(dependencyHydra, topLevelHydra), nil
	}
}

func mergeRootAppHydraValuesIntoGlobal(valuesMap types.ValuesMap, rootApp *RootApp) (types.ValuesMap, error) {
	hydraMap, err := rootAppHydraValuesMap(rootApp, valuesMap)
	if err != nil {
		return nil, err
	}
	if hydraMap == nil {
		return valuesMap, nil
	}
	return values.MergeValues(map[string]any{
		"global": map[string]any{
			"hydra": hydraMap,
		},
	}, valuesMap), nil
}

// HydraValues retrieves the Hydra configuration values from a Hydra implementation.
func HydraValues(h Hydra, networkMode types.HelmNetworkMode) (*types.HydraValues, error) {
	if h.AsCluster() == nil {
		return nil, log.CreateError(errors.ErrHydraConfigError, "cluster config can not be defined on context level")
	}

	description := h.Description()
	valuesMap, err := h.LoadValuesMap(networkMode)
	if err != nil {
		return nil, log.CreateError(
			errors.ErrValuesFailed,
			"Failed to load hydra values from {description}",
			log.String("description", description),
			log.Err(err))
	}

	valuesMap, err = mergeRootAppHydraValuesIntoGlobal(valuesMap, h.AsRootApp())
	if err != nil {
		return nil, err
	}

	valuesYaml, err := yaml.ToYaml(valuesMap)
	if err != nil {
		return nil, err
	}

	hydraGlobal, err := yaml.FromYaml[types.HydraGlobal](valuesYaml)
	if err != nil {
		return nil, err
	}
	if hydraGlobal.Global == nil || hydraGlobal.Global.Hydra == nil {
		h.L().DebugLog(logIdHydra, "No global.hydra values found in {description}",
			log.String("description", description))
		return nil, nil
	}

	hydra := *hydraGlobal.Global.Hydra

	// Validate the hydra configuration
	if err := hydra.Validate(); err != nil {
		return nil, err
	}

	return &hydra, nil
}

func KubernetesVersionOrFallback(h Hydra, kubernetesVersion types.KubernetesVersion, networkMode types.HelmNetworkMode) (types.KubernetesVersionOrFallback, error) {
	if kubernetesVersion != "" {
		h.L().DebugLog(logIdHydra, "Using Kubernetes version '{version}' from flag", log.String("version", string(kubernetesVersion)))
		return types.KubernetesVersionOrFallback(kubernetesVersion), nil
	}

	hydraValues, err := HydraValues(h, networkMode)
	if err != nil {
		return "", err
	}
	if hydraValues == nil || hydraValues.KubernetesVersion == "" {
		h.L().DebugLog(logIdHydra, "No Kubernetes version found in hydra values, using version provided by helm as fallback")
	} else {
		h.L().DebugLog(logIdHydra, "Using Kubernetes version '{version}' from hydra values", log.String("version", string(hydraValues.KubernetesVersion)))
	}
	return types.KubernetesVersionOrFallback(hydraValues.KubernetesVersion), nil
}
