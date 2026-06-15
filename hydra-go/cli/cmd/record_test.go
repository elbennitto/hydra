package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/record"
)

func TestCollectHelpCommandPaths_MatchesProductionCLI(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())

	got := record.CollectHelpCommandPaths(rootCmd)
	gotPaths := make([]string, 0, len(got))
	for _, c := range got {
		gotPaths = append(gotPaths, c.Path)
	}

	required := []string{
		"local",
		"local template",
		"cluster",
		"gitops",
		"gitops uninstall",
	}
	for _, want := range required {
		assert.Contains(t, gotPaths, want)
	}

	// Independent reference: same walk rules as base/record/commands_test.go
	wantPaths := referenceProductionHelpPaths(rootCmd)
	assert.ElementsMatch(t, wantPaths, gotPaths)
	assert.Greater(t, len(gotPaths), 20, "expected a substantial command tree")
}

func referenceProductionHelpPaths(root *cobra.Command) []string {
	var out []string
	var walk func(*cobra.Command, []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		for _, child := range cmd.Commands() {
			if child.Hidden {
				continue
			}
			parts := splitCommandUse(child.Use)
			if len(parts) == 0 {
				continue
			}
			name := parts[0]
			path := append(append([]string{}, prefix...), name)
			if len(path) > 0 && path[0] == "record" {
				continue
			}
			out = append(out, strings.Join(path, " "))
			walk(child, path)
		}
	}
	walk(root, nil)
	return out
}

func splitCommandUse(use string) []string {
	name := strings.Fields(use)
	if len(name) == 0 {
		return nil
	}
	return []string{name[0]}
}

func TestRootCommandContainsRecord(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())
	assert.Contains(t, commandUseNames(rootCmd.Commands()), "record")
}

func TestRootCommandContainsMessage(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())
	assert.Contains(t, commandUseNames(rootCmd.Commands()), "message")
}

func TestRecordCommandContainsFileSubcommand(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())

	recordCmd := commandByUsePath(rootCmd, "record")
	require.NotNil(t, recordCmd)

	fileCmd := commandByUsePath(rootCmd, "record", "file")
	require.NotNil(t, fileCmd)
	assert.Equal(t, "file <file>...", fileCmd.Use)
}

func TestRecordOutputDirHelpers(t *testing.T) {
	wantRecordDir := filepath.ToSlash(filepath.Join("docs", "asciinema", recordSpecDirName()))
	baseDir := filepath.ToSlash(filepath.Join("docs", "asciinema"))
	assert.Equal(t, "docs/asciinema/help", recordHelpOutputDir("docs/asciinema"))
	assert.Equal(t, "docs/asciinema/help", recordHelpOutputDir("docs/asciinema/help"))
	assert.Equal(t, wantRecordDir, recordFileOutputDir(baseDir))
	assert.Equal(t, wantRecordDir, recordFileOutputDir(wantRecordDir))
	assert.Equal(t, "docs/asciinema/tutorials/demo.cast", recordFileOutputPath("docs/asciinema/tutorials/demo.yaml", ""))
	assert.Equal(t, "custom/path/out.cast", recordFileOutputPath("docs/asciinema/tutorials/demo.yaml", "custom/path/out.cast"))
}

func TestRunRecordFiles_RejectsOutputWithMultipleFiles(t *testing.T) {
	err := runRecordFiles([]string{"one.yaml", "two.yaml"}, recordCLIParams{output: "out.cast"})
	require.EqualError(t, err, "--output can only be used with a single record file")
}

func TestRecordingHydraGlobalArgsFromEnv(t *testing.T) {
	assert.Equal(t, []string{"--no-timestamps"}, recordingHydraGlobalArgsFromEnv(os.LookupEnv))

	t.Setenv(recordNoTimestampsEnvName, "true")
	assert.Equal(t, []string{"--no-timestamps"}, recordingHydraGlobalArgsFromEnv(os.LookupEnv))

	t.Setenv(recordNoTimestampsEnvName, "0")
	assert.Nil(t, recordingHydraGlobalArgsFromEnv(os.LookupEnv))
}
