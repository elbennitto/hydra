package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestRefRelationLabel(t *testing.T) {
	assert.Equal(t, "a, b", RefRelationLabel(types.Ref{Labels: []string{"a", "b"}}))
	assert.Equal(t, "d", RefRelationLabel(types.Ref{Desc: "d"}))
}

func TestEdgesForID(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	idC := types.Id("c")
	refs := []types.Ref{
		{From: idB, To: idA, Labels: []string{"in"}},
		{From: idA, To: idC, Labels: []string{"out"}},
	}
	incoming, outgoing := EdgesForID(refs, idA)
	require.Len(t, incoming, 1)
	require.Len(t, outgoing, 1)
	assert.Equal(t, idB, incoming[0].OtherID)
	assert.Equal(t, "in", incoming[0].Relation)
	assert.Equal(t, refs[0], incoming[0].Ref)
	assert.Equal(t, idC, outgoing[0].OtherID)
	assert.Equal(t, "out", outgoing[0].Relation)
	assert.Equal(t, refs[1], outgoing[0].Ref)
}

func TestEdgesForID_reverseOwnerRef(t *testing.T) {
	child := types.Id("apps/v1/ReplicaSet/ns/rs")
	parent := types.Id("apps/v1/Deployment/ns/deploy")
	refs := []types.Ref{
		{From: child, To: parent, Labels: []string{"controller"}, Reverse: true},
	}
	in, out := EdgesForID(refs, child)
	require.Len(t, in, 1)
	require.Empty(t, out)
	assert.Equal(t, parent, in[0].OtherID)
	assert.Equal(t, "controller", in[0].Relation)
	assert.Equal(t, refs[0], in[0].Ref)

	in2, out2 := EdgesForID(refs, parent)
	require.Empty(t, in2)
	require.Len(t, out2, 1)
	assert.Equal(t, child, out2[0].OtherID)
	assert.Equal(t, refs[0], out2[0].Ref)
}

func TestTransitiveEdgesForID_includesTransitiveNeighbors(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	idB := types.Id("v1/ConfigMap/ns/b")
	idC := types.Id("v1/Secret/ns/c")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"config"}},
		{From: idB, To: idC, Labels: []string{"secret"}},
	}

	in, out := TransitiveEdgesForID(refs, idA)
	require.Empty(t, in)
	require.Len(t, out, 2)
	assert.Equal(t, idB, out[0].OtherID)
	assert.Equal(t, 1, out[0].Distance)
	assert.Equal(t, idC, out[1].OtherID)
	assert.Equal(t, 2, out[1].Distance)
}

func TestRefTreeModel_escRoot_exitToPickerWhenAllowed(t *testing.T) {
	idA := types.Id("a")
	refs := []types.Ref{}
	m := NewRefTreeModel(refs, idA, true, false)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	rm := next.(*RefTreeModel)
	assert.True(t, rm.exitToPicker)
}

func TestRefTreeModel_escRoot_noExitToPickerWhenNotAllowed(t *testing.T) {
	idA := types.Id("a")
	refs := []types.Ref{}
	m := NewRefTreeModel(refs, idA, false, false)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	rm := next.(*RefTreeModel)
	assert.False(t, rm.exitToPicker)
}

