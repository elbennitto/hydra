package action

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestRenderClusterApplyOptionalBehaviorsTable_Defaults(t *testing.T) {
	f := ClusterApplyFlags{}
	f.EffectiveSyncWindow = types.ClusterApplySyncWindowDefault
	out := renderClusterApplyOptionalBehaviorsTable(false, f)
	require.Contains(t, out, "Decode SOPS")
	require.Contains(t, out, "false")
	require.Contains(t, out, "default")
	require.Contains(t, out, "ArgoCD sync policy")
	require.Contains(t, out, "--sync=<mode>")
	require.Contains(t, out, "--sync=default (overrides bootstrap keep-or-prevent)")
	require.NotContains(t, out, "\033[") // no ANSI without color
}

func TestRenderClusterApplyOptionalBehaviorsTable_AllOptionalEnabled(t *testing.T) {
	f := ClusterApplyFlags{
		ClusterApplyBehaviorFlags: flags.ClusterApplyBehaviorFlags{
			SopsDecode:      true,
			DownScaled:      true,
			ScaleUp:         true,
			OrphanScaleDown: true,
			BootstrapGuard:  true,
			BootstrapClones: true,
			BackupRestore:   true,
			DisableWebhooks: true,
		},
	}
	f.EffectiveSyncWindow = types.ClusterApplySyncWindowKeepOrPrevent
	out := renderClusterApplyOptionalBehaviorsTable(false, f)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 10)
	// Data row for Decode SOPS must show true
	require.Contains(t, lines[1], "Decode SOPS")
	require.Contains(t, lines[1], " true ")
}

func TestRenderClusterApplyOptionalBehaviorsTable_WithColorUsesANSI(t *testing.T) {
	f := ClusterApplyFlags{}
	f.EffectiveSyncWindow = types.ClusterApplySyncWindowDefault
	out := renderClusterApplyOptionalBehaviorsTable(true, f)
	require.Contains(t, out, "\033[")
}

func TestShouldLogBootstrapBundleHint(t *testing.T) {
	t.Parallel()
	require.True(t, shouldLogBootstrapBundleHint(ClusterApplyFlags{}))
	require.True(t, shouldLogBootstrapBundleHint(ClusterApplyFlags{
		BootstrapFlag: flags.BootstrapFlag{Bootstrap: types.BootstrapNo},
	}))
	require.False(t, shouldLogBootstrapBundleHint(ClusterApplyFlags{
		BootstrapFlag: flags.BootstrapFlag{Bootstrap: types.BootstrapYes},
	}))
}
