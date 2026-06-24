package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	htypes "hydra-gitops.org/hydra/hydra-go/core/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var appProjectGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "appprojects",
}

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

var deploymentGVR = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

var statefulSetGVR = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "statefulsets",
}

func syncWindowColor(kind string, manualSync bool) (colors.Color, string) {
	switch {
	case kind == "allow":
		return colors.Green, "auto"
	case kind == "deny" && manualSync:
		return colors.Yellow, "manual"
	default:
		return colors.Red, "prevent"
	}
}

// rootAppKey extracts "<cluster>.<rootApp>" from an appId like "<cluster>.<rootApp>.<childApp>"
func rootAppKey(appName string) string {
	parts := strings.SplitN(appName, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return appName
}

type syncWindowState struct {
	kind       string
	manualSync bool
}

func newDynamicClient(h hydra.Hydra) (dynamic.Interface, error) {
	restConfig, err := RestConfigForHydra(h)
	if err != nil {
		return nil, log.CreateError(errors.ErrSyncWindowFailed,
			"failed to create REST config: {err}", log.Err(err))
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, log.CreateError(errors.ErrSyncWindowFailed,
			"failed to create dynamic client: {err}", log.Err(err))
	}

	return dynamicClient, nil
}

func SyncStatus(h hydra.Hydra, color htypes.Color, appIds []string) error {
	return ArgocdStatus(h, color, appIds)
}

func ArgocdStatus(h hydra.Hydra, color htypes.Color, appIds []string) error {
	l := h.L()

	dynamicClient, err := newDynamicClient(h)
	if err != nil {
		return err
	}

	// Load AppProjects and build syncWindow lookup
	appProjects, err := dynamicClient.Resource(appProjectGVR).Namespace("argocd").List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"failed to list AppProjects (argoproj.io/v1alpha1) in namespace 'argocd' — are ArgoCD CRDs installed on this cluster? {err}",
			log.Err(err))
	}

	syncWindowMap := map[string]syncWindowState{}
	for _, proj := range appProjects.Items {
		name := proj.GetName()
		syncWindows, found, _ := unstructuredNestedSlice(proj.Object, "spec", "syncWindows")
		if !found || len(syncWindows) == 0 {
			continue
		}
		sw, ok := syncWindows[0].(map[string]any)
		if !ok {
			continue
		}
		kind, _ := sw["kind"].(string)
		manualSync, _ := sw["manualSync"].(bool)
		syncWindowMap[name] = syncWindowState{kind: kind, manualSync: manualSync}
	}

	// Load Applications
	apps, err := dynamicClient.Resource(applicationGVR).Namespace("argocd").List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"failed to list Applications (argoproj.io/v1alpha1) in namespace 'argocd': {err}",
			log.Err(err))
	}

	// Build filter set from resolved app IDs
	appIdSet := map[string]bool{}
	for _, id := range appIds {
		appIdSet[id] = true
	}

	grouped := map[string][]ArgocdAppStatusEntry{}
	matchCount := 0

	filteredApps := make([]unstructured.Unstructured, 0, len(apps.Items))
	for _, app := range apps.Items {
		name := app.GetName()
		if len(appIdSet) > 0 && !appIdSet[name] {
			continue
		}
		matchCount++
		filteredApps = append(filteredApps, app)
	}

	if len(appIds) > 0 {
		l.Info(logIdCommands, "found {projects} AppProjects, {apps} Applications, {matched} matching filter",
			log.Int("projects", len(appProjects.Items)),
			log.Int("apps", len(apps.Items)),
			log.Int("matched", matchCount))
	} else {
		l.Info(logIdCommands, "found {projects} AppProjects, {apps} Applications",
			log.Int("projects", len(appProjects.Items)),
			log.Int("apps", len(apps.Items)))
	}

	entries, err := BuildArgocdAppStatusEntries(filteredApps, syncWindowMap)
	if err != nil {
		return err
	}
	globalWidths := newArgocdStatusTableWidths(entries)
	for _, entry := range entries {
		key := rootAppKey(entry.Name)
		grouped[key] = append(grouped[key], entry)
	}

	// Sort group keys
	keys := make([]string, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	colorReset := ""
	colorGroup := ""
	colorApp := ""
	if color {
		colorReset = colors.Reset.String()
		colorGroup = colors.LightWhite.String()
		colorApp = colors.LightMagenta.String()
	}

	for _, key := range keys {
		entries := grouped[key]
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})

		fmt.Println()
		printArgocdStatusTable(key, entries, globalWidths, color, colorGroup, colorApp, colorReset)
	}

	// ArgoCD status
	fmt.Println()
	printWorkloadStatus(l, dynamicClient, color, "argocd-application-controller")
	printWorkloadStatus(l, dynamicClient, color, "argocd-server")
	printWorkloadStatus(l, dynamicClient, color, "argocd-repo-server")

	return nil
}