func TestRefTreeCursorLineIndex_incomingThenOutgoing(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	idC := types.Id("c")
	refs := []types.Ref{
		{From: idB, To: idA, Labels: []string{"in"}},
		{From: idA, To: idC, Labels: []string{"out"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.cursor = 0
	assert.Equal(t, 2, m.cursorLineIndex())
	m.cursor = 1
	assert.Equal(t, 5, m.cursorLineIndex())
	m.cursor = 2
	assert.Equal(t, 6, m.cursorLineIndex())
}

func TestRefTreeView_stickyColumnHeaderWhenListScrolled(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	var refs []types.Ref
	for i := 0; i < 30; i++ {
		refs = append(refs, types.Ref{
			From:   types.Id(fmt.Sprintf("v1/ConfigMap/ns/cm-%d", i)),
			To:     idA,
			Labels: []string{"in"},
		})
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 80
	m.height = 18
	m.listScroll = 8
	out := stripANSIForRefTreeTest(m.View())
	lines := strings.Split(out, "\n")
	var innerFirst int
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "│") && strings.Contains(line, "Dist") && strings.Contains(line, "Ref id") {
			innerFirst = i
			break
		}
	}
	require.GreaterOrEqual(t, innerFirst, 0, "expected list panel line with Dist and Ref id headers")
	assert.Contains(t, lines[innerFirst], "Dist")
	assert.Contains(t, lines[innerFirst], "Ref id")
}

func TestRefTree_listScroll_windowSmallerThanFullList(t *testing.T) {
	idA := types.Id("a")
	var refs []types.Ref
	for i := 0; i < 25; i++ {
		refs = append(refs, types.Ref{
			From:   types.Id(fmt.Sprintf("v1/ConfigMap/ns/cm-%d", i)),
			To:     idA,
			Labels: []string{"in"},
		})
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 80
	m.height = 18
	contentW := 76
	all := m.refTreeStyledLines(contentW)
	max := m.listInnerMaxLines(contentW)
	require.Greater(t, len(all), max, "fixture should produce more list lines than fit in a short terminal")
	m.listScroll = 3
	m.clampListScrollBounds()
	assert.LessOrEqual(t, m.listScroll, len(all)-max)
}

func TestRefTreeSelectedRef_flatCursor(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	idC := types.Id("c")
	refs := []types.Ref{
		{From: idB, To: idA, Labels: []string{"in"}},
		{From: idA, To: idC, Labels: []string{"out"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	require.Len(t, m.incoming, 1)
	require.Len(t, m.outgoing, 2)

	m.cursor = 0
	require.NotNil(t, m.selectedRef())
	assert.Equal(t, "in", m.selectedRef().Labels[0])

	m.cursor = 1
	require.Nil(t, m.selectedRef())

	m.cursor = 2
	require.NotNil(t, m.selectedRef())
	assert.Equal(t, "out", m.selectedRef().Labels[0])
}

func TestRefTreeView_containsIncomingAndOutgoingSectionsWithoutStatusInLocalMode(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	refs := []types.Ref{
		{From: idB, To: idA, Labels: []string{"in"}},
		{From: idA, To: idB, Labels: []string{"out"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 80
	m.height = 40
	out := m.View()
	assert.Contains(t, out, "Incoming refs")
	assert.Contains(t, out, "Outgoing refs")
	assert.Contains(t, out, "Ref id")
	assert.NotContains(t, out, refStatusColumnHeader)
}

func TestRefTreeView_doesNotExceedTerminalWidth(t *testing.T) {
	idA := types.Id("kafka.strimzi.io/v1beta2/Kafka/demo/demo-kafka")
	refs := []types.Ref{
		{From: idA, To: types.Id("apiextensions.k8s.io/v1/CustomResourceDefinition//kafkas.kafka.strimzi.io"), Labels: []string{"crd"}},
		{From: idA, To: types.Id("apps/v1/Deployment/operator-kafka/strimzi-cluster-operator"), Desc: "Strimzi CRDs require the Strimzi operator to be running"},
		{From: idA, To: types.Id("v1/Namespace//demo"), Labels: []string{"namespace"}},
		{From: idA, To: types.Id("v1/Secret/demo/demo-kafka-cluster-ca-cert")},
	}

	m := NewRefTreeModel(refs, idA, false, true)
	m.width = 80
	m.height = 24

	out := stripANSIForRefTreeTest(m.View())
	for _, line := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), m.width, "line exceeds terminal width: %q", line)
	}
}

func TestRefTreeView_doesNotExceedTerminalHeight(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/very-long-namespace-name/very-long-deployment-name")
	idB := types.Id("apps/v1/StatefulSet/very-long-namespace-name/very-long-statefulset-name")
	refs := []types.Ref{
		{
			From: idA,
			To:   idB,
			Desc: "This is a deliberately long relation description that should wrap in the detail panel without making the overall view higher than the terminal window.",
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
				{Type: types.RefAttributeOriginSource, Value: types.RefSourceCluster},
				{Type: types.RefAttributeOriginOwner, Value: types.RefOwnerRoleDependent},
			},
		},
	}

	m := NewRefTreeModel(refs, idA, false, true)
	m.width = 80
	m.height = 18

	out := stripANSIForRefTreeTest(m.View())
	assert.LessOrEqual(t, lipgloss.Height(out), m.height, "view exceeds terminal height:\n%s", out)
}

func TestRefTreeView_localDetailSelectionDoesNotExceedTerminalHeight(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/very-long-namespace-name/very-long-deployment-name")
	idB := types.Id("apps/v1/StatefulSet/very-long-namespace-name/very-long-statefulset-name")
	refs := []types.Ref{
		{
			From: idA,
			To:   idB,
			Desc: "This is a deliberately long relation description that should be clipped to the available detail height in local tree mode.",
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
				{Type: types.RefAttributeOriginOwner, Value: types.RefOwnerRoleDependent},
			},
		},
	}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 80
	m.height = 12
	m.cursor = 1

	out := stripANSIForRefTreeTest(m.View())
	assert.LessOrEqual(t, lipgloss.Height(out), m.height, "view exceeds terminal height:\n%s", out)
}

func TestRefTreeStyledLines_selectedRowsFitPanelWidth(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/extremely-long-namespace-name/very-long-deployment-name")
	idB := types.Id("apiextensions.k8s.io/v1/CustomResourceDefinition//very-long-custom-resource-definition-name.example.com")
	refs := []types.Ref{
		{
			From:   idA,
			To:     idB,
			Labels: []string{"very-long-relation-label-that-previously-caused-the-selected-row-to-wrap"},
		},
	}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 60
	m.height = 12
	m.cursor = 1

	contentW := m.panelInnerWidth()
	for _, line := range m.refTreeStyledLines(contentW) {
		assert.LessOrEqual(t, lipgloss.Width(stripANSIForRefTreeTest(line)), contentW, "list line exceeds panel width: %q", line)
	}
}

func TestRefTreeModel_usesTransitiveReachabilityInList(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	idB := types.Id("v1/ConfigMap/ns/b")
	idC := types.Id("v1/Secret/ns/c")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"config"}},
		{From: idB, To: idC, Labels: []string{"secret"}},
	}

	m := NewRefTreeModel(refs, idA, false, false)
	require.Len(t, m.incoming, 0)
	require.Len(t, m.outgoing, 3)
	assert.True(t, m.outgoing[0].IsSelf)
	assert.Equal(t, 0, m.outgoing[0].Distance)
	assert.Equal(t, 1, m.outgoing[1].Distance)
	assert.Equal(t, 2, m.outgoing[2].Distance)
}

func TestRefTreeModel_transitiveIncomingUsesNegativeDistances(t *testing.T) {
	child := types.Id("apps/v1/ReplicaSet/ns/child")
	parent := types.Id("apps/v1/Deployment/ns/parent")
	root := types.Id("apps/v1/StatefulSet/ns/root")
	refs := []types.Ref{
		{From: child, To: parent, Reverse: true, Labels: []string{"controller"}},
		{From: parent, To: root, Reverse: true, Labels: []string{"owner"}},
	}

	m := NewRefTreeModel(refs, child, false, false)
	require.Len(t, m.incoming, 2)
	require.Len(t, m.outgoing, 1)
	assert.True(t, m.outgoing[0].IsSelf)
	assert.Equal(t, parent, m.incoming[0].OtherID)
	assert.Equal(t, -1, m.incoming[0].Distance)
	assert.Equal(t, root, m.incoming[1].OtherID)
	assert.Equal(t, -2, m.incoming[1].Distance)
}

func TestRefTreeModel_slashOpensFilterPopupWithQueryFocus(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	m := NewRefTreeModel([]types.Ref{}, idA, false, false)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	rm := next.(*RefTreeModel)
	assert.True(t, rm.filterOpen)
	assert.Equal(t, filterPopupFocusQuery, rm.filterFocus)
}

func TestRefTreeModel_filterPopupApplyByKindReducesVisibleRows(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	idB := types.Id("v1/ConfigMap/ns/cm")
	idC := types.Id("v1/Secret/ns/secret")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"config"}},
		{From: idA, To: idC, Labels: []string{"secret"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	rm := next.(*RefTreeModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyTab})
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyDown})
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyDown})
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyDown})
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Secret")})
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rm = next.(*RefTreeModel)

	require.Len(t, rm.incoming, 0)
	require.Len(t, rm.outgoing, 2)
	assert.True(t, rm.outgoing[0].IsSelf)
	assert.Equal(t, idC, rm.outgoing[1].OtherID)
	assert.False(t, rm.filterOpen)
}

