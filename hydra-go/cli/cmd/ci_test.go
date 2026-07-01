package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/ci"
)

func newCapturingCiParams() (CiCommandParams, *action.CiFlags) {
	var captured action.CiFlags
	capture := func(f action.CiFlags) error {
		captured = f
		return nil
	}
	return CiCommandParams{
		CiDownload: capture,
		CiTest:     capture,
		CiRelease:  capture,
		CiPromote:  capture,
		CiAuto:     capture,
		CiPublish:  capture,
		CiValidate: capture,
		CiSprint:   capture,
		CiUpgrade:  capture,
		CiSync:     capture,
		CiUpdate:   capture,
		CiConfig: func(path string, in io.Reader, out io.Writer, useColor bool) error {
			return nil
		},
	}, &captured
}

func TestNewCiSubcommand_AutoResolvesConfigPath(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "auto", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filePath, captured.ConfigPath)
}

func TestNewCiSubcommand_DownloadResolvesConfigPath(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "download", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filePath, captured.ConfigPath)
}

func TestNewCiSubcommand_FilePathUnchanged(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filePath, captured.ConfigPath)
}

func TestNewCiSubcommand_DirectoryResolvesToFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ci.ConfigFileName), []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", dir})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filepath.Join(dir, ci.ConfigFileName), captured.ConfigPath)
}

func TestNewCiSubcommand_TrailingSlashResolved(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ci.ConfigFileName), []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", dir + "/"})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filepath.Join(dir, ci.ConfigFileName), captured.ConfigPath)
}

func TestNewCiSubcommand_DotResolvesToFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ci.ConfigFileName), []byte("ci: {}"), 0644))

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(orig) })

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", "."})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filepath.Join(".", ci.ConfigFileName), captured.ConfigPath)
}

func TestNewCiSubcommand_NonexistentPathPassedAsIs(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", nonexistent})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, nonexistent, captured.ConfigPath)
}

// --- Target Branch Flag Tests ---

func TestCiCommand_TargetBranchParsed(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", "--target-branch", "my-branch", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "my-branch", captured.TargetBranch)
}

func TestCiCommand_TargetBranchWithDryRun(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", "--target-branch", "my-branch", "--dry-run", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "my-branch", captured.TargetBranch)
	assert.True(t, captured.DryRun)
}

func TestCiCommand_TargetBranchWithLocal(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", "--target-branch", "my-branch", "--local", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "my-branch", captured.TargetBranch)
	assert.True(t, captured.Local)
}

func TestCiCommand_TargetBranchDefault(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "test", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "", captured.TargetBranch)
}

func TestCiVerifyCommand_ParsesBuildTagAndCharts(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{
		"run",
		"verify",
		"--build-tag", "build-202601011200",
		"--force-run",
		"--chart", "demo/service-ui/dev",
		"--chart", "apps/demo/service-auth/dev",
		filePath,
	})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "build-202601011200", captured.BuildTag)
	assert.True(t, captured.ForceRun)
	assert.Equal(t, []string{"demo/service-ui/dev", "apps/demo/service-auth/dev"}, captured.Charts)
}

func TestCiUpgradeCommand_ParsesVersionsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	versionsFile := filepath.Join(dir, "versions.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))
	require.NoError(t, os.WriteFile(versionsFile, []byte("versions: []\n"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "upgrade", "--versions-file", versionsFile, filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filePath, captured.ConfigPath)
	assert.Equal(t, versionsFile, captured.VersionsFile)
}

func TestCiUpgradeCommand_ParsesSkipMissing(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "upgrade", "--skip-missing", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filePath, captured.ConfigPath)
	assert.True(t, captured.SkipMissing)
}

func TestCiUpgradeCommand_AllowsMissingVersionsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, captured := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"run", "upgrade", filePath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, filePath, captured.ConfigPath)
	assert.Equal(t, "", captured.VersionsFile)
}

func TestCiCommand_ConfigSucceedsWithTargetBranchFlag(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, _ := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"config", "--target-branch", "my-branch", filePath})
	err := cmd.Execute()
	assert.NoError(t, err, "config should succeed; --target-branch is a parent flag that config ignores")
}

func TestCiSecretsCreate_RequiresNameFlag(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	params, _ := newCapturingCiParams()
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"secrets", "create", filePath})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s) \"name\" not set")
}

func TestCiSecretsCreate_PassesNameFlag(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ci.ConfigFileName)
	require.NoError(t, os.WriteFile(filePath, []byte("ci: {}"), 0644))

	var gotPath string
	var gotName string
	var gotForce bool
	params, _ := newCapturingCiParams()
	params.CiSecretCreate = func(path string, name string, force bool, signers string) error {
		gotPath = path
		gotName = name
		gotForce = force
		assert.Equal(t, "both", signers)
		return nil
	}
	cmd := NewCiCommand(params)
	cmd.SetArgs([]string{"secrets", "create", "--name", "build-bot", "--force", filePath})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, filePath, gotPath)
	assert.Equal(t, "build-bot", gotName)
	assert.True(t, gotForce)
}
