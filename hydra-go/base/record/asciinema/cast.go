package asciinema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
	"hydra-gitops.org/hydra/hydra-go/base/record/expect"
)

// CastStreamBuilder receives terminal output chunks and converts them into an asciicast stream.
type CastStreamBuilder struct {
	documentationCommand string
	chunks               []string
}

func NewCastStreamBuilder(documentationCommand string) *CastStreamBuilder {
	return &CastStreamBuilder{documentationCommand: documentationCommand}
}

func (c *CastStreamBuilder) WriteString(chunk string) {
	if chunk == "" {
		return
	}
	c.chunks = append(c.chunks, chunk)
}

func (c *CastStreamBuilder) WriteBytes(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	c.chunks = append(c.chunks, string(chunk))
}

func (c *CastStreamBuilder) Build() ([]byte, error) {
	cleanOutput := StripRecordingControlSequences(strings.Join(c.chunks, ""))
	events := linesToEvents(splitTerminalLines(cleanOutput), "o")
	if c.documentationCommand != "" {
		events = normalizeDocumentationCastEvents(events)
	}
	return buildCastStream(events, c.documentationCommand)
}

// CastFileSink writes rendered cast bytes to disk.
type CastFileSink struct{}

func (CastFileSink) Write(path string, cast []byte) error {
	if err := os.WriteFile(path, cast, 0o644); err != nil {
		return fmt.Errorf("write cast file: %w", err)
	}
	return nil
}

// defaultCastCols and defaultCastRows define the PTY/cast geometry.
const defaultCastCols = 120
const defaultCastRows = 36

// captureScriptOutput runs scriptPath in a bash PTY and returns all terminal output.
// When mirror is non-nil, captured bytes are also written there (typically os.Stdout).
func captureScriptOutput(scriptPath string, env []string, mirror io.Writer) ([]byte, error) {
	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Env = env
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: defaultCastRows,
		Cols: defaultCastCols,
	})
	if err != nil {
		return nil, fmt.Errorf("start bash with pty: %w", err)
	}
	defer ptmx.Close()

	var output bytes.Buffer
	dest := io.Writer(&output)
	var filteredMirror *RecordingFilterWriter
	if mirror != nil {
		filteredMirror = NewRecordingFilterWriter(mirror)
		dest = io.MultiWriter(&output, filteredMirror)
	}

	copyDone := make(chan struct{})
	var copyErr error
	go func() {
		defer close(copyDone)
		_, copyErr = copyPTYWithQueryResponses(ptmx, dest)
	}()

	if err := cmd.Wait(); err != nil {
		<-copyDone
		if filteredMirror != nil {
			_ = filteredMirror.Flush()
		}
		return nil, fmt.Errorf("run recording script: %w", err)
	}
	<-copyDone
	if copyErr != nil && !isPTYReadClosed(copyErr) {
		if filteredMirror != nil {
			_ = filteredMirror.Flush()
		}
		return nil, fmt.Errorf("read pty output: %w", copyErr)
	}
	if filteredMirror != nil {
		_ = filteredMirror.Flush()
	}
	return output.Bytes(), nil
}

// writeRawCast writes the final documentation asciicast v3 file directly.
func writeRawCast(path string, ptyOutput []byte, documentationCommand string) error {
	builder := NewCastStreamBuilder(documentationCommand)
	builder.WriteBytes(ptyOutput)
	stream, err := builder.Build()
	if err != nil {
		return err
	}

	return CastFileSink{}.Write(path, stream)
}

func buildCastStream(events []castEvent, documentationCommand string) ([]byte, error) {
	header := map[string]any{
		"version": 3,
		"term": map[string]any{
			"cols": defaultCastCols,
			"rows": defaultCastRows,
			"type": expect.RecordingTerm,
		},
		"env": map[string]any{
			"SHELL": "/bin/bash",
		},
	}
	if documentationCommand != "" {
		header["command"] = documentationCommand
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("encode cast header: %w", err)
	}

	var out bytes.Buffer
	out.Write(headerJSON)
	out.WriteByte('\n')
	for _, event := range events {
		eventJSON, err := json.Marshal([]any{event.time, event.kind, event.data})
		if err != nil {
			return nil, fmt.Errorf("encode cast event: %w", err)
		}
		out.Write(eventJSON)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

// WriteCast writes a documentation asciicast v3 file from captured PTY output.
func WriteCast(path string, ptyOutput []byte, documentationCommand string) error {
	return writeRawCast(path, ptyOutput, documentationCommand)
}

func normalizeDocumentationCastEvents(events []castEvent) []castEvent {
	if len(events) == 0 {
		return nil
	}

	out := make([]castEvent, 0, len(events))
	pendingTime := 0.0
	hasPendingTime := false
	for i, event := range events {
		if hasPendingTime {
			event.time = pendingTime
			hasPendingTime = false
		}

		if i > 0 &&
			isOnlyLineEnding(event.data) &&
			event.time != defaultLineDelaySeconds &&
			strings.HasSuffix(events[i-1].data, "\n") {
			out = append(out, castEvent{
				time: defaultLineDelaySeconds,
				kind: event.kind,
				data: "",
			})
			pendingTime = event.time
			hasPendingTime = true
			continue
		}

		out = append(out, event)
	}
	if hasPendingTime {
		out = append(out, castEvent{time: pendingTime, kind: "o", data: ""})
	}
	return out
}
