package asciinema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteRawCast_AsciicastV3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.cast")
	require.NoError(t, writeRawCast(path, []byte(" $ \r\nhydra --help\r\n"), ""))

	header := readCastHeader(t, path)
	assert.Equal(t, float64(3), header["version"])
	term := header["term"].(map[string]any)
	assert.Equal(t, float64(defaultCastCols), term["cols"])
	assert.Equal(t, float64(defaultCastRows), term["rows"])
	assert.Equal(t, "xterm-256color", term["type"])

	events := readCastEvents(t, path)
	require.Len(t, events, 2)
	assert.InDelta(t, defaultLineDelaySeconds, events[0].time, 1e-9)
	assert.Equal(t, "o", events[0].kind)
	assert.Equal(t, " $ \r\n", events[0].data)
	assert.InDelta(t, defaultLineDelaySeconds, events[1].time, 1e-9)
	assert.Equal(t, "hydra --help\r\n", events[1].data)
}

func TestCaptureScriptOutput_RunsBashScript(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "rec.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/bash\nprintf 'ok\\n'\n"), 0o755))

	out, err := captureScriptOutput(scriptPath, recordingEnv(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(out), "ok")
}

func TestWriteRawCast_WritesDocumentationCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.cast")
	body := " $ \r\n<<hydra sleep 0.5>>\r\nline\r\n"
	require.NoError(t, writeRawCast(path, []byte(body), "hydra record help -- hydra demo --help"))

	events := readCastEvents(t, path)
	require.Len(t, events, 3)
	assert.InDelta(t, defaultLineDelaySeconds, events[0].time, 1e-9)
	assert.Equal(t, " $ \r\n", events[0].data)
	assert.InDelta(t, defaultLineDelaySeconds, events[1].time, 1e-9)
	assert.Equal(t, "", events[1].data)
	assert.InDelta(t, 0.5, events[2].time, 1e-9)
	assert.Equal(t, "line\r\n", events[2].data)

	var header map[string]any
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	idx := 0
	for i, b := range data {
		if b == '\n' {
			idx = i
			break
		}
	}
	require.NoError(t, json.Unmarshal(data[:idx], &header))
	assert.Equal(t, "hydra record help -- hydra demo --help", header["command"])
}
