package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestMaterializePostApplyInventory_RenderedOverridesClusterSkipsOrphans(t *testing.T) {
	clusterYaml := types.YamlString(`---
apiVersion: v1
kind: ConfigMap
metadata:
  name: stay-cluster-only
  namespace: ns1
data:
  k: v
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: orphan
  namespace: ns1
data:
  x: "1"
`)
	renderedYaml := types.YamlString(`---
apiVersion: v1
kind: ConfigMap
metadata:
  name: from-template
  namespace: ns1
data:
  a: b
`)
	clusterEnts, err := entity.NewEntitiesFromYaml(log.Default(), clusterYaml, types.KeyClusterEntity)
	require.NoError(t, err)

	renderedEnts, err := entity.NewEntitiesFromYaml(log.Default(), renderedYaml, types.KeyTemplateEntity)
	require.NoError(t, err)

	orphanYaml := types.YamlString(`---
apiVersion: v1
kind: ConfigMap
metadata:
  name: orphan
  namespace: ns1
data:
  x: "1"
`)
	orphans, err := entity.NewEntitiesFromYaml(log.Default(), orphanYaml, types.KeyClusterEntity)
	require.NoError(t, err)

	out, err := materializePostApplyInventory(renderedEnts, clusterEnts, orphans)
	require.NoError(t, err)

	require.Equal(t, 2, out.Len())
	_, ok := out.EntityMap["v1/ConfigMap/ns1/from-template"]
	require.True(t, ok)
	_, ok = out.EntityMap["v1/ConfigMap/ns1/stay-cluster-only"]
	require.True(t, ok)
	_, ok = out.EntityMap["v1/ConfigMap/ns1/orphan"]
	require.False(t, ok)
}
