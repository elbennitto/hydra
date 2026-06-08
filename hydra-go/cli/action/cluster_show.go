package action

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
	"k8s.io/apimachinery/pkg/util/sets"
)

// ClusterShowFlags configures hydra gitops show.
type ClusterShowFlags struct {
	flags.ClusterRESTClientFlags
	flags.ContextFlag
	flags.ColorFlag
	flags.ClusterFlag
	flags.HelmNetworkModeFlag
	flags.ExcludeAppFlag
	flags.NoCacheFlag
	flags.ClusterListParallelFlag
	Builtin    bool
	YamlOutput bool
}

func (f *ClusterShowFlags) Flags() flags.Flags {
	return f
}

func (f *ClusterShowFlags) WithColorFlag() *flags.ColorFlag {
	return &f.ColorFlag
}

func (f *ClusterShowFlags) WithClusterListParallelFlag() *flags.ClusterListParallelFlag {
	return &f.ClusterListParallelFlag
}

var _ flags.WithColorFlag = (*ClusterShowFlags)(nil)
var _ flags.WithClusterListParallelFlag = (*ClusterShowFlags)(nil)

type clusterShowReport struct {
	Cluster    string                      `yaml:"cluster"`
	Apps       []clusterShowAppEntry       `yaml:"apps"`
	Ambiguous  []clusterShowAmbiguousEntry `yaml:"ambiguous,omitempty"`
	Unassigned []clusterShowResourceEntry  `yaml:"unassigned,omitempty"`
}

type clusterShowAppEntry struct {
	AppId     string                     `yaml:"appId"`
	Count     int                        `yaml:"count"`
	Resources []clusterShowResourceEntry `yaml:"resources"`
}

type clusterShowResourceEntry struct {
	Id      string                   `yaml:"id"`
	Reasons []clusterShowReasonEntry `yaml:"reasons,omitempty"`
}

type clusterShowAmbiguousEntry struct {
	Id         string                               `yaml:"id"`
	Candidates []clusterShowAmbiguousCandidateEntry `yaml:"candidates"`
}

type clusterShowAmbiguousCandidateEntry struct {
	AppId   string                   `yaml:"appId"`
	Reasons []clusterShowReasonEntry `yaml:"reasons,omitempty"`
}

type clusterShowReasonEntry struct {
	Kind         string                        `yaml:"kind"`
	Preset       string                        `yaml:"preset,omitempty"`
	PresetIDs    []string                      `yaml:"presetIds,omitempty"`
	PresetRules  []map[string]any              `yaml:"presetRules,omitempty"`
	OwnerRefs    []string                      `yaml:"ownerRefs,omitempty"`
	RefOwnership *clusterShowRefOwnershipEntry `yaml:"refOwnership,omitempty"`
}

type clusterShowRefOwnershipEntry struct {
	Via    *clusterShowRefOwnershipViaEntry    `yaml:"via,omitempty"`
	Tags   []string                            `yaml:"tags,omitempty"`
	Parser map[string]any                      `yaml:"parser,omitempty"`
	Pick   map[string]any                      `yaml:"pick,omitempty"`
	Rule   map[string]any                      `yaml:"rule,omitempty"`
	Cel    string                              `yaml:"cel,omitempty"`
	Source *clusterShowRefOwnershipSourceEntry `yaml:"source,omitempty"`
}

type clusterShowRefOwnershipViaEntry struct {
	EventRef      string   `yaml:"eventRef,omitempty"`
	EventSubjects []string `yaml:"eventSubjects,omitempty"`
}

type clusterShowRefOwnershipSourceEntry struct {
	Kind      string   `yaml:"kind,omitempty"`
	GroupName string   `yaml:"groupName,omitempty"`
	Path      string   `yaml:"path,omitempty"`
	Source    string   `yaml:"source,omitempty"`
	Sources   []string `yaml:"sources,omitempty"`
}

var refParsersIndexPattern = regexp.MustCompile(`ref-parsers\[\d+\]`)

type clusterShowRefOwnershipSourceCache struct {
	decodedByPath map[string]any
}

