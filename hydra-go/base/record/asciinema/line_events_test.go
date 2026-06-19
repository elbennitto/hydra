package asciinema

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/record/directive"
)

func TestHelpCastDocumentationCommand(t *testing.T) {
	assert.Equal(t, "hydra record cli -- hydra argocd sync manual --help",
		HelpCastDocumentationCommand("hydra argocd sync manual --help"))
}

func TestLinesToEvents_SleepDirective(t *testing.T) {
	lines := []string{" $ \r\n", "<<hydra sleep 1.2>>\r\n", "hydra --help\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 3)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, " $ \r\n", out[0].data)
	assert.InDelta(t, 1.2, out[1].time, 1e-9)
	assert.Equal(t, "\r\n", out[1].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[2].time, 1e-9)
	assert.Equal(t, "hydra --help\r\n", out[2].data)
}

func TestLinesToEvents_DropsExitCodeButKeepsBlankLines(t *testing.T) {
	lines := []string{"\r\n", "0\n", "hydra --help\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 2)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "\r\n", out[0].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[1].time, 1e-9)
	assert.Equal(t, "hydra --help\r\n", out[1].data)
}

func TestLinesToEvents_SleepDirectiveEmbeddedInOutputLine(t *testing.T) {
	lines := []string{"partial<<hydra sleep 0.5>>\r\n", "hydra --help\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 3)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "partial", out[0].data)
	assert.InDelta(t, 0.5, out[1].time, 1e-9)
	assert.Equal(t, "\r\n", out[1].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[2].time, 1e-9)
	assert.Equal(t, "hydra --help\r\n", out[2].data)
}

func TestLinesToEvents_InlineSleepDirective_DoesNotConsumeFollowingDigit(t *testing.T) {
	lines := []string{"a<<hydra sleep 0.2>>2"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 2)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "a", out[0].data)
	assert.InDelta(t, 0.2, out[1].time, 1e-9)
	assert.Equal(t, "2", out[1].data)
}

func TestLinesToEvents_MarkerDirective_EmitsMarkerEvent(t *testing.T) {
	lines := []string{"output\r\n", "<<hydra marker Deploy>>\r\n", "next\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 4)
	assert.Equal(t, "o", out[0].kind)
	assert.Equal(t, "output\r\n", out[0].data)
	assert.Equal(t, "m", out[1].kind)
	assert.Equal(t, "Deploy", out[1].data)
	assert.Equal(t, "o", out[2].kind)
	assert.Equal(t, "\r\n", out[2].data)
	assert.Equal(t, "o", out[3].kind)
	assert.Equal(t, "next\r\n", out[3].data)
}

func TestLinesToEvents_SleepThenMarkerDirective_AppliesDelayToMarker(t *testing.T) {
	lines := []string{"<<hydra sleep 0.3>><<hydra marker Deploy>>\r\n", "next\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 3)
	assert.Equal(t, "m", out[0].kind)
	assert.InDelta(t, 0.3, out[0].time, 1e-9)
	assert.Equal(t, "Deploy", out[0].data)
	assert.Equal(t, "o", out[1].kind)
	assert.InDelta(t, defaultLineDelaySeconds, out[1].time, 1e-9)
	assert.Equal(t, "\r\n", out[1].data)
	assert.Equal(t, "o", out[2].kind)
	assert.Equal(t, "next\r\n", out[2].data)
}

func TestLinesToEvents_TypedCharsWithSpeed_DoNotAppendNewlinePerChar(t *testing.T) {
	raw := directive.SleepInline(0.2) + "c" + directive.SleepInline(0.2) + "o"
	lines := splitTerminalLines(raw)
	out := linesToEvents(lines, "o")

	require.Len(t, out, 2)
	assert.Equal(t, "c", out[0].data)
	assert.Equal(t, "o", out[1].data)
}

func TestLinesToEvents_SleepDirectiveWithLeadingCarriageReturn(t *testing.T) {
	lines := []string{"\r<<hydra sleep 0.3>>\r\n", "hydra --help\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 3)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "\r", out[0].data)
	assert.InDelta(t, 0.3, out[1].time, 1e-9)
	assert.Equal(t, "\r\n", out[1].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[2].time, 1e-9)
	assert.Equal(t, "hydra --help\r\n", out[2].data)
}

func TestLinesToEvents_MovesTrailingCarriageReturnToFront(t *testing.T) {
	lines := []string{"\r", "typed\r"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 2)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "\r", out[0].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[1].time, 1e-9)
	assert.Equal(t, "\rtyped", out[1].data)
}

func TestLinesToEvents_ResetWithTrailingCarriageReturn_DropsCarriageReturn(t *testing.T) {
	lines := []string{"\x1b[0m\r", "next\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 2)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "\x1b[0m", out[0].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[1].time, 1e-9)
	assert.Equal(t, "next\r\n", out[1].data)
}

func TestLinesToEvents_PreservesDirectiveSpacerEnterLine(t *testing.T) {
	lines := []string{"<<hydra sleep 1>>\r\n", "\r\n", "<<hydra sleep 1>>\r\n", "output\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 4)
	assert.InDelta(t, 1.0, out[0].time, 1e-9)
	assert.Equal(t, "\r\n", out[0].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[1].time, 1e-9)
	assert.Equal(t, "\r\n", out[1].data)
	assert.InDelta(t, 1.0, out[2].time, 1e-9)
	assert.Equal(t, "\r\n", out[2].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[3].time, 1e-9)
	assert.Equal(t, "output\r\n", out[3].data)
}

func TestLinesToEvents_PreservesExplicitNewlineBetweenDifferentSleeps(t *testing.T) {
	lines := []string{"<<hydra sleep 1>>\r\n", "\r\n", "<<hydra sleep 0.2>>\r\n", "output\r\n"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 4)
	assert.InDelta(t, 1.0, out[0].time, 1e-9)
	assert.Equal(t, "\r\n", out[0].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[1].time, 1e-9)
	assert.Equal(t, "\r\n", out[1].data)
	assert.InDelta(t, 0.2, out[2].time, 1e-9)
	assert.Equal(t, "\r\n", out[2].data)
	assert.InDelta(t, defaultLineDelaySeconds, out[3].time, 1e-9)
	assert.Equal(t, "output\r\n", out[3].data)
}

func TestLinesToEvents_PreservesExplicitNewlineAfterResetBeforePrompt(t *testing.T) {
	lines := []string{"d<<hydra sleep 0.2>>\x1b[0m\r\n", "<<hydra sleep 0.2>>\x1b[1;95m $ \x1b[0m\x1b[1;97m"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 3)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "d", out[0].data)
	assert.InDelta(t, 0.2, out[1].time, 1e-9)
	assert.True(t, strings.HasSuffix(out[1].data, "\r\n"))
	assert.InDelta(t, 0.2, out[2].time, 1e-9)
	assert.Equal(t, "\x1b[1;95m $ \x1b[0m\x1b[1;97m", out[2].data)
}

func TestLinesToEvents_TrailingSleepWithoutNewline_EmitsEmptyEvent(t *testing.T) {
	lines := []string{"output\r\n", "<<hydra sleep 0.25>>"}
	out := linesToEvents(lines, "o")
	require.Len(t, out, 2)
	assert.InDelta(t, defaultLineDelaySeconds, out[0].time, 1e-9)
	assert.Equal(t, "output\r\n", out[0].data)
	assert.InDelta(t, 0.25, out[1].time, 1e-9)
	assert.Equal(t, "", out[1].data)
}

func readCastEvents(t *testing.T, path string) []castEvent {
	t.Helper()
	lines := splitLines(mustRead(t, path))
	var events []castEvent
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		var raw []any
		require.NoError(t, json.Unmarshal([]byte(line), &raw))
		data, _ := raw[2].(string)
		events = append(events, castEvent{
			time: raw[0].(float64),
			kind: raw[1].(string),
			data: data,
		})
	}
	return events
}

func readCastHeader(t *testing.T, path string) map[string]any {
	t.Helper()
	data := mustRead(t, path)
	idx := 0
	for i, b := range data {
		if b == '\n' {
			idx = i
			break
		}
	}
	var header map[string]any
	require.NoError(t, json.Unmarshal(data[:idx], &header))
	return header
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
