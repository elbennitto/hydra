package record

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/base/record/asciinema"
	"hydra-gitops.org/hydra/hydra-go/base/record/directive"
	"hydra-gitops.org/hydra/hydra-go/base/record/expect"
)

const (
	recordFileIdleWait     = 150 * time.Millisecond
	recordFileStatusWait   = 45 * time.Second
	recordFileSessionCols  = 120
	recordFileSessionRows  = 36
	recordFileTypeCharWait = 0.2
	recordFileVirtualRoot  = "/home/hydra"
)

type RecordOptions struct {
	HydraBin        string
	HydraGlobalArgs []string
	SpecDir         string
	OutputDir       string
	OutputPath      string
	MirrorOutput    bool
}

type RecordFileOutputKind string

const (
	RecordFileOutputText    RecordFileOutputKind = "text"
	RecordFileOutputControl RecordFileOutputKind = "control"
)

type RecordFileOutput struct {
	Kind  RecordFileOutputKind
	Value string
}

type execStepSemantics struct {
	showCommand bool
	showOutput  bool
	slow        bool
}

type historyState struct {
	history       string
	stdoutHistory string
	stderrHistory string
	castHistory   string
	lines         []string
	env           map[string]string
	virtualPath   string
	record        string
	rootDir       string
	currentDir    string
}

var logIdRecordFile = log.Hydra().Child("core").Child("record")

func RecordOne(file string, opts RecordOptions) error {
	return newRecordFileOrchestrator(opts).RecordOne(file)
}

func RecordAll(opts RecordOptions) error {
	return newRecordFileOrchestrator(opts).RecordAll()
}

type recordFileOrchestrator struct {
	opts   RecordOptions
	runner recordFileRunner
	sink   asciinema.CastFileSink
}

func newRecordFileOrchestrator(opts RecordOptions) recordFileOrchestrator {
	return recordFileOrchestrator{
		opts:   opts,
		runner: recordFileRunner{},
		sink:   asciinema.CastFileSink{},
	}
}

func (a recordFileOrchestrator) RecordOne(file string) error {
	spec, err := Resolve(a.opts.SpecDir, file)
	if err != nil {
		return err
	}
	return a.recordSpec(spec)
}

func (a recordFileOrchestrator) RecordAll() error {
	specs, err := Discover(a.opts.SpecDir)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := a.recordSpec(spec); err != nil {
			return err
		}
	}
	return nil
}

func (a recordFileOrchestrator) recordSpec(spec RecordSpec) error {
	outPath := strings.TrimSpace(a.opts.OutputPath)
	if outPath == "" {
		outPath = filepath.Join(a.opts.OutputDir, spec.Slug+".cast")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create record file output dir: %w", err)
	}

	raw, err := executeRecordFileSpec(spec, a.opts)
	if err != nil {
		return err
	}

	builder := asciinema.NewCastStreamBuilder("hydra record file " + spec.Slug)
	builder.WriteBytes(raw)
	stream, err := builder.Build()
	if err != nil {
		return fmt.Errorf("build record file cast stream: %w", err)
	}
	if err := a.sink.Write(outPath, stream); err != nil {
		return fmt.Errorf("write record file cast: %w", err)
	}
	return nil
}

type recordFileRunner struct{}

func (recordFileRunner) Run(spec RecordSpec, _ RecordOptions) ([]RecordFileOutput, error) {
	return buildRecordFileOutputs(spec.File.Steps), nil
}

func executeRecordFileSpec(spec RecordSpec, opts RecordOptions) ([]byte, error) {
	if strings.TrimSpace(opts.HydraBin) == "" {
		opts.HydraBin = "/bin/echo"
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("output dir is empty")
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "hydra-record-file-"+strings.ReplaceAll(spec.Slug, "/", "-")+"-")
	if err != nil {
		return nil, fmt.Errorf("create record file temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	virtualPath := recordFileVirtualRoot

	var mirror io.Writer
	if opts.MirrorOutput {
		log.FlushProgressForStdout()
		mirror = newRecordFileMirror(os.Stdout, tmpDir, virtualPath)
	}

	session, err := startRecordFileShell(tmpDir, opts.HydraBin, opts.HydraGlobalArgs, mirror)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	history := historyState{
		env:         envMapFromEnviron(os.Environ()),
		virtualPath: virtualPath,
		record:      spec.Slug,
		rootDir:     tmpDir,
		currentDir:  tmpDir,
	}
	history.env["PWD"] = tmpDir
	history.env["HOME"] = tmpDir
	history.env["HYDRA_CONTEXT"] = filepath.Join(tmpDir, "group", "context")
	l := log.Default()

	for i, step := range spec.File.Steps {
		command := stepCommandForLog(step)
		semantics, _ := resolveExecSemantics(step)
		l.DebugLog(logIdRecordFile, "running record file step",
			log.String("record", spec.Slug),
			log.Int("stepIndex", i),
			log.String("type", step.Kind),
			log.Bool("input", semantics.showCommand),
			log.Bool("output", semantics.showOutput),
			log.Bool("slow", semantics.slow),
			log.String("command", command))
		if err := runStep(session, spec.Slug, virtualPath, step, i, &history); err != nil {
			return nil, fmt.Errorf("record file %q step %d (%s): %w", spec.Slug, i, step.Kind, err)
		}
	}

	raw := rewriteVirtualPath(session.Bytes(), tmpDir, virtualPath)
	raw = sanitizeRecordFileOutput(raw)
	return raw, nil
}

func buildRecordFileOutputs(steps []RecordStep) []RecordFileOutput {
	outputs := make([]RecordFileOutput, 0, 32)
	for _, step := range steps {
		switch step.Kind {
		case "run":
			semantics, err := resolveExecSemantics(step)
			if err != nil {
				continue
			}
			if !semantics.showCommand || !semantics.slow || strings.TrimSpace(step.Run) == "" {
				continue
			}
			runes := []rune(strings.TrimSpace(step.Run))
			for i, r := range runes {
				outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputText, Value: string(r)})
				if i == len(runes)-1 {
					outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputControl, Value: strings.TrimSpace(directive.SleepLine(*step.Speed))})
				} else {
					outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputControl, Value: strings.TrimSpace(directive.SleepLine(*step.Speed))})
				}
			}
			outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputControl, Value: "newline"})
		case "write":
			message := strings.TrimSpace(step.Write)
			if message == "" {
				continue
			}
			if step.Speed != nil {
				runes := []rune(message)
				for i, r := range runes {
					outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputText, Value: string(r)})
					if i == len(runes)-1 {
						outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputControl, Value: strings.TrimSpace(directive.SleepLine(*step.Speed))})
					} else {
						outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputControl, Value: strings.TrimSpace(directive.SleepLine(*step.Speed))})
					}
				}
				continue
			}
			outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputText, Value: message})
		case "prompt":
			outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputText, Value: colors.RecordingShellPrompt() + colors.RecordingShellCommand()})
		case "newline":
			outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputText, Value: "\r\n"})
		case "sleep":
			outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputControl, Value: strings.TrimSpace(directive.SleepLine(step.SleepSeconds))})
		case "color":
			if value, ok := renderRecordColor(step.Color); ok {
				outputs = append(outputs, RecordFileOutput{Kind: RecordFileOutputText, Value: value})
			}
		}
	}
	return outputs
}

