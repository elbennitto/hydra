package asciinema

import (
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/record/directive"
)

// defaultLineDelaySeconds is applied to each output line unless a sleep directive overrides it.
const defaultLineDelaySeconds = 0.01

// HelpCastDocumentationCommand builds the header command field for help recordings.
func HelpCastDocumentationCommand(recordedHydraCommand string) string {
	return "hydra record cli -- " + recordedHydraCommand
}

type castEvent struct {
	time float64
	kind string
	data string
}

func linesToEvents(lines []string, kind string) []castEvent {
	var out []castEvent
	var pendingTime *float64
	lastLineHasEnding := true
	if len(lines) > 0 {
		lastLine := lines[len(lines)-1]
		lastLineHasEnding = strings.HasSuffix(lastLine, "\n") || strings.HasSuffix(lastLine, "\r")
	}
	for _, line := range lines {
		lineHasEnding := strings.HasSuffix(line, "\n") || strings.HasSuffix(line, "\r")
		segments, trailingDelay, trailingMarkers := splitLineByDirectives(line)
		for _, segment := range segments {
			if segment.delayBefore != nil {
				pendingTime = segment.delayBefore
			}
			for _, markerLabel := range segment.markersBefore {
				t := 0.0
				if pendingTime != nil {
					t = *pendingTime
					pendingTime = nil
				}
				out = append(out, castEvent{time: t, kind: "m", data: markerLabel})
			}
			if segment.text == "" {
				if segment.delayBefore == nil && len(segment.markersBefore) == 0 && !(trailingDelay != nil && !lineHasEnding) {
					t := defaultLineDelaySeconds
					if pendingTime != nil {
						t = *pendingTime
						pendingTime = nil
					}
					out = append(out, castEvent{time: t, kind: kind, data: ""})
				}
				continue
			}
			if isOnlyLineEnding(segment.text) && len(out) > 0 && shouldFoldTypedEnterLineEnding(out[len(out)-1], kind) {
				out[len(out)-1].data = foldTypedEnterLineEnding(out[len(out)-1].data, segment.text)
				continue
			}
			if isExitCodeOnlyLine(segment.text) {
				continue
			}
			t := defaultLineDelaySeconds
			if pendingTime != nil {
				t = *pendingTime
				pendingTime = nil
			}
			out = append(out, castEvent{time: t, kind: kind, data: normalizeCarriageReturnForEvent(segment.text)})
		}
		if trailingDelay != nil {
			pendingTime = trailingDelay
		}
		for _, markerLabel := range trailingMarkers {
			t := 0.0
			if pendingTime != nil {
				t = *pendingTime
				pendingTime = nil
			}
			out = append(out, castEvent{time: t, kind: "m", data: markerLabel})
		}
	}
	if pendingTime != nil && !lastLineHasEnding {
		// Preserve a trailing sleep directive when the stream ends mid-line.
		out = append(out, castEvent{time: *pendingTime, kind: kind, data: ""})
	}
	return out
}

type sleepSplitSegment struct {
	text          string
	delayBefore   *float64
	markersBefore []string
}

func splitLineByDirectives(line string) ([]sleepSplitSegment, *float64, []string) {
	segments := make([]sleepSplitSegment, 0, 2)
	remaining := line
	var nextDelay *float64
	nextMarkers := make([]string, 0, 1)

	for {
		sleepStart, sleepEnd, sleepSecs, hasSleep := directive.FindSleepDirective(remaining)
		markerStart, markerEnd, markerLabel, hasMarker := directive.FindMarkerDirective(remaining)

		if !hasSleep && !hasMarker {
			if remaining != "" {
				markers := append([]string(nil), nextMarkers...)
				segments = append(segments, sleepSplitSegment{text: remaining, delayBefore: nextDelay, markersBefore: markers})
				nextDelay = nil
				nextMarkers = nextMarkers[:0]
			} else if len(segments) == 0 {
				// Keep legacy behavior: directive-only lines emit a placeholder event
				// with default timing, while the directive delay applies to the next output.
				if nextDelay != nil && len(nextMarkers) == 0 {
					segments = append(segments, sleepSplitSegment{text: "", delayBefore: nil})
				}
			}
			break
		}

		start := 0
		end := 0
		isSleep := false
		if hasSleep && (!hasMarker || sleepStart <= markerStart) {
			start = sleepStart
			end = sleepEnd
			isSleep = true
		} else {
			start = markerStart
			end = markerEnd
		}

		before := remaining[:start]
		afterDirective := remaining[end:]
		if before != "" || nextDelay != nil || len(nextMarkers) > 0 {
			markers := append([]string(nil), nextMarkers...)
			segments = append(segments, sleepSplitSegment{text: before, delayBefore: nextDelay, markersBefore: markers})
			nextDelay = nil
			nextMarkers = nextMarkers[:0]
		}

		if isSleep {
			d := sleepSecs
			nextDelay = &d
		} else {
			nextMarkers = append(nextMarkers, markerLabel)
		}
		remaining = afterDirective
	}

	return segments, nextDelay, append([]string(nil), nextMarkers...)
}