// ClusterShow prints the central live-cluster app assignment grouped by app id. Builtin preset
// pseudo apps are only included when requested via the CLI flag. Resources with zero or multiple
// app assignments are shown in dedicated sections and cause a non-zero exit.
func ClusterShow(f ClusterShowFlags) (hydra.Hydra, string, error) {
	l := log.Default()
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedYes)

	clusterName := f.Cluster
	appIds, err := commands.ResolveAppIdsInClusterWithExcludes(
		l, f.HydraContext, config, clusterName, f.ExcludeAppPatterns, f.HelmNetworkMode,
		f.ToRESTClientLimits())
	if err != nil {
		return nil, "", err
	}
	if len(appIds) == 0 {
		return nil, "", log.CreateError(errors.ErrNoAppsSpecified, "no apps left for gitops show after excludes")
	}

	cluster, err := commands.ResolveCommandCluster(commands.ResolveCommandClusterOptions{
		Config:       config,
		HydraContext: f.HydraContext,
		Limits:       f.ToRESTClientLimits(),
		ClusterName:  clusterName,
	})
	if err != nil {
		return nil, "", err
	}
	showProgress := log.TerminalProgressUI()

	renderAppIds, err := cluster.AppIds(f.HelmNetworkMode)
	if err != nil {
		return nil, "", err
	}
	scopeInfo, err := commands.ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
	if err != nil {
		return nil, "", err
	}
	renderedAllApps, err := commands.RenderClusterSelectedApps(
		cluster, f.HelmNetworkMode, "", renderAppIds, types.KeyTemplateEntity,
		commands.WithDefinitionsProgress(showProgress))
	if err != nil {
		return nil, "", err
	}
	renderedAllApps, err = commands.NormalizeApiVersions(cluster.L(), renderedAllApps, types.KeyTemplateEntity, cluster, func() (types.ScopeInfoMap, error) {
		return scopeInfo, nil
	})
	if err != nil {
		return nil, "", err
	}
	liveEntities, err := commands.ListClusterAll(cluster, types.KeyClusterEntity, showProgress, f.Parallel)
	if err != nil {
		return nil, "", err
	}
	perAppRendered, err := commands.PartitionTemplateEntitiesByPrimaryApp(renderedAllApps)
	if err != nil {
		return nil, "", err
	}
	for appId := range perAppRendered {
		if !appIds.Has(appId) {
			delete(perAppRendered, appId)
		}
	}
	var selectedRenderedItems []entity.Entity
	for _, ents := range perAppRendered {
		selectedRenderedItems = append(selectedRenderedItems, ents.Items...)
	}
	selectedRenderedAllApps, err := entity.NewEntities(selectedRenderedItems)
	if err != nil {
		return nil, "", err
	}

	model, err := commands.BuildResourceModel(commands.ResourceModelInput{
		Cluster:                cluster,
		NetworkMode:            f.HelmNetworkMode,
		Bootstrap:              types.BootstrapNo,
		TemplateEntities:       &selectedRenderedAllApps,
		ClusterEntities:        &liveEntities,
		PerAppTemplateEntities: perAppRendered,
		AppIds:                 appIds,
		PredicateAppIds:        appIds,
		ScopeInfo:              scopeInfo,
		Parallel:               f.Parallel,
	}, showProgress)
	if err != nil {
		return nil, "", err
	}
	metadata := model.AssignmentMetadata()

	displayAppIds := appIds
	if f.Builtin {
		displayAppIds = sets.New[types.AppId]()
		for _, row := range model.Rows() {
			if row.HasAssignedApp {
				displayAppIds.Insert(row.AssignedApp)
			}
		}
		for app := range appIds {
			displayAppIds.Insert(app)
		}
	}

	var auditErr error
	if len(metadata.AmbiguousAppIDsByClusterEntity) > 0 || metadata.UnassignedIDs.Len() > 0 {
		auditErr = log.CreateError(errors.ErrAborted,
			"cluster app assignment audit found {ambiguous} ambiguous and {unassigned} unassigned resource(s)",
			log.Int("ambiguous", len(metadata.AmbiguousAppIDsByClusterEntity)),
			log.Int("unassigned", metadata.UnassignedIDs.Len()))
		log.LogLazy(auditErr)
		auditErr = log.ReturnedErrorAlreadyEmitted(auditErr)
	}

	log.CloseActiveProgressBars()

	if err := writeClusterShowOutput(
		os.Stdout,
		string(clusterName),
		model,
		metadata,
		displayAppIds,
		f.Color,
		f.YamlOutput,
	); err != nil {
		return nil, "", err
	}

	if auditErr != nil {
		return cluster, "", auditErr
	}
	return cluster, "", nil
}

