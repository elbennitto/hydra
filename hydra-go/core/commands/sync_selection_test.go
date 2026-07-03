package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	corehydra "hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type zeroArgSelectionHydra struct{}

var _ corehydra.Hydra = zeroArgSelectionHydra{}

func (zeroArgSelectionHydra) L() log.Logger { return log.Default() }

func (zeroArgSelectionHydra) AsContext() *corehydra.Context {
	panic("unexpected context lookup")
}

func (zeroArgSelectionHydra) AsCluster() *corehydra.Cluster {
	panic("unexpected cluster lookup")
}

func (zeroArgSelectionHydra) AsRootApp() *corehydra.RootApp {
	panic("unexpected root app lookup")
}

func (zeroArgSelectionHydra) AsChildApp() *corehydra.ChildApp {
	panic("unexpected child app lookup")
}

func (zeroArgSelectionHydra) AsApp() corehydra.HydraApp {
	panic("unexpected app lookup")
}

func (zeroArgSelectionHydra) Config() types.Config {
	panic("unexpected config lookup")
}

func (zeroArgSelectionHydra) WithCluster(types.ClusterName, corehydra.RESTClientLimits) (*corehydra.Cluster, error) {
	panic("unexpected WithCluster call")
}

func (zeroArgSelectionHydra) WithApp(types.AppId) (corehydra.HydraApp, error) {
	panic("unexpected WithApp call")
}

func (zeroArgSelectionHydra) LoadValuesMap(types.HelmNetworkMode) (types.ValuesMap, error) {
	panic("unexpected LoadValuesMap call")
}

func (zeroArgSelectionHydra) Description() string { return "zero-arg-selection-test" }

func TestResolveAppNames_ExactNamesStayReadOnlyAndBypassClusterLookup(t *testing.T) {
	resolved, err := ResolveAppNames(zeroArgSelectionHydra{}, []string{"prod.apps.api", "prod.apps.worker"})
	require.NoError(t, err)
	assert.Equal(t, []string{"prod.apps.api", "prod.apps.worker"}, resolved)
}

func TestArgocdStatusZeroArgSelectionStartsWithAllVisibleApplications(t *testing.T) {
	allVisibleApps := []string{
		"prod.infra.argocd",
		"prod.infra.cert-manager",
		"prod.apps.api",
		"dev.apps.api",
	}

	resolved, err := ResolveArgocdStatusSelection(nil, nil, allVisibleApps)
	require.NoError(t, err)
	assert.ElementsMatch(t, allVisibleApps, resolved)
}

func TestArgocdStatusZeroArgSelectionSubtractsRepeatedExcludePatternsAfterImplicitAll(t *testing.T) {
	allVisibleApps := []string{
		"prod.infra.argocd",
		"prod.infra.cert-manager",
		"prod.apps.api",
		"dev.apps.api",
	}

	filtered, err := ResolveArgocdStatusSelection(
		nil,
		[]string{"prod.infra.*", "dev.apps.api"},
		allVisibleApps,
	)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"prod.apps.api"}, filtered)
}

func TestArgocdSyncSelectionSubtractsRepeatedExcludePatternsFromExplicitIncludeSet(t *testing.T) {
	allVisibleApps := []string{
		"prod.infra.argocd",
		"prod.infra.cert-manager",
		"prod.apps.api",
		"prod.apps.worker",
		"dev.apps.api",
	}

	resolved, err := ResolveArgocdSyncTargets(
		[]string{"prod.**"},
		[]string{"prod.infra.*", "prod.apps.worker"},
		allVisibleApps,
	)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"prod.apps.api"}, resolved)
}
