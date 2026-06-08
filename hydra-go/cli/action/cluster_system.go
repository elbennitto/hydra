package action

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/mattn/go-runewidth"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/k8s"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
	"k8s.io/apimachinery/pkg/util/sets"
)

// ClusterSystemFlags configures hydra gitops system.
type ClusterSystemFlags struct {
	flags.ClusterRESTClientFlags
	flags.ContextFlag
	flags.ColorFlag
	flags.ClusterFlag
	flags.HelmNetworkModeFlag
	flags.ExcludeAppFlag
	flags.NoCacheFlag
	flags.ReviewRefsYamlFlag
	flags.ClusterListParallelFlag
	// All includes presets with effectiveEnabled=false in the report (and evaluates their CEL lines for progress).
	All bool
}

func (f *ClusterSystemFlags) Flags() flags.Flags {
	return f
}

func (f *ClusterSystemFlags) WithColorFlag() *flags.ColorFlag {
	return &f.ColorFlag
}

func (f *ClusterSystemFlags) WithReviewRefsYamlFlag() *flags.ReviewRefsYamlFlag {
	return &f.ReviewRefsYamlFlag
}

func (f *ClusterSystemFlags) WithClusterListParallelFlag() *flags.ClusterListParallelFlag {
	return &f.ClusterListParallelFlag
}

var _ flags.WithColorFlag = (*ClusterSystemFlags)(nil)
var _ flags.WithReviewRefsYamlFlag = (*ClusterSystemFlags)(nil)
var _ flags.WithClusterListParallelFlag = (*ClusterSystemFlags)(nil)

// clusterSystemReport is the YAML document for `hydra gitops system` stdout.
type clusterSystemReport struct {
	Cluster         string                     `yaml:"cluster"`
	MatchCount      int                        `yaml:"matchCount"`
	MissingCount    int                        `yaml:"missingCount"`
	MissingIds      []string                   `yaml:"missingIds,omitempty"`
	UnexpectedCount int                        `yaml:"unexpectedCount,omitempty"`
	Presets         []clusterSystemPresetEntry `yaml:"presets"`
}

type clusterSystemPresetEntry struct {
	ID                    string `yaml:"id"`
	BuiltinDefaultEnabled bool   `yaml:"builtinDefaultEnabled"`
	EffectiveEnabled      bool   `yaml:"effectiveEnabled"`
	// Excluded is true when the preset is off but another enabled preset lists "!id" in activates.
	Excluded        bool                          `yaml:"excluded,omitempty"`
	MatchCount      int                           `yaml:"matchCount"`
	MissingCount    int                           `yaml:"missingCount"`
	MissingIds      []string                      `yaml:"missingIds,omitempty"`
	UnexpectedCount int                           `yaml:"unexpectedCount,omitempty"`
	Predicates      []clusterSystemPredicateEntry `yaml:"predicates"`
}

type clusterSystemPredicateEntry struct {
	Name            string                      `yaml:"name"`
	Enabled         bool                        `yaml:"enabled"`
	MatchCount      int                         `yaml:"matchCount"`
	MissingCount    int                         `yaml:"missingCount"`
	MissingIds      []string                    `yaml:"missingIds,omitempty"`
	UnexpectedCount int                         `yaml:"unexpectedCount,omitempty"`
	Ids             []clusterSystemIdEntry      `yaml:"ids,omitempty"`
	CelLines        []clusterSystemCelLineEntry `yaml:"celLines"`
}

type clusterSystemIdEntry struct {
	Id           string `yaml:"id"`
	MatchCount   int    `yaml:"matchCount"`
	MissingCount int    `yaml:"missingCount"`
	Optional     bool   `yaml:"optional,omitempty"`
	Unexpected   bool   `yaml:"unexpected,omitempty"`
}

type clusterSystemCelLineEntry struct {
	Index      int    `yaml:"index"`
	Expression string `yaml:"expression"`
	// Label is a short human-readable description for text output (replaces long CEL where possible).
	Label      string   `yaml:"label,omitempty"`
	Optional   bool     `yaml:"optional,omitempty"`
	MatchCount int      `yaml:"matchCount"`
	MatchIds   []string `yaml:"matchIds"`
	Unexpected bool     `yaml:"unexpected,omitempty"`
}