func writeClusterShowOutput(
	w io.Writer,
	cluster string,
	model *commands.ResourceModel,
	metadata commands.ClusterEntityAssignmentMetadata,
	appIds sets.Set[types.AppId],
	color types.Color,
	yamlOutput bool,
) error {
	assignment := model.Assignment()
	annotations := buildClusterShowAnnotations(model, metadata)
	report := buildClusterShowReport(cluster, assignment, metadata, appIds, annotations)
	if yamlOutput {
		return writeClusterShowYaml(w, color, report)
	}
	if len(report.Ambiguous) > 0 || len(report.Unassigned) > 0 {
		return writeClusterShowErrorYaml(w, color, report)
	}
	return writeClusterShowTable(w, color, report)
}

func writeClusterShowYaml(
	w io.Writer,
	color types.Color,
	report clusterShowReport,
) error {
	data, err := yq.ToYaml(color, report)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, data)
	return err
}

func writeClusterShowErrorYaml(
	w io.Writer,
	color types.Color,
	report clusterShowReport,
) error {
	errReport := clusterShowReport{
		Cluster:    report.Cluster,
		Ambiguous:  report.Ambiguous,
		Unassigned: report.Unassigned,
	}
	return writeClusterShowYaml(w, color, errReport)
}

func writeClusterShowTable(
	w io.Writer,
	color types.Color,
	report clusterShowReport,
) error {
	colorOn := bool(color)
	clusterLine := "cluster " + report.Cluster
	if colorOn {
		clusterLine = colors.LightCyan.String() + clusterLine + colors.Reset.String()
	}
	if _, err := fmt.Fprintln(w, clusterLine); err != nil {
		return err
	}

	appWidth := len("APP")
	countWidth := len("COUNT")
	for _, app := range report.Apps {
		if len(app.AppId) > appWidth {
			appWidth = len(app.AppId)
		}
		if n := len(strconv.Itoa(app.Count)); n > countWidth {
			countWidth = n
		}
	}

	headerApp := "APP"
	headerCount := "COUNT"
	headerResource := "RESOURCE"
	if colorOn {
		headerApp = colors.BoldLightMagenta() + headerApp + colors.Reset.String()
		headerCount = colors.BoldLightMagenta() + headerCount + colors.Reset.String()
		headerResource = colors.BoldLightMagenta() + headerResource + colors.Reset.String()
	}
	headerLine := headerApp + "     " +
		headerResource + strings.Repeat(" ", appWidth-len("RESOURCE")) + "  " + headerCount
	if _, err := fmt.Fprintln(w, headerLine); err != nil {
		return err
	}
	for _, app := range report.Apps {
		appCol := fmt.Sprintf("%-*s", appWidth, app.AppId)
		countCol := fmt.Sprintf("%*d", countWidth, app.Count)
		if colorOn {
			appCol = colors.LightBlue.String() + appCol + colors.Reset.String()
			countCol = colors.LightGreen.String() + countCol + colors.Reset.String()
		}
		if len(app.Resources) == 0 {
			if _, err := fmt.Fprintf(w, "%s       %s\n", appCol, countCol); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "%s       %s\n", appCol, countCol); err != nil {
			return err
		}
		for _, resource := range app.Resources {
			resourceCol := resource.Id
			if colorOn {
				resourceCol = colors.LightGray.String() + resourceCol + colors.Reset.String()
			}
			if _, err := fmt.Fprintf(w, "        %s\n", resourceCol); err != nil {
				return err
			}
		}
	}
	return nil
}