func TestRefTreeModel_sortCycleChangesOutgoingOrderByRelation(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	idB := types.Id("v1/Secret/ns/secret-a")
	idC := types.Id("v1/ConfigMap/ns/config-a")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"zeta"}},
		{From: idA, To: idC, Labels: []string{"alpha"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	require.Len(t, m.outgoing, 3)
	assert.Equal(t, idC, m.outgoing[1].OtherID, "default dist sort tiebreak by id keeps ConfigMap before Secret")

	pressS := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	next, _ := m.Update(pressS)
	rm := next.(*RefTreeModel)
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)

	assert.Equal(t, refTreeSortRelation, rm.sortField)
	assert.False(t, rm.sortDescending, "relation ascending after 4 presses (dist↓ id↑ id↓ relation)")
	require.Len(t, rm.outgoing, 3)
	assert.Equal(t, idC, rm.outgoing[1].OtherID, "relation sort should place alpha before zeta")
}

func TestRefTreeHelpPlain_mentionsFilterAndSortHotkeys(t *testing.T) {
	m := NewRefTreeModel([]types.Ref{}, types.Id("apps/v1/Deployment/ns/a"), true, false)
	help := m.refTreeHelpPlain()
	assert.Contains(t, help, "/ filter")
	assert.Contains(t, help, "s/S sort")
}

func TestRefTreeModel_shiftSortReversesSortCycle(t *testing.T) {
	m := NewRefTreeModel([]types.Ref{}, types.Id("apps/v1/Deployment/ns/a"), false, false)
	assert.False(t, m.sortDescending)
	assert.Equal(t, refTreeSortDist, m.sortField)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(*RefTreeModel)
	assert.True(t, m.sortDescending, "s from dist ascending should toggle to descending")

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = next.(*RefTreeModel)
	assert.False(t, m.sortDescending, "S from dist descending should return to ascending on same column")
	assert.Equal(t, refTreeSortDist, m.sortField)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = next.(*RefTreeModel)
	assert.True(t, m.sortDescending, "S from dist ascending should go to previous column descending")
	assert.Equal(t, refTreeSortStatus, m.sortField)
}

// stripANSIForRefTreeTest removes minimal CSI sequences so assertions can see plain text.
func stripANSIForRefTreeTest(s string) string {
	var b strings.Builder
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if esc {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				esc = false
			}
			continue
		}
		if c == '\x1b' {
			esc = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// refTreeExtractDetailPlain returns plain text of the detail panel from a full RefTree View().
// It relies on the "Selected relation" title and the graph footer ("Esc back" / picker variant).
func refTreeExtractDetailPlain(view string) string {
	p := stripANSIForRefTreeTest(view)
	const start = "Selected relation"
	i := strings.Index(p, start)
	if i < 0 {
		return ""
	}
	rest := p[i:]
	cut := len(rest)
	for _, sep := range []string{"↑/↓ move", "Esc back"} {
		if j := strings.Index(rest, sep); j >= 0 && j < cut {
			cut = j
		}
	}
	return strings.TrimSpace(rest[:cut])
}

// refTreeDetailScrollDownKeyMsg / refTreeDetailScrollUpKeyMsg are the key messages the shared tree TUI
// uses for detail-only scrolling (list paging stays on PgUp/PgDn). Bindings must stay in sync with
// refTreeHelpPlain when detail overflows; see hydra/docs/manual/cli/shared/hydra-tree.md.
func refTreeDetailScrollDownKeyMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}
}

func refTreeDetailScrollUpKeyMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}}
}

func refTreeDetailLogicalLineCount(ref *types.Ref, contentW int) int {
	if ref == nil {
		return 0
	}
	plain := strings.TrimSpace(stripANSIForRefTreeTest(renderRefDetail(ref, contentW, 500)))
	if plain == "" {
		return 0
	}
	return strings.Count(plain, "\n") + 1
}

func refTreeHeavyRefBetween(from, to types.Id, lastAttrValue string) types.Ref {
	const n = 18
	attrs := make([]types.RefAttribute, 0, n)
	for i := 0; i < n; i++ {
		val := fmt.Sprintf("v-%02d", i)
		if i == n-1 {
			val = lastAttrValue
		}
		attrs = append(attrs, types.RefAttribute{
			Type:  fmt.Sprintf("tui:test:attr:%02d", i),
			Value: val,
		})
	}
	return types.Ref{
		From:       from,
		To:         to,
		Labels:     []string{"heavy-test"},
		Attributes: attrs,
	}
}

// refTreeHeavyRefForDetailTest is an incoming edge to the anchor (appears under Incoming refs).
func refTreeHeavyRefForDetailTest(lastAttrValue string) types.Ref {
	idA := types.Id("apps/v1/Deployment/ns/web")
	idB := types.Id("v1/ConfigMap/ns/cm-heavy")
	return refTreeHeavyRefBetween(idB, idA, lastAttrValue)
}

// refTreeHeavyOutgoingRefForDetailTest is an outgoing edge from the anchor (appears under Outgoing after the self row).
func refTreeHeavyOutgoingRefForDetailTest(lastAttrValue string) types.Ref {
	idA := types.Id("apps/v1/Deployment/ns/web")
	idB := types.Id("v1/ConfigMap/ns/cm-heavy")
	return refTreeHeavyRefBetween(idA, idB, lastAttrValue)
}