const systemFooterReport = "system · report"

func sortedStringList(s sets.Set[string]) []string {
	if s.Len() == 0 {
		return nil
	}
	out := s.UnsortedList()
	slices.SortFunc(out, func(a, b string) int { return cmp.Compare(a, b) })
	return out
}

func countUniqueUnexpectedInPreset(predicates []clusterSystemPredicateEntry) int {
	uniq := sets.New[string]()
	for _, pe := range predicates {
		for _, id := range pe.Ids {
			if id.Unexpected {
				uniq.Insert(id.Id)
			}
		}
		for _, cl := range pe.CelLines {
			if cl.Unexpected {
				for _, mid := range cl.MatchIds {
					uniq.Insert(mid)
				}
			}
		}
	}
	return uniq.Len()
}

// countClusterSystemCelLines returns how many CEL expressions are evaluated in buildClusterSystemReport
// (one MatchingEntityIdsForCEL per line).
func countClusterSystemCelLines(effectivePresets []hydra.ClusterDefaultsPresetEffective, includeDisabled bool) int {
	n := 0
	for _, eff := range effectivePresets {
		if !includeDisabled && !eff.Enabled {
			continue
		}
		for _, pe := range eff.Predicates {
			n += len(pe.CelLines)
		}
	}
	return n
}

func advanceClusterSystemPostListProgress(l log.Logger, p log.Progress, task log.ProgressTask, step, total int, detail string) {
	if p != nil {
		if task != nil {
			task.SetDetail(k8s.TruncateFooterDetail(detail))
		}
		p.Advance(step, total)
		return
	}
	l.DebugLog(logIdAction, "gitops system post-list {step}/{total}: {detail}",
		log.Int("step", step),
		log.Int("total", total),
		log.String("detail", detail))
}

// presetIDsExcludedByEnabledActivators returns preset ids listed as "!id" on any currently enabled preset.
func presetIDsExcludedByEnabledActivators(effs []hydra.ClusterDefaultsPresetEffective) sets.Set[string] {
	out := sets.New[string]()
	for _, e := range effs {
		if !e.Enabled {
			continue
		}
		for _, raw := range e.Activates {
			if !raw.Exclude {
				continue
			}
			out.Insert(raw.Preset)
		}
	}
	return out
}