const argocdTableRowPrefix = "  "
const argocdTableColGap = "  "

// argocdOpNoOperationFallback is shown in the Operation column when Application.status.operationState is absent.
const argocdOpNoOperationFallback = "none"

const (
	argocdHdrState     = "State"
	argocdHdrMode      = "Mode"
	argocdHdrOperation = "Operation"
)

// operationCellText returns the part after "sync operation: " for table alignment.
func operationCellText(operationLine string) string {
	if operationLine == "" {
		return ""
	}
	const prefix = "sync operation: "
	if strings.HasPrefix(operationLine, prefix) {
		return strings.TrimSpace(operationLine[len(prefix):])
	}
	return operationLine
}

func argocdOperationPhaseColor(phase string) colors.Color {
	c := colors.Yellow
	switch phase {
	case "Succeeded":
		c = colors.Green
	case "Failed", "Error":
		c = colors.Red
	case "Running":
		c = colors.Yellow
	}
	return c
}

func padRightASCII(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// effectiveArgocdConditionBlockWidth clamps the condition line width used for colon breaks and wrapping.
func effectiveArgocdConditionBlockWidth(width int) int {
	const minW = 8
	if width < minW {
		return minW
	}
	return width
}

// splitConditionDisplayLines formats one condition line for display:
// 1. if a ": " exists at or after blockWidth, split there and keep the left side unchanged
// 2. recursively apply the same rule to the right side only
// 3. if no such ": " exists, wrap the remaining segment at word boundaries
func splitConditionDisplayLines(line string, blockWidth int) []string {
	r := []rune(line)
	if len(r) <= blockWidth {
		return []string{line}
	}
	idx := findFirstColonSpaceAfterRunes(r, blockWidth)
	if idx < 0 {
		return wrapWords(line, blockWidth)
	}
	left := string(r[:idx+1])
	right := strings.TrimSpace(string(r[idx+2:]))
	out := []string{left}
	if right == "" {
		return out
	}
	out = append(out, splitConditionDisplayLines(right, blockWidth)...)
	return out
}

// breakNewlineAfterColonSpacePastWidth replaces the first ": " (rune index >= blockWidth) with a line
// break after the colon; the left segment is kept as-is. The right segment is processed recursively
// the same way. If no eligible ": " exists, the remaining segment is wrapped at word boundaries.
// blockWidth matches the table row text width minus condition indent.
func breakNewlineAfterColonSpacePastWidth(s string, blockWidth int) string {
	if s == "" {
		return s
	}
	w := effectiveArgocdConditionBlockWidth(blockWidth)
	parts := strings.Split(s, "\n")
	var out []string
	for _, line := range parts {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, splitConditionDisplayLines(line, w)...)
	}
	return strings.Join(out, "\n")
}

func findFirstColonSpaceAfterRunes(r []rune, blockWidth int) int {
	if len(r) <= blockWidth {
		return -1
	}
	for j := blockWidth; j < len(r)-1; j++ {
		if r[j] == ':' && r[j+1] == ' ' {
			return j
		}
	}
	return -1
}

func breakOneLineColonAfterWidth(line string, blockWidth int) []string {
	r := []rune(line)
	if len(r) <= blockWidth {
		return []string{line}
	}
	idx := findFirstColonSpaceAfterRunes(r, blockWidth)
	if idx < 0 {
		return []string{line}
	}
	left := string(r[:idx+1])
	right := strings.TrimSpace(string(r[idx+2:]))
	out := []string{left}
	if right == "" {
		return out
	}
	out = append(out, breakOneLineColonAfterWidth(right, blockWidth)...)
	return out
}

