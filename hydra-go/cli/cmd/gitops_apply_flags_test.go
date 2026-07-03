package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestClusterApplyCommand_BootstrapAndSkipRefChecksMutuallyExclusive(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error { return nil })
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--skip-ref-checks"})
	err := cmd.Execute()
	require.Error(t, err)
	low := strings.ToLower(err.Error())
	assert.True(t,
		strings.Contains(low, "cannot combine") ||
			strings.Contains(low, "exclusive") ||
			strings.Contains(low, "were all set"),
		"expected mutually exclusive flag error, got: %v", err)
}

func TestClusterApplyCommand_BootstrapAndSkipBootstrapGuardMutuallyExclusive(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var ran bool
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error {
		ran = true
		return nil
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--skip-bootstrap-guard"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.False(t, ran)
	low := strings.ToLower(err.Error())
	assert.True(t, strings.Contains(low, "flags in the group") || strings.Contains(low, "exclusive"),
		"expected mutually exclusive flag error, got: %v", err)
}

func TestClusterApplyCommand_ScaleUpRequiresDownScaled(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var ran bool
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error {
		ran = true
		return nil
	})
	cmd.SetArgs([]string{"cluster.test.app", "--scale-up"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.False(t, ran)
	assert.Contains(t, err.Error(), "--scale-up requires --down-scaled")
}

func TestClusterApplyCommand_BootstrapGuardAndSkipMutuallyExclusive(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error { return nil })
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap-guard", "--skip-bootstrap-guard"})
	err := cmd.Execute()
	require.Error(t, err)
	low := strings.ToLower(err.Error())
	assert.True(t, strings.Contains(low, "flags in the group") || strings.Contains(low, "exclusive"),
		"expected mutually exclusive flag error, got: %v", err)
}

func TestClusterApplyCommand_DisableWebhooks(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var got action.ClusterApplyFlags
	stop := errors.New("stop")
	cmd := NewClusterApplyCommand(func(f action.ClusterApplyFlags) error {
		got = f
		return stop
	})
	cmd.SetArgs([]string{"cluster.test.app", "--disable-webhooks"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, stop)
	assert.True(t, got.DisableWebhooks)
}

func TestClusterApplyCommand_BackupRestoreAndSkipMutuallyExclusive(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error { return nil })
	cmd.SetArgs([]string{"cluster.test.app", "--backup-restore", "--skip-backup-restore"})
	err := cmd.Execute()
	require.Error(t, err)
	low := strings.ToLower(err.Error())
	assert.True(t, strings.Contains(low, "flags in the group") || strings.Contains(low, "exclusive"),
		"expected mutually exclusive flag error, got: %v", err)
}

func TestClusterApplyCommand_DefaultSyncWindowIsDefault(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var got action.ClusterApplyFlags
	stop := errors.New("stop")
	cmd := NewClusterApplyCommand(func(f action.ClusterApplyFlags) error {
		got = f
		return stop
	})
	cmd.SetArgs([]string{"cluster.test.app"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, stop)
	assert.Equal(t, types.ClusterApplySyncWindowDefault, got.EffectiveSyncWindow)
}

func TestClusterApplyCommand_BootstrapSyncWindowDefaultOverridesKeepOrPrevent(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var got action.ClusterApplyFlags
	stop := errors.New("stop")
	cmd := NewClusterApplyCommand(func(f action.ClusterApplyFlags) error {
		got = f
		return stop
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--sync=default"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, stop)
	assert.Equal(t, types.ClusterApplySyncWindowDefault, got.EffectiveSyncWindow)
}

func TestClusterApplyCommand_BootstrapImpliesOptionalFlags(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var got action.ClusterApplyFlags
	stop := errors.New("stop")
	cmd := NewClusterApplyCommand(func(f action.ClusterApplyFlags) error {
		got = f
		return stop
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, stop)
	assert.True(t, got.SopsDecode)
	assert.True(t, got.DownScaled)
	assert.True(t, got.ScaleUp)
	assert.True(t, got.OrphanScaleDown)
	assert.Equal(t, types.ClusterApplySyncWindowKeepOrPrevent, got.EffectiveSyncWindow)
	assert.True(t, got.BootstrapGuard)
	assert.True(t, got.BootstrapClones)
	assert.True(t, got.BackupRestore)
	assert.True(t, got.DisableWebhooks)
}

func TestValidateClusterApplyFlags_BootstrapGuardAndSkipTogether(t *testing.T) {
	err := validateClusterApplyFlags(nil, &action.ClusterApplyFlags{
		ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
			BootstrapGuard: true,
		},
		SkipBootstrapGuardFlag: flags.SkipBootstrapGuardFlag{
			SkipBootstrapGuard: true,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--bootstrap-guard and --skip-bootstrap-guard")
}

func TestValidateClusterApplyFlags_PredicatesWithReplace(t *testing.T) {
	err := validateClusterApplyFlags(nil, &action.ClusterApplyFlags{
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`kind == "Deployment"`},
		},
		ReplaceFlag: flags.ReplaceFlag{Replace: true},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--include/--exclude")
	assert.Contains(t, err.Error(), "--replace")
}

func TestValidateClusterRESTClientFlags_APIBurstRequiresAPIQPS(t *testing.T) {
	err := flags.ValidateClusterRESTClientFlags(&flags.ClusterRESTClientFlags{
		APIClientBurst: 50,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--api-burst")
	assert.Contains(t, err.Error(), "--qps")
}

func TestValidateClusterRESTClientFlags_APIBurstWithNegativeAPIQPS(t *testing.T) {
	err := flags.ValidateClusterRESTClientFlags(&flags.ClusterRESTClientFlags{
		APIClientQPS:   -1,
		APIClientBurst: 10,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--api-burst")
}

func TestValidateClusterApplyFlags_PredicatesWithOrphanScaleDown(t *testing.T) {
	err := validateClusterApplyFlags(nil, &action.ClusterApplyFlags{
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`kind == "Deployment"`},
		},
		ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
			OrphanScaleDown: true,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--include/--exclude")
	assert.Contains(t, err.Error(), "--orphan-scale-down")
}

func TestClusterApplyCommand_BootstrapNoScaleUp(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var got action.ClusterApplyFlags
	stop := errors.New("stop")
	cmd := NewClusterApplyCommand(func(f action.ClusterApplyFlags) error {
		got = f
		return stop
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--no-scale-up"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, stop)
	assert.True(t, got.DownScaled)
	assert.False(t, got.ScaleUp)
	assert.True(t, got.SopsDecode)
	assert.True(t, got.OrphanScaleDown)
}

func TestClusterApplyCommand_BootstrapWithDownScaledRejected(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var ran bool
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error {
		ran = true
		return nil
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--down-scaled"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.False(t, ran)
	assert.Contains(t, err.Error(), "cannot combine --bootstrap")
}

func TestClusterApplyCommand_NoScaleUpWithoutBootstrapRejected(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var ran bool
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error {
		ran = true
		return nil
	})
	cmd.SetArgs([]string{"cluster.test.app", "--no-scale-up"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.False(t, ran)
	assert.Contains(t, err.Error(), "--no-scale-up requires --bootstrap")
}

func TestClusterApplyCommand_BootstrapNoDownScaledWithImpliedScaleUpFails(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var ran bool
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error {
		ran = true
		return nil
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--no-down-scaled"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.False(t, ran)
	assert.Contains(t, err.Error(), "--scale-up requires --down-scaled")
}

func TestClusterApplyCommand_BootstrapNoBackupRestore(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var got action.ClusterApplyFlags
	stop := errors.New("stop")
	cmd := NewClusterApplyCommand(func(f action.ClusterApplyFlags) error {
		got = f
		return stop
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--no-backup-restore"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, stop)
	assert.False(t, got.BackupRestore)
	assert.True(t, got.ScaleUp)
}

func TestClusterApplyCommand_BackupRestoreAndNoBackupRestoreMutuallyExclusive(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error { return nil })
	cmd.SetArgs([]string{"cluster.test.app", "--backup-restore", "--no-backup-restore"})
	err := cmd.Execute()
	require.Error(t, err)
	low := strings.ToLower(err.Error())
	assert.True(t, strings.Contains(low, "flags in the group") || strings.Contains(low, "exclusive"),
		"expected mutually exclusive flag error, got: %v", err)
}

func TestClusterApplyCommand_BootstrapWithIncludeAndNoOrphanScaleDownPassesValidate(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	var got action.ClusterApplyFlags
	stop := errors.New("stop")
	cmd := NewClusterApplyCommand(func(f action.ClusterApplyFlags) error {
		got = f
		return stop
	})
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--no-orphan-scale-down", "--include", `kind == "Deployment"`})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, stop)
	assert.False(t, got.OrphanScaleDown)
}

func TestClusterApplyCommand_BootstrapWithIncludeRequiresOrphanScaleDownFalse(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error { return nil })
	cmd.SetArgs([]string{"cluster.test.app", "--bootstrap", "--include", `kind == "Deployment"`})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--include/--exclude")
}

func TestClusterApplyHelp_GroupedFlagSections(t *testing.T) {
	t.Setenv("HYDRA_CONTEXT", "/tmp")
	cmd := NewClusterApplyCommand(func(action.ClusterApplyFlags) error { return nil })
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)
	out := buf.String()
	gen := strings.Index(out, "General:\n")
	opt := strings.Index(out, "Optional apply behaviors (enable / bootstrap opt-out pairs):\n")
	boot := strings.Index(out, "Bootstrap bundle:\n")
	require.NotEqual(t, -1, gen, "missing General section")
	require.NotEqual(t, -1, opt, "missing Optional apply behaviors section")
	require.NotEqual(t, -1, boot, "missing Bootstrap bundle section")
	require.True(t, gen < opt && opt < boot, "sections out of order: General → Optional → Bootstrap")
	sops := strings.Index(out, "--sops-decode")
	nosops := strings.Index(out, "--no-sops-decode")
	require.NotEqual(t, -1, sops)
	require.NotEqual(t, -1, nosops)
	require.True(t, sops < nosops, "--sops-decode should appear before --no-sops-decode in help")
	syncFlag := strings.Index(out, "      --sync")
	bootstrap := strings.Index(out, "--bootstrap")
	require.NotEqual(t, -1, syncFlag)
	require.NotEqual(t, -1, bootstrap)
	require.True(t, bootstrap < syncFlag, "--bootstrap should appear before --sync in Bootstrap bundle")
}
