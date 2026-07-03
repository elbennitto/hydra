package hydra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestPreferredVersions_CachesFirstCompute(t *testing.T) {
	c := &Cluster{}
	c.ResetPreferredVersionsCache()
	calls := 0
	pv1, err := c.PreferredVersions(func() (types.ScopeInfoMap, error) {
		calls++
		return types.ScopeInfoMap{
			"apps/v1/Deployment": {},
		}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, pv1)
	assert.Equal(t, types.Version("v1"), pv1[types.NewGroupKindKey("apps", "Deployment")])

	pv2, err := c.PreferredVersions(func() (types.ScopeInfoMap, error) {
		calls++
		return types.ScopeInfoMap{}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, pv1, pv2)
	assert.Equal(t, 1, calls)
}

func TestPreferredVersions_nilClusterRunsComputeWithoutCache(t *testing.T) {
	calls := 0
	pv, err := (*Cluster)(nil).PreferredVersions(func() (types.ScopeInfoMap, error) {
		calls++
		return types.ScopeInfoMap{"v1/Pod": {}}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, pv)
	assert.Equal(t, 1, calls)
}

func TestResetPreferredVersionsCache_allowsRecompute(t *testing.T) {
	c := &Cluster{}
	calls := 0
	_, err := c.PreferredVersions(func() (types.ScopeInfoMap, error) {
		calls++
		return types.ScopeInfoMap{"apps/v1/Deployment": {}}, nil
	})
	require.NoError(t, err)
	c.ResetPreferredVersionsCache()
	_, err = c.PreferredVersions(func() (types.ScopeInfoMap, error) {
		calls++
		return types.ScopeInfoMap{"v1/Service": {}}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}
