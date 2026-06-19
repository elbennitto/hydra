package record

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
)

func TestLoadRecordSpec_NewDSL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	yaml := "name: Demo\n" +
		"steps:\n" +
		"  - type: prompt\n" +
		"  - write: echo hello\n" +
		"    speed: 0.2\n" +
		"  - color:\n" +
		"      fg: lightgreen\n" +
		"      bg: blue\n" +
		"      bold: true\n" +
		"  - color: reset\n" +
		"  - type: newline\n" +
		"  - run: printf 'hello\\n'\n" +
		"    expectedExitCode: 0\n" +
		"  - assert:\n" +
		"      cast: plain(cast).contains(\"hello\")\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	spec, err := Load(path)
	require.NoError(t, err)
	require.Len(t, spec.Steps, 7)
	assert.Equal(t, "prompt", spec.Steps[0].Kind)
	assert.Equal(t, "write", spec.Steps[1].Kind)
	require.NotNil(t, spec.Steps[1].Speed)
	assert.Equal(t, 0.2, *spec.Steps[1].Speed)
	assert.Equal(t, "color", spec.Steps[2].Kind)
	require.NotNil(t, spec.Steps[2].Color)
	assert.Equal(t, "lightgreen", spec.Steps[2].Color.FG)
	assert.Equal(t, "blue", spec.Steps[2].Color.BG)
	assert.True(t, spec.Steps[2].Color.Bold)
	assert.Equal(t, "color", spec.Steps[3].Kind)
	require.NotNil(t, spec.Steps[3].Color)
	assert.True(t, spec.Steps[3].Color.Reset)
	assert.Equal(t, "run", spec.Steps[5].Kind)
	assert.Equal(t, "assert", spec.Steps[6].Kind)
}

func TestLoadRecordSpec_MarkerStep(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	yaml := "steps:\n" +
		"  - marker: Create Chart.yaml\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	spec, err := Load(path)
	require.NoError(t, err)
	require.Len(t, spec.Steps, 1)
	assert.Equal(t, "marker", spec.Steps[0].Kind)
	assert.Equal(t, "Create Chart.yaml", spec.Steps[0].Marker)
}

func TestLoadRecordSpec_EnvRequiresListSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	yaml := "steps:\n" +
		"  - env:\n" +
		"      name: HYDRA_CONTEXT\n" +
		"      value: \"\"\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env requires non-empty list")
}

func TestRecordFile_CDStaysWithinRecordingRoot(t *testing.T) {
	specDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`steps:
  - cd: ..
`), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "would leave recording root")
}

func TestRecordFile_RunOutputHiddenButAssertable(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: printf 'hidden output\\n'\n" +
		"    input: false\n" +
		"    output: false\n" +
		"  - assert:\n" +
		"      cast: plain(cast).contains(\"hidden output\")\n" +
		"  - write: visible output\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	castPath := filepath.Join(outDir, "demo.cast")
	castBytes, err := os.ReadFile(castPath)
	require.NoError(t, err)
	cast := string(castBytes)
	assert.Contains(t, cast, "visible output")
	assert.NotContains(t, cast, "hidden output")
}

func TestRecordFile_MarkerLabelNotVisibleInPlainCast(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - write: visible output\n" +
		"  - marker: Hidden Marker Label\n" +
		"  - assert:\n" +
		"      cast: plain(cast).contains(\"visible output\") && !plain(cast).contains(\"Hidden Marker Label\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)
}

func TestRecordFile_EnvValueCelExec(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - env:\n" +
		"      - name: BASE\n" +
		"        value: alpha\n" +
		"      - name: DERIVED\n" +
		"        cel: env.BASE + \"-beta\"\n" +
		"      - name: WORKDIR\n" +
		"        exec: printf '%s' \"$PWD\"\n" +
		"  - run: printf '%s\\n' \"$DERIVED\"\n" +
		"  - assert:\n" +
		"      cast: plain(cast).contains(\"alpha-beta\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	castPath := filepath.Join(outDir, "demo.cast")
	castBytes, err := os.ReadFile(castPath)
	require.NoError(t, err)
	assert.Contains(t, string(castBytes), "alpha-beta")
}

func TestRecordFile_RunPersistsNewEnvVarBetweenRuns(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: export RECORD_VAR_CREATE=created\n" +
		"  - run: printf '%s\\n' \"$RECORD_VAR_CREATE\"\n" +
		"  - assert:\n" +
		"      stdout: plain(stdout).contains(\"created\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)
}

func TestRecordFile_RunPersistsUpdatedEnvVarBetweenRuns(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: export RECORD_VAR_CHANGE=before\n" +
		"  - run: export RECORD_VAR_CHANGE=after\n" +
		"  - run: printf '%s\\n' \"$RECORD_VAR_CHANGE\"\n" +
		"  - assert:\n" +
		"      stdout: plain(stdout).contains(\"after\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)
}

