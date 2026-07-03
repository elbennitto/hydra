package workloadclosure

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestEmptyMatchInput_NoRefsOrEntities(t *testing.T) {
	in := EmptyMatchInput(types.KeyClusterEntity)
	require.Empty(t, in.Refs)
	require.Empty(t, in.EntityByID)
	require.Empty(t, in.UIDMap)
	require.Empty(t, in.Index)
	require.Equal(t, types.KeyClusterEntity, in.Key)
}