func runStep(session *recordFileShell, recordSlug, virtualPath string, step RecordStep, index int, history *historyState) error {
	switch step.Kind {
	case "run":
		semantics, err := resolveExecSemantics(step)
		if err != nil {
			return err
		}
		expected := 0
		if step.ExpectedExitCode != nil {
			expected = *step.ExpectedExitCode
		}
		command := strings.TrimSpace(step.Run)
		if command == "" {
			return nil
		}

		if semantics.showCommand {
			display := command
			if semantics.slow {
				chunk := buildPromptTypingOutput(display, false, true, *step.Speed)
				if err := session.WriteOutput(chunk); err != nil {
					return err
				}
			} else {
				chunk := buildPromptLineOutput(display, true)
				if err := session.WriteOutput(chunk); err != nil {
					return err
				}
			}
		}

		stdout, stderr, capturedEnv, err := runCommandCaptureOutput(session, command, expected, index, false)
		if err != nil {
			return err
		}
		if err := syncShellEnv(session, history.env, capturedEnv, index); err != nil {
			return err
		}
		history.env = capturedEnv
		if history.rootDir != "" && history.virtualPath != "" {
			stdout = strings.ReplaceAll(stdout, history.rootDir, history.virtualPath)
			stderr = strings.ReplaceAll(stderr, history.rootDir, history.virtualPath)
		}
		combined := stdout + stderr
		history.history += combined
		history.stdoutHistory += stdout
		history.stderrHistory += stderr
		history.castHistory += combined
		history.lines = append(history.lines, splitVisibleLines(combined)...)

		if semantics.showOutput {
			normalized := normalizeRecordWriteMessage(combined, false)
			if err := session.WriteOutput(normalized); err != nil {
				return err
			}
		}
		return nil
	case "prompt":
		chunk := colors.RecordingShellPrompt() + colors.RecordingShellCommand()
		if err := session.WriteOutput(chunk); err != nil {
			return err
		}
		return nil
	case "newline":
		chunk := "\r\n"
		if err := session.WriteOutput(chunk); err != nil {
			return err
		}
		return nil
	case "env":
		for envIndex, entry := range step.Env {
			value, err := resolveEnvEntryValue(session, entry, index, envIndex, history)
			if err != nil {
				return err
			}
			history.env[entry.Name] = value
			if err := runHiddenCommandSilently(session, fmt.Sprintf("export %s=%s", entry.Name, shellQuote(value)), 0, index, true); err != nil {
				return err
			}
		}
		return nil
	case "cd":
		if err := changeHistoryDir(history, step.CD); err != nil {
			return err
		}
		if err := session.WriteOutput(buildPromptLineOutput("cd "+step.CD, true)); err != nil {
			return err
		}
		if err := runHiddenCommandSilently(session, "cd "+shellQuote(step.CD), 0, index, true); err != nil {
			return err
		}
		history.env["PWD"] = history.currentDir
		return nil
	case "sleep":
		if err := emitSleepDirective(session, step.SleepSeconds); err != nil {
			return err
		}
		return nil
	case "write":
		message := step.Write
		if strings.TrimSpace(message) == "" {
			return nil
		}
		text := message
		if step.Speed != nil {
			text = buildTypedOutput(message, *step.Speed)
		}
		normalized := normalizeRecordWriteMessage(text, false)
		if err := session.WriteOutput(normalized); err != nil {
			return err
		}
		visible := strings.ReplaceAll(normalized, "\r\n", "\n")
		history.history += visible
		history.stdoutHistory += visible
		history.lines = append(history.lines, splitVisibleLines(visible)...)
		history.virtualPath = virtualPath
		history.record = recordSlug
		return nil
	case "assert":
		if step.Assert.Stdout != "" {
			if err := evalAssertionExpr(step.Assert.Stdout, history.stdoutHistory, history.stdoutHistory, history.stderrHistory, history.env); err != nil {
				return err
			}
		}
		if step.Assert.Stderr != "" {
			if err := evalAssertionExpr(step.Assert.Stderr, history.stderrHistory, history.stdoutHistory, history.stderrHistory, history.env); err != nil {
				return err
			}
		}
		if step.Assert.Cast != "" {
			if err := evalAssertionExpr(step.Assert.Cast, history.castHistory, history.stdoutHistory, history.stderrHistory, history.env); err != nil {
				return err
			}
		}
		return nil
	case "color":
		value, ok := renderRecordColor(step.Color)
		if !ok {
			return nil
		}
		return session.WriteOutput(value)
	default:
		return fmt.Errorf("unsupported step type %q", step.Kind)
	}
}

