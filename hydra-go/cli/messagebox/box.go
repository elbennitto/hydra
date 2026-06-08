package messagebox

import (
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderBox(title, text string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Hint"
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	content := lipgloss.NewStyle().
		Padding(0, 1).
		Render(title + "\n" + strings.TrimSpace(text))
	plain := boxStyle.Render(content)
	return colorize(plain)
}

func Render(title, text string) (string, error) {
	return renderBox(title, text) + "\n", nil
}

func Print(w io.Writer, title, text string) error {
	_, err := io.WriteString(w, renderBox(title, text)+"\n")
	return err
}

func colorize(box string) string {
	const (
		reset      = "\x1b[0m"
		border     = "\x1b[38;5;228m"
		titleStyle = "\x1b[1;38;5;16;48;5;228m"
		bodyStyle  = "\x1b[38;5;231m"
	)

	lines := strings.Split(box, "\n")
	titleStyled := false
	for i, line := range lines {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "╭") || strings.HasPrefix(line, "╰"):
			lines[i] = border + line + reset
		case strings.HasPrefix(line, "│"):
			style := bodyStyle
			if !titleStyled {
				style = titleStyle
				titleStyled = true
			}
			lines[i] = colorizeContentLine(line, border, style, reset)
		default:
			lines[i] = border + line + reset
		}
	}
	return strings.Join(lines, "\n")
}

func colorizeContentLine(line, border, content, reset string) string {
	runes := []rune(line)
	if len(runes) < 2 {
		return border + line + reset
	}
	middle := string(runes[1 : len(runes)-1])
	return border + string(runes[0]) + reset + content + middle + reset + border + string(runes[len(runes)-1]) + reset
}
