package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestMergePersistentVolumesFromInventoryRefs_AddsPvForPvcRef(t *testing.T) {
	pvc := makeEntity("", "v1", "PersistentVolumeClaim", "default", "my-pvc")
	pv := makeEntity("", "v1", "PersistentVolume", "", "my-pv")
	cm := makeEntity("", "v1", "ConfigMap", "default", "other")

	uninstalls, err := entity.NewEntities([]entity.Entity{pvc})
	require.NoError(t, err)
	clusterEntities, err := entity.NewEntities([]entity.Entity{pvc, pv, cm})
	require.NoError(t, err)

	pvcId, err := pvc.Id()
	require.NoError(t, err)
	pvId, err := pv.Id()
	require.NoError(t, err)

	refs := []types.Ref{
		{From: pvcId, To: pvId},
	}

	merged, err := mergePersistentVolumesFromInventoryRefs(refs, uninstalls, clusterEntities)
	require.NoError(t, err)
	assert.Equal(t, 2, merged.Len())
	ids := entityIds(t, merged)
	assert.Contains(t, ids, pvcId)
	assert.Contains(t, ids, pvId)
}

func TestMergePersistentVolumesFromInventoryRefs_IgnoresNonPvOutgoing(t *testing.T) {
	pvc := makeEntity("", "v1", "PersistentVolumeClaim", "default", "my-pvc")
	sc := makeEntity("storage.k8s.io", "v1", "StorageClass", "", "fast")

	uninstalls, err := entity.NewEntities([]entity.Entity{pvc})
	require.NoError(t, err)
	clusterEntities, err := entity.NewEntities([]entity.Entity{pvc, sc})
	require.NoError(t, err)

	pvcId, err := pvc.Id()
	require.NoError(t, err)
	scId, err := sc.Id()
	require.NoError(t, err)

	refs := []types.Ref{{From: pvcId, To: scId}}

	merged, err := mergePersistentVolumesFromInventoryRefs(refs, uninstalls, clusterEntities)
	require.NoError(t, err)
	assert.Equal(t, 1, merged.Len())
}

func TestMergePersistentVolumesFromInventoryRefs_NoPvcInUninstalls(t *testing.T) {
	pv := makeEntity("", "v1", "PersistentVolume", "", "my-pv")
	deploy := makeEntity("apps", "v1", "Deployment", "default", "app")

	uninstalls, err := entity.NewEntities([]entity.Entity{deploy})
	require.NoError(t, err)
	clusterEntities, err := entity.NewEntities([]entity.Entity{pv})
	require.NoError(t, err)

	pvId, err := pv.Id()
	require.NoError(t, err)
	deployId, err := deploy.Id()
	require.NoError(t, err)

	refs := []types.Ref{{From: deployId, To: pvId}}

	merged, err := mergePersistentVolumesFromInventoryRefs(refs, uninstalls, clusterEntities)
	require.NoError(t, err)
	assert.Equal(t, 1, merged.Len())
}
