package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestFilterTreeIDs_byGVK(t *testing.T) {
	ids := []types.Id{
		"v1/Pod/ns-a/p1",
		"apps/v1/Deployment/ns-b/d1",
	}
	out := FilterTreeIDs(ids, FilterFieldGVK, "Deployment")
	assert.Len(t, out, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns-b/d1"), out[0])
}

func TestFilterTreeIDs_byNamespace(t *testing.T) {
	ids := []types.Id{
		"v1/Pod/monitoring/p1",
		"v1/Pod/default/p2",
	}
	out := FilterTreeIDs(ids, FilterFieldNamespace, "monitoring")
	assert.Len(t, out, 1)
	assert.Equal(t, types.Id("v1/Pod/monitoring/p1"), out[0])
}

func TestFilterTreeIDs_combined(t *testing.T) {
	ids := []types.Id{
		"v1/Pod/monitoring/p1",
		"v1/ConfigMap/monitoring/cm1",
	}
	out := FilterTreeIDs(FilterTreeIDs(ids, FilterFieldKind, "Pod"), FilterFieldNamespace, "monitoring")
	assert.Len(t, out, 1)
}

func TestFilterTreeIDs_byName(t *testing.T) {
	ids := []types.Id{
		"v1/Pod/monitoring/api-pod",
		"v1/Pod/monitoring/worker-pod",
	}
	out := FilterTreeIDs(ids, FilterFieldName, "api")
	require.Len(t, out, 1)
	assert.Equal(t, types.Id("v1/Pod/monitoring/api-pod"), out[0])
}

func TestIdGVKString(t *testing.T) {
	assert.Equal(t, "v1/Pod", idGVKString(types.Id("v1/Pod/ns/n")))
	assert.Equal(t, "apps/v1/Deployment", idGVKString(types.Id("apps/v1/Deployment/ns/n")))
}

func TestIDPickerView_localModeOmitsStatusColumn(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{"v1/Pod/ns/p1"}, nil)
	out := m.View()
	assert.NotContains(t, out, "Status")
}

func TestIDPickerView_clusterModeShowsStatusColumn(t *testing.T) {
	id := types.Id("v1/Pod/ns/p1")
	m := NewIDPickerModel("Cluster", []types.Id{id}, map[types.Id]string{id: "ok"})
	out := m.View()
	assert.Contains(t, out, "Status")
}

func TestIDPickerModel_slashOpensFilterPopupWithQueryFocus(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{"v1/Pod/ns/p1"}, nil)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	pm := next.(*IDPickerModel)
	assert.True(t, pm.filterOpen)
	assert.Equal(t, filterPopupFocusQuery, pm.filterFocus)
}

func TestIDPickerModel_filterPopupTabMovesFocusToField(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{"v1/Pod/ns/p1"}, nil)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	pm := next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyTab})
	pm = next.(*IDPickerModel)
	assert.Equal(t, filterPopupFocusField, pm.filterFocus)
}

func TestIDPickerModel_filterPopupApplyByKind(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{
		"v1/ConfigMap/ns/cm-a",
		"apps/v1/Deployment/ns/deploy-a",
	}, nil)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	pm := next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyTab})
	pm = next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyDown})
	pm = next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyDown})
	pm = next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyDown})
	pm = next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	pm = next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Deploy")})
	pm = next.(*IDPickerModel)
	next, _ = pm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm = next.(*IDPickerModel)

	require.Len(t, pm.filtered, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/deploy-a"), pm.filtered[0])
	assert.False(t, pm.filterOpen)
}

func TestIDPickerModel_sortCycleChangesVisibleOrder(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{
		"z.io/v1/Alpha/z-ns/aaa",
		"a.io/v1/Zulu/a-ns/zzz",
	}, nil)
	require.Len(t, m.filtered, 2)
	assert.Equal(t, types.Id("a.io/v1/Zulu/a-ns/zzz"), m.filtered[0], "id sort should start lexicographically")

	pressS := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	next, _ := m.Update(pressS)
	pm := next.(*IDPickerModel)
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortKind, pm.sortField)
	require.Len(t, pm.filtered, 2)
	assert.Equal(t, types.Id("z.io/v1/Alpha/z-ns/aaa"), pm.filtered[0], "kind sort should bring Alpha before Zulu")
}

