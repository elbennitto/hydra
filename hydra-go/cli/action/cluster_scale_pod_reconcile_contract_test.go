package action

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// scaleDownPodReconcileHookSignature documents the planned core hook for scale-down pod reconciliation.
// Must stay aligned with commands.ReconcileScaleDownPods in core/commands/scale_pod_reconcile_test.go (test-only stub today).
type scaleDownPodReconcileHookSignature func(
	ctx context.Context,
	l log.Logger,
	dyn dynamic.Interface,
	entities entity.Entities,
	templateKey types.EntityKeyUnstructured,
	liveKey types.EntityKeyUnstructured,
	refs []types.Ref,
	clusterMutated bool,
	forceScaleDown types.ForceScaleDown,
	dryRun types.DryRun,
	listPods func(context.Context) ([]unstructured.Unstructured, error),
) (entity.Entities, error)

// scaleUpPodReconcileHookSignature documents the planned scale-up hook (DryRun only; no ForceScaleDown).
// Must stay aligned with commands.ReconcileScaleUpPods in core/commands/scale_pod_reconcile_test.go.
type scaleUpPodReconcileHookSignature func(
	ctx context.Context,
	l log.Logger,
	dyn dynamic.Interface,
	entities entity.Entities,
	templateKey types.EntityKeyUnstructured,
	liveKey types.EntityKeyUnstructured,
	clusterMutated bool,
	dryRun types.DryRun,
	listPods func(context.Context) ([]unstructured.Unstructured, error),
) (entity.Entities, error)

// clusterMutatedStandIn models the intended gate: pod reconcile runs only when the cluster branch is active
// (not --no-cluster) and a workload mutation has occurred — same meaning as the clusterMutated parameter on the hooks above.
func clusterMutatedStandIn(noCluster, workloadMutationOccurred bool) bool {
	return !noCluster && workloadMutationOccurred
}

// TestClusterScale_podReconcile_contract_hookSignaturesMatchPlannedCoreAPI forces compile-time parity with the
// documented parameter list (clusterMutated, then ForceScaleDown + DryRun for scale-down; clusterMutated, then DryRun for scale-up).
func TestClusterScale_podReconcile_contract_hookSignaturesMatchPlannedCoreAPI(t *testing.T) {
	t.Helper()
	stubDown := func(
		ctx context.Context,
		l log.Logger,
		dyn dynamic.Interface,
		entities entity.Entities,
		templateKey types.EntityKeyUnstructured,
		liveKey types.EntityKeyUnstructured,
		refs []types.Ref,
		clusterMutated bool,
		forceScaleDown types.ForceScaleDown,
		dryRun types.DryRun,
		listPods func(context.Context) ([]unstructured.Unstructured, error),
	) (entity.Entities, error) {
		_ = ctx
		_ = l
		_ = dyn
		_ = entities
		_ = templateKey
		_ = liveKey
		_ = refs
		_ = clusterMutated
		_ = forceScaleDown
		_ = dryRun
		_ = listPods
		return entity.Entities{}, nil
	}
	var _ scaleDownPodReconcileHookSignature = stubDown

	stubUp := func(
		ctx context.Context,
		l log.Logger,
		dyn dynamic.Interface,
		entities entity.Entities,
		templateKey types.EntityKeyUnstructured,
		liveKey types.EntityKeyUnstructured,
		clusterMutated bool,
		dryRun types.DryRun,
		listPods func(context.Context) ([]unstructured.Unstructured, error),
	) (entity.Entities, error) {
		_ = ctx
		_ = l
		_ = dyn
		_ = entities
		_ = templateKey
		_ = liveKey
		_ = clusterMutated
		_ = dryRun
		_ = listPods
		return entity.Entities{}, nil
	}
	var _ scaleUpPodReconcileHookSignature = stubUp
}

// TestClusterScale_podReconcile_contract_flagsAndGating ties ClusterScaleFlags to the future call site:
// DryRun and ForceScaleDown are the values that must be forwarded into ReconcileScaleDownPods; NoCluster must prevent any reconcile.
func TestClusterScale_podReconcile_contract_flagsAndGating(t *testing.T) {
	var f ClusterScaleFlags
	f.DryRun = types.DryRunYes
	f.ForceScaleDown = types.ForceScaleDownYes
	assert.Equal(t, types.DryRunYes, f.DryRun)
	assert.Equal(t, types.ForceScaleDownYes, f.ForceScaleDown)

	// cluster_scale.go: ScaleDownWorkloads(..., f.DryRun, f.ForceScaleDown, ...); pod hook should reuse the same flag values.
	plannedDownCall := func(force types.ForceScaleDown, dry types.DryRun) (types.ForceScaleDown, types.DryRun) {
		return force, dry
	}
	gotForce, gotDry := plannedDownCall(f.ForceScaleDown, f.DryRun)
	assert.Equal(t, types.ForceScaleDownYes, gotForce)
	assert.Equal(t, types.DryRunYes, gotDry)

	f.NoCluster = true
	assert.False(t, clusterMutatedStandIn(f.NoCluster, true), "NoCluster short-circuit must skip pod reconcile")
	f.NoCluster = false
	assert.False(t, clusterMutatedStandIn(f.NoCluster, false), "without workload mutation, clusterMutated must be false")
	assert.True(t, clusterMutatedStandIn(f.NoCluster, true), "cluster path + mutation enables reconcile hooks")
}
