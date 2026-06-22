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

func TestLoadRecordSpec_BackgroundStep(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	yaml := "steps:\n" +
		"  - background: lightblue\n" +
		"  - background: reset\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	spec, err := Load(path)
	require.NoError(t, err)
	require.Len(t, spec.Steps, 2)
	assert.Equal(t, "background", spec.Steps[0].Kind)
	require.NotNil(t, spec.Steps[0].Background)
	assert.Equal(t, "lightblue", spec.Steps[0].Background.Name)
	assert.Equal(t, "background", spec.Steps[1].Kind)
	require.NotNil(t, spec.Steps[1].Background)
	assert.True(t, spec.Steps[1].Background.Reset)
}

func TestLoadRecordSpec_BackgroundHexStep(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	yaml := "steps:\n" +
		"  - background: '#0b1f4d'\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	spec, err := Load(path)
	require.NoError(t, err)
	require.Len(t, spec.Steps, 1)
	require.NotNil(t, spec.Steps[0].Background)
	assert.Equal(t, "#0b1f4d", spec.Steps[0].Background.Name)
}

func TestDecorateRecordOutputWithBackground_ReappliesAfterReset(t *testing.T) {
	got := decorateRecordOutputWithBackground(colors.RecordingShellPrompt()+colors.RecordingShellCommand()+"hydra message"+colors.Reset.String(), "\x1b[104m")
	assert.Equal(t, "\x1b[104m"+colors.BoldLightMagenta()+" $ "+"\x1b[0m\x1b[104m"+colors.BoldWhite()+"hydra message"+colors.Reset.String()+"\x1b[104m", got)
}

func TestDecorateRecordOutputWithBackground_FillsLineBeforeNewline(t *testing.T) {
	got := decorateRecordOutputWithBackground("setup\r\nnext line\r\n", "\x1b[44m")
	assert.Equal(t, "\x1b[44msetup\x1b[K\r\nnext line\x1b[K\r\n", got)
}

func TestRenderRecordBackground_Hex(t *testing.T) {
	got, ok := renderRecordBackground(&RecordBackground{Name: "#0b1f4d"})
	require.True(t, ok)
	assert.Equal(t, "\x1b[48;2;11;31;77m", got)
}

func TestLoadRecordSpec_ExportToDirectoryStep(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	yaml := "steps:\n" +
		"  - export-to-directory: docs/tutorials/introduction/demo\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	spec, err := Load(path)
	require.NoError(t, err)
	require.Len(t, spec.Steps, 1)
	assert.Equal(t, "export-to-directory", spec.Steps[0].Kind)
	assert.Equal(t, "docs/tutorials/introduction/demo", spec.Steps[0].ExportToDirectory)
}

func TestLoadRecordSpec_RunTypedFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	yaml := "steps:\n" +
		"  - run: hydra local apps\n" +
		"    typed: true\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	spec, err := Load(path)
	require.NoError(t, err)
	require.Len(t, spec.Steps, 1)
	assert.Equal(t, "run", spec.Steps[0].Kind)
	require.NotNil(t, spec.Steps[0].Typed)
	assert.True(t, *spec.Steps[0].Typed)
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

func TestRecordFile_MergesStdoutAndStderrInEmissionOrder(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	yaml := "steps:\n" +
		"  - run: |\n" +
		"      printf 'out-1\\n'\n" +
		"      sleep 0.05\n" +
		"      printf 'err-1\\n' >&2\n" +
		"      sleep 0.05\n" +
		"      printf 'out-2\\n'\n" +
		"  - assert:\n" +
		"      cast: plain(cast).contains(\"out-1\") && plain(cast).contains(\"err-1\") && plain(cast).contains(\"out-2\")\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	castPath := filepath.Join(outDir, "demo.cast")
	castBytes, err := os.ReadFile(castPath)
	require.NoError(t, err)
	cast := string(castBytes)

	out1 := strings.Index(cast, "out-1")
	err1 := strings.Index(cast, "err-1")
	out2 := strings.Index(cast, "out-2")
	require.NotEqual(t, -1, out1)
	require.NotEqual(t, -1, err1)
	require.NotEqual(t, -1, out2)
	assert.True(t, out1 < err1 && err1 < out2, "expected stdout/stderr lines to stay interleaved in cast output")
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

func TestRecordFile_ExportToDirectory_ReplacesDirectoryWithVirtualHome(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	exportRoot := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	exportTarget := filepath.Join(exportRoot, "tutorial")

	require.NoError(t, os.MkdirAll(exportTarget, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(exportTarget, "stale.txt"), []byte("stale"), 0o644))

	yaml := "steps:\n" +
		"  - run: mkdir -p \"$HOME/group/context/cluster/app\"\n" +
		"  - run: |\n" +
		"      cat > \"$HOME/group/context/cluster/app/Chart.yaml\" <<EOF\n" +
		"      apiVersion: v2\n" +
		"      name: app\n" +
		"      version: 0.1.0\n" +
		"      EOF\n" +
		"  - export-to-directory: " + exportTarget + "\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(exportTarget, "stale.txt"))
	assert.True(t, os.IsNotExist(err))

	data, err := os.ReadFile(filepath.Join(exportTarget, "group", "context", "cluster", "app", "Chart.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "apiVersion: v2")
	assert.Contains(t, string(data), "name: app")
}

func TestRecordFile_ExportToDirectory_CreatesGitkeepForEmptyVirtualHome(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	exportRoot := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	exportTarget := filepath.Join(exportRoot, "tutorial")

	yaml := "steps:\n" +
		"  - export-to-directory: " + exportTarget + "\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(exportTarget, ".gitkeep"))
	require.NoError(t, err)
}

func TestRecordFile_ExportToDirectory_CreatesGitkeepForEmptyNestedDirectories(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	exportRoot := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	exportTarget := filepath.Join(exportRoot, "tutorial")

	yaml := "steps:\n" +
		"  - run: mkdir -p \"$HOME/group/context\"\n" +
		"  - export-to-directory: " + exportTarget + "\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(exportTarget, "group", "context", ".gitkeep"))
	require.NoError(t, err)
}

func TestRecordFile_ExportToDirectory_SkipsChartArchives(t *testing.T) {
	specDir := t.TempDir()
	outDir := t.TempDir()
	exportRoot := t.TempDir()
	specPath := filepath.Join(specDir, "demo.yaml")
	exportTarget := filepath.Join(exportRoot, "tutorial")

	yaml := "steps:\n" +
		"  - run: mkdir -p \"$HOME/group/context/cluster/app/charts\"\n" +
		"  - run: printf 'archive' > \"$HOME/group/context/cluster/app/charts/demo-1.0.0.tgz\"\n" +
		"  - run: printf 'keep' > \"$HOME/group/context/cluster/app/charts/README.md\"\n" +
		"  - export-to-directory: " + exportTarget + "\n"
	require.NoError(t, os.WriteFile(specPath, []byte(yaml), 0o644))

	err := RecordOne(specPath, RecordOptions{SpecDir: specDir, OutputDir: outDir})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(exportTarget, "group", "context", "cluster", "app", "charts", "demo-1.0.0.tgz"))
	assert.True(t, os.IsNotExist(err))

	data, err := os.ReadFile(filepath.Join(exportTarget, "group", "context", "cluster", "app", "charts", "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data))
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