func buildClusterSystemReport(
	clusterName string,
	effectivePresets []hydra.ClusterDefaultsPresetEffective,
	presetEnv cel.Env,
	clusterEntities entity.Entities,
	k8sMinor int,
	includeDisabled bool,
	afterEachCelLine func(presetID string, predicateName string, lineIndex int, expression string),
) (clusterSystemReport, error) {
	report := clusterSystemReport{
		Cluster: clusterName,
		Presets: make([]clusterSystemPresetEntry, 0, len(effectivePresets)),
	}
	reportMissingIds := sets.New[string]()
	var reportMatchSum, reportMissingSum, reportUnexpectedSum int
	excludedByActivators := presetIDsExcludedByEnabledActivators(effectivePresets)

	for _, eff := range effectivePresets {
		if !includeDisabled && !eff.Enabled {
			continue
		}
		presetID := eff.ID
		omittedCelExplicitIds := hydra.OmittedExplicitIdsForKubernetesMinor(eff, k8sMinor)
		names := sets.KeySet(eff.Predicates).UnsortedList()
		slices.SortFunc(names, func(a, b string) int { return cmp.Compare(a, b) })

		peOut := make([]clusterSystemPredicateEntry, 0, len(names))
		presetMissingIds := sets.New[string]()
		var presetMatchSum, presetMissingSum int

		for _, pname := range names {
			pe := eff.Predicates[pname]
			minorApplies := hydra.ClusterDefaultsPredicateMinorApplies(k8sMinor, pe)

			var idsOut []clusterSystemIdEntry
			var idMatchSum, idMissingSum int
			var predUnexpectedCount int
			predMissingIds := sets.New[string]()
			if len(pe.Ids) > 0 {
				idsOut = make([]clusterSystemIdEntry, 0, len(pe.Ids))
				for _, idl := range pe.Ids {
					if minorApplies {
						mc, miss := 0, 0
						if clusterEntities.IdSet.Has(types.Id(idl.Id)) {
							mc = 1
						} else {
							miss = 1
							if !idl.Optional {
								predMissingIds.Insert(idl.Id)
							}
						}
						idMatchSum += mc
						if !idl.Optional {
							idMissingSum += miss
						}
						idsOut = append(idsOut, clusterSystemIdEntry{Id: idl.Id, MatchCount: mc, MissingCount: miss, Optional: idl.Optional})
					} else if clusterEntities.IdSet.Has(types.Id(idl.Id)) {
						idsOut = append(idsOut, clusterSystemIdEntry{Id: idl.Id, Unexpected: true})
						predUnexpectedCount++
					}
				}
				slices.SortFunc(idsOut, func(a, b clusterSystemIdEntry) int { return cmp.Compare(a.Id, b.Id) })
			}
			linesOut := make([]clusterSystemCelLineEntry, 0, len(pe.CelLines))
			var celMatchSum int
			for i, line := range pe.CelLines {
				expr := line.Expr
				if m := celSingleIdEquals.FindStringSubmatch(expr); len(m) > 1 {
					tid := types.Id(m[1])
					if omittedCelExplicitIds.Has(tid) {
						if clusterEntities.IdSet.Has(tid) {
							linesOut = append(linesOut, clusterSystemCelLineEntry{
								Index:      i,
								Expression: expr,
								Label:      clusterSystemCelLineHumanLabel(pname, expr),
								Optional:   line.Optional,
								MatchCount: 1,
								MatchIds:   []string{string(tid)},
								Unexpected: true,
							})
							predUnexpectedCount++
						}
						if afterEachCelLine != nil {
							afterEachCelLine(presetID, pname, i, expr)
						}
						continue
					}
				}
				if strings.TrimSpace(expr) == "" {
					if line.Selector.IsZero() {
						linesOut = append(linesOut, clusterSystemCelLineEntry{
							Index:      i,
							Expression: expr,
							Label:      clusterSystemCelLineHumanLabel(pname, expr),
							Optional:   line.Optional,
							MatchCount: 0,
							MatchIds:   nil,
						})
						if afterEachCelLine != nil {
							afterEachCelLine(presetID, pname, i, expr)
						}
						continue
					}
					pred, err := presetEnv.CompileSelectedPredicateAt(
						fmt.Sprintf("hydra gitops system · preset %q · predicate %q · cel[%d] (selector-only)", presetID, pname, i),
						line.Selector,
					)
					if err != nil {
						return clusterSystemReport{}, err
					}
					_, matched, err := pred.Select(clusterEntities)
					if err != nil {
						return clusterSystemReport{}, err
					}
					var idStrs []string
					for _, ent := range matched.Items {
						id, err := ent.Id()
						if err != nil {
							return clusterSystemReport{}, err
						}
						idStrs = append(idStrs, string(id))
					}
					slices.Sort(idStrs)
					celMatchSum += len(idStrs)
					linesOut = append(linesOut, clusterSystemCelLineEntry{
						Index:      i,
						Expression: expr,
						Label:      clusterSystemCelLineHumanLabel(pname, expr),
						Optional:   line.Optional,
						MatchCount: len(idStrs),
						MatchIds:   idStrs,
					})
					if afterEachCelLine != nil {
						afterEachCelLine(presetID, pname, i, expr)
					}
					continue
				}
				ids, err := hydra.MatchingEntityIdsForCEL(presetEnv, clusterEntities, expr)
				if err != nil {
					return clusterSystemReport{}, err
				}
				celMatchSum += len(ids)
				idStrs := make([]string, len(ids))
				for j, id := range ids {
					idStrs[j] = string(id)
				}
				slices.Sort(idStrs)
				linesOut = append(linesOut, clusterSystemCelLineEntry{
					Index:      i,
					Expression: expr,
					Label:      clusterSystemCelLineHumanLabel(pname, expr),
					Optional:   line.Optional,
					MatchCount: len(ids),
					MatchIds:   idStrs,
				})
				if afterEachCelLine != nil {
					afterEachCelLine(presetID, pname, i, expr)
				}
			}
			predMatch := idMatchSum + celMatchSum
			predMissing := idMissingSum
			peOut = append(peOut, clusterSystemPredicateEntry{
				Name:            pname,
				Enabled:         pe.Enabled,
				MatchCount:      predMatch,
				MissingCount:    predMissing,
				MissingIds:      sortedStringList(predMissingIds),
				UnexpectedCount: predUnexpectedCount,
				Ids:             idsOut,
				CelLines:        linesOut,
			})
			presetMatchSum += predMatch
			presetMissingSum += predMissing
			presetMissingIds.Insert(predMissingIds.UnsortedList()...)
		}
		presetUnexpectedSum := countUniqueUnexpectedInPreset(peOut)
		excluded := !eff.Enabled && excludedByActivators.Has(eff.ID)
		report.Presets = append(report.Presets, clusterSystemPresetEntry{
			ID:                    eff.ID,
			BuiltinDefaultEnabled: eff.BuiltinFile.DefaultEnabled,
			EffectiveEnabled:      eff.Enabled,
			Excluded:              excluded,
			MatchCount:            presetMatchSum,
			MissingCount:          presetMissingSum,
			MissingIds:            sortedStringList(presetMissingIds),
			UnexpectedCount:       presetUnexpectedSum,
			Predicates:            peOut,
		})
		reportMatchSum += presetMatchSum
		reportMissingSum += presetMissingSum
		reportUnexpectedSum += presetUnexpectedSum
		reportMissingIds.Insert(presetMissingIds.UnsortedList()...)
	}
	report.MatchCount = reportMatchSum
	report.MissingCount = reportMissingSum
	report.MissingIds = sortedStringList(reportMissingIds)
	report.UnexpectedCount = reportUnexpectedSum
	return report, nil
}

