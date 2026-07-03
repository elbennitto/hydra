package action

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	hyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
)

func TestLocalRefs_ListsTransitiveReachabilityForResource(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeReviewRefsTestContext(t)

	output, err := captureStdout(t, func() error {
		return LocalRefs(LocalRefsFlags{
			ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
			HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
			Cluster:             "prod",
			ResourceId:          types.Id("v1/ConfigMap/demo/shared-config"),
		})
	})
	require.NoError(t, err)

	rows, parseErr := hyaml.FromYaml[[]commands.TransitiveRefRow](types.YamlString(output))
	require.NoError(t, parseErr)
	require.NotEmpty(t, rows)

	var sawAnchor, sawIncomingConsumer bool
	for _, row := range rows {
		if row.ID == "v1/ConfigMap/demo/shared-config" && row.Distance == 0 {
			sawAnchor = true
		}
		if row.ID == "apps/v1/Deployment/demo/consumer" && row.Distance == -1 && row.Direction == "incoming" {
			sawIncomingConsumer = true
		}
	}
	assert.True(t, sawAnchor, "expected anchor row at distance 0, got: %s", output)
	assert.True(t, sawIncomingConsumer, "expected consumer as incoming transitive neighbor, got: %s", output)
}

func TestLocalRefs_InvalidIdReturnsError(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeReviewRefsTestContext(t)

	err := LocalRefs(LocalRefsFlags{
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		Cluster:             "prod",
		ResourceId:          types.Id("too/few/parts"),
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "invalid resource id") || strings.Contains(err.Error(), "invalid Id format"))
}
