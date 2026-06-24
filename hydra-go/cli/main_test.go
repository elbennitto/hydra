package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func TestNewErrorMarkdownRenderer_RendersANSIWhenColorProfileEnabled(t *testing.T) {
	originalProfile := lipgloss.ColorProfile()
	originalDark := lipgloss.HasDarkBackground()

	lipgloss.SetColorProfile(termenv.ANSI256)
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(originalProfile)
		lipgloss.SetHasDarkBackground(originalDark)
	})

	renderer, err := newErrorMarkdownRenderer()
	require.NoError(t, err)

	out, err := renderer.Render("## Heading\n\n`code`\n")
	require.NoError(t, err)
	require.Contains(t, out, "\x1b[")
	require.True(t, strings.Contains(out, "Heading") || strings.Contains(out, "code"))
}