func runCommandCaptureOutput(session *recordFileShell, command string, expectedExitCode int, index int, parentShell bool) (string, string, map[string]string, error) {
	stdoutFile := filepath.Join(session.controlDir, fmt.Sprintf("%04d.stdout", index+1))
	stderrFile := filepath.Join(session.controlDir, fmt.Sprintf("%04d.stderr", index+1))
	envFile := filepath.Join(session.controlDir, fmt.Sprintf("%04d.env", index+1))
	if err := os.RemoveAll(stdoutFile); err != nil {
		return "", "", nil, fmt.Errorf("prepare stdout capture file: %w", err)
	}
	if err := os.RemoveAll(stderrFile); err != nil {
		return "", "", nil, fmt.Errorf("prepare stderr capture file: %w", err)
	}
	if err := os.RemoveAll(envFile); err != nil {
		return "", "", nil, fmt.Errorf("prepare env capture file: %w", err)
	}

	captureCmd := fmt.Sprintf("(\n%s\n__hydra_record_status=$?\nenv -0 > %s\nexit $__hydra_record_status\n) > %s 2> %s", command, shellQuote(envFile), shellQuote(stdoutFile), shellQuote(stderrFile))
	if err := runHiddenCommandSilently(session, captureCmd, expectedExitCode, index, parentShell); err != nil {
		return "", "", nil, err
	}

	stdoutBytes, err := os.ReadFile(stdoutFile)
	if err != nil && !os.IsNotExist(err) {
		return "", "", nil, fmt.Errorf("read stdout capture file: %w", err)
	}
	stderrBytes, err := os.ReadFile(stderrFile)
	if err != nil && !os.IsNotExist(err) {
		return "", "", nil, fmt.Errorf("read stderr capture file: %w", err)
	}
	envBytes, err := os.ReadFile(envFile)
	if err != nil && !os.IsNotExist(err) {
		return "", "", nil, fmt.Errorf("read env capture file: %w", err)
	}

	stdout := strings.ReplaceAll(string(stdoutBytes), "\r\n", "\n")
	stderr := strings.ReplaceAll(string(stderrBytes), "\r\n", "\n")
	stdout = strings.ReplaceAll(stdout, "\r", "\n")
	stderr = strings.ReplaceAll(stderr, "\r", "\n")
	return stdout, stderr, envMapFromNullDelimited(envBytes), nil
}

func syncShellEnv(session *recordFileShell, before map[string]string, after map[string]string, index int) error {
	if len(before) == 0 {
		before = map[string]string{}
	}
	if len(after) == 0 {
		after = map[string]string{}
	}

	updates := make([]string, 0, len(after))
	for key, value := range after {
		if !isShellIdentifier(key) {
			continue
		}
		if current, ok := before[key]; ok && current == value {
			continue
		}
		updates = append(updates, fmt.Sprintf("export %s=%s", key, shellQuote(value)))
	}

	unsets := make([]string, 0, len(before))
	for key := range before {
		if !isShellIdentifier(key) {
			continue
		}
		if _, ok := after[key]; ok {
			continue
		}
		unsets = append(unsets, "unset "+key)
	}

	sort.Strings(updates)
	sort.Strings(unsets)
	if len(updates) == 0 && len(unsets) == 0 {
		return nil
	}

	lines := make([]string, 0, len(updates)+len(unsets))
	for _, update := range updates {
		lines = append(lines, update+" >/dev/null 2>&1 || true")
	}
	for _, unset := range unsets {
		lines = append(lines, unset+" >/dev/null 2>&1 || true")
	}
	return runHiddenCommandSilently(session, strings.Join(lines, "\n"), 0, index, true)
}

func appendCastHistoryChunk(history *historyState, chunk string) {
	if history == nil || chunk == "" {
		return
	}
	visible := visibleRecordOutputChunk(chunk)
	if visible == "" {
		return
	}
	visible = strings.ReplaceAll(visible, "\r\n", "\n")
	visible = strings.ReplaceAll(visible, "\r", "\n")
	history.castHistory += visible
}

func renderRecordColor(spec *RecordColor) (string, bool) {
	if spec == nil {
		return "", false
	}

	parts := make([]string, 0, 3)

	if spec.Reset {
		parts = append(parts, "0")
	}
	if spec.Bold {
		parts = append(parts, "1")
	}
	if spec.FG != "" {
		fg, ok := lookupRecordColorCode(spec.FG, false)
		if !ok {
			return "", false
		}
		parts = append(parts, fg)
	}
	if spec.BG != "" {
		bg, ok := lookupRecordColorCode(spec.BG, true)
		if !ok {
			return "", false
		}
		parts = append(parts, bg)
	}

	if len(parts) == 0 {
		return "", false
	}

	return "\x1b[" + strings.Join(parts, ";") + "m", true
}

