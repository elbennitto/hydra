package references

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestCanonicalizeOwnerRefTargetsToClusterIDs_rewritesVersionSkew(t *testing.T) {
	nodePool, err := entity.NewEntityBuilder().
		WithGroup("kafka.strimzi.io").
		WithVersion("v1").
		WithKind("KafkaNodePool").
		WithNamespace("demo").
		WithName("demo-kafka-broker").
		Build()
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{nodePool})
	require.NoError(t, err)

	canonicalID, err := nodePool.Id()
	require.NoError(t, err)

	from := types.Id("core.strimzi.io/v1/StrimziPodSet/demo/demo-kafka-demo-kafka-broker")
	staleTo := types.Id("kafka.strimzi.io/v1beta2/KafkaNodePool/demo/demo-kafka-broker")

	refs := []types.Ref{{
		From:         from,
		To:           staleTo,
		Reverse:      true,
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		Labels:       []string{"owner"},
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginOwner, Value: types.RefOwnerRoleDependent},
		},
	}}

	out := CanonicalizeOwnerRefTargetsToClusterIDs(refs, ents)
	require.Len(t, out, 1)
	assert.Equal(t, canonicalID, out[0].To)
	assert.Equal(t, from, out[0].From)
}

func TestCanonicalizeOwnerRefTargetsToClusterIDs_skipsNonOwnerRefs(t *testing.T) {
	nodePool, err := entity.NewEntityBuilder().
		WithGroup("kafka.strimzi.io").
		WithVersion("v1").
		WithKind("KafkaNodePool").
		WithNamespace("demo").
		WithName("demo-kafka-broker").
		Build()
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{nodePool})
	require.NoError(t, err)

	staleTo := types.Id("kafka.strimzi.io/v1beta2/KafkaNodePool/demo/demo-kafka-broker")
	refs := []types.Ref{{
		From:         types.Id("v1/Secret/demo/s"),
		To:           staleTo,
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
	}}

	out := CanonicalizeOwnerRefTargetsToClusterIDs(refs, ents)
	require.Len(t, out, 1)
	assert.Equal(t, staleTo, out[0].To, "non-owner refs must not be rewritten")
}

func TestCanonicalizeOwnerRefTargetsToClusterIDs_noMatchLeavesToUnchanged(t *testing.T) {
	refs := []types.Ref{{
		From:         types.Id("core.strimzi.io/v1/StrimziPodSet/demo/x"),
		To:           types.Id("kafka.strimzi.io/v1beta2/KafkaNodePool/demo/missing"),
		Reverse:      true,
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		Attributes: []types.RefAttribute{
			{Type: types.RefAttributeOriginOwner, Value: types.RefOwnerRoleDependent},
		},
	}}

	out := CanonicalizeOwnerRefTargetsToClusterIDs(refs, entity.Entities{})
	require.Len(t, out, 1)
	assert.Equal(t, types.Id("kafka.strimzi.io/v1beta2/KafkaNodePool/demo/missing"), out[0].To)
}