func TestRecordFile_RunPersistsUnsetEnvVarBetweenRuns(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: export RECORD_VAR_UNSET=gone\n" +
		"  - run: unset RECORD_VAR_UNSET\n" +
		"  - run: if [[ -z \"${RECORD_VAR_UNSET+x}\" ]]; then printf 'unset\\n'; else printf 'set\\n'; fi\n" +
		"  - assert:\n" +
		"      stdout: plain(stdout).contains(\"unset\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)
}

func TestRecordFile_AssertStdoutAndStderr(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: printf 'ok\\n' && printf 'warn\\n' >&2\n" +
		"  - assert:\n" +
		"      stdout: plain(stdout).contains(\"ok\")\n" +
		"      stderr: plain(stderr).contains(\"warn\")\n" +
		"      cast: plain(cast).contains(\"ok\") && plain(cast).contains(\"warn\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)
}

func TestRecordFile_AssertStdoutStderrAndBoth(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: printf 'out-line\\n' && printf 'err-line\\n' >&2\n" +
		"    input: false\n" +
		"  - assert:\n" +
		"      stdout: plain(stdout).contains(\"out-line\")\n" +
		"      stderr: plain(stderr).contains(\"err-line\")\n" +
		"      cast: plain(cast).contains(\"out-line\") && plain(cast).contains(\"err-line\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)
}

func TestRecordFile_RunHereDoc(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: |\n" +
		"      cat <<EOF\n" +
		"      hello\n" +
		"      EOF\n" +
		"  - assert:\n" +
		"      cast: plain(cast).contains(\"hello\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	castPath := filepath.Join(outDir, "demo.cast")
	castBytes, err := os.ReadFile(castPath)
	require.NoError(t, err)
	assert.Contains(t, string(castBytes), "hello")
}

func TestSanitizeRecordFileOutput_HidesPromptPrefixedInternalRunWrapper(t *testing.T) {
	raw := strings.Join([]string{
		"\x1b[1;95m $ \x1b[0m\x1b[1;97m__hydra_record_run '/installation/homebrew/.hydra-record/0000.status' 'printf '\"'\"'%b'\"'\"' $'\"'\"'\\n'\"'\"''\r\n",
		"\x1b[0m\r\n",
		"\x1b[1;95m $ \x1b[0m\x1b[1;97mcommand -v hydra\r\n",
	}, "")

	sanitized := string(sanitizeRecordFileOutput([]byte(raw)))

	assert.NotContains(t, sanitized, "__hydra_record_run")
	assert.Contains(t, sanitized, "command -v hydra")
}

func TestSanitizeRecordFileOutput_StripsEmbeddedInternalRunWrapperSuffix(t *testing.T) {
	raw := strings.Join([]string{
		"\x1b[1;95m $ \x1b[0m\x1b[1;97mcommand -v hydra__hydra_record_run '/installation/homebrew/.hydra-record/0000.status' 'printf '\"'\"'%b'\"'\"' $'\"'\"'\\n'\"'\"''\r\n",
		"\x1b[0m\r\n",
	}, "")

	sanitized := string(sanitizeRecordFileOutput([]byte(raw)))

	assert.NotContains(t, sanitized, "__hydra_record_run")
	assert.Contains(t, sanitized, "command -v hydra")
}

func TestRecordFileShell_EndsWithLineEnding(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var shell recordFileShell
		assert.False(t, shell.endsWithLineEnding())
	})

	t.Run("line ending", func(t *testing.T) {
		var shell recordFileShell
		shell.raw.WriteString("hello\r\n")
		assert.True(t, shell.endsWithLineEnding())
	})

	t.Run("no line ending", func(t *testing.T) {
		var shell recordFileShell
		shell.raw.WriteString("hello")
		assert.False(t, shell.endsWithLineEnding())
	})
}

func TestRecordFile_SleepAfterPrompt_DoesNotInsertNewline(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`steps:
  - type: prompt
  - sleep: 1
  - write: '# no output means command not found,'
`), 0o644))

	require.NoError(t, RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir}))

	castPath := filepath.Join(outDir, "demo.cast")
	castBytes, err := os.ReadFile(castPath)
	require.NoError(t, err)

	promptWithNewline := fmt.Sprintf("%s%s\\r\\n", colors.RecordingShellPrompt(), colors.RecordingShellCommand())
	assert.NotContains(t, string(castBytes), promptWithNewline)
}

func TestBuildPromptLineOutput_NormalizesEmbeddedNewlines(t *testing.T) {
	output := buildPromptLineOutput("cat <<EOF\nhello\nEOF", true)

	assert.Contains(t, output, "cat <<EOF\r\nhello\r\nEOF")
	assert.NotContains(t, output, "cat <<EOF\nhello\nEOF")
}

func TestBuildPromptTypingOutput_NormalizesEmbeddedNewlines(t *testing.T) {
	output := buildPromptTypingOutput("cat <<EOF\nhello\nEOF", false, true, 0.1)

	assert.Contains(t, output, "<<hydra sleep 0.1>>\r\n<<hydra sleep 0.1>>")
	assert.NotContains(t, output, "<<hydra sleep 0.1>>\n<<hydra sleep 0.1>>")
}