type clusterShowAnnotation struct {
	Reason commands.AssignmentReason
}

func buildClusterShowAnnotations(
	model *commands.ResourceModel,
	metadata commands.ClusterEntityAssignmentMetadata,
) map[types.Id][]clusterShowAnnotation {
	annotations := make(map[types.Id][]clusterShowAnnotation)
	if model == nil {
		return annotations
	}
	for _, row := range model.Rows() {
		if !row.HasCluster {
			continue
		}
		notes := clusterShowRecognitionNotes(row, metadata)
		if len(notes) > 0 {
			annotations[row.ID] = notes
		}
	}
	return annotations
}

func clusterShowRecognitionNotes(
	item commands.ResourceModelRow,
	metadata commands.ClusterEntityAssignmentMetadata,
) []clusterShowAnnotation {
	var notes []clusterShowAnnotation
	addReason := func(reason commands.AssignmentReason) {
		for _, existing := range notes {
			if clusterShowReasonText(existing.Reason) == clusterShowReasonText(reason) {
				return
			}
		}
		notes = append(notes, clusterShowAnnotation{Reason: reason})
	}
	for _, reason := range item.Reasons {
		addReason(reason)
	}
	if item.HasAssignedApp {
		hasAssignmentReason := len(item.Reasons) > 0
		if item.HasTemplate {
			if item.AssignedApp.IsPresetApp() {
				addReason(commands.AssignmentReason{Kind: commands.AssignmentReasonKindAssignedViaPresetTemplate})
			} else {
				addReason(commands.AssignmentReason{Kind: commands.AssignmentReasonKindAssignedViaTemplateID})
			}
			hasAssignmentReason = true
		}
		if !hasAssignmentReason {
			addReason(commands.AssignmentReason{Kind: commands.AssignmentReasonKindAssignedViaOwnerRef})
		}
	}
	if _, ok := metadata.AmbiguousAppIDsByClusterEntity[item.ID]; ok {
		addReason(commands.AssignmentReason{Kind: commands.AssignmentReasonKindAmbiguousAppAssignment})
	}
	if metadata.UnassignedIDs.Has(item.ID) && !item.HasAssignedApp {
		addReason(commands.AssignmentReason{Kind: commands.AssignmentReasonKindNoAppAssignment})
	}
	return notes
}