func lookupRecordColorCode(name string, background bool) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")

	fgCodes := map[string]string{
		"black":        "30",
		"red":          "31",
		"green":        "32",
		"yellow":       "33",
		"blue":         "34",
		"magenta":      "35",
		"cyan":         "36",
		"white":        "37",
		"lightgray":    "90",
		"lightred":     "91",
		"lightgreen":   "92",
		"lightyellow":  "93",
		"lightblue":    "94",
		"lightmagenta": "95",
		"lightcyan":    "96",
		"lightwhite":   "97",
		"gray":         "90",
		"grey":         "90",
	}

	code, ok := fgCodes[normalized]
	if !ok {
		return "", false
	}
	if !background {
		return code, true
	}

	bgPairs := map[string]string{
		"30": "40",
		"31": "41",
		"32": "42",
		"33": "43",
		"34": "44",
		"35": "45",
		"36": "46",
		"37": "47",
		"90": "100",
		"91": "101",
		"92": "102",
		"93": "103",
		"94": "104",
		"95": "105",
		"96": "106",
		"97": "107",
	}

	bg, ok := bgPairs[code]
	return bg, ok
}

func resolveExecSemantics(step RecordStep) (execStepSemantics, error) {
	showCommand := true
	if step.Input != nil {
		showCommand = *step.Input
	}

	showOutput := true
	if step.Output != nil {
		showOutput = *step.Output
	}
	slow := step.Speed != nil && *step.Speed > 0
	return execStepSemantics{showCommand: showCommand, showOutput: showOutput, slow: slow}, nil
}

func stepCommandForLog(step RecordStep) string {
	switch step.Kind {
	case "run":
		return strings.TrimSpace(step.Run)
	case "write":
		return strings.TrimSpace(step.Write)
	case "cd":
		return strings.TrimSpace(step.CD)
	default:
		return ""
	}
}

func envMapFromEnviron(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, kv := range values {
		key, val, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		out[key] = val
	}
	return out
}