func isOnlyLineEnding(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '\r' && s[i] != '\n' {
			return false
		}
	}
	return true
}

func normalizeCarriageReturnForEvent(line string) string {
	if strings.HasSuffix(line, "\r") && !strings.HasSuffix(line, "\r\n") {
		body := strings.TrimSuffix(line, "\r")
		if body == "\x1b[0m" {
			return body
		}
		return "\r" + body
	}
	return line
}

func shouldFoldTypedEnterLineEnding(prev castEvent, kind string) bool {
	if prev.kind != kind {
		return false
	}
	if strings.Contains(prev.data, "\n") {
		return false
	}
	if !strings.HasPrefix(prev.data, "\r") {
		return false
	}
	// Typed enter often yields a carriage return followed by ANSI reset output.
	// Fold a subsequent pure line-ending segment into this event.
	return strings.Contains(prev.data, "\x1b[")
}

func foldTypedEnterLineEnding(prev, lineEnding string) string {
	if strings.HasPrefix(prev, "\r") {
		return strings.Replace(prev, "\r", lineEnding, 1)
	}
	return prev + lineEnding
}

func stripSleepDirective(line string) (string, float64, bool) {
	start, end, secs, ok := directive.FindSleepDirective(line)
	if !ok {
		return line, 0, false
	}
	before := line[:start]
	if strings.TrimSpace(before) == "" {
		return "", secs, true
	}

	afterDirective := line[end:]
	switch {
	case strings.HasPrefix(afterDirective, "\r\n"):
		if !strings.HasSuffix(before, "\r\n") {
			before += "\r\n"
		}
	case strings.HasPrefix(afterDirective, "\n"):
		if !strings.HasSuffix(before, "\n") {
			before += "\n"
		}
	case strings.HasPrefix(afterDirective, "\r"):
		if !strings.HasSuffix(before, "\r") {
			before += "\r"
		}
	}

	return before, secs, true
}

// splitTerminalLines splits on line boundaries and keeps original line endings (\r\n, \r, or \n).
func splitTerminalLines(data string) []string {
	if data == "" {
		return nil
	}
	var lines []string
	start := 0
	for start < len(data) {
		contentEnd, ending := findLineEndingAt(data, start)
		if ending == "" {
			lines = append(lines, data[start:])
			break
		}
		lines = append(lines, data[start:contentEnd]+ending)
		start = contentEnd + len(ending)
	}
	return lines
}

func findLineEndingAt(data string, start int) (contentEnd int, ending string) {
	for i := start; i < len(data); i++ {
		switch data[i] {
		case '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				return i, "\r\n"
			}
			return i, "\r"
		case '\n':
			return i, "\n"
		}
	}
	return len(data), ""
}

func detectPrimaryLineEnding(data string) string {
	if strings.Contains(data, "\r\n") {
		return "\r\n"
	}
	if strings.Contains(data, "\r") {
		return "\r"
	}
	return "\n"
}

func ensureTrailingLineEnding(data string) string {
	_, ending := findLineEndingAt(data, 0)
	if ending != "" {
		return data
	}
	return data + detectPrimaryLineEnding(data)
}
