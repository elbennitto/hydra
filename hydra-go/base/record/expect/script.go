package expect

import (
	"fmt"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/base/record/directive"
)

// BashScript builds a bash script with expect-style steps for non-interactive
// asciicast recordings (prompt setup, command execution, optional waits).
type BashScript struct {
	lines []string
}

// NewBashScript returns an empty script.
func NewBashScript() *BashScript {
	return &BashScript{}
}

// SetupGenericPrompt configures the colored recording shell (alias for SetupRecordingShell).
func (s *BashScript) SetupGenericPrompt() {
	s.SetupRecordingShell()
}

// RecordingTerm is exported in the recording shell so CLI colors resolve correctly.
const RecordingTerm = "xterm-256color"

// SetupRecordingShell defines colored prompt variables and PS1 for follow-up prompts.
func (s *BashScript) SetupRecordingShell() {
	ps1 := colors.RecordingShellPS1()
	s.lines = append(s.lines,
		`export TERM=`+shellQuote(RecordingTerm),
		`_HYDRA_PROMPT=$'`+escapeBashANSI(colors.RecordingShellPrompt())+`'`,
		`_HYDRA_CMD=$'`+escapeBashANSI(colors.RecordingShellCommand())+`'`,
		`_HYDRA_RST=$'`+escapeBashANSI(colors.Reset.String())+`'`,
		`export PS1='`+ps1+`'`,
		`export PS2='`+ps1+`'`,
		`unset PROMPT_COMMAND`,
	)
}

// ShowInitialPrompt prints the purple " $ " prompt (faked shell, no newline).
func (s *BashScript) ShowInitialPrompt() {
	s.lines = append(s.lines, `printf '%b' "$_HYDRA_PROMPT"`)
}

// ShowAndRunCommand prints the command in dunkel weiß after the prompt, then executes execLine.
// displayCommand is the full visible text (e.g. "hydra local template --help").
func (s *BashScript) ShowAndRunCommand(displayCommand, execLine string) {
	s.lines = append(s.lines,
		`printf '%b%s%b\n' "$_HYDRA_CMD" `+shellQuote(displayCommand)+` "$_HYDRA_RST"`,
	)
	s.lines = append(s.lines, execLine)
}

// SendLine appends a shell command line.
func (s *BashScript) SendLine(line string) {
	s.lines = append(s.lines, line)
}

// WriteSleepDirective prints "<<hydra sleep <seconds>>" to the terminal for cast timing.
func (s *BashScript) WriteSleepDirective(seconds float64) {
	s.lines = append(s.lines, `printf `+shellQuote(directive.SleepLine(seconds)))
}

func escapeBashANSI(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// ExpectWait appends a bash helper call that waits until the given substring
// appears on the PTY (used when driving a live session; no-op in plain scripts
// unless _hydra_expect_wait is defined).
func (s *BashScript) ExpectWait(substr string, timeoutSeconds int) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	quoted := shellQuote(substr)
	s.lines = append(s.lines, fmt.Sprintf(
		"if declare -F _hydra_expect_wait >/dev/null 2>&1; then _hydra_expect_wait %s %d; fi",
		quoted, timeoutSeconds,
	))
}

// Render returns the full bash script body.
func (s *BashScript) Render() string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n")
	for _, line := range s.lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("exit 0\n")
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