const clusterSystemTextExprMax = 96

// celSingleIdEquals matches a single-line predicate of the form id == "…" (common for synthesized RBAC CEL).
var celSingleIdEquals = regexp.MustCompile(`^\s*id\s*==\s*"([^"]+)"\s*$`)

// celK8sNameEquals and celNameMatches extract readable fragments from common preset CEL patterns.
var (
	celK8sNameEquals = regexp.MustCompile(`\bname\s*==\s*"([^"]+)"`)
	celNameMatches   = regexp.MustCompile(`name\.matches\("\^([^"]+)"\)`)
)

func clusterSystemTextForEmptyCelMatch(predicateName, expr string) string {
	return clusterSystemCelLineHumanLabel(predicateName, expr)
}

// clusterSystemCelLineHumanLabel returns one line of text for cluster system: prefer explicit id, else a
// [predicate] shortName derived from the expression, else [predicate] only (full CEL stays in YAML expression).
func clusterSystemCelLineHumanLabel(predicateName, expr string) string {
	expr = strings.TrimSpace(expr)
	if m := celSingleIdEquals.FindStringSubmatch(expr); len(m) > 1 {
		return m[1]
	}
	if short := clusterSystemInferCelLineShortName(expr); short != "" {
		return fmt.Sprintf("[%s] %s", predicateName, short)
	}
	return fmt.Sprintf("[%s]", predicateName)
}

