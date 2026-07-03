package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestReviewFindingMessageGroup(t *testing.T) {
	t.Parallel()
	assert.Equal(t, RefOwnershipUnassignedClusterOnlyMessageGroupTitle, ReviewFindingMessageGroup(
		RefOwnershipUnassignedClusterOnlyFinding+": resource v1/Pod/ns/p "+RefOwnershipUnassignedClusterOnlyScopeNote))
	assert.Equal(t, "missing referenced key", ReviewFindingMessageGroup(`missing referenced key "K"`))
	assert.Equal(t, "missing target resource", ReviewFindingMessageGroup("missing target resource"))
}

func TestWriteReviewFindingText_Plain(t *testing.T) {
	var buf bytes.Buffer
	err := WriteReviewFindingText(&buf, ReviewFinding{
		Target:  types.Id("v1/ConfigMap/ns/cfg"),
		Message: "missing target resource",
		Sources: []types.Id{"apps/v1/Deployment/ns/app"},
	}, types.ColorNo)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Review finding")
	assert.Contains(t, out, "v1/ConfigMap/ns/cfg")
	assert.Contains(t, out, "missing target resource")
	assert.Contains(t, out, "apps/v1/Deployment/ns/app")
	assert.NotContains(t, out, "\x1b[")
}

func TestWriteReviewFindingText_OmitsEmptySources(t *testing.T) {
	var buf bytes.Buffer
	err := WriteReviewFindingText(&buf, ReviewFinding{
		Target:  types.Id("v1/Secret/ns/x"),
		Message: "ref ownership note",
		Sources: nil,
	}, types.ColorNo)
	require.NoError(t, err)
	out := buf.String()
	assert.NotContains(t, out, "Sources")
}

func TestWriteReviewFindingText_ColorUsesAnsi(t *testing.T) {
	var buf bytes.Buffer
	err := WriteReviewFindingText(&buf, ReviewFinding{
		Target:  types.Id("v1/Secret/ns/x"),
		Message: `missing referenced key "K"`,
		Sources: []types.Id{"v1/Pod/ns/p"},
	}, types.ColorYes)
	require.NoError(t, err)
	assert.True(t, strings.Contains(buf.String(), "\x1b["))
}

func TestWriteReviewFindingsGroupedText_GroupsByMessageType(t *testing.T) {
	var buf bytes.Buffer
	msgPrefix := RefOwnershipUnassignedClusterOnlyFinding + ": resource "
	err := WriteReviewFindingsGroupedText(&buf, []ReviewFinding{
		{
			Target:  types.Id("rbac.authorization.k8s.io/v1/RoleBinding/kube-system/a"),
			Message: msgPrefix + `rbac.authorization.k8s.io/v1/RoleBinding/kube-system/a (would remain unassigned for hydra gitops uninstall in this namespace scope)`,
		},
		{
			Target:  types.Id("rbac.authorization.k8s.io/v1/RoleBinding/kube-system/b"),
			Message: msgPrefix + `rbac.authorization.k8s.io/v1/RoleBinding/kube-system/b (would remain unassigned for hydra gitops uninstall in this namespace scope)`,
		},
	}, types.ColorNo)
	require.NoError(t, err)
	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "Message type:"))
	assert.Contains(t, out, RefOwnershipUnassignedClusterOnlyMessageGroupTitle)
	assert.NotContains(t, out, "Detail:")
	assert.Contains(t, out, "kube-system/a")
	assert.Contains(t, out, "kube-system/b")
}
