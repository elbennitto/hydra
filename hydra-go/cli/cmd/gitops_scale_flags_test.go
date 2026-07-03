package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestClusterScaleUpCommand_RegistersDryRunFlag(t *testing.T) {
	cmd := newClusterScaleUpCommand(func(action.ClusterScaleFlags) error { return nil })
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"), "cluster scale up should expose --dry-run")
}

func TestClusterScaleUpCommand_SilenceUsageOnError(t *testing.T) {
	cmd := newClusterScaleUpCommand(func(action.ClusterScaleFlags) error { return assert.AnError })
	assert.True(t, cmd.SilenceUsage, "errors from RunE must not print CLI usage")
}

func TestClusterScaleDownCommand_SilenceUsageOnError(t *testing.T) {
	cmd := newClusterScaleDownCommand(func(action.ClusterScaleFlags) error { return assert.AnError })
	assert.True(t, cmd.SilenceUsage, "errors from RunE must not print CLI usage")
}

func TestClusterScaleDownCommand_RegistersDryRunFlag(t *testing.T) {
	cmd := newClusterScaleDownCommand(func(action.ClusterScaleFlags) error { return nil })
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"), "cluster scale down should expose --dry-run")
}

func TestClusterScaleUpCommand_ParsesDryRunFlag(t *testing.T) {
	var captured *action.ClusterScaleFlags
	cmd := newClusterScaleUpCommand(func(f action.ClusterScaleFlags) error {
		captured = &f
		return nil
	})
	cmd.SetArgs([]string{
		"prod.test.app",
		"--hydra-context", "/tmp/hydra-context",
		"--dry-run",
	})
	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, types.DryRunYes, captured.DryRun)
}

func TestClusterScaleDownCommand_RegistersClusterWorkloadTimeoutFlag(t *testing.T) {
	cmd := newClusterScaleDownCommand(func(action.ClusterScaleFlags) error { return nil })
	assert.NotNil(t, cmd.Flags().Lookup("cluster-workload-timeout"), "cluster scale down should expose --cluster-workload-timeout")
}

func TestClusterScaleDownCommand_ExclusiveForceAndClusterWorkloadTimeout(t *testing.T) {
	cmd := newClusterScaleDownCommand(func(action.ClusterScaleFlags) error { return nil })
	cmd.SetArgs([]string{
		"prod.test.app",
		"--hydra-context", "/tmp/hydra-context",
		"--force-scale-down",
		"--cluster-workload-timeout", "30s",
	})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use --force-scale-down and --cluster-workload-timeout together")
}

func TestClusterScaleStatusCommand_SilenceUsageOnError(t *testing.T) {
	cmd := newClusterScaleStatusCommand(func(action.ClusterScaleStatusFlags) error { return assert.AnError })
	assert.True(t, cmd.SilenceUsage, "errors from RunE must not print CLI usage")
}

func TestClusterScaleStatusCommand_RegistersYamlFlag(t *testing.T) {
	cmd := newClusterScaleStatusCommand(func(action.ClusterScaleStatusFlags) error { return nil })
	assert.NotNil(t, cmd.Flags().Lookup("yaml"), "cluster scale status should expose --yaml")
}

func TestClusterScaleStatusCommand_RegistersAllFlag(t *testing.T) {
	cmd := newClusterScaleStatusCommand(func(action.ClusterScaleStatusFlags) error { return nil })
	assert.NotNil(t, cmd.Flags().Lookup("all"), "cluster scale status should expose --all")
}

func TestClusterScaleStatusCommand_ParsesAllShortFlag(t *testing.T) {
	var captured *action.ClusterScaleStatusFlags
	cmd := newClusterScaleStatusCommand(func(f action.ClusterScaleStatusFlags) error {
		captured = &f
		return nil
	})
	cmd.SetArgs([]string{
		"prod.test.app",
		"--hydra-context", "/tmp/hydra-context",
		"-A",
	})
	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.True(t, captured.ShowAllHealthyApps)
}

func TestClusterScaleDownCommand_ParsesDryRunFlag(t *testing.T) {
	var captured *action.ClusterScaleFlags
	cmd := newClusterScaleDownCommand(func(f action.ClusterScaleFlags) error {
		captured = &f
		return nil
	})
	cmd.SetArgs([]string{
		"prod.test.app",
		"--hydra-context", "/tmp/hydra-context",
		"-d",
	})
	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, types.DryRunYes, captured.DryRun)
}