func clusterShowReasonText(reason commands.AssignmentReason) string {
	switch reason.Kind {
	case commands.AssignmentReasonKindMatchedByPresetID:
		return "matched-by-preset-id=" + strings.Join(reason.PresetIDs, ",") + clusterShowPresetRuleSuffix(reason.PresetRules)
	case commands.AssignmentReasonKindMatchedByPresetCEL:
		return "matched-by-preset-rule=" + strings.Join(reason.PresetIDs, ",") + clusterShowPresetRuleSuffix(reason.PresetRules)
	case commands.AssignmentReasonKindAssignedPreset:
		return "assigned-preset=" + reason.Preset
	case commands.AssignmentReasonKindAssignedViaBuiltinRef:
		return "assigned-via-builtin-ref"
	case commands.AssignmentReasonKindAssignedViaAppRef:
		return "assigned-via-app-ref"
	case commands.AssignmentReasonKindAssignedViaTemplateID:
		return "assigned-via-template-id"
	case commands.AssignmentReasonKindAssignedViaPresetTemplate:
		return "assigned-via-preset-template"
	case commands.AssignmentReasonKindAssignedViaPresetMatch:
		return "assigned-via-preset-match"
	case commands.AssignmentReasonKindAssignedViaClusterRootOverride:
		if reason.OverrideTarget == "" {
			return "assigned-via-cluster-root-override"
		}
		return "assigned-via-cluster-root-override=" + reason.OverrideTarget
	case commands.AssignmentReasonKindAssignedViaOwnerRef:
		if len(reason.OwnerRefs) == 0 {
			return "assigned-via-owner-ref"
		}
		ownerRefs := make([]string, 0, len(reason.OwnerRefs))
		for _, ownerRef := range reason.OwnerRefs {
			ownerRefs = append(ownerRefs, string(ownerRef))
		}
		return "assigned-via-owner-ref | ownerRefs=" + strings.Join(ownerRefs, ",")
	case commands.AssignmentReasonKindAssignedViaInspectRef:
		return "assigned-via-inspect-ref"
	case commands.AssignmentReasonKindAssignedViaRefOwnership:
		parts := []string{"assigned-via-ref-ownership"}
		if reason.EventRef != "" {
			parts = append(parts, "via="+reason.EventRef)
		}
		if len(reason.EventSubjects) > 0 {
			subjects := make([]string, 0, len(reason.EventSubjects))
			for _, subject := range reason.EventSubjects {
				subjects = append(subjects, string(subject))
			}
			parts = append(parts, "subjects="+strings.Join(subjects, ","))
		}
		if reason.RefOwnership == nil {
			return strings.Join(parts, " | ")
		}
		expr := strings.Join(strings.Fields(strings.TrimSpace(reason.RefOwnership.Cel)), " ")
		if src := reason.RefOwnership.Source; src != nil {
			if src.Kind != "" {
				parts = append(parts, "type="+string(src.Kind))
			}
			if src.BlockPath != "" {
				parts = append(parts, "rule="+src.BlockPath)
			}
			if len(src.Sources) > 0 {
				parts = append(parts, "source="+strings.Join(src.Sources, ", "))
			}
			if expr != "" {
				parts = append(parts, "cel="+expr)
			}
			return strings.Join(parts, " | ")
		}
		if expr == "" {
			return strings.Join(parts, " | ")
		}
		parts = append(parts, "cel="+expr)
		return strings.Join(parts, " | ")
	case commands.AssignmentReasonKindAmbiguousAppAssignment:
		return "ambiguous-app-assignment"
	case commands.AssignmentReasonKindNoAppAssignment:
		return "no-app-assignment"
	default:
		return string(reason.Kind)
	}
}

func clusterShowPresetRuleSuffix(rules []string) string {
	if len(rules) == 0 {
		return ""
	}
	return " rule=" + strings.Join(rules, " | ")
}

func buildClusterShowReport(
	cluster string,
	assignment map[types.Id]types.AppId,
	metadata commands.ClusterEntityAssignmentMetadata,
	appIds sets.Set[types.AppId],
	annotations map[types.Id][]clusterShowAnnotation,
) clusterShowReport {
	cache := &clusterShowRefOwnershipSourceCache{decodedByPath: map[string]any{}}
	grouped := map[types.AppId][]types.Id{}
	for id, appId := range assignment {
		grouped[appId] = append(grouped[appId], id)
	}

	report := clusterShowReport{Cluster: cluster}

	orderedApps := appIds.UnsortedList()
	slices.SortFunc(orderedApps, func(a, b types.AppId) int { return cmp.Compare(a, b) })
	for _, appId := range orderedApps {
		ids := grouped[appId]
		slices.SortFunc(ids, func(a, b types.Id) int { return cmp.Compare(a, b) })
		entry := clusterShowAppEntry{
			AppId: string(appId),
			Count: len(ids),
		}
		for _, id := range ids {
			entry.Resources = append(entry.Resources, clusterShowResourceEntry{
				Id:      string(id),
				Reasons: clusterShowAnnotationEntries(annotations[id], cache),
			})
		}
		report.Apps = append(report.Apps, entry)
	}

	if len(metadata.AmbiguousAppIDsByClusterEntity) > 0 {
		ambiguousIDs := make([]types.Id, 0, len(metadata.AmbiguousAppIDsByClusterEntity))
		for id := range metadata.AmbiguousAppIDsByClusterEntity {
			ambiguousIDs = append(ambiguousIDs, id)
		}
		slices.SortFunc(ambiguousIDs, func(a, b types.Id) int { return cmp.Compare(a, b) })
		for _, id := range ambiguousIDs {
			apps := append([]types.AppId{}, metadata.AmbiguousAppIDsByClusterEntity[id]...)
			slices.SortFunc(apps, func(a, b types.AppId) int { return cmp.Compare(a, b) })
			entry := clusterShowAmbiguousEntry{Id: string(id)}
			for _, app := range apps {
				reasons := metadata.AmbiguousAppReasonsByClusterEntity[id][app]
				if len(reasons) == 0 {
					reasons = []commands.AssignmentReason{{Kind: commands.AssignmentReasonKindAmbiguousAppAssignment}}
				}
				candidate := clusterShowAmbiguousCandidateEntry{AppId: string(app)}
				for _, reason := range reasons {
					candidate.Reasons = append(candidate.Reasons, clusterShowReasonEntryFromAssignmentReason(reason, cache))
				}
				entry.Candidates = append(entry.Candidates, candidate)
			}
			report.Ambiguous = append(report.Ambiguous, entry)
		}
	}
	if metadata.UnassignedIDs.Len() > 0 {
		ids := metadata.UnassignedIDs.UnsortedList()
		slices.SortFunc(ids, func(a, b types.Id) int { return cmp.Compare(a, b) })
		for _, id := range ids {
			report.Unassigned = append(report.Unassigned, clusterShowResourceEntry{
				Id:      string(id),
				Reasons: clusterShowAnnotationEntries(annotations[id], cache),
			})
		}
	}
	return report
}

