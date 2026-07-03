package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	htypes "hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestScaleDependencyRefRoleFromLabeledDirectEdges(t *testing.T) {
	from := htypes.Id("batch/v1/Job/default/j")
	to := htypes.Id("v1/Secret/default/s")

	assert.Equal(t, ScaleDependencyRefRoleUnspecified, scaleDependencyRefRoleFromLabeledDirectEdges(from, to, nil))

	assert.Equal(t, ScaleDependencyRefRoleProduces, scaleDependencyRefRoleFromLabeledDirectEdges(from, to, []htypes.Ref{
		{From: from, To: to, Labels: []string{"source"}},
	}))

	assert.Equal(t, ScaleDependencyRefRolePrerequisite, scaleDependencyRefRoleFromLabeledDirectEdges(from, to, []htypes.Ref{
		{From: from, To: to, Labels: []string{"imagePullSecret"}},
	}))

	assert.Equal(t, ScaleDependencyRefRolePrerequisite, scaleDependencyRefRoleFromLabeledDirectEdges(from, to, []htypes.Ref{
		{From: from, To: to, Labels: []string{"env"}},
	}))

	// source wins over prerequisite labels on the same edge
	assert.Equal(t, ScaleDependencyRefRoleProduces, scaleDependencyRefRoleFromLabeledDirectEdges(from, to, []htypes.Ref{
		{From: from, To: to, Labels: []string{"env", "source"}},
	}))

	// reverse: logical edge is still from→to
	assert.Equal(t, ScaleDependencyRefRolePrerequisite, scaleDependencyRefRoleFromLabeledDirectEdges(from, to, []htypes.Ref{
		{From: to, To: from, Reverse: true, Labels: []string{"imagePullSecret"}},
	}))
}

func TestScaleDependencyRefRoleGVKFallback(t *testing.T) {
	from := htypes.Id("apps/v1/Deployment/demo/w")
	to := htypes.Id("v1/Secret/demo/s")

	assert.Equal(t, ScaleDependencyRefRolePrerequisite, scaleDependencyRefRole(from, to, nil, htypes.KubernetesGvkV1Secret))
	assert.Equal(t, ScaleDependencyRefRolePrerequisite, scaleDependencyRefRole(from, to, nil, htypes.KubernetesGvkV1ConfigMap))
	assert.Equal(t, ScaleDependencyRefRoleDownstream, scaleDependencyRefRole(from, to, nil, htypes.KubernetesGvkAppsV1ReplicaSet))
	assert.Equal(t, ScaleDependencyRefRoleDownstream, scaleDependencyRefRole(from, to, nil, htypes.KubernetesGvkV1Pod))

	assert.Equal(t, ScaleDependencyRefRoleUnspecified, scaleDependencyRefRole(from, to, nil, ""))
	assert.Equal(t, ScaleDependencyRefRoleUnspecified, scaleDependencyRefRole(from, to, nil, htypes.KubernetesGvkAppsV1Deployment))

	// Labeled direct edge wins over GVK (source on Secret stays produces, not prerequisite fallback)
	assert.Equal(t, ScaleDependencyRefRoleProduces, scaleDependencyRefRole(from, to, []htypes.Ref{
		{From: from, To: to, Labels: []string{"source"}},
	}, htypes.KubernetesGvkV1Secret))
}

func TestScaleDependencyRefFlowTagDownstream(t *testing.T) {
	assert.Equal(t, "out", scaleDependencyRefFlowTag(ScaleDependencyRefRoleDownstream))
}
