package directive

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const prefix = "<<hydra "

var sleepDirectivePattern = regexp.MustCompile(`<<hydra sleep ([0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)>>`)
var markerDirectivePattern = regexp.MustCompile(`<<hydra marker ([^\r\n>]+)>>`)

// SleepLine returns a terminal marker that sets the timestamp for the next cast event.
func SleepLine(seconds float64) string {
	return fmt.Sprintf("%ssleep %s>>", prefix, formatSeconds(seconds))
}

// SleepInline returns an inline terminal marker that sets the timestamp for the next cast event.
func SleepInline(seconds float64) string {
	return SleepLine(seconds)
}

// WriteSleep writes a sleep directive to w (for terminal output during recordings).
func WriteSleep(w io.Writer, seconds float64) error {
	_, err := io.WriteString(w, SleepLine(seconds))
	return err
}

// MarkerLine returns a terminal marker that creates an asciinema timeline marker event.
func MarkerLine(label string) string {
	trimmed := strings.TrimSpace(label)
	trimmed = strings.ReplaceAll(trimmed, "\r", " ")
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	return fmt.Sprintf("%smarker %s>>", prefix, trimmed)
}

// ParseMarkerLine reports whether line contains a "<<hydra marker <label>>" directive.
func ParseMarkerLine(line string) (label string, ok bool) {
	_, _, value, found := FindMarkerDirective(line)
	if !found {
		return "", false
	}
	return value, true
}

// FindMarkerDirective returns the first marker directive match within s.
func FindMarkerDirective(s string) (start int, end int, label string, ok bool) {
	m := markerDirectivePattern.FindStringSubmatchIndex(s)
	if len(m) != 4 {
		return 0, 0, "", false
	}
	value := strings.TrimSpace(s[m[2]:m[3]])
	if value == "" {
		return 0, 0, "", false
	}
	return m[0], m[1], value, true
}

// StripMarkerDirectives removes all marker directives from s.
func StripMarkerDirectives(s string) (stripped string, found bool) {
	found = markerDirectivePattern.MatchString(s)
	if !found {
		return s, false
	}
	return markerDirectivePattern.ReplaceAllString(s, ""), true
}

// ColorResetLine returns a terminal comment describing a color reset control.
func ColorResetLine() string {
	return prefix + "color reset>>"
}

// ParseSleepLine reports whether line contains a "<<hydra sleep <seconds>>" directive.
func ParseSleepLine(line string) (seconds float64, ok bool) {
	_, _, secs, found := FindSleepDirective(line)
	if !found {
		return 0, false
	}
	return secs, true
}

// FindSleepDirective returns the first sleep directive match within s.
func FindSleepDirective(s string) (start int, end int, seconds float64, ok bool) {
	m := sleepDirectivePattern.FindStringSubmatchIndex(s)
	if len(m) != 4 {
		return 0, 0, 0, false
	}
	secs, err := strconv.ParseFloat(s[m[2]:m[3]], 64)
	if err != nil || secs < 0 {
		return 0, 0, 0, false
	}
	return m[0], m[1], secs, true
}

// StripSleepDirectives removes all sleep directives from s.
func StripSleepDirectives(s string) (stripped string, found bool) {
	found = sleepDirectivePattern.MatchString(s)
	if !found {
		return s, false
	}
	return sleepDirectivePattern.ReplaceAllString(s, ""), true
}

// IsSleepDirectiveOnlyLine reports whether line contains only sleep directive markers and whitespace.
func IsSleepDirectiveOnlyLine(line string) bool {
	stripped, found := StripSleepDirectives(strings.TrimRight(line, "\r\n"))
	if !found {
		return false
	}
	return strings.TrimSpace(stripped) == ""
}

// IsMarkerDirectiveOnlyLine reports whether line contains only marker directive markers and whitespace.
func IsMarkerDirectiveOnlyLine(line string) bool {
	stripped, found := StripMarkerDirectives(strings.TrimRight(line, "\r\n"))
	if !found {
		return false
	}
	return strings.TrimSpace(stripped) == ""
}

func formatSeconds(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', -1, 64)
}
