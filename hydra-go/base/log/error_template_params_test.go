package log

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
)

func TestCreateError_ExposesTemplateParams(t *testing.T) {
	err := CreateError(
		errors.ErrCloneTargetOwnerAmbiguous,
		"ambiguous app owners for clone target resolution in {count} namespace(s): {summary}",
		Int("count", 2),
		String("summary", "argocd: [app-a, app-b]"),
		String("ambiguous", "string-view"),
		ExtendedHelp("ambiguous", []map[string]any{{
			"namespace": "argocd",
			"apps":      []string{"app-a", "app-b"},
		}}),
		Any("namespaces", []string{"argocd: [app-a, app-b]"}),
	)

	params, ok := errors.TemplateParams(err)
	require.True(t, ok)
	require.Equal(t, int64(2), params["count"])
	require.Equal(t, "argocd: [app-a, app-b]", params["summary"])
	require.Equal(t, []string{"argocd: [app-a, app-b]"}, params["namespaces"])
	require.Equal(t, []map[string]any{{
		"namespace": "argocd",
		"apps":      []string{"app-a", "app-b"},
	}}, params["ambiguous"])
}