func TestIdPickerView_helpTextAboveList(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{
		"v1/Pod/ns/p1",
		"v1/ConfigMap/ns/cm1",
	}, nil)

	out := m.View()
	lines := strings.Split(out, "\n")

	helpIdx := -1
	listIdx := -1
	for i, line := range lines {
		if helpIdx < 0 && strings.Contains(line, "↑/↓") {
			helpIdx = i
		}
		if listIdx < 0 && (strings.Contains(line, "IDs (") || strings.Contains(line, "Resource id")) {
			listIdx = i
		}
	}
	require.GreaterOrEqual(t, helpIdx, 0, "help text (containing ↑/↓) must appear in the view")
	require.GreaterOrEqual(t, listIdx, 0, "list panel or header must appear in the view")
	assert.Less(t, helpIdx, listIdx, "help text must appear ABOVE the list panel, not as a footer below")
}

func TestIdPickerModel_sortToggleAscendingDescending(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{
		"z.io/v1/Alpha/z-ns/aaa",
		"a.io/v1/Zulu/a-ns/zzz",
	}, nil)

	require.Equal(t, pickerSortID, m.sortField, "default sort field should be id")
	assert.Equal(t, types.Id("a.io/v1/Zulu/a-ns/zzz"), m.filtered[0], "default id ascending: a.io before z.io")

	pressS := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}

	// s once → id DESCENDING
	next, _ := m.Update(pressS)
	pm := next.(*IDPickerModel)
	assert.Equal(t, pickerSortID, pm.sortField, "first s press should stay on id field")
	assert.True(t, pm.sortDescending, "first s press should toggle to descending")
	assert.Equal(t, types.Id("z.io/v1/Alpha/z-ns/aaa"), pm.filtered[0], "id descending: z.io before a.io")

	// s again → kind ASCENDING
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortKind, pm.sortField, "second s press should advance to kind")
	assert.False(t, pm.sortDescending, "advancing to next column should reset to ascending")

	// s again → kind DESCENDING
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortKind, pm.sortField)
	assert.True(t, pm.sortDescending)

	// s again → namespace ASCENDING
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortNamespace, pm.sortField)
	assert.False(t, pm.sortDescending)

	// s again → namespace DESCENDING
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortNamespace, pm.sortField)
	assert.True(t, pm.sortDescending)

	// s again → name ASCENDING
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortName, pm.sortField)
	assert.False(t, pm.sortDescending)

	// s again → name DESCENDING
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortName, pm.sortField)
	assert.True(t, pm.sortDescending)

	// s again → wraps back to id ASCENDING (full cycle)
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	assert.Equal(t, pickerSortID, pm.sortField, "full cycle should return to id")
	assert.False(t, pm.sortDescending, "full cycle should return to ascending")
}

func TestIdPickerView_sortIndicatorInColumnHeader(t *testing.T) {
	m := NewIDPickerModel("Local", []types.Id{
		"v1/Pod/ns/p1",
		"v1/ConfigMap/ns/cm1",
	}, nil)

	out := m.View()
	assert.Contains(t, out, "▲", "default ascending sort should show ▲ indicator")

	pressS := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}

	// s once → id descending
	next, _ := m.Update(pressS)
	pm := next.(*IDPickerModel)
	out = pm.View()
	assert.Contains(t, out, "▼", "descending sort should show ▼ indicator")

	// s again → kind ascending
	next, _ = pm.Update(pressS)
	pm = next.(*IDPickerModel)
	out = pm.View()
	assert.Contains(t, out, "▲", "ascending kind sort should show ▲ indicator")
}
