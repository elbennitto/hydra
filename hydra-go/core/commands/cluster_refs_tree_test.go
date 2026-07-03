package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestClusterTreeGraph_CandidateIds_Empty(t *testing.T) {
	g := &ClusterTreeGraph{}
	ids := g.CandidateIds()
	if len(ids) != 0 {
		t.Fatalf("expected no ids, got %d", len(ids))
	}
}

func TestClusterTreeGraph_PickerRowStatus(t *testing.T) {
	idT := types.Id("v1/ConfigMap/ns/cm-t")
	idC := types.Id("v1/Secret/ns/secret-c")
	idBoth := types.Id("v1/ServiceAccount/ns/sa-both")
	g := &ClusterTreeGraph{
		TemplateEnts: entity.Entities{IdSet: sets.New(idT, idBoth)},
		ClusterEnts:  entity.Entities{IdSet: sets.New(idC, idBoth)},
	}
	assert.Equal(t, "template only", g.PickerRowStatus(idT))
	assert.Equal(t, "cluster only", g.PickerRowStatus(idC))
	assert.Equal(t, "ok", g.PickerRowStatus(idBoth))
	idNeither := types.Id("v1/Pod/orphan/ns/p")
	assert.Equal(t, "neither", g.PickerRowStatus(idNeither))
}

func TestResourceInventoryPresenceStatus(t *testing.T) {
	assert.Equal(t, "ok", ResourceInventoryPresenceStatus(true, true))
	assert.Equal(t, "template only", ResourceInventoryPresenceStatus(true, false))
	assert.Equal(t, "cluster only", ResourceInventoryPresenceStatus(false, true))
	assert.Equal(t, "neither", ResourceInventoryPresenceStatus(false, false))
}

func TestClusterTreeGraph_EnsureStartId_EmptyId(t *testing.T) {
	g := &ClusterTreeGraph{
		Refs:         nil,
		TemplateEnts: entity.Entities{},
		ClusterEnts:  entity.Entities{},
	}
	if err := g.EnsureStartId(types.Id("")); err != nil {
		t.Fatalf("empty start id: %v", err)
	}
}

func TestLocalTreeGraph_CandidateIds_Empty(t *testing.T) {
	g := &LocalTreeGraph{}
	ids := g.CandidateIds()
	if len(ids) != 0 {
		t.Fatalf("expected no ids, got %d", len(ids))
	}
}

func TestLocalTreeGraph_CandidateIds_FromEntitiesAndRefs(t *testing.T) {
	idEnt := types.Id("v1/ConfigMap/ns/cm-ent-only")
	idFrom := types.Id("v1/Secret/ns/secret-from")
	idTo := types.Id("v1/Service/ns/svc-to")

	entOnly := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithName(types.Name("cm-ent-only")).
		WithNamespace(types.Namespace("ns")))
	svcTo := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Service"))).
		WithName(types.Name("svc-to")).
		WithNamespace(types.Namespace("ns")))
	ents, err := entity.NewEntities([]entity.Entity{entOnly, svcTo})
	require.NoError(t, err)

	g := &LocalTreeGraph{
		Refs: []types.Ref{
			{From: idFrom, To: idTo},
		},
		Entities: ents,
	}
	ids := g.CandidateIds()
	want := []types.Id{idEnt, idFrom, idTo}
	assert.Equal(t, want, ids, "expected sorted deduplicated ids from entities and ref endpoints")
}

func TestLocalTreeGraph_EnsureStartId_EmptyId(t *testing.T) {
	g := &LocalTreeGraph{
		Refs:     nil,
		Entities: entity.Entities{},
	}
	require.NoError(t, g.EnsureStartId(types.Id("")))
}

func TestLocalTreeGraph_EnsureStartId_Known(t *testing.T) {
	cm := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithName(types.Name("cm-only-entities")).
		WithNamespace(types.Namespace("ns")))
	ents, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	gEnt := &LocalTreeGraph{
		Entities: ents,
		Refs:     nil,
	}
	require.NoError(t, gEnt.EnsureStartId(types.Id("v1/ConfigMap/ns/cm-only-entities")))

	idFrom := types.Id("v1/Pod/ns/pod-from")
	idTo := types.Id("v1/Service/ns/svc-to")
	gRefs := &LocalTreeGraph{
		Entities: entity.Entities{},
		Refs:     []types.Ref{{From: idFrom, To: idTo}},
	}
	require.NoError(t, gRefs.EnsureStartId(idFrom))
	require.NoError(t, gRefs.EnsureStartId(idTo))
}

func TestLocalTreeGraph_EnsureStartId_Unknown(t *testing.T) {
	cm := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithName(types.Name("cm")).
		WithNamespace(types.Namespace("ns")))
	ents, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	g := &LocalTreeGraph{
		Entities: ents,
		Refs: []types.Ref{
			{From: types.Id("v1/Secret/ns/sec"), To: types.Id("v1/Service/ns/svc")},
		},
	}
	err = g.EnsureStartId(types.Id("apps/v1/Deployment/ns/not-in-graph"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown resource id")
}

func TestLocalTreeGraph_RefsStableAcrossMultipleCalls(t *testing.T) {
	refs := []types.Ref{
		{From: types.Id("v1/ConfigMap/ns/cm"), To: types.Id("v1/Secret/ns/sec")},
	}
	g := &LocalTreeGraph{
		Refs:     refs,
		Entities: entity.Entities{},
	}
	refsSnapshot := append([]types.Ref(nil), g.Refs...)

	ids1 := g.CandidateIds()
	ids2 := g.CandidateIds()
	require.Equal(t, ids1, ids2)

	for i := 0; i < 5; i++ {
		_ = g.CandidateIds()
		got := append([]types.Ref(nil), g.Refs...)
		require.Equal(t, refsSnapshot, got, "Refs must not be mutated by CandidateIds (iteration %d)", i)
	}
	require.Equal(t, refsSnapshot, append([]types.Ref(nil), g.Refs...))
}

// TestClusterTree_strimziKafkaNormalizedIdMatchesClusterStyleId guards the cluster-tree pipeline
// (clusterRefsAllClusterTreeWithEntities): template manifests may use Kafka v1beta2 while the
// apiserver stores v1. MergeRefLists keys refs by types.Id; without NormalizeApiVersions on template
// entities, the same Kafka resource appears twice with misleading "missing" / "cluster only" status.
func TestClusterTree_strimziKafkaNormalizedIdMatchesClusterStyleId(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "ns",
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	clusterScope := types.ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka": types.ScopeInfo{},
	}

	result, err := testNormalizeApiVersions(t, entities, clusterScope)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	id, err := result.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("kafka.strimzi.io/v1/Kafka/ns/my-kafka"), id)
}
