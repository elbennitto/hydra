package commands

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type entityLogCapture struct {
	level    slog.Level
	entities []string
}

func (h *entityLogCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *entityLogCapture) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "entity" {
			h.entities = append(h.entities, a.Value.String())
		}
		return true
	})
	return nil
}

func (h *entityLogCapture) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *entityLogCapture) WithGroup(_ string) slog.Handler      { return h }

func TestLogEntityIds_LogsEntityIdsInLexicographicOrder(t *testing.T) {
	z := makeEntity("", "v1", "ConfigMap", "ns", "zebra")
	a := makeEntity("", "v1", "ConfigMap", "ns", "alpha")
	ents, err := entity.NewEntities([]entity.Entity{z, a})
	require.NoError(t, err)

	h := &entityLogCapture{level: slog.LevelInfo}
	oldDefault := slog.Default()
	oldLogger := log.Default()
	slog.SetDefault(slog.New(h))
	log.SetDefault(log.NewLoggerWithHandler(h))
	defer func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldDefault)
	}()

	LogEntityIds(log.Default(), ents)

	idA, err := a.Id()
	require.NoError(t, err)
	idZ, err := z.Id()
	require.NoError(t, err)
	require.Len(t, h.entities, 2)
	assert.Equal(t, string(idA), h.entities[0])
	assert.Equal(t, string(idZ), h.entities[1])
}

func TestSeparateEntitiesByForcePredicates_AllMatchForce(t *testing.T) {
	secret1 := makeEntity("", "v1", "Secret", "default", "tls-cert")
	secret2 := makeEntity("", "v1", "Secret", "default", "db-password")

	leftovers, err := entity.NewEntities([]entity.Entity{secret1, secret2})
	require.NoError(t, err)

	predicates := []string{`kind == "Secret"`}

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, predicates, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 2, forceLeftovers.Len())
	assert.Equal(t, 0, untrackedLeftovers.Len())
}

func TestSeparateEntitiesByForcePredicates_NoneMatchForce(t *testing.T) {
	cm := makeEntity("", "v1", "ConfigMap", "default", "app-config")
	sa := makeEntity("", "v1", "ServiceAccount", "default", "runner")

	leftovers, err := entity.NewEntities([]entity.Entity{cm, sa})
	require.NoError(t, err)

	predicates := []string{`kind == "Secret"`}

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, predicates, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 0, forceLeftovers.Len())
	assert.Equal(t, 2, untrackedLeftovers.Len())
}

func TestSeparateEntitiesByForcePredicates_MixedMatch(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "default", "tls-cert")
	cm := makeEntity("", "v1", "ConfigMap", "default", "app-config")
	pvc := makeEntity("", "v1", "PersistentVolumeClaim", "default", "data-vol")

	leftovers, err := entity.NewEntities([]entity.Entity{secret, cm, pvc})
	require.NoError(t, err)

	predicates := []string{`kind == "Secret"`}

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, predicates, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 1, forceLeftovers.Len())
	assert.Equal(t, 2, untrackedLeftovers.Len())

	forceIds := entityIds(t, forceLeftovers)
	assert.Contains(t, forceIds, types.Id("v1/Secret/default/tls-cert"))
}

func TestSeparateEntitiesByForcePredicates_MultiplePredicates(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "demo", "tls-cert")
	pvc := makeEntity("", "v1", "PersistentVolumeClaim", "demo", "data-vol")
	cm := makeEntity("", "v1", "ConfigMap", "demo", "app-config")

	leftovers, err := entity.NewEntities([]entity.Entity{secret, pvc, cm})
	require.NoError(t, err)

	predicates := []string{
		`kind == "Secret"`,
		`id.startsWith("v1/PersistentVolumeClaim/demo/")`,
	}

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, predicates, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 2, forceLeftovers.Len())
	assert.Equal(t, 1, untrackedLeftovers.Len())

	untrackedIds := entityIds(t, untrackedLeftovers)
	assert.Contains(t, untrackedIds, types.Id("v1/ConfigMap/demo/app-config"))
}

func TestSeparateEntitiesByForcePredicates_EmptyLeftovers(t *testing.T) {
	leftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	predicates := []string{`kind == "Secret"`}

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, predicates, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 0, forceLeftovers.Len())
	assert.Equal(t, 0, untrackedLeftovers.Len())
}

func TestSeparateEntitiesByForcePredicates_EmptyPredicates(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "default", "tls-cert")
	cm := makeEntity("", "v1", "ConfigMap", "default", "app-config")

	leftovers, err := entity.NewEntities([]entity.Entity{secret, cm})
	require.NoError(t, err)

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, nil, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 0, forceLeftovers.Len())
	assert.Equal(t, 2, untrackedLeftovers.Len())
}

func TestSeparateEntitiesByForcePredicates_ComplexCelExpression(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "kube-system", "admin-token")
	pvc := makeEntity("", "v1", "PersistentVolumeClaim", "demo", "postgres-data")
	specificRes := makeEntity("apps", "v1", "Deployment", "demo", "specific-resource")
	cm := makeEntity("", "v1", "ConfigMap", "demo", "unrelated")

	leftovers, err := entity.NewEntities([]entity.Entity{secret, pvc, specificRes, cm})
	require.NoError(t, err)

	predicates := []string{
		`kind == "Secret"`,
		`id.startsWith("v1/PersistentVolumeClaim/demo/")`,
		`name == "specific-resource"`,
	}

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, predicates, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 3, forceLeftovers.Len())
	assert.Equal(t, 1, untrackedLeftovers.Len())

	forceIds := entityIds(t, forceLeftovers)
	assert.Contains(t, forceIds, types.Id("v1/Secret/kube-system/admin-token"))
	assert.Contains(t, forceIds, types.Id("v1/PersistentVolumeClaim/demo/postgres-data"))
	assert.Contains(t, forceIds, types.Id("apps/v1/Deployment/demo/specific-resource"))

	untrackedIds := entityIds(t, untrackedLeftovers)
	assert.Contains(t, untrackedIds, types.Id("v1/ConfigMap/demo/unrelated"))
}

func TestSeparateEntitiesByForcePredicates_InvalidCelExpression(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "default", "tls-cert")
	leftovers, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	predicates := []string{"invalid %%% expression"}

	_, _, err = separateEntitiesByForcePredicates(leftovers, predicates, entity.Entities{})
	require.Error(t, err)
}

func TestSeparateEntitiesByForcePredicates_ManagedNamespacesMatchesRendered(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "apps", "image-pull-secret")
	leftovers, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	predicate := `gvk == "v1/Secret" && name == "image-pull-secret" && size(managedNamespaces().filter(n, n == "apps")) > 0`

	forceLeftovers, untrackedLeftovers, err := separateEntitiesByForcePredicates(leftovers, []string{predicate}, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 0, forceLeftovers.Len())
	assert.Equal(t, 1, untrackedLeftovers.Len())

	nsEnt := makeEntity("", "v1", "Namespace", "", "apps")
	rendered, err := entity.NewEntities([]entity.Entity{nsEnt})
	require.NoError(t, err)

	forceLeftovers, untrackedLeftovers, err = separateEntitiesByForcePredicates(leftovers, []string{predicate}, rendered)
	require.NoError(t, err)
	assert.Equal(t, 1, forceLeftovers.Len())
	assert.Equal(t, 0, untrackedLeftovers.Len())
}