// clusterSystemInferCelLineShortName maps common CEL to a short label; empty means fall back to truncation.
func clusterSystemInferCelLineShortName(expr string) string {
	expr = strings.TrimSpace(expr)
	if strings.Contains(expr, `v1/Event"`) || strings.Contains(expr, `events.k8s.io/v1/Event"`) {
		if m := celNameMatches.FindStringSubmatch(expr); len(m) > 1 {
			pat := strings.TrimSuffix(m[1], ".*")
			if pat != "" {
				return "Event (" + pat + "…)"
			}
		}
		return "Event"
	}
	// Heuristic: kube-system name.matches without explicit gvk often described legacy satellite/Event-oriented lines; still useful for workload-controller anchor CEL.
	if !strings.Contains(expr, "gvk ==") && strings.Contains(expr, `ns == "kube-system"`) && strings.Contains(expr, "name.matches") &&
		!strings.Contains(expr, "PodDisruptionBudget") && !strings.Contains(expr, "PodMetrics") {
		if m := celNameMatches.FindStringSubmatch(expr); len(m) > 1 {
			pat := strings.TrimSuffix(m[1], ".*")
			if pat != "" {
				return "Event (" + pat + "…)"
			}
		}
		return "Event"
	}
	if strings.Contains(expr, "PodDisruptionBudget") {
		if m := celK8sNameEquals.FindStringSubmatch(expr); len(m) > 1 {
			return "PodDisruptionBudget (" + m[1] + ")"
		}
		return "PodDisruptionBudget"
	}
	if strings.Contains(expr, "PodMetrics") {
		if m := celNameMatches.FindStringSubmatch(expr); len(m) > 1 {
			pat := strings.TrimSuffix(m[1], ".*")
			if pat != "" {
				return "PodMetrics (" + pat + "…)"
			}
		}
		return "PodMetrics"
	}
	if strings.Contains(expr, `gvk == "v1/Secret"`) {
		if m := celK8sNameEquals.FindStringSubmatch(expr); len(m) > 1 {
			return "Secret (" + m[1] + ")"
		}
		return "Secret"
	}
	if strings.Contains(expr, "bootstrap-token") {
		return "bootstrap token Secret"
	}
	if strings.Contains(expr, "coordination.k8s.io/v1/Lease") {
		if m := celK8sNameEquals.FindStringSubmatch(expr); len(m) > 1 {
			return "Lease (" + m[1] + ")"
		}
		return "Lease"
	}
	if strings.Contains(expr, `gvk == "v1/ConfigMap"`) && strings.Contains(expr, "kube-root-ca") {
		return "kube-root-ca ConfigMaps"
	}
	if strings.Contains(expr, `gvk == "v1/Node"`) {
		return "Node list"
	}
	if strings.Contains(expr, `discovery.k8s.io/v1/EndpointSlice`) {
		if m := celK8sNameEquals.FindStringSubmatch(expr); len(m) > 1 {
			return "EndpointSlice (" + m[1] + ")"
		}
	}
	return ""
}

type clusterSystemTextRow struct {
	text   string
	status string
}

func clusterSystemPresetTitle(p clusterSystemPresetEntry) string {
	var idFound, idTotal int
	unexpectedIDs := sets.New[string]()
	for _, pr := range p.Predicates {
		for _, id := range pr.Ids {
			if id.Unexpected {
				unexpectedIDs.Insert(id.Id)
				continue
			}
			if id.MatchCount+id.MissingCount == 0 {
				continue
			}
			idTotal++
			if id.MatchCount == 1 {
				idFound++
			}
		}
		for _, cl := range pr.CelLines {
			if cl.Unexpected {
				for _, mid := range cl.MatchIds {
					unexpectedIDs.Insert(mid)
				}
			}
		}
	}
	idUnexpected := unexpectedIDs.Len()
	if idTotal > 0 {
		s := fmt.Sprintf("%s (%d/%d)", p.ID, idFound, idTotal)
		if idUnexpected > 0 {
			s += fmt.Sprintf(", %d unexpected", idUnexpected)
		}
		return s
	}
	if idUnexpected > 0 {
		return fmt.Sprintf("%s (%d unexpected)", p.ID, idUnexpected)
	}
	return fmt.Sprintf("%s (cel)", p.ID)
}

// clusterSystemPresetTitleSuffix prints merged effective on/off and builtin defaultEnabled for human output.
func clusterSystemPresetTitleSuffix(p clusterSystemPresetEntry) string {
	eff := "disabled"
	if p.EffectiveEnabled {
		eff = "enabled"
	}
	presetWord := "preset disabled"
	if p.BuiltinDefaultEnabled {
		presetWord = "preset enabled"
	}
	parts := []string{eff}
	if p.Excluded {
		parts = append(parts, "excluded")
	}
	parts = append(parts, presetWord)
	return "(" + strings.Join(parts, ", ") + ")"
}

func clusterSystemTruncateExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	if len(expr) <= clusterSystemTextExprMax {
		return expr
	}
	return expr[:clusterSystemTextExprMax-1] + "…"
}

func clusterSystemTextRowStatusRank(status string) int {
	switch status {
	case "found":
		return 5
	case "unexpected":
		return 4
	case "not found":
		return 3
	case "optional":
		return 2
	case "skipped":
		return 2
	default:
		return 0
	}
}

// dedupeAndSortClusterSystemTextRows merges rows that show the same resource id (or CEL synopsis) twice:
// explicit ids and synthesized rbac CEL lines can both emit the same line. When statuses differ, "found" wins, then "unexpected".
func dedupeAndSortClusterSystemTextRows(rows []clusterSystemTextRow) []clusterSystemTextRow {
	if len(rows) < 2 {
		return rows
	}
	best := make(map[string]clusterSystemTextRow, len(rows))
	order := make([]string, 0, len(rows))
	for _, r := range rows {
		prev, ok := best[r.text]
		if !ok {
			best[r.text] = r
			order = append(order, r.text)
			continue
		}
		if clusterSystemTextRowStatusRank(r.status) > clusterSystemTextRowStatusRank(prev.status) {
			best[r.text] = r
		}
	}
	out := make([]clusterSystemTextRow, 0, len(order))
	for _, key := range order {
		out = append(out, best[key])
	}
	slices.SortFunc(out, func(a, b clusterSystemTextRow) int {
		if c := cmp.Compare(a.text, b.text); c != 0 {
			return c
		}
		return cmp.Compare(a.status, b.status)
	})
	return out
}

func clusterSystemPresetRows(p clusterSystemPresetEntry) []clusterSystemTextRow {
	var rows []clusterSystemTextRow
	for _, pr := range p.Predicates {
		for _, id := range pr.Ids {
			var status string
			switch {
			case id.Unexpected:
				status = "unexpected"
			case id.MatchCount == 1:
				status = "found"
			case id.MissingCount == 1:
				if id.Optional {
					status = "optional"
				} else {
					status = "not found"
				}
			default:
				status = "skipped"
			}
			rows = append(rows, clusterSystemTextRow{text: id.Id, status: status})
		}
		for _, cl := range pr.CelLines {
			if len(cl.MatchIds) == 0 {
				text := cl.Label
				if text == "" {
					text = clusterSystemTextForEmptyCelMatch(pr.Name, cl.Expression)
				}
				st := "not found"
				switch {
				case cl.Unexpected:
					st = "unexpected"
				case cl.Optional:
					st = "optional"
				}
				rows = append(rows, clusterSystemTextRow{text: text, status: st})
				continue
			}
			for _, mid := range cl.MatchIds {
				st := "found"
				if cl.Unexpected {
					st = "unexpected"
				}
				rows = append(rows, clusterSystemTextRow{text: mid, status: st})
			}
		}
	}
	return dedupeAndSortClusterSystemTextRows(rows)
}

func clusterSystemTextColumnDisplayWidth(s string) int {
	return runewidth.StringWidth(s)
}

func clusterSystemTextMaxWidthForRows(rows []clusterSystemTextRow) int {
	maxW := 0
	for _, r := range rows {
		if n := clusterSystemTextColumnDisplayWidth(r.text); n > maxW {
			maxW = n
		}
	}
	return maxW
}

// clusterSystemTextMaxWidthForBlocks returns the widest resource / label column among all preset blocks
// so human text output aligns the status column across the whole report.
func clusterSystemTextMaxWidthForBlocks(blocks [][]clusterSystemTextRow) int {
	maxW := 0
	for _, rows := range blocks {
		if w := clusterSystemTextMaxWidthForRows(rows); w > maxW {
			maxW = w
		}
	}
	return maxW
}