func envMapFromNullDelimited(data []byte) map[string]string {
	parts := bytes.Split(data, []byte{0})
	out := make(map[string]string, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		key, value, found := strings.Cut(string(part), "=")
		if !found || key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func isShellIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				return false
			}
			continue
		}
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func resolveEnvEntryValue(session *recordFileShell, entry RecordEnvEntry, stepIndex, envIndex int, history *historyState) (string, error) {
	if entry.Value != nil {
		return *entry.Value, nil
	}
	if entry.CEL != nil {
		return evalEnvCEL(*entry.CEL, history.env)
	}
	if entry.Exec != nil {
		outFile := filepath.Join(session.controlDir, fmt.Sprintf("%04d-%02d.env", stepIndex+1, envIndex+1))
		if err := os.RemoveAll(outFile); err != nil {
			return "", fmt.Errorf("prepare env exec output file: %w", err)
		}
		captureCmd := fmt.Sprintf("(%s) > %s", *entry.Exec, shellQuote(outFile))
		if err := runHiddenCommandSilently(session, captureCmd, 0, stepIndex*100+envIndex, true); err != nil {
			return "", err
		}
		data, err := os.ReadFile(outFile)
		if err != nil {
			return "", fmt.Errorf("read env exec output: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	return "", fmt.Errorf("env %q has no value/cel/exec", entry.Name)
}

func changeHistoryDir(history *historyState, requested string) error {
	target := filepath.Clean(filepath.Join(history.currentDir, requested))
	rel, err := filepath.Rel(history.rootDir, target)
	if err != nil {
		return fmt.Errorf("resolve cd target: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("cd %q would leave recording root", requested)
	}
	history.currentDir = target
	return nil
}

func buildPromptLineOutput(display string, appendTrailingNewline bool) string {
	line := colors.RecordingShellPrompt() + colors.RecordingShellCommand() + normalizeRecordWriteMessage(display, false)
	line += colors.Reset.String()
	if appendTrailingNewline {
		line += "\r\n"
	}
	return line
}

func buildTypedOutput(text string, speed float64) string {
	if speed <= 0 {
		return text
	}
	var b strings.Builder
	for _, r := range text {
		b.WriteRune(r)
		b.WriteString(inlineSleepMarker(speed))
	}
	return b.String()
}

func normalizeRecordWriteMessage(message string, appendTrailingNewline bool) string {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.ReplaceAll(message, "\r", "\n")
	if appendTrailingNewline && !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	return strings.ReplaceAll(message, "\n", "\r\n")
}

func runHiddenCommandSilently(session *recordFileShell, command string, expectedExitCode int, index int, parentShell bool) error {
	var (
		exitCode int
		err      error
	)
	if err := session.withOutputDiscarded(func() error {
		exitCode, err = runCommandWithStatusMode(session, command, index, parentShell)
		return err
	}); err != nil {
		return err
	}
	if exitCode != expectedExitCode {
		return fmt.Errorf("exit code %d, expected %d", exitCode, expectedExitCode)
	}
	return nil
}

func buildPromptTypingOutput(display string, leadingNewline bool, appendTrailingNewline bool, speed float64) string {
	if speed <= 0 {
		speed = recordFileTypeCharWait
	}
	var b strings.Builder
	if leadingNewline {
		b.WriteString("\r\n")
	}
	b.WriteString(colors.RecordingShellPrompt())
	b.WriteString(colors.RecordingShellCommand())
	display = strings.ReplaceAll(display, "\r\n", "\n")
	display = strings.ReplaceAll(display, "\r", "\n")
	for _, r := range display {
		if r == '\n' {
			b.WriteString("\r\n")
			b.WriteString(inlineSleepMarker(speed))
			continue
		}
		b.WriteRune(r)
		b.WriteString(inlineSleepMarker(speed))
	}
	b.WriteString(colors.Reset.String())
	if appendTrailingNewline {
		b.WriteString("\r\n")
		b.WriteString(inlineSleepMarker(speed))
	} else {
		b.WriteString(inlineSleepMarker(speed))
	}
	return b.String()
}

func inlineSleepMarker(seconds float64) string {
	return directive.SleepInline(seconds)
}

func buildPrintfCommand(text string) string {
	return fmt.Sprintf("printf '%%b' %s", bashCStyleQuote(text))
}

func emitSleepDirective(session *recordFileShell, seconds float64) error {
	return session.WriteOutput(directive.SleepInline(seconds))
}

func runHiddenCommand(session *recordFileShell, command string, index int) error {
	_, err := runCommandWithStatus(session, command, index)
	return err
}

func runCommandWithStatus(session *recordFileShell, command string, index int) (int, error) {
	return runCommandWithStatusMode(session, command, index, false)
}

func runCommandWithStatusMode(session *recordFileShell, command string, index int, parentShell bool) (int, error) {
	statusFile := filepath.Join(session.controlDir, fmt.Sprintf("%04d.status", index+1))
	if err := os.RemoveAll(statusFile); err != nil {
		return 0, fmt.Errorf("prepare status file: %w", err)
	}
	runner := "__hydra_record_run"
	if parentShell {
		runner = "__hydra_record_run_parent"
	}
	line := fmt.Sprintf("%s %s %s", runner, shellQuote(statusFile), shellQuote(command))
	if err := session.SendLine(line); err != nil {
		return 0, err
	}
	data, err := waitForStatusFile(statusFile, recordFileStatusWait)
	if err != nil {
		return 0, err
	}
	if err := session.WaitForIdle(recordFileIdleWait, recordFileStatusWait); err != nil {
		return 0, err
	}
	exitCode, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse exit code from %q: %w", statusFile, err)
	}
	return exitCode, nil
}

func waitForStatusFile(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	sawEmpty := false
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if strings.TrimSpace(string(data)) != "" {
				return data, nil
			}
			sawEmpty = true
			time.Sleep(25 * time.Millisecond)
		case os.IsNotExist(err):
			time.Sleep(25 * time.Millisecond)
		default:
			return nil, fmt.Errorf("read status file %q: %w", path, err)
		}
	}
	if sawEmpty {
		return nil, fmt.Errorf("timeout waiting for status file %q to contain exit code", path)
	}
	return nil, fmt.Errorf("timeout waiting for status file %q", path)
}

type recordFileShell struct {
	cmd         *exec.Cmd
	pty         *os.File
	tmpDir      string
	controlRoot string
	controlDir  string
	mirror      io.Writer
	mirrorMu    sync.RWMutex

	mu         sync.Mutex
	raw        bytes.Buffer
	discardRaw bool
	lastOutput time.Time
	readErr    error
	readerDone chan struct{}
}

type recordFileMirror struct {
	filter  *asciinema.RecordingFilterWriter
	visible *recordFileVisibleWriter
}

type recordFileVisibleWriter struct {
	dst     io.Writer
	actual  string
	virtual string
	mu      sync.Mutex
	pending string
	started bool
}

func startRecordFileShell(tmpDir, hydraBin string, hydraGlobalArgs []string, mirror io.Writer) (*recordFileShell, error) {
	controlRoot, err := os.MkdirTemp("", "hydra-record-control-")
	if err != nil {
		return nil, fmt.Errorf("create control root dir: %w", err)
	}

	controlDir := filepath.Join(controlRoot, ".hydra-record")
	if err := os.MkdirAll(controlDir, 0o755); err != nil {
		_ = os.RemoveAll(controlRoot)
		return nil, fmt.Errorf("create control dir: %w", err)
	}

	cmd := exec.Command("/bin/bash", "--noprofile", "--norc", "-i")
	cmd.Env = recordFileShellEnv()

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: recordFileSessionRows,
		Cols: recordFileSessionCols,
	})
	if err != nil {
		return nil, fmt.Errorf("start record file shell: %w", err)
	}

	s := &recordFileShell{
		cmd:         cmd,
		pty:         ptmx,
		tmpDir:      tmpDir,
		controlRoot: controlRoot,
		controlDir:  controlDir,
		mirror:      nil,
		lastOutput:  time.Now(),
		readerDone:  make(chan struct{}),
		discardRaw:  true,
	}
	go s.readLoop()

	for _, line := range []string{
		"stty -echo -echonl",
		"export TERM=" + shellQuote("xterm-256color"),
		"export HOME=" + shellQuote(tmpDir),
		"export HYDRA_CONTEXT=\"$HOME/group/context\"",
		"export PS1=''",
		"export PS0=''",
		"export PS2=''",
		"__hydra_record_status_file=''",
		"__hydra_record_direct_skip_status=''",
		"__hydra_record_post_direct=''",
		`__hydra_record_prompt_hook() { local __hydra_status=$?; if [[ -n "$__hydra_record_status_file" ]]; then if [[ "$__hydra_record_direct_skip_status" == "1" ]]; then __hydra_record_direct_skip_status=""; else printf '%s' "$__hydra_status" > "$__hydra_record_status_file"; __hydra_record_status_file=""; fi; fi; if [[ "$__hydra_record_post_direct" == "1" && -z "$__hydra_record_status_file" ]]; then stty -echo -echonl; export PS1=''; export PS0=''; export PS2=''; __hydra_record_post_direct=""; fi; }`,
		"PROMPT_COMMAND=__hydra_record_prompt_hook",
		"cd " + shellQuote(tmpDir),
		buildRecordFileHydraFunction(hydraBin, hydraGlobalArgs),
		`__hydra_record_run() { local status_file="$1"; local command="$2"; ( eval "$command" ); local status=$?; printf '%s' "$status" > "$status_file"; }`,
		`__hydra_record_run_parent() { local status_file="$1"; local command="$2"; eval "$command"; local status=$?; printf '%s' "$status" > "$status_file"; }`,
	} {
		if err := s.SendLine(line); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	if err := s.WaitForIdle(100*time.Millisecond, 2*time.Second); err != nil {
		_ = s.Close()
		return nil, err
	}

	s.mu.Lock()
	s.discardRaw = false
	s.mu.Unlock()

	s.mirrorMu.Lock()
	s.mirror = mirror
	s.mirrorMu.Unlock()

	return s, nil
}

