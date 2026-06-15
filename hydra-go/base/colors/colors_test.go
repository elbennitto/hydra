package colors

import (
	"strings"
	"testing"
)

func TestBoldLightMagenta(t *testing.T) {
	if got, want := BoldLightMagenta(), "\033[1;95m"; got != want {
		t.Errorf("BoldLightMagenta() = %q, want %q", got, want)
	}
}

func TestBoldWhite(t *testing.T) {
	if got, want := BoldWhite(), "\033[1;97m"; got != want {
		t.Errorf("BoldWhite() = %q, want %q", got, want)
	}
}

func TestRecordingShellPS1(t *testing.T) {
	if got, want := RecordingShellPS1(), `\[\033[1;95m\] \$ \[\033[0m\]`; got != want {
		t.Errorf("RecordingShellPS1() = %q, want %q", got, want)
	}
}

func TestRecordingShellPrompt(t *testing.T) {
	if got, want := RecordingShellPrompt(), BoldLightMagenta()+" $ "+Reset.String(); got != want {
		t.Errorf("RecordingShellPrompt() = %q, want %q", got, want)
	}
}

func TestRecordingShellCommand(t *testing.T) {
	if got, want := RecordingShellCommand(), BoldWhite(); got != want {
		t.Errorf("RecordingShellCommand() = %q, want %q", got, want)
	}
}

func TestColor_String(t *testing.T) {
	tests := []struct {
		name     string
		color    Color
		expected string
	}{
		{"Reset", Reset, "\033[0m"},
		{"Red", Red, "\033[31m"},
		{"Green", Green, "\033[32m"},
		{"Yellow", Yellow, "\033[33m"},
		{"Blue", Blue, "\033[34m"},
		{"Cyan", Cyan, "\033[36m"},
		{"LightWhite", LightWhite, "\033[97m"},
		{"RedBg", RedBg, "\033[41m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.color.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestColorDiff_EmptyString(t *testing.T) {
	result := ColorDiff("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestColorDiff_AddedLines(t *testing.T) {
	diff := "+added line"
	result := ColorDiff(diff)

	if !strings.Contains(result, Green.String()) {
		t.Error("added line should contain green color")
	}
	if !strings.Contains(result, Reset.String()) {
		t.Error("added line should contain reset")
	}
	if !strings.Contains(result, "added line") {
		t.Error("result should contain the line content")
	}
}

func TestColorDiff_RemovedLines(t *testing.T) {
	diff := "-removed line"
	result := ColorDiff(diff)

	if !strings.Contains(result, Red.String()) {
		t.Error("removed line should contain red color")
	}
	if !strings.Contains(result, Reset.String()) {
		t.Error("removed line should contain reset")
	}
}

func TestColorDiff_HeaderLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"diff header", "diff --git a/file b/file"},
		{"hunk header", "@@ -1,3 +1,4 @@"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ColorDiff(tt.input)
			if !strings.Contains(result, Cyan.String()) {
				t.Errorf("%s should contain cyan color", tt.name)
			}
		})
	}
}

func TestColorDiff_FileHeaders(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"plus file", "+++ b/file.txt"},
		{"minus file", "--- a/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ColorDiff(tt.input)
			if !strings.Contains(result, LightWhite.String()) {
				t.Errorf("%s should contain light white color", tt.name)
			}
		})
	}
}

func TestColorDiff_UnchangedLines(t *testing.T) {
	diff := " unchanged line"
	result := ColorDiff(diff)

	// Unchanged lines should not have color codes (except potentially from other lines)
	if strings.Contains(result, Green.String()) || strings.Contains(result, Red.String()) {
		t.Error("unchanged line should not have green or red color")
	}
	if !strings.Contains(result, "unchanged line") {
		t.Error("result should contain the unchanged line")
	}
}

func TestColorDiff_MultipleLines(t *testing.T) {
	diff := `diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 unchanged
-removed
+added
 also unchanged`

	result := ColorDiff(diff)

	// Check that all expected colors are present
	if !strings.Contains(result, Cyan.String()) {
		t.Error("diff header should be cyan")
	}
	if !strings.Contains(result, LightWhite.String()) {
		t.Error("file headers should be light white")
	}
	if !strings.Contains(result, Green.String()) {
		t.Error("added lines should be green")
	}
	if !strings.Contains(result, Red.String()) {
		t.Error("removed lines should be red")
	}

	// Check line count is preserved
	inputLines := strings.Count(diff, "\n")
	outputLines := strings.Count(result, "\n")
	if inputLines != outputLines {
		t.Errorf("line count mismatch: input %d, output %d", inputLines, outputLines)
	}
}

func TestColorDiff_PreservesContent(t *testing.T) {
	diff := "+added line with special chars: <>&\"\n-removed line"
	result := ColorDiff(diff)

	// Content should be preserved (ignoring color codes)
	if !strings.Contains(result, "added line with special chars: <>&\"") {
		t.Error("special characters should be preserved")
	}
	if !strings.Contains(result, "removed line") {
		t.Error("removed line content should be preserved")
	}
}
