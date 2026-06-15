package asciinema

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripRecordingControlSequences(t *testing.T) {
	in := " $ \r\n\x1b]11;?\x1b\\\x1b[6nhydra local --help\r\n"
	want := " $ \r\nhydra local --help\r\n"
	assert.Equal(t, want, StripRecordingControlSequences(in))
}

func TestStripRecordingControlSequences_OnlyQueries(t *testing.T) {
	assert.Empty(t, StripRecordingControlSequences("\x1b]11;?\x1b\\\x1b[6n"))
}

func TestStripRecordingControlSequences_TerminalResponses(t *testing.T) {
	in := "\x1b]11;rgb:0000/2b2b/3636\x1b\\\x1b[55;37Rhydra version --help\r\n"
	want := "hydra version --help\r\n"
	assert.Equal(t, want, StripRecordingControlSequences(in))
}

func TestRecordingFilterWriter_StreamedChunks(t *testing.T) {
	var buf strings.Builder
	fw := NewRecordingFilterWriter(&buf)
	part1 := []byte(" $ \r\n\x1b]11;rgb:0000/2b2b/")
	part2 := []byte("3636\x1b\\\x1b[55;37Rhydra --help\r\n")
	_, err := fw.Write(part1)
	require.NoError(t, err)
	_, err = fw.Write(part2)
	require.NoError(t, err)
	require.NoError(t, fw.Flush())
	assert.Equal(t, " $ \r\nhydra --help\r\n", buf.String())
}

func TestIsExitCodeOnlyLine(t *testing.T) {
	assert.True(t, isExitCodeOnlyLine("0\n"))
	assert.True(t, isExitCodeOnlyLine("0\r\n"))
	assert.False(t, isExitCodeOnlyLine("hydra\n"))
}

func TestWriteRawCast_RemovesTerminalQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.cast")
	body := " $ \r\n\x1b]11;?\x1b\\\x1b[6nhydra --help\r\n"
	require.NoError(t, writeRawCast(path, []byte(body), ""))

	events := readCastEvents(t, path)
	require.Len(t, events, 2)
	for _, e := range events {
		assert.NotContains(t, e.data, "\x1b]11")
		assert.NotContains(t, e.data, "\x1b[6n")
	}
}
