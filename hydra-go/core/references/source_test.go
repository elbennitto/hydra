package references

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestAnnotateRefsWithSource(t *testing.T) {
	refs := []types.Ref{{
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		From:         "a", To: "b",
	}}
	out := AnnotateRefsWithSource(refs, types.RefSourceTemplate)
	require.Len(t, out, 1)
	assert.Equal(t, []types.RefAttribute{
		{Type: types.RefAttributeOriginSource, Value: types.RefSourceTemplate},
	}, out[0].Attributes)
}

func TestMergeRefLists_unionsAttributes(t *testing.T) {
	a := AnnotateRefsWithSource([]types.Ref{{
		RefType: types.RefTypeDirect, EndpointType: types.RefEndpointTypeId,
		From: "x", To: "y", Labels: []string{"one"},
	}}, types.RefSourceTemplate)
	b := AnnotateRefsWithSource([]types.Ref{{
		RefType: types.RefTypeDirect, EndpointType: types.RefEndpointTypeId,
		From: "x", To: "y", Labels: []string{"two"},
	}}, types.RefSourceTest)
	m := MergeRefLists(a, b)
	require.Len(t, m, 1)
	assert.ElementsMatch(t, []string{"one", "two"}, m[0].Labels)
	var sources []string
	for _, attr := range m[0].Attributes {
		if attr.Type == types.RefAttributeOriginSource {
			sources = append(sources, attr.Value)
		}
	}
	assert.ElementsMatch(t, []string{types.RefSourceTemplate, types.RefSourceTest}, sources)
}

func TestRefSourceForEntityKey(t *testing.T) {
	assert.Equal(t, types.RefSourceCluster, RefSourceForEntityKey(types.KeyClusterEntity))
	assert.Equal(t, types.RefSourceTemplate, RefSourceForEntityKey(types.KeyTemplateEntity))
}

func TestEnsureRefsHaveOriginSource(t *testing.T) {
	refs := []types.Ref{{From: "a", To: "b", RefType: types.RefTypeDirect, EndpointType: types.RefEndpointTypeId}}
	out := EnsureRefsHaveOriginSource(refs, types.RefSourceTemplate)
	require.Len(t, out, 1)
	assert.Equal(t, types.RefSourceTemplate, out[0].Attributes[0].Value)
}