const clusterSystemStatusColWidth = 11

func clusterSystemPadStatus(status string) string {
	if len(status) >= clusterSystemStatusColWidth {
		return status
	}
	return strings.Repeat(" ", clusterSystemStatusColWidth-len(status)) + status
}

func writeClusterSystemHumanText(w io.Writer, report clusterSystemReport, useColor types.Color) error {
	var blocks [][]clusterSystemTextRow
	titles := make([]string, 0, len(report.Presets))
	for _, p := range report.Presets {
		titles = append(titles, clusterSystemPresetTitle(p))
		blocks = append(blocks, clusterSystemPresetRows(p))
	}
	textColW := clusterSystemTextMaxWidthForBlocks(blocks)
	if textColW < 12 {
		textColW = 12
	}

	colorOn := bool(useColor)
	writeLine := func(s string) error {
		_, err := fmt.Fprintln(w, s)
		return err
	}

	if colorOn {
		if err := writeLine(colors.LightCyan.String() + "cluster " + report.Cluster + colors.Reset.String()); err != nil {
			return err
		}
	} else {
		if err := writeLine("cluster " + report.Cluster); err != nil {
			return err
		}
	}

	for bi, rows := range blocks {
		title := titles[bi]
		p := report.Presets[bi]
		suffix := clusterSystemPresetTitleSuffix(p)
		if colorOn {
			titleOut := colors.BoldLightMagenta() + title + colors.Reset.String()
			if !p.EffectiveEnabled {
				titleOut += " " + colors.Yellow.String() + suffix + colors.Reset.String()
			} else {
				titleOut += " " + suffix
			}
			if err := writeLine(titleOut); err != nil {
				return err
			}
		} else {
			line := title + " " + suffix
			if err := writeLine(line); err != nil {
				return err
			}
		}

		if len(rows) == 0 {
			msg := "  (no id or cel rows)"
			if colorOn {
				msg = colors.LightGray.String() + msg + colors.Reset.String()
			}
			if p.Excluded {
				if colorOn {
					msg += " " + colors.LightGray.String() + "(excluded)" + colors.Reset.String()
				} else {
					msg += " (excluded)"
				}
			}
			if err := writeLine(msg); err != nil {
				return err
			}
			continue
		}

		for _, r := range rows {
			padN := textColW - clusterSystemTextColumnDisplayWidth(r.text)
			if padN < 0 {
				padN = 0
			}
			pad := strings.Repeat(" ", padN)
			statusPlain := clusterSystemPadStatus(r.status)
			var textCol, st string
			excludedTag := ""
			if !p.EffectiveEnabled {
				if colorOn {
					g := colors.LightGray.String()
					textCol = g + r.text + colors.Reset.String()
					st = g + statusPlain + colors.Reset.String()
					if p.Excluded {
						excludedTag = " " + g + "(excluded)" + colors.Reset.String()
					}
				} else {
					textCol = r.text
					st = statusPlain
					if p.Excluded {
						excludedTag = " (excluded)"
					}
				}
			} else {
				textCol = r.text
				st = clusterSystemFormatStatus(statusPlain, r.status, colorOn)
			}
			line := "  " + textCol + pad + "  " + st + excludedTag
			if err := writeLine(line); err != nil {
				return err
			}
		}
	}
	return nil
}

func clusterSystemFormatStatus(paddedStatus, rawStatus string, colorOn bool) string {
	if !colorOn {
		return paddedStatus
	}
	switch rawStatus {
	case "found":
		return colors.LightGreen.String() + paddedStatus + colors.Reset.String()
	case "unexpected":
		return colors.LightMagenta.String() + paddedStatus + colors.Reset.String()
	case "not found":
		return colors.LightRed.String() + paddedStatus + colors.Reset.String()
	case "optional":
		return colors.LightGray.String() + paddedStatus + colors.Reset.String()
	case "skipped":
		return colors.LightYellow.String() + paddedStatus + colors.Reset.String()
	default:
		return colors.LightGray.String() + paddedStatus + colors.Reset.String()
	}
}

