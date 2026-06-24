package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/errors/errormarkdown"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/cmd"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		printExtendedErrorMarkdown(err)
		if !log.HasWrittenRecord() {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		// Best-effort flush for short-lived TTY failures so the final log/error line
		// is not lost when the process exits immediately afterward.
		_ = os.Stderr.Sync()
		log.SyncStdoutBestEffort()
		if code, ok := exitcode.As(err); ok {
			os.Exit(code)
		}
		os.Exit(1)
	}
}

func printExtendedErrorMarkdown(err error) {
	code := errors.Id(err)
	if code == errors.ErrUnknown {
		return
	}

	params, ok := errors.TemplateParams(err)
	if !ok {
		return
	}

	md, renderErr := errormarkdown.Render(code, params)
	if renderErr != nil {
		return
	}

	out := md
	if log.ColorEnabled() {
		if renderer, glowErr := newErrorMarkdownRenderer(); glowErr == nil {
			if rendered, renderGlowErr := renderer.Render(md); renderGlowErr == nil {
				out = rendered
			}
		}
	}
	out = withLeftMarkerRail(out, log.ColorEnabled())

	_, _ = fmt.Fprintln(os.Stderr)
	_, _ = fmt.Fprintln(os.Stderr, out)
}

func newErrorMarkdownRenderer() (*glamour.TermRenderer, error) {
	style := styles.LightStyleConfig
	if lipgloss.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}

	return glamour.NewTermRenderer(
		glamour.WithColorProfile(lipgloss.ColorProfile()),
		glamour.WithStyles(style),
		glamour.WithWordWrap(0),
		glamour.WithPreservedNewLines(),
	)
}

func withLeftMarkerRail(s string, color bool) string {
	var borderColor lipgloss.TerminalColor = lipgloss.Color("8")
	if color {
		borderColor = lipgloss.AdaptiveColor{
			Light: "#1C8760",
			Dark:  "#89F0CB",
		}
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.DoubleBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1)

	return trimRightPaddingPerLine(style.Render(s))
}

func trimRightPaddingPerLine(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}