// argocdStatusTableRowWidth is the character width of one full data row (prefix + name + sync + window + op columns).
func argocdStatusTableRowWidth(wName, wSync, wWindow, wOp int) int {
	return len(argocdTableRowPrefix) + wName + len(argocdTableColGap) + wSync + len(argocdTableColGap) + wWindow + len(argocdTableColGap) + wOp
}

type argocdStatusTableWidths struct {
	Name  int
	State int
	Mode  int
	Op    int
	Row   int
}

func newArgocdStatusTableWidths(entries []ArgocdAppStatusEntry) argocdStatusTableWidths {
	widths := argocdStatusTableWidths{
		State: len(argocdHdrState),
		Mode:  len(argocdHdrMode),
		Op:    len(argocdHdrOperation),
	}
	for _, e := range entries {
		syncPlain := formatSyncStatus(e.SyncStatus)
		modePlain := e.WindowStatus
		opPlain := operationCellText(e.OperationLine)
		if len(e.Name) > widths.Name {
			widths.Name = len(e.Name)
		}
		if len(syncPlain) > widths.State {
			widths.State = len(syncPlain)
		}
		if len(modePlain) > widths.Mode {
			widths.Mode = len(modePlain)
		}
		if len(opPlain) > widths.Op {
			widths.Op = len(opPlain)
		}
	}
	widths.Row = argocdStatusTableRowWidth(widths.Name, widths.State, widths.Mode, widths.Op)
	return widths
}

// wrapWords wraps text at word boundaries; maxWidth counts runes (display width for ASCII matches).
func wrapWords(text string, maxWidth int) []string {
	if maxWidth < 8 {
		maxWidth = 8
	}
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var b strings.Builder
	for _, w := range words {
		if utf8.RuneCountInString(w) > maxWidth {
			if b.Len() > 0 {
				lines = append(lines, b.String())
				b.Reset()
			}
			lines = append(lines, splitLongTokenRunes(w, maxWidth)...)
			continue
		}
		if b.Len() == 0 {
			b.WriteString(w)
			continue
		}
		candidate := b.String() + " " + w
		if utf8.RuneCountInString(candidate) <= maxWidth {
			b.WriteByte(' ')
			b.WriteString(w)
			continue
		}
		lines = append(lines, b.String())
		b.Reset()
		b.WriteString(w)
	}
	if b.Len() > 0 {
		lines = append(lines, b.String())
	}
	return lines
}

