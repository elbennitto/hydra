package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	corehydra "hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestClusterStatus_ResolvesIncludeMinusExcludeBeforeComputingStatus(t *testing.T) {
	originalResolveAppIds := clusterStatusResolveAppIdsFromConfig
	originalClusterForAppIds := clusterStatusClusterForAppIds
	originalRun := clusterStatusRun
	defer func() {
		clusterStatusResolveAppIdsFromConfig = originalResolveAppIds
		clusterStatusClusterForAppIds = originalClusterForAppIds
		clusterStatusRun = originalRun
	}()

	selectedAppIds := sets.New[types.AppId](
		types.AppId("prod.apps.api"),
		types.AppId("prod.apps.worker"),
	)
	cluster := &corehydra.Cluster{}

	clusterStatusResolveAppIdsFromConfig = func(l log.Logger, hydraContext types.HydraContext, config types.Config, patterns []types.AppIdPattern, excludePatterns []types.AppIdPattern, networkMode types.HelmNetworkMode, suppressGlobPatternSummary bool) (sets.Set[types.AppId], error) {
		assert.NotNil(t, l)
		assert.Equal(t, types.HydraContext("/ctx"), hydraContext)
		assert.Equal(t, []types.AppIdPattern{"prod.**"}, patterns)
		assert.Equal(t, []types.AppIdPattern{"prod.infra.argocd"}, excludePatterns)
		assert.Equal(t, types.HelmNetworkModeOffline, networkMode)
		assert.False(t, suppressGlobPatternSummary)
		return selectedAppIds, nil
	}

	clusterStatusClusterForAppIds = func(config types.Config, hydraContext types.HydraContext, appIds sets.Set[types.AppId], limits corehydra.RESTClientLimits) (*corehydra.Cluster, error) {
		assert.Equal(t, types.HydraContext("/ctx"), hydraContext)
		assert.Equal(t, selectedAppIds, appIds)
		_ = limits
		return cluster, nil
	}

	runCalls := 0
	clusterStatusRun = func(actualCluster *corehydra.Cluster, color types.Color, appIds sets.Set[types.AppId], networkMode types.HelmNetworkMode) error {
		runCalls++
		assert.Same(t, cluster, actualCluster)
		assert.True(t, bool(color))
		assert.Equal(t, selectedAppIds, appIds)
		assert.Equal(t, types.HelmNetworkModeOffline, networkMode)
		return nil
	}

	err := ClusterStatus(ClusterStatusFlags{
		ContextFlag:         flags.ContextFlag{HydraContext: "/ctx"},
		ColorFlag:           flags.ColorFlag{Color: true},
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeOffline},
		ExcludeAppFlag:      flags.ExcludeAppFlag{ExcludeAppPatterns: []types.AppIdPattern{"prod.infra.argocd"}},
		AppIdPatterns:       []types.AppIdPattern{"prod.**"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, runCalls)
}

func TestClusterStatus_StopsBeforeStatusComputationWhenSelectionSpansMultipleClusters(t *testing.T) {
	originalResolveAppIds := clusterStatusResolveAppIdsFromConfig
	originalClusterForAppIds := clusterStatusClusterForAppIds
	originalRun := clusterStatusRun
	defer func() {
		clusterStatusResolveAppIdsFromConfig = originalResolveAppIds
		clusterStatusClusterForAppIds = originalClusterForAppIds
		clusterStatusRun = originalRun
	}()

	selectedAppIds := sets.New[types.AppId](
		types.AppId("prod.apps.api"),
		types.AppId("dev.apps.api"),
	)

	clusterStatusResolveAppIdsFromConfig = func(log.Logger, types.HydraContext, types.Config, []types.AppIdPattern, []types.AppIdPattern, types.HelmNetworkMode, bool) (sets.Set[types.AppId], error) {
		return selectedAppIds, nil
	}

	clusterStatusClusterForAppIds = func(types.Config, types.HydraContext, sets.Set[types.AppId], corehydra.RESTClientLimits) (*corehydra.Cluster, error) {
		return nil, assert.AnError
	}

	clusterStatusRun = func(*corehydra.Cluster, types.Color, sets.Set[types.AppId], types.HelmNetworkMode) error {
		t.Fatalf("cluster status computation must not run when single-cluster enforcement fails")
		return nil
	}

	err := ClusterStatus(ClusterStatusFlags{
		ContextFlag:   flags.ContextFlag{HydraContext: "/ctx"},
		AppIdPatterns: []types.AppIdPattern{"prod.**", "dev.**"},
	})
	require.Error(t, err)
}