func buildRecordFileHydraFunction(hydraBin string, hydraGlobalArgs []string) string {
	parts := []string{shellQuote(hydraBin)}
	for _, arg := range hydraGlobalArgs {
		parts = append(parts, shellQuote(arg))
	}
	return fmt.Sprintf("hydra() { %s%s \"$@\"; }", expect.RecordingHydraEnvPrefix(), strings.Join(parts, " "))
}

func recordFileShellEnv() []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+5)
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "TERM="):
			continue
		case strings.HasPrefix(kv, "PS1="):
			continue
		case strings.HasPrefix(kv, "PS0="):
			continue
		case strings.HasPrefix(kv, "PS2="):
			continue
		case strings.HasPrefix(kv, "PROMPT_COMMAND="):
			continue
		case strings.HasPrefix(kv, "BASH_ENV="):
			continue
		case strings.HasPrefix(kv, "ENV="):
			continue
		case strings.HasPrefix(kv, "VSCODE_SHELL_INTEGRATION="):
			continue
		default:
			out = append(out, kv)
		}
	}

	out = append(out,
		"TERM=xterm-256color",
		"PS1=",
		"PS0=",
		"PS2=",
		"PROMPT_COMMAND=",
	)

	return out
}

func (s *recordFileShell) readLoop() {
	defer close(s.readerDone)
	defer func() {
		s.mirrorMu.RLock()
		mirror := s.mirror
		s.mirrorMu.RUnlock()
		if flusher, ok := mirror.(interface{ Flush() error }); ok {
			_ = flusher.Flush()
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			s.mu.Lock()
			if !s.discardRaw {
				_, _ = s.raw.Write(chunk)
			}
			s.lastOutput = time.Now()
			s.mu.Unlock()
			s.mirrorMu.RLock()
			mirror := s.mirror
			s.mirrorMu.RUnlock()
			if mirror != nil {
				_, _ = mirror.Write(chunk)
			}
		}
		if err != nil {
			s.mu.Lock()
			s.readErr = err
			s.mu.Unlock()
			return
		}
	}
}

func (s *recordFileShell) withMirrorSuppressed(run func() error) error {
	s.mirrorMu.Lock()
	previous := s.mirror
	s.mirror = nil
	s.mirrorMu.Unlock()

	defer func() {
		s.mirrorMu.Lock()
		s.mirror = previous
		s.mirrorMu.Unlock()
	}()

	return run()
}

func (s *recordFileShell) withOutputDiscarded(run func() error) error {
	s.mirrorMu.Lock()
	previousMirror := s.mirror
	s.mirror = nil
	s.mirrorMu.Unlock()

	s.mu.Lock()
	previousDiscard := s.discardRaw
	s.discardRaw = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.discardRaw = previousDiscard
		s.mu.Unlock()

		s.mirrorMu.Lock()
		s.mirror = previousMirror
		s.mirrorMu.Unlock()
	}()

	return run()
}

func (s *recordFileShell) SendLine(line string) error {
	if _, err := io.WriteString(s.pty, line+"\n"); err != nil {
		return fmt.Errorf("write to record file shell: %w", err)
	}
	return nil
}

func (s *recordFileShell) WriteOutput(text string) error {
	if text == "" {
		return nil
	}

	chunk := []byte(text)

	s.mu.Lock()
	if !s.discardRaw {
		_, _ = s.raw.Write(chunk)
	}
	s.lastOutput = time.Now()
	s.mu.Unlock()

	s.mirrorMu.RLock()
	mirror := s.mirror
	s.mirrorMu.RUnlock()
	if mirror != nil {
		if _, err := mirror.Write(chunk); err != nil {
			return fmt.Errorf("write to record file mirror: %w", err)
		}
	}

	return nil
}

func newRecordFileMirror(dst io.Writer, actualPath, virtualPath string) *recordFileMirror {
	visible := &recordFileVisibleWriter{dst: dst, actual: actualPath, virtual: virtualPath}
	return &recordFileMirror{
		filter:  asciinema.NewRecordingFilterWriter(visible),
		visible: visible,
	}
}

func (m *recordFileMirror) Write(p []byte) (int, error) {
	return m.filter.Write(p)
}

func (m *recordFileMirror) Flush() error {
	if err := m.filter.Flush(); err != nil {
		return err
	}
	return m.visible.Flush()
}

