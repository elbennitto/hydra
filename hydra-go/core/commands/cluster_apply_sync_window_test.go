package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestApplyClusterApplySyncWindowToEntities_KeepOrPreventNew(t *testing.T) {
	proj := makeAppProjectEntity("p", []any{
		map[string]any{"kind": "allow", "manualSync": true},
	}, types.KeyTemplateEntity)
	ents, err := entity.NewEntities([]entity.Entity{proj})
	require.NoError(t, err)

	out, n, err := ApplyClusterApplySyncWindowToEntities(
		log.Default(), ents, entity.Entities{}, types.KeyTemplateEntity, types.KeyClusterEntity,
		types.ClusterApplySyncWindowKeepOrPrevent, true)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, out.Items, 1)
	tu, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	require.True(t, ok)
	sw, _, _ := unstructured.NestedSlice(tu.Object, "spec", "syncWindows")
	require.Len(t, sw, 1)
	m := sw[0].(map[string]any)
	assert.Equal(t, "deny", m["kind"])
	assert.Equal(t, false, m["manualSync"])
}
