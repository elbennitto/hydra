package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHydraValues_Validate_DiffIgnore_AllowsEmptyPatchesWhenIgnoreWhenMissingInCluster(t *testing.T) {
	h := &HydraValues{
		Path: "/apps/example",
		Diff: &HydraDiffSection{
			Ignore: map[string]HydraDiffIgnoreRule{
				"prometheusOperatorAdmissionJobs": {
					Predicate:                  `id == "batch/v1/Job/monitoring/x"`,
					IgnoreWhenMissingInCluster: true,
				},
			},
		},
	}
	require.NoError(t, h.Validate())
}

func TestHydraValues_Validate_RejectsScopeFromHelmValues(t *testing.T) {
	h := &HydraValues{
		Path: "/apps/example",
		Scope: []any{
			map[string]any{"mode": "include", "values": []any{"**"}},
		},
	}
	require.Error(t, h.Validate())
}

func TestHydraValues_Validate_RejectsScopeKeyPresentEvenEmptyList(t *testing.T) {
	h := &HydraValues{
		Path:  "/apps/example",
		Scope: []any{},
	}
	require.Error(t, h.Validate())
}

func TestHydraValues_Validate_DiffIgnore_RejectsEmptyPatchesWithoutIgnoreWhenMissing(t *testing.T) {
	h := &HydraValues{
		Path: "/apps/example",
		Diff: &HydraDiffSection{
			Ignore: map[string]HydraDiffIgnoreRule{
				"bad": {
					Predicate: `gvk == "v1/Pod"`,
				},
			},
		},
	}
	require.Error(t, h.Validate())
}

func TestValidateHydraPresetActivates_RejectsEnableAndExcludeSameTarget(t *testing.T) {
	t.Parallel()
	err := ValidateHydraPresetActivates("flannel", PresetActivateList{{Preset: "canal"}, {Preset: "canal", Exclude: true}})
	require.Error(t, err)
}

func TestValidateHydraPresetActivates_AllowsExclusionEntries(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateHydraPresetActivates("flannel", PresetActivateList{{Preset: "canal", Exclude: true}}))
}

func TestHydraValues_Validate_Presets_RejectsConflictingActivatesInMergedSection(t *testing.T) {
	t.Parallel()
	on := true
	h := &HydraValues{
		Path: "/apps/example",
		Presets: &HydraPresetsSection{
			"flannel": {
				Enabled:   &on,
				Activates: PresetActivateList{{Preset: "coredns"}, {Preset: "coredns", Exclude: true}},
			},
		},
	}
	require.Error(t, h.Validate())
}

func TestHydraValues_Validate_TemplateFiles_AllowsMoveAndDelete(t *testing.T) {
	h := &HydraValues{
		Path: "/apps/example",
		TemplateFiles: &HydraTemplateFiles{
			Move: []HydraTemplateFileMove{{From: "templates/local.yaml", To: "charts/sub/templates/dep.yaml"}},
			Delete: []string{
				"templates/obsolete.yaml",
			},
		},
	}
	require.NoError(t, h.Validate())
}

func TestHydraValues_Validate_TemplateFiles_RejectsMissingMoveFrom(t *testing.T) {
	h := &HydraValues{
		Path: "/apps/example",
		TemplateFiles: &HydraTemplateFiles{
			Move: []HydraTemplateFileMove{{To: "charts/sub/templates/dep.yaml"}},
		},
	}
	require.Error(t, h.Validate())
}

func TestHydraValues_Validate_TemplateFiles_RejectsInvalidDeleteRegex(t *testing.T) {
	h := &HydraValues{
		Path: "/apps/example",
		TemplateFiles: &HydraTemplateFiles{
			Delete: []string{"["},
		},
	}
	require.Error(t, h.Validate())
}