// assertRefTreeListHeaderColumnOrder enforces hydra-ui–style column order in the scrollable list header.
func assertRefTreeListHeaderColumnOrder(t *testing.T, plainHeader string) {
	t.Helper()
	idxDist := strings.Index(plainHeader, "Dist")
	idxRef := strings.Index(plainHeader, "Ref id")
	idxRel := strings.Index(plainHeader, "Relation")
	require.GreaterOrEqual(t, idxDist, 0, "header must label the Dist column for transitive tree")
	require.GreaterOrEqual(t, idxRef, 0, "header must label the ref id column")
	require.GreaterOrEqual(t, idxRel, 0, "header must label the relation column")
	assert.Less(t, idxDist, idxRef, "Dist must appear before Ref id")
	assert.Less(t, idxRef, idxRel, "Ref id must appear before Relation")
	if idxSt := strings.Index(plainHeader, refStatusColumnHeader); idxSt >= 0 {
		assert.Less(t, idxRel, idxSt, "Relation must appear before Status when Status is shown")
	}
}

type refTreeTransitiveRowParsed struct {
	dist, refID, relation, status string
}

func refTreeParseTransitiveListRowSpec(row string, expectStatus bool) (refTreeTransitiveRowParsed, bool) {
	s := strings.TrimSpace(row)
	if strings.HasPrefix(s, "▸") {
		s = strings.TrimSpace(s[len("▸"):])
	}
	var re *regexp.Regexp
	if expectStatus {
		re = regexp.MustCompile(`^([+-]?\d+)\s+(\S+)\s+(.+?)\s{2,}(\S.*)$`)
	} else {
		re = regexp.MustCompile(`^([+-]?\d+)\s+(\S+)\s+(.+?)$`)
	}
	m := re.FindStringSubmatch(s)
	if m == nil {
		return refTreeTransitiveRowParsed{}, false
	}
	status := ""
	if expectStatus {
		status = strings.TrimSpace(m[4])
	}
	return refTreeTransitiveRowParsed{
		dist:     strings.TrimSpace(m[1]),
		refID:    strings.TrimSpace(m[2]),
		relation: strings.TrimSpace(m[3]),
		status:   status,
	}, true
}

