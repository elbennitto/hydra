package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
	baseerrors "hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
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

func TestFindExtendedErrorMarkdownInput_PrefersNestedRenderableError(t *testing.T) {
	inner := log.CreateError(
		baseerrors.ErrSopsDecryptFailed,
		"failed to decrypt {target}",
		log.String("target", "file /tmp/test.sops.yaml"),
		log.String("reason", "age-key-missing"),
		log.String("stderr", "Failed to get the data key required to decrypt the SOPS file."),
	)
	err := log.CreateError(
		baseerrors.ErrCiDownload,
		"ci download: prepare OCI registry auth: {err}",
		log.Err(fmt.Errorf("prepare OCI registry auth: %w", inner)),
	)

	code, params, ok := findExtendedErrorMarkdownInput(err)
	require.True(t, ok)
	require.Equal(t, baseerrors.ErrSopsDecryptFailed, code)
	require.Equal(t, "file /tmp/test.sops.yaml", params["target"])
	require.Equal(t, "age-key-missing", params["reason"])
	require.Contains(t, params["stderr"], "Failed to get the data key")
}
