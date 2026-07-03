package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	corehydra "hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestArgocdStatus_ZeroArgsUsesImplicitAllSelectionAndStaysReadOnly(t *testing.T) {
	originalResolvePathWithCluster := argocdResolvePathWithCluster
	originalResolveStatusSelection := argocdResolveStatusSelection
	originalStatusRun := argocdStatusRun
	originalSyncSetRun := argocdSyncSetRun
	defer func() {
		argocdResolvePathWithCluster = originalResolvePathWithCluster
		argocdResolveStatusSelection = originalResolveStatusSelection
		argocdStatusRun = originalStatusRun
		argocdSyncSetRun = originalSyncSetRun
	}()

	cluster := &corehydra.Cluster{}
	resolvePathCalls := 0
	argocdResolvePathWithCluster = func(_ log.Logger, context types.HydraContext, clusterName types.ClusterName, config types.Config, _ corehydra.RESTClientLimits) (*corehydra.Cluster, error) {
		resolvePathCalls++
		assert.Equal(t, types.HydraContext("/ctx"), context)
		assert.Equal(t, types.InCluster, clusterName)
		return cluster, nil
	}

	argocdResolveStatusSelection = func(_ corehydra.Hydra, includePatterns []string, excludePatterns []types.AppIdPattern) ([]string, error) {
		assert.Empty(t, includePatterns, "zero args must trigger the implicit all-visible selection")
		assert.Equal(t, []types.AppIdPattern{"prod.infra.argocd", "prod.apps.worker"}, excludePatterns)
		return []string{"prod.apps.api"}, nil
	}

	statusCalls := 0
	argocdStatusRun = func(actualCluster *corehydra.Cluster, color types.Color, appIds []string) error {
		statusCalls++
		assert.Same(t, cluster, actualCluster)
		assert.True(t, bool(color))
		assert.Equal(t, []string{"prod.apps.api"}, appIds)
		return nil
	}

	argocdSyncSetRun = func(*corehydra.Cluster, string, string, bool, types.DryRun) error {
		t.Fatalf("argocd status must stay read-only and must not call the AppProject sync mutator")
		return nil
	}

	err := ArgocdStatus(ArgocdStatusFlags{
		ContextFlag:    flags.ContextFlag{HydraContext: "/ctx"},
		ColorFlag:      flags.ColorFlag{Color: true},
		ExcludeAppFlag: flags.ExcludeAppFlag{ExcludeAppPatterns: []types.AppIdPattern{"prod.infra.argocd", "prod.apps.worker"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, resolvePathCalls)
	assert.Equal(t, 1, statusCalls)
}

func TestArgocdSyncManual_MutatesOnlyTargetsRemainingAfterExcludeSubtraction(t *testing.T) {
	originalResolvePathWithCluster := argocdResolvePathWithCluster
	originalResolveSyncTargets := argocdResolveSyncTargets
	originalStatusRun := argocdStatusRun
	originalSyncSetRun := argocdSyncSetRun
	defer func() {
		argocdResolvePathWithCluster = originalResolvePathWithCluster
		argocdResolveSyncTargets = originalResolveSyncTargets
		argocdStatusRun = originalStatusRun
		argocdSyncSetRun = originalSyncSetRun
	}()

	cluster := &corehydra.Cluster{}
	argocdResolvePathWithCluster = func(_ log.Logger, context types.HydraContext, clusterName types.ClusterName, config types.Config, _ corehydra.RESTClientLimits) (*corehydra.Cluster, error) {
		assert.Equal(t, types.HydraContext("/ctx"), context)
		assert.Equal(t, types.InCluster, clusterName)
		return cluster, nil
	}

	argocdResolveSyncTargets = func(_ corehydra.Hydra, includePatterns []string, excludePatterns []types.AppIdPattern) ([]string, error) {
		assert.Equal(t, []string{"prod.**"}, includePatterns)
		assert.Equal(t, []types.AppIdPattern{"prod.apps.worker", "prod.infra.argocd"}, excludePatterns)
		return []string{"prod.apps.api"}, nil
	}

	argocdStatusRun = func(*corehydra.Cluster, types.Color, []string) error {
		t.Fatalf("argocd sync must not route through the read-only status runner")
		return nil
	}

	var mutated []string
	argocdSyncSetRun = func(actualCluster *corehydra.Cluster, appId string, kind string, manualSync bool, dryRun types.DryRun) error {
		assert.Same(t, cluster, actualCluster)
		assert.Equal(t, "deny", kind)
		assert.True(t, manualSync)
		assert.True(t, bool(dryRun))
		mutated = append(mutated, appId)
		return nil
	}

	err := ArgocdSyncManual(ArgocdSyncSetFlags{
		ContextFlag:    flags.ContextFlag{HydraContext: "/ctx"},
		ColorFlag:      flags.ColorFlag{Color: true},
		DryRunFlag:     flags.DryRunFlag{DryRun: true},
		ExcludeAppFlag: flags.ExcludeAppFlag{ExcludeAppPatterns: []types.AppIdPattern{"prod.apps.worker", "prod.infra.argocd"}},
		AppIds:         []string{"prod.**"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"prod.apps.api"}, mutated)
}

func TestArgocdStatus_ExplicitIncludePatternsSubtractExcludesBeforeReadOnlyStatusRun(t *testing.T) {
	originalResolvePathWithCluster := argocdResolvePathWithCluster
	originalResolveStatusSelection := argocdResolveStatusSelection
	originalStatusRun := argocdStatusRun
	originalSyncSetRun := argocdSyncSetRun
	defer func() {
		argocdResolvePathWithCluster = originalResolvePathWithCluster
		argocdResolveStatusSelection = originalResolveStatusSelection
		argocdStatusRun = originalStatusRun
		argocdSyncSetRun = originalSyncSetRun
	}()

	cluster := &corehydra.Cluster{}
	argocdResolvePathWithCluster = func(_ log.Logger, context types.HydraContext, clusterName types.ClusterName, config types.Config, _ corehydra.RESTClientLimits) (*corehydra.Cluster, error) {
		assert.Equal(t, types.HydraContext("/ctx"), context)
		assert.Equal(t, types.InCluster, clusterName)
		return cluster, nil
	}

	argocdResolveStatusSelection = func(_ corehydra.Hydra, includePatterns []string, excludePatterns []types.AppIdPattern) ([]string, error) {
		assert.Equal(t, []string{"prod.**"}, includePatterns)
		assert.Equal(t, []types.AppIdPattern{"prod.apps.worker"}, excludePatterns)
		return []string{"prod.apps.api"}, nil
	}

	argocdSyncSetRun = func(*corehydra.Cluster, string, string, bool, types.DryRun) error {
		t.Fatalf("argocd status must not mutate sync windows")
		return nil
	}

	statusCalls := 0
	argocdStatusRun = func(actualCluster *corehydra.Cluster, color types.Color, appIds []string) error {
		statusCalls++
		assert.Same(t, cluster, actualCluster)
		assert.Equal(t, []string{"prod.apps.api"}, appIds)
		assert.False(t, bool(color))
		return nil
	}

	err := ArgocdStatus(ArgocdStatusFlags{
		ContextFlag:    flags.ContextFlag{HydraContext: "/ctx"},
		ExcludeAppFlag: flags.ExcludeAppFlag{ExcludeAppPatterns: []types.AppIdPattern{"prod.apps.worker"}},
		AppIds:         []string{"prod.**"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, statusCalls)
}