func refTreeDistCellValidForTransitive(cell string) bool {
	cell = strings.TrimSpace(cell)
	if !regexp.MustCompile(`^[+-]?\d+$`).MatchString(cell) {
		return false
	}
	s := cell
	if len(s) > 0 && s[0] == '+' {
		s = s[1:]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	if n < -10 || n > 10 {
		return false
	}
	return true
}

func refTreeTruncateForTest(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func refTreeTruncatedIDEqualsEntity(refCell string, entity types.Id, idW int) bool {
	refCell = strings.TrimSpace(refCell)
	full := string(entity)
	if refCell == full {
		return true
	}
	if strings.HasPrefix(full, refCell) {
		return true
	}
	return refCell == refTreeTruncateForTest(full, idW)
}

func refTreeColumnWidthsForTest(maxW int, showStatus bool) (distW, idW, relW, statusW int) {
	layoutW := maxW
	statusW = 0
	if showStatus {
		statusW = refStatusColumnWidth
	}
	distW = refDistanceColumnWidth
	layoutW = maxW - statusW - distW - 2
	if layoutW < 24 {
		layoutW = maxW - distW - 1
		statusW = 0
	}
	idW = max(16, layoutW*6/10)
	relW = layoutW - idW - 3
	if relW < 8 {
		relW = 8
	}
	return distW, idW, relW, statusW
}

// refTreeListShowsCurrentEntityAtDistZero: a data row in the scrollable list shows the focused id in the
// Ref id column with Dist 0 (same row shape as transitive neighbors).
func refTreeListShowsCurrentEntityAtDistZero(m *RefTreeModel, contentW int) bool {
	lines := m.refTreeStyledLines(contentW)
	if len(lines) == 0 {
		return false
	}
	hdr := stripANSIForRefTreeTest(lines[0])
	if !strings.Contains(hdr, "Dist") {
		return false
	}
	_, idW, _, _ := refTreeColumnWidthsForTest(contentW, m.showStatus)
	for _, ln := range lines[1:] {
		pl := stripANSIForRefTreeTest(ln)
		t := strings.TrimSpace(pl)
		if t == "" || strings.HasPrefix(t, "Incoming refs") || strings.HasPrefix(t, "Outgoing refs") {
			continue
		}
		if strings.Contains(pl, "(none)") {
			continue
		}
		parsed, ok := refTreeParseTransitiveListRowSpec(pl, m.showStatus)
		if !ok {
			continue
		}
		if !refTreeTruncatedIDEqualsEntity(parsed.refID, m.current, idW) {
			continue
		}
		d := strings.TrimSpace(parsed.dist)
		if d == "0" || d == "+0" || d == "-0" {
			return true
		}
	}
	return false
}

func refTreeEntityListRowIsSelfAtDistZero(entity types.Id, got refTreeTransitiveRowParsed) bool {
	d := strings.TrimSpace(got.dist)
	if d != "0" && d != "+0" && d != "-0" {
		return false
	}
	return refTreeTruncatedIDEqualsEntity(got.refID, entity, 80)
}

func TestRefTreeView_containsDistColumn(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	refs := []types.Ref{
		{From: idB, To: idA, Labels: []string{"in"}},
		{From: idA, To: idB, Labels: []string{"out"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 40
	contentW := m.width - 4
	lines := m.refTreeStyledLines(contentW)
	require.NotEmpty(t, lines)
	hdr := stripANSIForRefTreeTest(lines[0])
	assert.Contains(t, hdr, "Dist", "list header should include a Dist column like hydra-ui Relations")
	assertRefTreeListHeaderColumnOrder(t, hdr)
}

func TestRefTreeView_currentEntityDistZero_listContract(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/web")
	idB := types.Id("v1/Service/ns/web")
	refs := []types.Ref{{From: idA, To: idB, Labels: []string{"svc"}}}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 120
	m.height = 40
	contentW := m.width - 4

	listOK := refTreeListShowsCurrentEntityAtDistZero(m, contentW)
	assert.True(t, listOK, "current entity must show Dist 0 as a list row in outgoing refs")
}

// TestRefTree_spec_distColumnCell_signedRange documents allowed Dist cell values for transitive rows
// (hydra-ui convention: max 10 levels → |value| ≤ 10).
func TestRefTree_spec_distColumnCell_signedRange(t *testing.T) {
	tests := []struct {
		cell string
		ok   bool
	}{
		{"0", true},
		{"+0", true},
		{"-0", true},
		{"+1", true},
		{"-1", true},
		{"+2", true},
		{"-9", true},
		{"10", true},
		{"+10", true},
		{"-10", true},
		{"11", false},
		{"-11", false},
		{"+100", false},
		{"", false},
		{"x", false},
	}
	for _, tt := range tests {
		t.Run(tt.cell, func(t *testing.T) {
			assert.Equal(t, tt.ok, refTreeDistCellValidForTransitive(tt.cell), "cell %q", tt.cell)
		})
	}
}

// TestRefTree_spec_transitiveRowMultiHop_examples parses representative future list rows: each row
// must expose Dist as the first column, followed by id, relation, and optional status.
func TestRefTree_spec_transitiveRowMultiHop_examples(t *testing.T) {
	tests := []struct {
		name    string
		row     string
		wantID  string
		wantRel string
		wantDst string
		wantSt  string
	}{
		{
			name:    "one hop outgoing",
			row:     "  +1  v1/ConfigMap/ns/cm-a    mounts        ok",
			wantID:  "v1/ConfigMap/ns/cm-a",
			wantRel: "mounts",
			wantDst: "+1",
			wantSt:  "ok",
		},
		{
			name:    "two hops outgoing",
			row:     "  +2  v1/Secret/ns/db         env, optional  template only",
			wantID:  "v1/Secret/ns/db",
			wantRel: "env, optional",
			wantDst: "+2",
			wantSt:  "template only",
		},
		{
			name:    "ten hops cap",
			row:     "  +10 chain/ns/node-10        hop            ok",
			wantID:  "chain/ns/node-10",
			wantRel: "hop",
			wantDst: "+10",
			wantSt:  "ok",
		},
		{
			name:    "incoming negative",
			row:     "  -2  apps/v1/Deployment/ns/x  owner         cluster only",
			wantID:  "apps/v1/Deployment/ns/x",
			wantRel: "owner",
			wantDst: "-2",
			wantSt:  "cluster only",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := refTreeParseTransitiveListRowSpec(tt.row, true)
			require.True(t, ok, "row %q", tt.row)
			assert.Equal(t, tt.wantID, got.refID)
			assert.Equal(t, tt.wantRel, got.relation)
			assert.Equal(t, tt.wantDst, got.dist)
			assert.Equal(t, tt.wantSt, got.status)
			assert.True(t, refTreeDistCellValidForTransitive(got.dist))
		})
	}
}

// TestRefTree_spec_transitiveRow_rejectsPlus11 ensures list Dist cells stay within the 10-hop cap.
func TestRefTree_spec_transitiveRow_rejectsPlus11(t *testing.T) {
	row := "  +11 v1/Pod/ns/x              direct        ok"
	got, ok := refTreeParseTransitiveListRowSpec(row, true)
	require.True(t, ok)
	assert.False(t, refTreeDistCellValidForTransitive(got.dist), "+11 must not be a valid displayed distance")
}

// TestRefTree_spec_entityListRowAtDistZero matches the list-row shape when the focused entity is
// duplicated in the scrollable list with Dist 0 (mirrors hydra-ui treating the anchor at distance 0).
func TestRefTree_spec_entityListRowAtDistZero(t *testing.T) {
	row := "  0 apps/v1/Deployment/ns/web  (current)"
	got, ok := refTreeParseTransitiveListRowSpec(row, false)
	require.True(t, ok)
	assert.Equal(t, "apps/v1/Deployment/ns/web", got.refID)
	assert.Equal(t, "0", got.dist)
	assert.True(t, refTreeEntityListRowIsSelfAtDistZero(types.Id("apps/v1/Deployment/ns/web"), got))
}

func TestRefTree_refTreeStyledLines_whenWide_showsStatusAndExpectsColumnOrderInClusterMode(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	refs := []types.Ref{
		{
			From: idB, To: idA, Labels: []string{"in"},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
				{Type: types.RefAttributeOriginSource, Value: types.RefSourceCluster},
			},
		},
	}
	m := NewRefTreeModel(refs, idA, false, true)
	m.width = 120
	m.height = 40
	contentW := m.width - 4
	lines := m.refTreeStyledLines(contentW)
	hdr := stripANSIForRefTreeTest(lines[0])
	// Wide terminal: Dist must be first, Status last in cluster mode.
	idxDist := strings.Index(hdr, "Dist")
	idxRef := strings.Index(hdr, "Ref id")
	idxRel := strings.Index(hdr, "Relation")
	idxSt := strings.Index(hdr, refStatusColumnHeader)
	require.GreaterOrEqual(t, idxDist, 0)
	require.GreaterOrEqual(t, idxRef, 0)
	require.GreaterOrEqual(t, idxRel, 0)
	require.GreaterOrEqual(t, idxSt, 0)
	assert.Less(t, idxDist, idxRef, "Dist must come before Ref id")
	assert.Less(t, idxRef, idxRel, "Ref id must come before Relation")
	assert.Less(t, idxRel, idxSt, "Relation must come before Status")
}

func TestClusterRefEdgeStatus(t *testing.T) {
	okRef := types.Ref{
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
			{Type: types.RefAttributeOriginSource, Value: types.RefSourceCluster},
		},
	}
	assert.Equal(t, refStatusOK, clusterRefEdgeStatus(okRef))

	missingRef := types.Ref{
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
		},
	}
	assert.Equal(t, refStatusTemplateOnly, clusterRefEdgeStatus(missingRef))

	clusterOnlyRef := types.Ref{
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginSource, Value: types.RefSourceCluster},
		},
	}
	assert.Equal(t, refStatusClusterOnly, clusterRefEdgeStatus(clusterOnlyRef))

	neitherRef := types.Ref{}
	assert.Equal(t, refStatusNeither, clusterRefEdgeStatus(neitherRef))
}