func clusterShowAnnotationEntries(annotations []clusterShowAnnotation, cache *clusterShowRefOwnershipSourceCache) []clusterShowReasonEntry {
	if len(annotations) == 0 {
		return nil
	}
	out := make([]clusterShowReasonEntry, 0, len(annotations))
	for _, annotation := range annotations {
		out = append(out, clusterShowReasonEntryFromAssignmentReason(annotation.Reason, cache))
	}
	return out
}

func clusterShowReasonEntryFromAssignmentReason(reason commands.AssignmentReason, cache *clusterShowRefOwnershipSourceCache) clusterShowReasonEntry {
	kind := string(reason.Kind)
	if reason.Kind == commands.AssignmentReasonKindMatchedByPresetCEL {
		kind = "matched-by-preset-rule"
	}
	entry := clusterShowReasonEntry{
		Kind: kind,
	}
	if len(reason.PresetIDs) > 0 {
		entry.PresetIDs = append([]string{}, reason.PresetIDs...)
	}
	if len(reason.PresetRules) > 0 {
		entry.PresetRules = make([]map[string]any, 0, len(reason.PresetRules))
		for _, rule := range reason.PresetRules {
			parsed := map[string]any{}
			if err := yaml.Unmarshal([]byte(rule), &parsed); err == nil && len(parsed) > 0 {
				entry.PresetRules = append(entry.PresetRules, parsed)
				continue
			}
			entry.PresetRules = append(entry.PresetRules, map[string]any{"raw": rule})
		}
	}
	if reason.Preset != "" {
		entry.Preset = reason.Preset
	}
	if len(reason.OwnerRefs) > 0 {
		entry.OwnerRefs = make([]string, 0, len(reason.OwnerRefs))
		for _, ownerRef := range reason.OwnerRefs {
			entry.OwnerRefs = append(entry.OwnerRefs, string(ownerRef))
		}
	}
	if reason.RefOwnership != nil || reason.EventRef != "" || len(reason.EventSubjects) > 0 {
		ref := &clusterShowRefOwnershipEntry{}
		if reason.EventRef != "" || len(reason.EventSubjects) > 0 {
			ref.Via = &clusterShowRefOwnershipViaEntry{EventRef: reason.EventRef}
			if len(reason.EventSubjects) > 0 {
				ref.Via.EventSubjects = make([]string, 0, len(reason.EventSubjects))
				for _, subject := range reason.EventSubjects {
					ref.Via.EventSubjects = append(ref.Via.EventSubjects, string(subject))
				}
			}
		}
		if reason.RefOwnership != nil {
			if reason.RefOwnership.Source != nil {
				ref.Source = clusterShowRefOwnershipSourceEntryFromRuleSource(reason.RefOwnership.Source)
				clusterShowPopulateRefOwnershipDetails(ref, reason.RefOwnership, cache)
			}
			if len(ref.Parser) == 0 && len(ref.Pick) == 0 && len(ref.Rule) == 0 {
				ref.Cel = strings.Join(strings.Fields(strings.TrimSpace(reason.RefOwnership.Cel)), " ")
			}
		}
		if ref.Via != nil || len(ref.Tags) > 0 || len(ref.Parser) > 0 || len(ref.Pick) > 0 || len(ref.Rule) > 0 || ref.Cel != "" || ref.Source != nil {
			entry.RefOwnership = ref
		}
	}
	return entry
}