func (w *recordFileVisibleWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	chunk := string(p)
	chunk = strings.ReplaceAll(chunk, "\x1b[?2004h", "")
	chunk = strings.ReplaceAll(chunk, "\x1b[?2004l", "")
	w.pending += chunk
	if err := w.writeReadyLinesLocked(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *recordFileVisibleWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeReadyLinesLocked(true)
}

func (w *recordFileVisibleWriter) writeReadyLinesLocked(flushAll bool) error {
	for {
		line, rest, ok := nextRecordFileLineChunk(w.pending)
		if !ok {
			break
		}
		w.pending = rest
		if err := w.writeLineLocked(line); err != nil {
			return err
		}
	}

	if flushAll && w.pending != "" {
		line := w.pending
		w.pending = ""
		if err := w.writeLineLocked(line); err != nil {
			return err
		}
	}

	return nil
}

func (w *recordFileVisibleWriter) writeLineLocked(line string) error {
	if w.actual != "" && w.virtual != "" {
		line = string(rewriteVirtualPath([]byte(line), w.actual, w.virtual))
	}
	line, _ = directive.StripSleepDirectives(line)
	normalized := strings.TrimRight(stripANSICodes(line), "\r\n")
	if shouldHideRecordFileLine(normalized) {
		return nil
	}
	if !w.started {
		if normalized == "" && !strings.HasSuffix(line, "\n") {
			return nil
		}
		w.started = true
	}
	if normalized == "" && !strings.HasSuffix(line, "\n") {
		return nil
	}
	_, err := io.WriteString(w.dst, line)
	return err
}

func (s *recordFileShell) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.raw.Bytes()...)
}

func (s *recordFileShell) endsWithLineEnding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.raw.Len() == 0 {
		return false
	}
	data := s.raw.Bytes()
	last := data[len(data)-1]
	return last == '\n' || last == '\r'
}

func (s *recordFileShell) WaitForIdle(idleFor, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		last := s.lastOutput
		err := s.readErr
		s.mu.Unlock()
		if err != nil && err != io.EOF && !strings.Contains(err.Error(), "input/output error") {
			return fmt.Errorf("record file shell read: %w", err)
		}
		if time.Since(last) >= idleFor {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for record file shell to become idle")
}

func (s *recordFileShell) Close() error {
	if s.pty != nil {
		_ = s.pty.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
	<-s.readerDone
	if s.controlRoot != "" {
		_ = os.RemoveAll(s.controlRoot)
	}
	return nil
}

func rewriteVirtualPath(data []byte, actualPath, virtualPath string) []byte {
	if actualPath == "" || virtualPath == "" {
		return append([]byte(nil), data...)
	}
	replaced := bytes.ReplaceAll(data, []byte(actualPath), []byte(virtualPath))
	return replaced
}

func sanitizeRecordFileOutput(data []byte) []byte {
	text := string(data)
	text = strings.ReplaceAll(text, "\x1b[?2004h\r\r\n", "")
	text = strings.ReplaceAll(text, "\x1b[?2004l\r\r\n", "")
	text = strings.ReplaceAll(text, "\x1b[?2004h\r\n", "")
	text = strings.ReplaceAll(text, "\x1b[?2004l\r\n", "")
	text = strings.ReplaceAll(text, "\x1b[?2004h", "")
	text = strings.ReplaceAll(text, "\x1b[?2004l", "")

	lines := splitRecordFileLines(text)
	out := make([]string, 0, len(lines))
	started := false
	for i, line := range lines {
		line, strippedWrapper := stripInternalRecordRunSuffixFromLine(line)

		normalized := strings.TrimRight(stripANSICodes(line), "\r\n")
		if strippedWrapper && strings.TrimSpace(normalized) == "$" && hasPromptCommandAhead(lines, i+1) {
			continue
		}
		if shouldHideRecordFileLineForSanitize(normalized) {
			continue
		}
		if !started {
			if normalized == "" {
				continue
			}
			started = true
		}
		if normalized == "" && !strings.HasSuffix(line, "\n") {
			continue
		}
		out = append(out, line)
	}
	out = dropDuplicatedEchoBeforePrompt(out)
	return []byte(strings.Join(out, ""))
}

func hasPromptCommandAhead(lines []string, start int) bool {
	for i := start; i < len(lines); i++ {
		candidate, _ := stripInternalRecordRunSuffixFromLine(lines[i])
		trimmed := strings.TrimSpace(strings.TrimRight(stripANSICodes(candidate), "\r\n"))
		if trimmed == "" {
			continue
		}
		_, ok := recordFilePromptCommand(candidate)
		return ok
	}
	return false
}

func stripInternalRecordRunSuffixFromLine(line string) (string, bool) {
	tokens := []string{"__hydra_record_run_parent ", "__hydra_record_run "}
	idx := -1
	for _, token := range tokens {
		i := strings.Index(line, token)
		if i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	if idx < 0 {
		return line, false
	}
	return line[:idx], true
}

func dropDuplicatedEchoBeforePrompt(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}

	result := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		echoCmd, ok := recordFileEchoedCommand(lines[i])
		if !ok {
			result = append(result, lines[i])
			continue
		}

		j := i + 1
		for j < len(lines) {
			trimmed := strings.TrimRight(stripANSICodes(lines[j]), "\r\n")
			if trimmed != "" {
				break
			}
			j++
		}

		if j < len(lines) {
			if promptCmd, ok := recordFilePromptCommand(lines[j]); ok && promptCmd == echoCmd {
				i = j - 1
				continue
			}
		}

		result = append(result, lines[i])
	}

	return result
}

func recordFileEchoedCommand(line string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimRight(stripANSICodes(line), "\r\n"))
	trimmed, _ = directive.StripSleepDirectives(trimmed)
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "$ ") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "$") {
		return "", false
	}
	if strings.Contains(trimmed, "bash-") {
		return "", false
	}
	if _, ok := directive.ParseSleepLine(trimmed); ok {
		return "", false
	}
	return trimmed, true
}

func recordFilePromptCommand(line string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimRight(stripANSICodes(line), "\r\n"))
	trimmed, _ = directive.StripSleepDirectives(trimmed)
	trimmed = strings.TrimSpace(trimmed)
	if !strings.HasPrefix(trimmed, "$ ") {
		return "", false
	}
	cmd := strings.TrimSpace(strings.TrimPrefix(trimmed, "$ "))
	if cmd == "" {
		return "", false
	}
	return cmd, true
}

