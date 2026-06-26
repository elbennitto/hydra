package cmd

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func TestApplyLipglossColorOverride_ForcedColorEnablesANSIProfile(t *testing.T) {
	originalProfile := lipgloss.ColorProfile()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(originalProfile)
	})

	lipgloss.SetColorProfile(termenv.Ascii)
	applyLipglossColorOverride(true, true)

	require.Equal(t, termenv.ANSI256, lipgloss.ColorProfile())
}

func TestApplyLipglossColorOverride_ForcedNoColorUsesAsciiProfile(t *testing.T) {
	originalProfile := lipgloss.ColorProfile()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(originalProfile)
	})

	lipgloss.SetColorProfile(termenv.ANSI256)
	applyLipglossColorOverride(false, true)

	require.Equal(t, termenv.Ascii, lipgloss.ColorProfile())
}

func TestApplyLipglossColorOverride_DoesNotChangeProfileWithoutForce(t *testing.T) {
	originalProfile := lipgloss.ColorProfile()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(originalProfile)
	})

	lipgloss.SetColorProfile(termenv.Ascii)
	applyLipglossColorOverride(true, false)

	require.Equal(t, termenv.Ascii, lipgloss.ColorProfile())
}