// ClusterSystem prints merged global.hydra.presets configuration and, for each builtin CEL line
// (cluster defaults: coredns/kubernetes/flannel), the live cluster entity ids that match (read-only diagnostic).
func ClusterSystem(f ClusterSystemFlags) (hydra.Hydra, string, error) {
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
		return nil, "", log.CreateError(errors.ErrNoAppsSpecified, "no apps left for gitops system after excludes")
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

	targetAppIds, err := cluster.AppIds(f.HelmNetworkMode)
	if err != nil {
		return nil, "", err
	}
	scopeInfo, err := commands.ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
	if err != nil {
		return nil, "", err
	}
	renderedAllApps, err := commands.RenderClusterSelectedApps(
		cluster, f.HelmNetworkMode, "", targetAppIds, types.KeyTemplateEntity,
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
	clusterEntities, err := commands.ListClusterAll(cluster, types.KeyClusterEntity, showProgress, f.Parallel)
	if err != nil {
		return nil, "", err
	}
	invModel, err := commands.BuildResourceModel(commands.ResourceModelInput{
		Cluster:          cluster,
		NetworkMode:      f.HelmNetworkMode,
		Bootstrap:        types.BootstrapNo,
		TemplateEntities: &renderedAllApps,
		ClusterEntities:  &clusterEntities,
		AppIds:           targetAppIds,
		ScopeInfo:        scopeInfo,
		Parallel:         f.Parallel,
	}, showProgress)
	if err != nil {
		return nil, "", err
	}
	renderedAllApps = invModel.TemplateEntities()

	k8sMinor := 99
	if sm, verr := commands.KubernetesServerMinorVersion(cluster); verr == nil {
		k8sMinor = sm
	}
	effectivePresets, err := hydra.EffectiveClusterDefaultsPresetsForCluster(cluster, targetAppIds, f.HelmNetworkMode, renderedAllApps, k8sMinor)
	if err != nil {
		return nil, "", err
	}
	presetEnv, err := cel.NewEnvWithEntityInventory(renderedAllApps)
	if err != nil {
		return nil, "", err
	}

	l.Info(logIdAction, "loading inventory for cluster defaults preset diagnostics")
	clusterEntities = invModel.ClusterEntities()

	includeDisabled := f.All
	celLineCount := countClusterSystemCelLines(effectivePresets, includeDisabled)
	postListSteps := 1 + celLineCount
	if postListSteps < 1 {
		postListSteps = 1
	}

	var postListBar log.Progress
	var postListTask log.ProgressTask
	if log.TerminalProgressUI() && postListSteps > 0 {
		var perr error
		postListBar, perr = l.NewProgress(systemFooterReport, postListSteps)
		if perr != nil {
			return nil, "", perr
		}
		if postListBar != nil {
			postListTask = postListBar.NewTask("")
		}
		defer func() {
			if postListBar != nil {
				_ = postListBar.Close()
			}
		}()
	}

	postListStep := 0
	advancePostList := func(detail string) {
		postListStep++
		advanceClusterSystemPostListProgress(l, postListBar, postListTask, postListStep, postListSteps, detail)
	}

	advancePostList("read kubernetes server minor version")

	report, err := buildClusterSystemReport(string(cluster.ClusterName), effectivePresets, presetEnv, clusterEntities, k8sMinor, includeDisabled,
		func(presetID, predicateName string, _ int, expression string) {
			detail := fmt.Sprintf("%s · %s · %s", presetID, predicateName, clusterSystemTruncateExpr(expression))
			advancePostList(detail)
		})
	if err != nil {
		return nil, "", err
	}

	log.FlushProgressForStdout()

	if f.Yaml {
		out, err := yq.ToYaml(f.Color, report)
		if err != nil {
			return nil, "", err
		}
		if _, err := fmt.Fprint(os.Stdout, out); err != nil {
			return nil, "", err
		}
	} else {
		if err := writeClusterSystemHumanText(os.Stdout, report, f.Color); err != nil {
			return nil, "", err
		}
	}
	return cluster, "", nil
}
