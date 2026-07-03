package references

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

const stsVCTReplicas0YAML = `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: web
  namespace: default
spec:
  serviceName: web
  replicas: 0
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: app
          image: nginx
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 1Gi
`

func pvcOverlayYAML(withOwner bool, ownerName string, controller bool) string {
	base := `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data-web-0
  namespace: default
`
	if !withOwner {
		return base
	}
	return base + `  ownerReferences:
    - apiVersion: apps/v1
      kind: StatefulSet
      name: ` + ownerName + `
      controller: ` + boolYAML(controller) + `
`
}

func boolYAML(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func vctRefFromStatefulSetToPVC(refs []types.Ref, stsID, pvcID types.Id) bool {
	for _, r := range refs {
		if r.From != stsID || r.To != pvcID {
			continue
		}
		if slices.Contains(r.Labels, "volumeClaimTemplate") {
			return true
		}
	}
	return false
}

func TestStatefulSetVCT_ClusterInventoryOverlay_LivePVCWithControllerOwnerRef(t *testing.T) {
	ents, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(stsVCTReplicas0YAML),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	overlay, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(pvcOverlayYAML(true, "web", true)),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	refs, err := Refs(log.Default(), ents, types.KeyClusterEntity, nil, entity.Entities{}, overlay, nil)
	require.NoError(t, err)

	stsID := types.Id("apps/v1/StatefulSet/default/web")
	pvcID := types.Id("v1/PersistentVolumeClaim/default/data-web-0")
	require.True(t, vctRefFromStatefulSetToPVC(refs, stsID, pvcID),
		"expected volumeClaimTemplate ref from StatefulSet to live PVC when replicas=0 and ownerRef matches")
}

func TestStatefulSetVCT_ClusterInventoryOverlay_IgnoresPVCWithoutControllerOwnerRef(t *testing.T) {
	ents, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(stsVCTReplicas0YAML),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	overlay, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(pvcOverlayYAML(false, "", false)),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	refs, err := Refs(log.Default(), ents, types.KeyClusterEntity, nil, entity.Entities{}, overlay, nil)
	require.NoError(t, err)

	stsID := types.Id("apps/v1/StatefulSet/default/web")
	pvcID := types.Id("v1/PersistentVolumeClaim/default/data-web-0")
	require.False(t, vctRefFromStatefulSetToPVC(refs, stsID, pvcID),
		"must not emit live volumeClaimTemplate ref without ownerReferences")
}

func TestStatefulSetVCT_ClusterInventoryOverlay_IgnoresPVCWithWrongStatefulSetOwner(t *testing.T) {
	ents, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(stsVCTReplicas0YAML),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	overlay, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(pvcOverlayYAML(true, "other-sts", true)),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	refs, err := Refs(log.Default(), ents, types.KeyClusterEntity, nil, entity.Entities{}, overlay, nil)
	require.NoError(t, err)

	stsID := types.Id("apps/v1/StatefulSet/default/web")
	pvcID := types.Id("v1/PersistentVolumeClaim/default/data-web-0")
	require.False(t, vctRefFromStatefulSetToPVC(refs, stsID, pvcID),
		"must not match live PVC owned by a different StatefulSet")
}

func TestStatefulSetVCT_ClusterInventoryOverlay_RequiresControllerTrue(t *testing.T) {
	ents, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(stsVCTReplicas0YAML),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	overlay, err := entity.NewEntitiesFromYaml(
		log.Default(),
		types.YamlString(pvcOverlayYAML(true, "web", false)),
		types.KeyClusterEntity,
	)
	require.NoError(t, err)

	refs, err := Refs(log.Default(), ents, types.KeyClusterEntity, nil, entity.Entities{}, overlay, nil)
	require.NoError(t, err)

	stsID := types.Id("apps/v1/StatefulSet/default/web")
	pvcID := types.Id("v1/PersistentVolumeClaim/default/data-web-0")
	require.False(t, vctRefFromStatefulSetToPVC(refs, stsID, pvcID),
		"must require ownerReferences.controller == true")
}
