package hydra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Builtin pseudo-app ids have no Helm chart; HydraAppValues leaves map entries nil for them.
// collectPredicatesByTag must tolerate nil values (regression: panic on hv.Refs).
func TestHydraAppUninstallPredicates_PresetAppsNilHydraValuesNoPanic(t *testing.T) {
	t.Parallel()
	appIds := sets.New[types.AppId]()
	appIds.Insert(types.AppId("in-cluster.preset.local-path-provisioner"))
	preds, err := HydraAppUninstallPredicates(nil, appIds, types.HelmNetworkModeOffline, entity.Entities{})
	require.NoError(t, err)
	assert.Empty(t, preds)
}

func TestHydraAppNamespaceOwners_SkipsPresetApps(t *testing.T) {
	t.Parallel()
	appIds := sets.New[types.AppId]()
	appIds.Insert(types.AppId("in-cluster.preset.local-path-provisioner"))
	out, err := HydraAppNamespaceOwners(nil, appIds, types.HelmNetworkModeOffline)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestHydraAppRefParsers_PresetAppsOnlyNoWithApp(t *testing.T) {
	t.Parallel()
	appIds := sets.New[types.AppId]()
	appIds.Insert(types.AppId("in-cluster.preset.foo"))
	parsers, err := HydraAppRefParsers(nil, appIds, types.HelmNetworkModeOffline, entity.Entities{})
	require.NoError(t, err)
	assert.Empty(t, parsers)
}

func TestHydraAppCloneRules_PresetAppsOnlyNoWithApp(t *testing.T) {
	t.Parallel()
	appIds := sets.New[types.AppId]()
	appIds.Insert(types.AppId("in-cluster.preset.foo"))
	rules, err := HydraAppCloneRules(nil, appIds, types.HelmNetworkModeOffline, entity.Entities{})
	require.NoError(t, err)
	assert.Empty(t, rules)
}
