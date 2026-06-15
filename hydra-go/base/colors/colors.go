package colors

import (
	"fmt"
	"strings"
)

type Color uint8

const (
	Reset          Color = 0
	Black          Color = 30
	Red            Color = 31
	Green          Color = 32
	Yellow         Color = 33
	Blue           Color = 34
	Magenta        Color = 35
	Cyan           Color = 36
	White          Color = 37
	LightGray      Color = 90
	LightRed       Color = 91
	LightGreen     Color = 92
	LightYellow    Color = 93
	LightBlue      Color = 94
	LightMagenta   Color = 95
	LightCyan      Color = 96
	LightWhite     Color = 97
	BlackBg        Color = 40
	RedBg          Color = 41
	GreenBg        Color = 42
	YellowBg       Color = 43
	BlueBg         Color = 44
	MagentaBg      Color = 45
	CyanBg         Color = 46
	WhiteBg        Color = 47
	LightGrayBg    Color = 100
	LightRedBg     Color = 101
	LightGreenBg   Color = 102
	LightYellowBg  Color = 103
	LightBlueBg    Color = 104
	LightMagentaBg Color = 105
	LightCyanBg    Color = 106
	LightWhiteBg   Color = 107
)

// String returns the ANSI escape code for the color
func (c Color) String() string {
	if c == 0 {
		return "\033[0m"
	}
	return fmt.Sprintf("\033[%dm", c)
}

// BoldWhite returns ANSI SGR for bold bright white foreground (1 + color 97), not dim gray (37).
func BoldWhite() string {
	return "\033[1;97m"
}

// BoldLightMagenta returns ANSI SGR for bold light magenta (bright lilac) foreground (1 + color 95).
func BoldLightMagenta() string {
	return "\033[1;95m"
}

// RecordingShellPS1 is the bash PS1 for asciicast help recordings (bold light
// magenta " $ ").
func RecordingShellPS1() string {
	return `\[\033[1;95m\] \$ \[\033[0m\]`
}

// RecordingShellPrompt returns the visible prompt for printf (same styling as
// RecordingShellPS1).
func RecordingShellPrompt() string {
	return BoldLightMagenta() + " $ " + Reset.String()
}

// RecordingShellCommand returns ANSI SGR for the typed hydra command (hellweiß).
func RecordingShellCommand() string {
	return BoldWhite()
}

// ColorDiff applies git-like colors to diff output
// Lines starting with '+++' are colored light white
// Lines starting with '---' are colored light white
// Lines starting with '+' are colored green
// Lines starting with '-' are colored red
// Lines starting with 'diff' are colored cyan (header)
// Lines starting with '@@' are colored cyan (hunk headers)
func ColorDiff(diff string) string {
	if diff == "" {
		return ""
	}

	lines := strings.Split(diff, "\n")
	var result strings.Builder

	for i, line := range lines {
		if strings.HasPrefix(line, "+++") {
			result.WriteString(LightWhite.String())
			result.WriteString(line)
			result.WriteString(Reset.String())
		} else if strings.HasPrefix(line, "---") {
			result.WriteString(LightWhite.String())
			result.WriteString(line)
			result.WriteString(Reset.String())
		} else if strings.HasPrefix(line, "+") {
			result.WriteString(Green.String())
			result.WriteString(line)
			result.WriteString(Reset.String())
		} else if strings.HasPrefix(line, "-") {
			result.WriteString(Red.String())
			result.WriteString(line)
			result.WriteString(Reset.String())
		} else if strings.HasPrefix(line, "diff") {
			result.WriteString(Cyan.String())
			result.WriteString(line)
			result.WriteString(Reset.String())
		} else if strings.HasPrefix(line, "@@") {
			result.WriteString(Cyan.String())
			result.WriteString(line)
			result.WriteString(Reset.String())
		} else {
			result.WriteString(line)
		}

		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}
