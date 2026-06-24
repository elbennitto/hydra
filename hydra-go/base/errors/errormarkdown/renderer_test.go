package errormarkdown

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
)

func TestRenderKnownTemplate(t *testing.T) {
	md, err := Render(errors.ErrCloneTargetOwnerAmbiguous, map[string]any{
		"count":      1,
		"summary":    "argocd: [dev.cluster-infra, dev.lmc, dev.lmc-infra]",
		"namespaces": []string{"argocd: [dev.cluster-infra, dev.lmc, dev.lmc-infra]"},
	})
	require.NoError(t, err)
	require.Contains(t, md, "ErrCloneTargetOwnerAmbiguous")
	require.Contains(t, md, "argocd: [dev.cluster-infra, dev.lmc, dev.lmc-infra]")
	require.Contains(t, md, "global.hydra.ownerNamespaces")
}

func TestRenderExtendedErrorTemplates(t *testing.T) {
	cases := []struct {
		code   errors.ErrorId
		params map[string]any
		needle string
	}{
		{
			code:   errors.ErrMissingHydraContext,
			params: map[string]any{"ENV": "HYDRA_CONTEXT"},
			needle: "HYDRA_CONTEXT",
		},
		{
			code: errors.ErrInvalidHydraStructure,
			params: map[string]any{
				"path":            "/tmp/context",
				"groupValuesPath": "/tmp/values.yaml",
				"valuesPath":      "/tmp/context/values.yaml",
				"reason":          "context-values-missing",
			},
			needle: "global.hydra.type: group",
		},
		{
			code: errors.ErrLoadingHelmChartFailed,
			params: map[string]any{
				"path":          "/tmp/context/cluster/app",
				"chartYamlPath": "/tmp/context/cluster/app/Chart.yaml",
			},
			needle: "Chart.yaml",
		},
		{
			code:   errors.ErrAppPatternNoMatch,
			params: map[string]any{"pattern": "**"},
			needle: "did not match any applications",
		},
		{
			code:   errors.ErrDidNotEvalToBool,
			params: map[string]any{"code": "templateEntity.spec.template.spec.containers"},
			needle: "did not return a boolean value",
		},
	}

	for _, tc := range cases {
		md, err := Render(tc.code, tc.params)
		require.NoError(t, err)
		require.Contains(t, md, string(tc.code))
		require.Contains(t, md, tc.needle)
	}
}

func TestRenderUnknownTemplate(t *testing.T) {
	_, err := Render(errors.ErrHydraConfigError, map[string]any{"count": 1})
	require.Error(t, err)
}