func TestRefTreeView_headerShowsStatusInParensOnlyInClusterMode(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	refs := []types.Ref{
		{
			From: idB, To: idA, Labels: []string{"in"},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
				{Type: types.RefAttributeOriginSource, Value: types.RefSourceCluster},
			},
		},
		{From: idA, To: idB, Labels: []string{"out"}},
	}
	m := NewRefTreeModel(refs, idA, false, true)
	m.width = 80
	m.height = 40
	out := m.View()
	assert.Contains(t, out, "Entity: "+string(idA))
	assert.Contains(t, out, "("+refStatusOK+")")
}

func TestRefTreeView_localModeOmitsStatusEverywhere(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"out"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 40

	out := stripANSIForRefTreeTest(m.View())
	assert.NotContains(t, out, refStatusColumnHeader)
	assert.NotContains(t, out, "(template only)")
	assert.NotContains(t, out, "(ok)")
}

func TestRenderRefDetail_ownerAttributes(t *testing.T) {
	ref := &types.Ref{
		From:    "a/b",
		To:      "c/d",
		Reverse: true,
		Labels:  []string{"owner"},
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeKubernetesOwnerController, Value: "false"},
			{Type: types.RefAttributeOriginOwner, Value: types.RefOwnerRoleDependent},
			{Type: types.RefAttributeKubernetesBlockOwnerDeletion, Value: "true"},
		},
	}
	out := renderRefDetail(ref, 80, 20)
	assert.Contains(t, out, "kubernetes:ownerController")
	assert.Contains(t, out, "false")
	assert.Contains(t, out, "kubernetes:blockOwnerDeletion")
	assert.Contains(t, out, "origin:owner")
}

// --- Ref tree detail panel: layout, scrolling, and help (hydra/docs/develop/hydra-go/cli.md, hydra-tree.md)

// refTreeDetailScrollHelpFragment must appear in refTreeHelpPlain whenever wrapped detail exceeds the
// visible detail inner height (same wording should appear in the rendered footer). Keeps tests from
// matching arbitrary "[" / "]" elsewhere on the line.
const refTreeDetailScrollHelpFragment = "[] detail"

func TestRefTree_computeLayout_tallTerminal_growsDetailInnerLinesForShallowList(t *testing.T) {
	heavy := refTreeHeavyRefForDetailTest("tail-marker")
	idA := types.Id("apps/v1/Deployment/ns/web")
	refs := []types.Ref{
		heavy,
		{From: idA, To: types.Id("v1/Service/ns/svc"), Labels: []string{"svc"}},
	}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 52
	m.cursor = 0
	require.NotNil(t, m.selectedRef())

	lay := m.computeLayout()
	require.GreaterOrEqual(t, lay.detailInnerLines, 14,
		"tall terminal should give the detail panel enough inner lines to avoid an artificially small cap when the list is shallow")

	logical := refTreeDetailLogicalLineCount(m.selectedRef(), lay.contentW)
	require.Greater(t, logical, 14,
		"fixture ref should be heavy enough that growing the detail panel (vs a tiny cap) is meaningful")
	require.GreaterOrEqual(t, lay.detailInnerLines, logical,
		"tall terminal should allocate enough inner detail lines to show the full wrapped ref")
}

func TestRefTree_computeLayout_shortTerminal_detailGetsAtLeastAsMuchInnerHeightAsList(t *testing.T) {
	heavy := refTreeHeavyRefForDetailTest("__ATTR_LAST_UNIQUE__")
	idA := types.Id("apps/v1/Deployment/ns/web")
	var refs []types.Ref
	for i := 0; i < 28; i++ {
		refs = append(refs, types.Ref{
			From:   types.Id(fmt.Sprintf("v1/Secret/ns/in-%02d", i)),
			To:     idA,
			Labels: []string{"in"},
		})
	}
	refs = append(refs, heavy)

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 15
	m.cursor = 0

	lay := m.computeLayout()
	require.GreaterOrEqual(t, lay.detailInnerLines, lay.listInnerLines,
		"when vertical space is tight, detail should not be starved below the list inner height (detail priority over a tall list)")
}

func TestRefTreeView_tallTerminal_detailShowsDeepAttributesWithoutEllipsis(t *testing.T) {
	heavy := refTreeHeavyRefForDetailTest("tail-marker-xyz")
	idA := types.Id("apps/v1/Deployment/ns/web")
	refs := []types.Ref{heavy, {From: idA, To: types.Id("v1/Service/ns/svc"), Labels: []string{"svc"}}}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 48
	m.cursor = 0

	detail := refTreeExtractDetailPlain(m.View())
	require.NotEmpty(t, detail)
	assert.Contains(t, detail, "tui:test:attr:00")
	assert.Contains(t, detail, "tui:test:attr:17")
	assert.Contains(t, detail, "tail-marker-xyz")
	assert.NotContains(t, detail, " …", "detail must not end with ellipsis truncation when the terminal budget can show the full ref")
}

func TestRefTreeView_shortTerminal_bracketKeyRevealsBottomDetailLine(t *testing.T) {
	heavy := refTreeHeavyRefForDetailTest("__ATTR_LAST_UNIQUE__")
	idA := types.Id("apps/v1/Deployment/ns/web")
	refs := []types.Ref{heavy, {From: idA, To: types.Id("v1/Service/ns/svc"), Labels: []string{"svc"}}}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 14
	m.cursor = 0

	before := refTreeExtractDetailPlain(m.View())
	require.NotEmpty(t, before)
	require.NotContains(t, before, "__ATTR_LAST_UNIQUE__", "short terminal should start with the top of the detail clipped")

	for i := 0; i < 30; i++ {
		next, _ := m.Update(refTreeDetailScrollDownKeyMsg())
		m = next.(*RefTreeModel)
	}
	after := refTreeExtractDetailPlain(m.View())
	assert.Contains(t, after, "__ATTR_LAST_UNIQUE__", "] should scroll the detail pane so later attribute lines become visible")
}