func clusterShowPopulateRefOwnershipDetails(
	ref *clusterShowRefOwnershipEntry,
	line *types.RefOwnershipPredicateLine,
	cache *clusterShowRefOwnershipSourceCache,
) {
	if ref == nil || line == nil || line.Source == nil || cache == nil {
		return
	}
	details, ok := cache.lookupRuleDetails(line.Source)
	if !ok {
		return
	}
	if len(details.tags) > 0 {
		ref.Tags = append([]string{}, details.tags...)
	}
	if len(details.parser) > 0 {
		ref.Parser = details.parser
	}
	if len(details.pick) > 0 {
		ref.Pick = details.pick
	}
	if len(details.rule) > 0 {
		ref.Rule = details.rule
	}
	if ref.Cel == "" && line.Cel != "" && len(ref.Parser) == 0 && len(ref.Pick) == 0 && len(ref.Rule) == 0 {
		ref.Cel = strings.Join(strings.Fields(strings.TrimSpace(line.Cel)), " ")
	}
}

func clusterShowRefOwnershipSourceEntryFromRuleSource(src *types.RefOwnershipRuleSource) *clusterShowRefOwnershipSourceEntry {
	if src == nil {
		return nil
	}
	entry := &clusterShowRefOwnershipSourceEntry{
		Kind:      string(src.Kind),
		GroupName: src.GroupName,
		Path:      clusterShowRefOwnershipSourcePath(src),
	}
	switch len(src.Sources) {
	case 0:
	case 1:
		entry.Source = src.Sources[0]
	default:
		entry.Sources = append([]string{}, src.Sources...)
	}
	return entry
}

type clusterShowRefOwnershipRuleDetails struct {
	tags   []string
	parser map[string]any
	pick   map[string]any
	rule   map[string]any
}

func (c *clusterShowRefOwnershipSourceCache) lookupRuleDetails(src *types.RefOwnershipRuleSource) (clusterShowRefOwnershipRuleDetails, bool) {
	if c == nil || src == nil || len(src.Sources) == 0 || src.BlockPath == "" {
		return clusterShowRefOwnershipRuleDetails{}, false
	}
	doc, ok := c.load(src.Sources[0])
	if !ok {
		return clusterShowRefOwnershipRuleDetails{}, false
	}
	switch src.Kind {
	case types.RefOwnershipRuleSourceKindHydraRefParser:
		return clusterShowHydraRefParserRuleDetails(doc, src.BlockPath)
	case types.RefOwnershipRuleSourceKindCloneRule:
		return clusterShowCloneRuleDetails(doc, src.BlockPath)
	default:
		return clusterShowRefOwnershipRuleDetails{}, false
	}
}

func (c *clusterShowRefOwnershipSourceCache) load(path string) (any, bool) {
	if c == nil || strings.TrimSpace(path) == "" {
		return nil, false
	}
	if doc, ok := c.decodedByPath[path]; ok {
		return doc, doc != nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		c.decodedByPath[path] = nil
		return nil, false
	}
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		c.decodedByPath[path] = nil
		return nil, false
	}
	c.decodedByPath[path] = doc
	return doc, true
}

