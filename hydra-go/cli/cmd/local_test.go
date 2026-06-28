package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommandContainsExpectedTopLevelCommands(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())

	require.NotNil(t, rootCmd)
	assert.ElementsMatch(t, []string{"argocd", "ci", "cluster", "cosign", "gitops", "helm", "local", "message", "record", "version", "yq"}, commandUseNames(rootCmd.Commands()))
}

func TestLocalCommandContainsExpectedSubcommands(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())

	require.NotNil(t, rootCmd)

	localCmd := childCommandWithUsePrefix(rootCmd, "local")
	require.NotNil(t, localCmd, "expected hydra local to exist")
	assert.ElementsMatch(t, []string{"find", "apps", "config", "template", "package", "list", "source", "values", "refs", "inspect", "review", "test", "export"}, commandUseNames(localCmd.Commands()))
}

func TestLocalReviewCommandShape(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())

	require.NotNil(t, rootCmd)

	localCmd := childCommandWithUsePrefix(rootCmd, "local")
	require.NotNil(t, localCmd, "expected hydra local to exist")

	reviewCmd := childCommandWithUsePrefix(localCmd, "review")
	require.NotNil(t, reviewCmd, "expected hydra local review to exist")
	assert.Equal(t, "review <appId...>", reviewCmd.Use)
	require.Empty(t, reviewCmd.Commands())
}

func TestLocalPackageCommandCapturesFlagsAndWritesPath(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())
	require.NotNil(t, rootCmd)

	rootCmd.SetArgs([]string{"local", "package", "prod.cluster-infra.cert-manager", "--hydra-context", "/tmp/mock-hydra-context", "--destination", "dist"})

	require.NoError(t, rootCmd.Execute())
	require.NotNil(t, mock.LocalPackageFlags)
	assert.Equal(t, "prod.cluster-infra.cert-manager", string(mock.LocalPackageFlags.AppId))
	assert.Equal(t, "dist", mock.LocalPackageFlags.Destination)
}

func commandUseNames(commands []*cobra.Command) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		useParts := strings.Fields(command.Use)
		if len(useParts) == 0 {
			continue
		}
		names = append(names, useParts[0])
	}
	return names
}
