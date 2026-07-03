package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestClusterBackupCreateCommand_RegistersSecretScopeFlags(t *testing.T) {
	cmd := newClusterBackupCreateCommand(func(flags action.ClusterBackupCreateFlags) error {
		return nil
	})

	assert.NotNil(t, cmd.Flags().Lookup("include"), "backup create should expose --include")
	assert.NotNil(t, cmd.Flags().Lookup("exclude"), "backup create should expose --exclude")
	assert.Nil(t, cmd.Flags().Lookup("all"), "backup create must not expose --all")
}

func TestClusterBackupRestoreCommand_RegistersSecretScopeFlags(t *testing.T) {
	cmd := newClusterBackupRestoreCommand(func(flags action.ClusterBackupRestoreFlags) error {
		return nil
	})

	assert.NotNil(t, cmd.Flags().Lookup("create-namespaces"), "backup restore should expose --create-namespaces")
	assert.NotNil(t, cmd.Flags().Lookup("include"), "backup restore should expose --include")
	assert.NotNil(t, cmd.Flags().Lookup("exclude"), "backup restore should expose --exclude")
	assert.Nil(t, cmd.Flags().Lookup("all"), "backup restore must not expose --all")
}

func TestClusterBackupCreateCommand_RejectsAllFlag(t *testing.T) {
	cmd := newClusterBackupCreateCommand(func(flags action.ClusterBackupCreateFlags) error {
		t.Fatal("create action should not be called when --all is provided")
		return nil
	})

	cmd.SetArgs([]string{
		"prod.*.*",
		"--hydra-context", "/tmp/hydra-context",
		"--all",
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag: --all")
}

func TestClusterBackupRestoreCommand_RejectsAllFlag(t *testing.T) {
	cmd := newClusterBackupRestoreCommand(func(flags action.ClusterBackupRestoreFlags) error {
		t.Fatal("restore action should not be called when --all is provided")
		return nil
	})

	cmd.SetArgs([]string{
		"prod.*.*",
		"--hydra-context", "/tmp/hydra-context",
		"--all",
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag: --all")
}

func TestClusterBackupRestoreCommand_ParsesSecretScopeFlags(t *testing.T) {
	var captured *action.ClusterBackupRestoreFlags

	cmd := newClusterBackupRestoreCommand(func(flags action.ClusterBackupRestoreFlags) error {
		captured = &flags
		return nil
	})

	cmd.SetArgs([]string{
		"prod.*.*",
		"--hydra-context", "/tmp/hydra-context",
		"--helm-network-mode", "local",
		"--exclude-app", "prod.cluster-infra.cert-manager",
		"--force-backup-restore",
		"--create-namespaces",
		"--dry-run",
		"--include", `id == "v1/Secret/cert-manager/wildcard-tls"`,
		"--exclude", `ns == "kube-system"`,
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)

	assert.Equal(t, []types.AppIdPattern{"prod.*.*"}, captured.AppIdPatterns)
	assert.Equal(t, types.HydraContext("/tmp/hydra-context"), captured.HydraContext)
	assert.Equal(t, types.HelmNetworkModeLocal, captured.HelmNetworkMode)
	assert.Equal(t, []types.AppIdPattern{"prod.cluster-infra.cert-manager"}, captured.ExcludeAppPatterns)
	assert.Equal(t, types.DryRunYes, captured.DryRun)
	assert.True(t, captured.ForceBackupRestore)
	assert.True(t, captured.CreateNamespaces)

	includes, includeErr := cmd.Flags().GetStringArray("include")
	require.NoError(t, includeErr)
	assert.Equal(t, []string{`id == "v1/Secret/cert-manager/wildcard-tls"`}, includes)

	excludes, excludeErr := cmd.Flags().GetStringArray("exclude")
	require.NoError(t, excludeErr)
	assert.Equal(t, []string{`ns == "kube-system"`}, excludes)
}