func clusterShowHydraRefParserRuleDetails(doc any, blockPath string) (clusterShowRefOwnershipRuleDetails, bool) {
	parserPath, pickPath := clusterShowSplitRefParserPath(blockPath)
	parserNode, ok := clusterShowLookupPath(doc, parserPath)
	if !ok {
		return clusterShowRefOwnershipRuleDetails{}, false
	}
	groupPath := parserPath
	if idx := strings.Index(groupPath, ".ref-parsers["); idx >= 0 {
		groupPath = groupPath[:idx]
	}
	groupNode, _ := clusterShowLookupPath(doc, groupPath)
	details := clusterShowRefOwnershipRuleDetails{
		tags: clusterShowMergeStringLists(
			clusterShowLookupStringSlice(groupNode, "tag"),
			clusterShowLookupStringSlice(parserNode, "tag"),
		),
		parser: clusterShowNormalizeRuleMap(parserNode, true),
	}
	if pickPath != "" {
		pickNode, ok := clusterShowLookupPath(doc, pickPath)
		if !ok {
			return details, true
		}
		details.pick = clusterShowNormalizeRuleMap(pickNode, false)
		details.tags = clusterShowMergeStringLists(details.tags, clusterShowLookupStringSlice(pickNode, "tag"))
	}
	return details, true
}

func clusterShowCloneRuleDetails(doc any, blockPath string) (clusterShowRefOwnershipRuleDetails, bool) {
	ruleNode, ok := clusterShowLookupPath(doc, blockPath)
	if !ok {
		return clusterShowRefOwnershipRuleDetails{}, false
	}
	details := clusterShowRefOwnershipRuleDetails{
		rule: clusterShowNormalizeRuleMap(ruleNode, false),
		tags: clusterShowLookupStringSlice(ruleNode, "tag"),
	}
	return details, true
}

func clusterShowSplitRefParserPath(blockPath string) (parserPath string, pickPath string) {
	pickIdx := strings.Index(blockPath, ".pick[")
	if pickIdx < 0 {
		return blockPath, ""
	}
	return blockPath[:pickIdx], blockPath
}

func clusterShowLookupPath(doc any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return doc, true
	}
	current := doc
	for _, token := range strings.Split(path, ".") {
		name := token
		indexes := []int{}
		for {
			bracket := strings.Index(name, "[")
			if bracket < 0 {
				break
			}
			end := strings.Index(name[bracket:], "]")
			if end < 0 {
				return nil, false
			}
			rawIdx := name[bracket+1 : bracket+end]
			idx, err := strconv.Atoi(rawIdx)
			if err != nil {
				return nil, false
			}
			indexes = append(indexes, idx)
			name = name[:bracket] + name[bracket+end+1:]
		}
		if name != "" {
			m, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok = m[name]
			if !ok {
				return nil, false
			}
		}
		for _, idx := range indexes {
			items, ok := current.([]any)
			if !ok || idx < 0 || idx >= len(items) {
				return nil, false
			}
			current = items[idx]
		}
	}
	return current, true
}

func clusterShowNormalizeRuleMap(node any, omitPick bool) map[string]any {
	raw, ok := node.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		switch {
		case key == "predicate":
			out["cel"] = value
		case omitPick && key == "pick":
			continue
		default:
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clusterShowLookupStringSlice(node any, key string) []string {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := m[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clusterShowMergeStringLists(lists ...[]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, list := range lists {
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clusterShowRefOwnershipSourcePath(src *types.RefOwnershipRuleSource) string {
	path := strings.TrimSpace(src.BlockPath)
	if path == "" {
		return ""
	}
	switch src.Kind {
	case types.RefOwnershipRuleSourceKindHydraRefParser:
		if idx := strings.Index(path, "ref-parsers["); idx >= 0 {
			path = path[idx:]
		}
	case types.RefOwnershipRuleSourceKindEmbeddedDefaultRefParser:
		path = strings.TrimPrefix(path, "embedded ")
	}
	return refParsersIndexPattern.ReplaceAllString(path, "ref-parsers[]")
}