func splitLongTokenRunes(s string, maxWidth int) []string {
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return []string{s}
	}
	var out []string
	for len(runes) > maxWidth {
		out = append(out, string(runes[:maxWidth]))
		runes = runes[maxWidth:]
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

// printArgocdStatusTable prints one root-app group as aligned columns: application, State, Mode, Operation.
func printArgocdStatusTable(groupKey string, entries []ArgocdAppStatusEntry, widths argocdStatusTableWidths, color htypes.Color, colorGroup, colorApp, colorReset string) {
	wName := widths.Name
	wSync := widths.State
	wWindow := widths.Mode
	wOp := widths.Op

	rows := make([]struct {
		name, syncPlain, windowPlain, opPlain string
	}, len(entries))

	for i, e := range entries {
		syncPlain := formatSyncStatus(e.SyncStatus)
		winPlain := e.WindowStatus
		opPlain := operationCellText(e.OperationLine)
		rows[i].name = e.Name
		rows[i].syncPlain = syncPlain
		rows[i].windowPlain = winPlain
		rows[i].opPlain = opPlain
	}

	// Header row uses the group key in the name column.
	if color {
		fmt.Printf("%s%s%s%s%s\n",
			argocdTableRowPrefix,
			colorGroup, groupKey, colorReset,
			padRightASCII("", wName-len(groupKey))+argocdTableColGap+
				padRightASCII(argocdHdrState, wSync)+argocdTableColGap+
				padRightASCII(argocdHdrMode, wWindow)+argocdTableColGap+
				argocdHdrOperation)
	} else {
		fmt.Printf("%s%s\n",
			argocdTableRowPrefix,
			padRightASCII(groupKey, wName)+argocdTableColGap+
				padRightASCII(argocdHdrState, wSync)+argocdTableColGap+
				padRightASCII(argocdHdrMode, wWindow)+argocdTableColGap+
				argocdHdrOperation)
	}

	for i, e := range entries {
		r := rows[i]
		syncC := syncStatusColor(e.SyncStatus)
		winC := colors.Yellow
		switch e.WindowStatus {
		case "auto":
			winC = colors.Green
		case "prevent":
			winC = colors.Red
		}
		opC := argocdOperationPhaseColor(e.OperationPhase)

		opColPlain := r.opPlain
		if opColPlain == "" {
			opColPlain = argocdOpNoOperationFallback
		}

		if color {
			namePart := colorApp + r.name + colorReset
			syncPart := applyStatusColor(color, syncC, r.syncPlain)
			winPart := applyStatusColor(color, winC, r.windowPlain)
			var opPart string
			if r.opPlain != "" {
				opPart = opC.String() + r.opPlain + colors.Reset.String()
			} else {
				opPart = colors.LightGray.String() + argocdOpNoOperationFallback + colors.Reset.String()
			}
			fmt.Printf("%s%s%s%s%s%s%s%s%s%s%s\n",
				argocdTableRowPrefix,
				namePart,
				padRightASCII("", wName-len(r.name)),
				argocdTableColGap,
				syncPart,
				padRightASCII("", wSync-len(r.syncPlain)),
				argocdTableColGap,
				winPart,
				padRightASCII("", wWindow-len(r.windowPlain)),
				argocdTableColGap,
				opPart+padRightASCII("", wOp-len(opColPlain)),
			)
		} else {
			fmt.Printf("%s%s\n",
				argocdTableRowPrefix,
				padRightASCII(r.name, wName)+argocdTableColGap+
					padRightASCII(r.syncPlain, wSync)+argocdTableColGap+
					padRightASCII(r.windowPlain, wWindow)+argocdTableColGap+
					padRightASCII(opColPlain, wOp))
		}

		printArgocdAppConditions(color, e.Conditions, argocdTableRowPrefix, widths.Row)
	}
}

// printArgocdAppConditions prints Application.status.conditions from the Argo CD API (same source as the "App conditions" panel in the Argo CD UI).
func printArgocdAppConditions(color htypes.Color, conditions []ArgocdAppCondition, baseIndent string, tableRowWidth int) {
	if len(conditions) == 0 {
		return
	}
	n := len(conditions)
	summary := fmt.Sprintf("Argo CD reports %d application condition error (Application.status.conditions)", n)
	if n != 1 {
		summary = fmt.Sprintf("Argo CD reports %d application condition errors (Application.status.conditions)", n)
	}
	condColor := colors.Red
	colorReset := ""
	detailIndent := baseIndent + "    "
	wrapW := tableRowWidth - len(detailIndent)
	wrapW = effectiveArgocdConditionBlockWidth(wrapW)
	if color {
		colorReset = colors.Reset.String()
		fmt.Printf("%s%s%s%s\n", baseIndent, condColor.String(), summary, colorReset)
	} else {
		fmt.Printf("%s%s\n", baseIndent, summary)
	}
	for _, c := range conditions {
		line := formatArgocdAppConditionLine(c)
		line = breakNewlineAfterColonSpacePastWidth(line, wrapW)
		for _, segment := range strings.Split(line, "\n") {
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}
			if color {
				fmt.Printf("%s%s%s%s\n", detailIndent, condColor.String(), segment, colorReset)
			} else {
				fmt.Printf("%s%s\n", detailIndent, segment)
			}
		}
	}
}

func formatArgocdAppConditionLine(c ArgocdAppCondition) string {
	msg := strings.ReplaceAll(c.Message, "\n", " ")
	msg = strings.TrimSpace(msg)
	if c.Type != "" && msg != "" {
		return fmt.Sprintf("%s: %s", c.Type, msg)
	}
	if c.Type != "" {
		return c.Type
	}
	return msg
}

