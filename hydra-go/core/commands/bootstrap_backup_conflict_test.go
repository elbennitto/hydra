package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestFindBootstrapBackupApplyConflicts_NoCandidates(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "ns", "my-secret")
	entities, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	conflicts, err := FindBootstrapBackupApplyConflicts(nil, entities)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestFindBootstrapBackupApplyConflicts_NoMatchingSecret(t *testing.T) {
	secret := makeEntity("", "v1", "Secret", "ns", "my-secret")
	entities, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	conflicts, err := FindBootstrapBackupApplyConflicts(
		[]BackupRestoreCandidateInfo{{SecretId: "v1/Secret/other/other", BackupFile: "/tmp/backup.yaml"}},
		entities,
	)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestFindBootstrapBackupApplyConflicts_FindsConflict(t *testing.T) {
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns")).
		WithName(types.Name("dup")).
		WithTemplatePath(types.TemplatePath("templates/secret.yaml")).
		WithAppIds([]types.AppId{"cluster.app.one"})
	secret := mustBuild(b)

	entities, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	conflicts, err := FindBootstrapBackupApplyConflicts(
		[]BackupRestoreCandidateInfo{{SecretId: "v1/Secret/ns/dup", BackupFile: "/backup/path/sops.yaml"}},
		entities,
	)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	assert.Equal(t, "v1/Secret/ns/dup", conflicts[0].SecretID)
	assert.Equal(t, "/backup/path/sops.yaml", conflicts[0].BackupFile)
	assert.Contains(t, conflicts[0].ApplySourceDesc, "template manifest")
	assert.Contains(t, conflicts[0].ApplySourceDesc, "cluster.app.one")
}

func TestFindBootstrapBackupApplyConflicts_BootstrapDerivedDescription(t *testing.T) {
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns")).
		WithName(types.Name("derived")).
		WithAppIds([]types.AppId{"cluster.app.two"})
	secret := mustBuild(b)

	entities, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)

	conflicts, err := FindBootstrapBackupApplyConflicts(
		[]BackupRestoreCandidateInfo{{SecretId: "v1/Secret/ns/derived", BackupFile: "/x/backup.yaml"}},
		entities,
	)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	assert.Contains(t, conflicts[0].ApplySourceDesc, "bootstrap-derived Secret from SopsSecret conversion")
	assert.Contains(t, conflicts[0].ApplySourceDesc, "cluster.app.two")
}