func TestRefTreeView_bracketScrollUp_restoresEarlyDetailMarkers(t *testing.T) {
	heavy := refTreeHeavyRefForDetailTest("__ATTR_LAST_UNIQUE__")
	idA := types.Id("apps/v1/Deployment/ns/web")
	refs := []types.Ref{heavy, {From: idA, To: types.Id("v1/Service/ns/svc"), Labels: []string{"svc"}}}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 14
	m.cursor = 0

	earlyMarker := "From:"
	before := refTreeExtractDetailPlain(m.View())
	require.Contains(t, before, earlyMarker, "initial window should show the top of the ref (From line)")
	require.NotContains(t, before, "__ATTR_LAST_UNIQUE__")

	for i := 0; i < 30; i++ {
		next, _ := m.Update(refTreeDetailScrollDownKeyMsg())
		m = next.(*RefTreeModel)
	}
	scrolledDown := refTreeExtractDetailPlain(m.View())
	require.Contains(t, scrolledDown, "__ATTR_LAST_UNIQUE__")
	require.NotContains(t, scrolledDown, earlyMarker, "after scrolling down, the From line should leave the visible slice")

	for i := 0; i < 30; i++ {
		next, _ := m.Update(refTreeDetailScrollUpKeyMsg())
		m = next.(*RefTreeModel)
	}
	scrolledUp := refTreeExtractDetailPlain(m.View())
	assert.Contains(t, scrolledUp, earlyMarker, "[ should scroll the detail pane back toward the top")
	assert.NotContains(t, scrolledUp, "__ATTR_LAST_UNIQUE__", "[ should move the window off the tail marker again")
}

func TestRefTreeView_pgUpPgDnMoveCursorByPage(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/web")
	var refs []types.Ref
	for i := 0; i < 18; i++ {
		refs = append(refs, types.Ref{
			From:   types.Id(fmt.Sprintf("v1/Secret/ns/in-%02d", i)),
			To:     idA,
			Labels: []string{"in"},
		})
	}

	t.Run("PgDn", func(t *testing.T) {
		m := NewRefTreeModel(refs, idA, false, false)
		m.width = 100
		m.height = 14
		m.cursor = 0

		prev := m.cursor
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(*RefTreeModel)
		assert.Greater(t, m.cursor, prev, "PgDn must advance the cursor")
	})

	t.Run("PgUp", func(t *testing.T) {
		m := NewRefTreeModel(refs, idA, false, false)
		m.width = 100
		m.height = 14
		m.cursor = m.totalSelectable() - 1

		prev := m.cursor
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		m = next.(*RefTreeModel)
		assert.Less(t, m.cursor, prev, "PgUp must move the cursor backwards")
	})

	t.Run("PgDn resetsDetailScroll", func(t *testing.T) {
		m := NewRefTreeModel(refs, idA, false, false)
		m.width = 100
		m.height = 14
		m.cursor = 0
		for i := 0; i < 10; i++ {
			nx, _ := m.Update(refTreeDetailScrollDownKeyMsg())
			m = nx.(*RefTreeModel)
		}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(*RefTreeModel)
		assert.Equal(t, 0, m.detailScroll, "PgDn must reset detail scroll when cursor moves to a new item")
	})
}

func TestRefTreeView_cursorMove_resetsDetailScrollToTop(t *testing.T) {
	heavy := refTreeHeavyOutgoingRefForDetailTest("__ATTR_LAST_UNIQUE__")
	idA := types.Id("apps/v1/Deployment/ns/web")
	light := types.Ref{
		From:   idA,
		To:     types.Id("v1/Secret/ns/light-next"),
		Labels: []string{"LIGHT_TOP_MARKER_REL"},
	}
	refs := []types.Ref{heavy, light}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	// Short terminals cannot fit the full light-ref block (through Labels:) alongside the list;
	// use enough rows so "reset to top" is observable without body-only scroll.
	m.height = 22
	require.GreaterOrEqual(t, len(m.incoming)+len(m.outgoing), 3, "fixture needs self + heavy + light outgoing rows")
	m.cursor = 1

	topOfHeavy := "tui:test:attr:00"
	for i := 0; i < 25; i++ {
		next, _ := m.Update(refTreeDetailScrollDownKeyMsg())
		m = next.(*RefTreeModel)
	}
	scrolledHeavy := refTreeExtractDetailPlain(m.View())
	require.Contains(t, scrolledHeavy, "__ATTR_LAST_UNIQUE__")
	require.NotContains(t, scrolledHeavy, topOfHeavy, "scrolled-down detail should no longer show the first heavy-ref attribute line")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*RefTreeModel)

	detail := refTreeExtractDetailPlain(m.View())
	require.Contains(t, detail, "LIGHT_TOP_MARKER_REL")
	require.Contains(t, detail, "Labels:")
	require.NotContains(t, detail, "__ATTR_LAST_UNIQUE__", "tail marker belongs only to the heavy ref; it must not remain visible after selecting the light edge")
	require.NotContains(t, detail, "tui:test:attr:17", "heavy-ref attribute types must not appear in the light ref detail")
	require.NotContains(t, detail, topOfHeavy, "light edge has no tui:test attributes")

	lines := strings.Split(detail, "\n")
	require.GreaterOrEqual(t, len(lines), 3, "detail should start with the standard Selected relation header and From/To lines")
	assert.Contains(t, lines[0], "Selected relation")
	assert.Contains(t, lines[1], "From:")
	assert.Contains(t, lines[2], "To:")
	assert.Contains(t, strings.Join(lines[:min(8, len(lines))], "\n"), "light-next",
		"after changing the list selection, detail scroll resets so the top of the new ref (To target) is visible again")
}