func shouldHideRecordFileLineForSanitize(trimmed string) bool {
	leftTrimmed := strings.TrimLeft(stripANSICodes(trimmed), " \t\r")
	if _, ok := directive.ParseSleepLine(leftTrimmed); ok {
		// Keep sleep directives in the sanitized stream so cast timing can honor them.
		return false
	}
	return shouldHideRecordFileLine(trimmed)
}

func visibleRecordOutputChunk(text string) string {
	lines := splitRecordFileLines(text)
	var b strings.Builder
	for _, line := range lines {
		clean := stripANSICodes(line)
		clean, _ = directive.StripSleepDirectives(clean)
		trimmed := strings.TrimRight(clean, "\r\n")
		leftTrimmed := strings.TrimLeft(trimmed, " \t\r")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(leftTrimmed, "__hydra_record_run ") || strings.HasPrefix(leftTrimmed, "__hydra_record_run_parent ") {
			continue
		}
		if strings.HasPrefix(leftTrimmed, "$ __hydra_record_run ") || strings.HasPrefix(leftTrimmed, "$ __hydra_record_run_parent ") {
			continue
		}
		if shouldHideRecordFileLine(leftTrimmed) {
			continue
		}
		b.WriteString(clean)
	}
	return b.String()
}

func splitRecordFileLines(data string) []string {
	if data == "" {
		return nil
	}

	lines := make([]string, 0, strings.Count(data, "\n")+strings.Count(data, "\r")+1)
	for len(data) > 0 {
		line, rest, ok := nextRecordFileLineChunk(data)
		if !ok {
			lines = append(lines, data)
			break
		}
		lines = append(lines, line)
		data = rest
	}
	return lines
}

func nextRecordFileLineChunk(data string) (line string, rest string, ok bool) {
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			return data[:i+1], data[i+1:], true
		case '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				return data[:i+2], data[i+2:], true
			}
			return data[:i+1], data[i+1:], true
		}
	}
	return "", data, false
}

func isRecordFileVisibleStart(line string) bool {
	line = stripANSICodes(line)
	leftTrimmed := strings.TrimLeft(line, " \t\r")
	return strings.HasPrefix(leftTrimmed, "$ ") ||
		strings.HasPrefix(line, "╭") ||
		strings.HasPrefix(line, "│") ||
		strings.HasPrefix(line, "╰")
}

func shouldHideRecordFileLine(trimmed string) bool {
	trimmed = stripANSICodes(trimmed)
	leftTrimmed := strings.TrimLeft(trimmed, " \t\r")
	promptTrimmed := strings.TrimSpace(leftTrimmed)
	if directive.IsSleepDirectiveOnlyLine(leftTrimmed) {
		return true
	}
	switch {
	case strings.Contains(leftTrimmed, "$ stty -echo"):
		return true
	case strings.HasPrefix(promptTrimmed, "[") && strings.HasSuffix(promptTrimmed, "$"):
		return true
	case strings.HasPrefix(leftTrimmed, "stty -echo"):
		return true
	case strings.HasPrefix(leftTrimmed, "export TERM="):
		return true
	case strings.HasPrefix(leftTrimmed, "_HYDRA_"):
		return true
	case strings.HasPrefix(leftTrimmed, "unset PROMPT_COMMAND"):
		return true
	case strings.HasPrefix(leftTrimmed, "export PS1="):
		return true
	case strings.HasPrefix(leftTrimmed, "export PS0="):
		return true
	case strings.HasPrefix(leftTrimmed, "export PS2="):
		return true
	case strings.HasPrefix(leftTrimmed, "PROMPT_COMMAND=__hydra_record_prompt_hook"):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_status_file="):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_direct_skip_status="):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_post_direct="):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_prompt_hook()"):
		return true
	case strings.HasPrefix(leftTrimmed, "cd '"):
		return true
	case strings.HasPrefix(leftTrimmed, "hydra() {"):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_run()"):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_run_parent()"):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_run '"):
		return true
	case strings.HasPrefix(leftTrimmed, "__hydra_record_run_parent '"):
		return true
	case strings.HasPrefix(leftTrimmed, "$ __hydra_record_run '"):
		return true
	case strings.HasPrefix(leftTrimmed, "$ __hydra_record_run_parent '"):
		return true
	case strings.HasPrefix(leftTrimmed, "bash-"):
		return true
	case strings.Contains(leftTrimmed, "bash-5.3$"):
		return true
	default:
		return false
	}
}

func stripANSICodes(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\x1b' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			continue
		}
		next := s[i+1]
		switch next {
		case '[':
			j := i + 2
			for j < len(s) {
				c := s[j]
				if c >= 0x40 && c <= 0x7e {
					i = j
					break
				}
				j++
			}
			if j >= len(s) {
				i = len(s)
			}
		case ']':
			j := i + 2
			for j < len(s) {
				if s[j] == '\x07' {
					i = j
					break
				}
				if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
					i = j + 1
					break
				}
				j++
			}
			if j >= len(s) {
				i = len(s)
			}
		default:
			i++
		}
	}
	return b.String()
}

func splitVisibleLines(text string) []string {
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func bashCStyleQuote(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\x1b':
			b.WriteString(`\e`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\v':
			b.WriteString(`\v`)
		default:
			if r < 0x20 || r == 0x7f {
				b.WriteString(`\x`)
				b.WriteString(strings.ToUpper(strconv.FormatInt(int64(r), 16)))
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}
