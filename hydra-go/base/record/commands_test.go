package record

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectHelpCommandPaths_IncludesRequiredCommands(t *testing.T) {
	root := testHelpRootCommand()
	paths := pathStrings(t, CollectHelpCommandPaths(root))

	required := []string{
		"local",
		"local template",
		"cluster",
		"gitops",
		"gitops uninstall",
	}
	for _, want := range required {
		assert.Contains(t, paths, want, "missing required help path %q", want)
	}
}

func TestCollectHelpCommandPaths_MatchesReferenceWalk(t *testing.T) {
	root := testHelpRootCommand()
	got := pathStrings(t, CollectHelpCommandPaths(root))
	want := referenceHelpPaths(root)

	assert.ElementsMatch(t, want, got)
	assert.Equal(t, len(want), len(got), "duplicate or missing paths")
}

func TestCollectHelpCommandPaths_SkipsRecordSubtree(t *testing.T) {
	root := testHelpRootCommand()
	paths := pathStrings(t, CollectHelpCommandPaths(root))

	for _, p := range paths {
		assert.False(t, strings.HasPrefix(p, "record"), "record subtree must be excluded, got %q", p)
	}
}

func TestPathToSlug(t *testing.T) {
	assert.Equal(t, "gitops-uninstall", pathToSlug("gitops uninstall"))
	assert.Equal(t, "local-template", pathToSlug("local template"))
}

// testHelpRootCommand builds a minimal tree that mirrors production layout for discovery tests.
func testHelpRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "hydra"}

	local := &cobra.Command{Use: "local"}
	local.AddCommand(&cobra.Command{Use: "template"})
	local.AddCommand(&cobra.Command{Use: "find"})
	root.AddCommand(local)

	root.AddCommand(&cobra.Command{Use: "cluster"})

	gitops := &cobra.Command{Use: "gitops"}
	gitops.AddCommand(&cobra.Command{Use: "uninstall <appId> [appId...]"})
	gitops.AddCommand(&cobra.Command{Use: "apply"})
	root.AddCommand(gitops)

	record := &cobra.Command{Use: "record"}
	record.AddCommand(&cobra.Command{Use: "help"})
	record.AddCommand(&cobra.Command{Use: "file <file>..."})
	root.AddCommand(record)

	hidden := &cobra.Command{Use: "validate", Hidden: true}
	root.AddCommand(hidden)

	return root
}

func referenceHelpPaths(root *cobra.Command) []string {
	var out []string
	var walk func(*cobra.Command, []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		for _, child := range cmd.Commands() {
			if child.Hidden {
				continue
			}
			name := commandNameFromUse(child.Use)
			if name == "" {
				continue
			}
			path := append(append([]string{}, prefix...), name)
			if shouldSkipHelpPath(path) {
				continue
			}
			out = append(out, strings.Join(path, " "))
			walk(child, path)
		}
	}
	walk(root, nil)
	return out
}

func pathStrings(t *testing.T, commands []HelpCommand) []string {
	t.Helper()
	out := make([]string, 0, len(commands))
	for _, c := range commands {
		require.NotEmpty(t, c.Slug)
		assert.Equal(t, pathToSlug(c.Path), c.Slug)
		out = append(out, c.Path)
	}
	return out
}