func TestRefTree_helpPlain_includesBracketDetailScrollWhenDetailOverflows(t *testing.T) {
	heavy := refTreeHeavyRefForDetailTest("__ATTR_LAST_UNIQUE__")
	idA := types.Id("apps/v1/Deployment/ns/web")
	refs := []types.Ref{heavy, {From: idA, To: types.Id("v1/Service/ns/svc"), Labels: []string{"svc"}}}

	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 100
	m.height = 14
	m.cursor = 0

	lay := m.computeLayout()
	logical := refTreeDetailLogicalLineCount(m.selectedRef(), lay.contentW)
	require.Greater(t, logical, lay.detailInnerLines, "fixture must overflow the visible detail inner height")

	help := m.refTreeHelpPlain()
	assert.Contains(t, help, refTreeDetailScrollHelpFragment,
		"footer must spell out detail-only scroll alongside PgUp/PgDn list paging when detail overflows")
}

func TestRefTreeView_clusterMode_tallTerminal_detailShowsDeepAttributesWithoutEllipsis(t *testing.T) {
	heavy := refTreeHeavyRefForDetailTest("tail-marker-cluster")
	heavy.Attributes = append(slices.Clone(heavy.Attributes),
		types.RefAttribute{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
		types.RefAttribute{Type: types.RefAttributeOriginSource, Value: types.RefSourceCluster},
	)
	idA := types.Id("apps/v1/Deployment/ns/web")
	refs := []types.Ref{heavy, {From: idA, To: types.Id("v1/Service/ns/svc"), Labels: []string{"svc"}}}

	m := NewRefTreeModel(refs, idA, false, true)
	m.width = 100
	m.height = 48
	m.cursor = 0

	detail := refTreeExtractDetailPlain(m.View())
	require.NotEmpty(t, detail)
	assert.Contains(t, detail, "tui:test:attr:00")
	assert.Contains(t, detail, "tui:test:attr:17")
	assert.Contains(t, detail, "tail-marker-cluster")
	assert.NotContains(t, detail, " …", "cluster mode must follow the same detail sizing rules as local mode when the terminal is tall")
}

func TestRefTreeView_helpTextAbovePanel(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	idB := types.Id("v1/ConfigMap/ns/b")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"config"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 80
	m.height = 30

	out := stripANSIForRefTreeTest(m.View())
	lines := strings.Split(out, "\n")

	helpIdx := -1
	panelIdx := -1
	for i, line := range lines {
		if helpIdx < 0 && strings.Contains(line, "↑/↓") {
			helpIdx = i
		}
		if panelIdx < 0 && strings.Contains(line, "╭") {
			panelIdx = i
		}
	}
	require.GreaterOrEqual(t, helpIdx, 0, "help text (containing ↑/↓) must appear in the view")
	require.GreaterOrEqual(t, panelIdx, 0, "panel top border (╭) must appear in the view")
	assert.Less(t, helpIdx, panelIdx, "help text must appear ABOVE the panel top border, not as a footer below")
}

func TestRefTreeModel_sortToggleAscendingDescending(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	idB := types.Id("v1/Secret/ns/b")
	idC := types.Id("v1/ConfigMap/ns/c")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"zeta"}},
		{From: idA, To: idC, Labels: []string{"alpha"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)

	require.Equal(t, refTreeSortDist, m.sortField, "default sort field should be dist ascending")
	require.Len(t, m.outgoing, 3)
	assert.Equal(t, idC, m.outgoing[1].OtherID, "default dist ascending: tiebreak by id keeps ConfigMap before Secret")

	pressS := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}

	// s once → dist DESCENDING
	next, _ := m.Update(pressS)
	rm := next.(*RefTreeModel)
	assert.Equal(t, refTreeSortDist, rm.sortField, "first s press should stay on dist field")
	assert.True(t, rm.sortDescending, "first s press should toggle to descending")

	// s again → id ASCENDING
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	assert.Equal(t, refTreeSortID, rm.sortField, "second s press should advance to id")
	assert.False(t, rm.sortDescending, "advancing to next column should reset to ascending")

	// s again → id DESCENDING
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	assert.Equal(t, refTreeSortID, rm.sortField)
	assert.True(t, rm.sortDescending)
	require.Len(t, rm.outgoing, 3)
	assert.Equal(t, idB, rm.outgoing[1].OtherID, "id descending: Secret before ConfigMap")

	// s again → relation ASCENDING
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	assert.Equal(t, refTreeSortRelation, rm.sortField)
	assert.False(t, rm.sortDescending)

	// s again → relation DESCENDING
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	assert.Equal(t, refTreeSortRelation, rm.sortField)
	assert.True(t, rm.sortDescending)

	// s again → status ASCENDING
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	assert.Equal(t, refTreeSortStatus, rm.sortField)
	assert.False(t, rm.sortDescending)

	// s again → status DESCENDING
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	assert.Equal(t, refTreeSortStatus, rm.sortField)
	assert.True(t, rm.sortDescending)

	// s again → wraps back to dist ASCENDING (full cycle)
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	assert.Equal(t, refTreeSortDist, rm.sortField, "full cycle should return to dist")
	assert.False(t, rm.sortDescending, "full cycle should return to ascending")
}

func TestRefTreeView_sortIndicatorInColumnHeader(t *testing.T) {
	idA := types.Id("apps/v1/Deployment/ns/a")
	idB := types.Id("v1/ConfigMap/ns/b")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"config"}},
	}
	m := NewRefTreeModel(refs, idA, false, false)
	m.width = 80
	m.height = 30

	out := stripANSIForRefTreeTest(m.View())
	assert.Contains(t, out, "▲", "default ascending sort should show ▲ indicator")
	assert.Contains(t, out, "Dist", "Dist column header must be present")

	pressS := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}

	// s once → dist descending
	next, _ := m.Update(pressS)
	rm := next.(*RefTreeModel)
	rm.width = 80
	rm.height = 30
	out = stripANSIForRefTreeTest(rm.View())
	assert.Contains(t, out, "▼", "descending sort should show ▼ indicator")

	// s again → id ascending
	next, _ = rm.Update(pressS)
	rm = next.(*RefTreeModel)
	rm.width = 80
	rm.height = 30
	out = stripANSIForRefTreeTest(rm.View())
	assert.Contains(t, out, "Ref id", "Ref id column header after advancing to id sort")
	assert.Contains(t, out, "▲", "ascending id sort should show ▲ indicator")
}