// printWorkloadStatus tries Deployment first, then StatefulSet for the given name.
func printWorkloadStatus(l log.Logger, client dynamic.Interface, color htypes.Color, name string) {
	dep, err := client.Resource(deploymentGVR).Namespace("argocd").Get(
		context.Background(), name, metav1.GetOptions{})
	if err == nil {
		printReplicaStatus(color, "Deployment", name, dep.Object)
		return
	}

	sts, err := client.Resource(statefulSetGVR).Namespace("argocd").Get(
		context.Background(), name, metav1.GetOptions{})
	if err == nil {
		printReplicaStatus(color, "StatefulSet", name, sts.Object)
		return
	}

	l.Warn(logIdCommands, "workload '{name}' not found as Deployment or StatefulSet in namespace 'argocd'",
		log.String("name", name))
}

func printReplicaStatus(color htypes.Color, kind string, name string, obj map[string]any) {
	replicas := int64(0)
	if spec, ok := obj["spec"].(map[string]any); ok {
		if r, ok := spec["replicas"].(int64); ok {
			replicas = r
		}
	}

	readyReplicas := int64(0)
	if status, ok := obj["status"].(map[string]any); ok {
		if r, ok := status["readyReplicas"].(int64); ok {
			readyReplicas = r
		}
	}

	statusColor := ""
	colorReset := ""
	if color {
		colorReset = colors.Reset.String()
		if readyReplicas > 0 {
			statusColor = colors.Green.String()
		} else {
			statusColor = colors.Red.String()
		}
	}

	fmt.Printf("%s/%s: %s%d/%d ready%s\n",
		kind, name, statusColor, readyReplicas, replicas, colorReset)
}

func SyncSet(
	h hydra.Hydra,
	appName string,
	kind string,
	manualSync bool,
	dryRun htypes.DryRun,
) error {
	l := h.L()

	dynamicClient, err := newDynamicClient(h)
	if err != nil {
		return err
	}

	// Look up the Application to find its AppProject via spec.project
	app, err := dynamicClient.Resource(applicationGVR).Namespace("argocd").Get(
		context.Background(), appName, metav1.GetOptions{})
	if err != nil {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"failed to get Application '{app}' in namespace 'argocd': {err}",
			log.String("app", appName), log.Err(err))
	}

	projectName, found, _ := unstructuredNestedString(app.Object, "spec", "project")
	if !found || projectName == "" {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"Application '{app}' has no spec.project field",
			log.String("app", appName))
	}

	l.DebugLog(logIdCommands, "Application '{app}' belongs to AppProject '{project}'",
		log.String("app", appName), log.String("project", projectName))

	appProjectClient := dynamicClient.Resource(appProjectGVR).Namespace("argocd")

	proj, err := appProjectClient.Get(context.Background(), projectName, metav1.GetOptions{})
	if err != nil {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"failed to get AppProject '{project}' for Application '{app}': {err}",
			log.String("project", projectName), log.String("app", appName), log.Err(err))
	}

	syncWindows, swFound, err := unstructuredNestedSlice(proj.Object, "spec", "syncWindows")
	if err != nil {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"failed to read syncWindows from AppProject '{project}': {err}",
			log.String("project", projectName), log.Err(err))
	}

	if !swFound || len(syncWindows) == 0 {
		return log.CreateWarn(errors.ErrNothingTodo,
			"AppProject '{project}' (for Application '{app}') has no syncWindows configured",
			log.String("project", projectName), log.String("app", appName))
	}

	for _, sw := range syncWindows {
		swMap, ok := sw.(map[string]any)
		if !ok {
			continue
		}
		swMap["kind"] = kind
		swMap["manualSync"] = manualSync
	}

	if err := setNestedSlice(proj.Object, syncWindows, "spec", "syncWindows"); err != nil {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"failed to update syncWindows in AppProject '{project}': {err}",
			log.String("project", projectName), log.Err(err))
	}

	_, statusText := syncWindowColor(kind, manualSync)
	l.Info(logIdCommands, "setting '{app}' (AppProject '{project}') to: {status}",
		log.String("app", appName),
		log.String("project", projectName),
		log.String("status", statusText))

	updateOpts := metav1.UpdateOptions{}
	if dryRun {
		updateOpts.DryRun = []string{metav1.DryRunAll}
		l.Warn(logIdCommands, "dry-run mode, no changes will be applied")
	}

	_, err = appProjectClient.Update(context.Background(), proj, updateOpts)
	if err != nil {
		return log.CreateError(errors.ErrSyncWindowFailed,
			"failed to update AppProject '{project}': {err}",
			log.String("project", projectName), log.Err(err))
	}

	return nil
}

