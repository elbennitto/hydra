package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestChartDirectoryForBetaContainsDeploymentTemplate(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)
	f := TemplateFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
	}
	h, err := hydra.ResolvePathWithAppId(
		log.Default(),
		types.HydraContext(contextDir),
		"target.platform.beta",
		flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo),
	)
	require.NoError(t, err)
	cd, err := hydra.ChartDirectoryForHydraApp(h)
	require.NoError(t, err)
	deploy := filepath.Join(cd.Path(), "templates", "deployment.yaml")
	_, err = os.Stat(deploy)
	require.NoError(t, err, "chartDir=%s", cd.Path())
}

func TestSourcePrintsUnrenderedChartTemplatesOnly(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, out, err := Source(SourceFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		AppId:               "target.platform.beta",
	})
	require.NoError(t, err)

	assert.Contains(t, out, "{{ .Release.Namespace }}")
	assert.Contains(t, out, "# Source: templates/deployment.yaml")
	// Source files may still contain literal YAML shape (e.g. "kind: Deployment"); this is not Hydra-rendered output.
}

func TestSourceIncludePathFiltersTemplateFiles(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, out, err := Source(SourceFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		IncludePathFlag: flags.IncludePathFlag{
			IncludePathPrefixes: []string{"templates/kafkauser-a.yaml"},
		},
		AppId: "target.platform.alpha",
	})
	require.NoError(t, err)

	assert.Contains(t, out, "# Source: templates/kafkauser-a.yaml")
	assert.NotContains(t, out, "kafkauser-b.yaml")
	assert.NotContains(t, out, "shared-user.yaml")
}

func TestSourceIncludePathNoMatchUsesEmptyMessage(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, out, err := Source(SourceFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		IncludePathFlag: flags.IncludePathFlag{
			IncludePathPrefixes: []string{"templates/does-not-exist"},
		},
		AppId: "target.platform.beta",
	})
	require.NoError(t, err)
	assert.Contains(t, out, sourceNoTemplates)
	assert.NotContains(t, strings.TrimSpace(out), "# Source: templates/deployment.yaml")
}

func TestSourceIncludeFiltersToMatchedManifestSources(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, out, err := Source(SourceFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`name == "alpha-user-a"`},
		},
		AppId: "target.platform.alpha",
	})
	require.NoError(t, err)

	assert.Contains(t, out, "# Source: templates/kafkauser-a.yaml")
	assert.NotContains(t, out, "kafkauser-b.yaml")
	assert.NotContains(t, out, "shared-user.yaml")
}

func TestSourcePredicateAndIncludePathIntersect(t *testing.T) {
	configureFindTestLogging()
	contextDir := writeFindTestContext(t)

	_, out, err := Source(SourceFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		ContextFlag:         flags.ContextFlag{HydraContext: types.HydraContext(contextDir)},
		PredicatesFlag: flags.PredicatesFlag{
			Predicates: []types.CelPredicate{`name == "alpha-user-a"`},
		},
		IncludePathFlag: flags.IncludePathFlag{
			IncludePathPrefixes: []string{"templates/kafkauser-b.yaml"},
		},
		AppId: "target.platform.alpha",
	})
	require.NoError(t, err)

	assert.Contains(t, out, sourceNoTemplates)
	assert.NotContains(t, strings.TrimSpace(out), "# Source: templates/kafkauser-a.yaml")
}