func ResolveAppNames(h hydra.Hydra, patterns []string) ([]string, error) {
	l := h.L()

	hasWildcard := false
	for _, p := range patterns {
		if htypes.IsGlobPattern(p) {
			hasWildcard = true
			break
		}
	}
	if !hasWildcard {
		return patterns, nil
	}

	dynamicClient, err := newDynamicClient(h)
	if err != nil {
		return nil, err
	}

	appList, err := dynamicClient.Resource(applicationGVR).Namespace("argocd").List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, log.CreateError(errors.ErrSyncWindowFailed,
			"failed to list Applications in namespace 'argocd': {err}",
			log.Err(err))
	}

	allAppNames := make([]string, 0, len(appList.Items))
	for _, app := range appList.Items {
		allAppNames = append(allAppNames, app.GetName())
	}

	resolved, warnings, err := ResolvePatterns(patterns, allAppNames)
	if err != nil {
		return nil, err
	}

	for _, w := range warnings {
		l.Warn(logIdCommands, w)
	}

	for _, p := range patterns {
		if htypes.IsGlobPattern(p) {
			count := 0
			for _, name := range resolved {
				if htypes.MatchAppIdGlob(p, name) {
					count++
				}
			}
			l.Info(logIdCommands, "resolved '{pattern}' to {count} application(s)",
				log.String("pattern", p), log.Int("count", count))
		}
	}

	return resolved, nil
}

func ResolvePatterns(patterns []string, allAppNames []string) ([]string, []string, error) {
	resultSet := map[string]bool{}
	var warnings []string

	for _, pattern := range patterns {
		if !htypes.IsGlobPattern(pattern) {
			resultSet[pattern] = true
			continue
		}

		matched := 0
		var singleMatch string
		for _, name := range allAppNames {
			if htypes.MatchAppIdGlob(pattern, name) {
				resultSet[name] = true
				matched++
				singleMatch = name
			}
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
	sort.Strings(result)
	return result, warnings, nil
}

func PreventSyncWindows(l log.Logger, entities entity.Entities, key htypes.EntityKeyUnstructured) (entity.Entities, error) {
	out, _, err := PreventSyncWindowsWithMutationCount(l, entities, key)
	return out, err
}

// PreventSyncWindowsWithMutationCount sets ArgoCD AppProject syncWindows to full prevent sync (no automatic
// or manual sync) and returns how many AppProjects were actually modified (skipped or already-matching projects do not increment the count).
func PreventSyncWindowsWithMutationCount(l log.Logger, entities entity.Entities, key htypes.EntityKeyUnstructured) (entity.Entities, int, error) {
	return SetAppProjectSyncWindowsWithMutationCount(l, entities, key, "deny", false)
}

func unstructuredNestedSlice(obj map[string]any, fields ...string) ([]any, bool, error) {
	current := any(obj)
	for _, field := range fields {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		current, ok = m[field]
		if !ok {
			return nil, false, nil
		}
	}
	if s, ok := current.([]any); ok {
		return s, true, nil
	}
	return nil, false, nil
}

func unstructuredNestedString(obj map[string]any, fields ...string) (string, bool, error) {
	current := any(obj)
	for _, field := range fields {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false, nil
		}
		current, ok = m[field]
		if !ok {
			return "", false, nil
		}
	}
	if s, ok := current.(string); ok {
		return s, true, nil
	}
	return "", false, nil
}

func setNestedSlice(obj map[string]any, value []any, fields ...string) error {
	current := obj
	for i, field := range fields {
		if i == len(fields)-1 {
			current[field] = value
			return nil
		}
		next, ok := current[field].(map[string]any)
		if !ok {
			return fmt.Errorf("field %q is not a map", field)
		}
		current = next
	}
	return nil
}
