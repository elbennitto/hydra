package action

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestPlannedUninstallForceSplit(t *testing.T) {
	deploy := withClusterUnstructured(makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "app"), nil, nil)
	secret := withClusterUnstructured(makeTestEntity("", "v1", "Secret", "secrets", "default", "tls"), nil, nil)
	uninstalls, err := entity.NewEntities([]entity.Entity{deploy})
	require.NoError(t, err)
	forceLeftovers, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)
	planned, err := uninstalls.Merge(forceLeftovers, types.KeyClusterEntity)
	require.NoError(t, err)

	u, f, total, err := plannedUninstallForceSplit(planned, uninstalls, forceLeftovers)
	require.NoError(t, err)
	assert.Equal(t, 1, u, "uninstall-only id")
	assert.Equal(t, 1, f, "force-only id")
	assert.Equal(t, 2, total)

	overlapU := withClusterUnstructured(makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "same"), nil, nil)
	overlapF := withClusterUnstructured(makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "same"), nil, nil)
	un2, err := entity.NewEntities([]entity.Entity{overlapU})
	require.NoError(t, err)
	fo2, err := entity.NewEntities([]entity.Entity{overlapF})
	require.NoError(t, err)
	planned2, err := un2.Merge(fo2, types.KeyClusterEntity)
	require.NoError(t, err)
	u2, f2, tot2, err := plannedUninstallForceSplit(planned2, un2, fo2)
	require.NoError(t, err)
	assert.Equal(t, 1, u2)
	assert.Equal(t, 0, f2)
	assert.Equal(t, 1, tot2, "same id in both maps counts as uninstall")
}

func TestErrAutomaticPreUninstallBackup(t *testing.T) {
	t.Run("nil means success", func(t *testing.T) {
		require.NoError(t, errAutomaticPreUninstallBackup(nil))
	})

	t.Run("backup error aborts with ErrAborted", func(t *testing.T) {
		underlying := fmt.Errorf(`failed to list cluster secrets: Get "https://198.51.100.42:6443/api/v1/secrets": dial tcp 198.51.100.42:6443: i/o timeout`)
		err := errAutomaticPreUninstallBackup(underlying)
		require.Error(t, err)
		assert.True(t, errors.ErrAborted.MatchesError(err))
		assert.Contains(t, err.Error(), "automatic backup before uninstallation failed")
		assert.Contains(t, err.Error(), "i/o timeout")
	})
}

func TestFinishClusterUninstallAfterEarlyCleanup(t *testing.T) {
	t.Run("cleanup work makes empty main delete phase successful", func(t *testing.T) {
		err := finishClusterUninstallAfterEarlyCleanup(testLogger(), fmt.Errorf("pending nothing todo"), 2, 0)
		require.NoError(t, err)
	})

	t.Run("empty with no cleanup preserves pending nothing-to-do error", func(t *testing.T) {
		pending := fmt.Errorf("pending nothing todo")
		err := finishClusterUninstallAfterEarlyCleanup(testLogger(), pending, 0, 0)
		require.EqualError(t, err, pending.Error())
	})

	t.Run("empty with no cleanup reports nothing to do", func(t *testing.T) {
		err := finishClusterUninstallAfterEarlyCleanup(testLogger(), nil, 0, 0)
		require.Error(t, err)
		assert.True(t, errors.ErrNothingTodo.MatchesError(err))
	})
}

func TestResolveForceLeftovers_ForceOnly_WithForceFlag(t *testing.T) {
	forceEntity := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceEntity})
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	uninstalls, err := entity.NewEntities(nil)
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForce, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "force entities should be added to uninstalls")
}

func TestResolveForceLeftovers_ForceOnly_WithKeepFlag(t *testing.T) {
	forceEntity := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceEntity})
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	existingEntity := makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "my-app")
	uninstalls, err := entity.NewEntities([]entity.Entity{existingEntity})
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallKeep, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "existing uninstalls preserved, force entities not added")
}

func TestResolveForceLeftovers_KeepFlag_FiltersNamespacesOfKeptResources(t *testing.T) {
	forceEntity := makeTestEntity("", "v1", "Secret", "secrets", "my-ns", "tls-cert")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceEntity})
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	deployEntity := makeTestEntity("apps", "v1", "Deployment", "deployments", "my-ns", "my-app")
	nsEntity := makeTestEntity("", "v1", "Namespace", "namespaces", "", "my-ns")
	otherNsEntity := makeTestEntity("", "v1", "Namespace", "namespaces", "", "other-ns")
	uninstalls, err := entity.NewEntities([]entity.Entity{deployEntity, nsEntity, otherNsEntity})
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallKeep, uninstalls,
	)
	require.NoError(t, err)

	ids := make([]types.Id, 0, result.Len())
	for _, e := range result.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}

	assert.Equal(t, 2, result.Len(), "namespace of kept resources should be removed from uninstalls")
	assert.Contains(t, ids, types.Id("apps/v1/Deployment/my-ns/my-app"), "non-namespace resources should remain")
	assert.Contains(t, ids, types.Id("v1/Namespace//other-ns"), "unrelated namespaces should remain")
	assert.NotContains(t, ids, types.Id("v1/Namespace//my-ns"), "namespace containing kept resources must be filtered out")
}

func TestMergePlannedUninstallForAbort(t *testing.T) {
	deploy := makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "my-app")
	secret := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls")
	cm := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "extra")

	uninstalls, err := entity.NewEntities([]entity.Entity{deploy})
	require.NoError(t, err)
	forceLeftovers, err := entity.NewEntities([]entity.Entity{secret})
	require.NoError(t, err)
	untracked, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	out, err := mergePlannedUninstallForAbort(uninstalls, forceLeftovers, entity.Entities{})
	require.NoError(t, err)
	assert.Equal(t, 2, out.Len())

	out, err = mergePlannedUninstallForAbort(uninstalls, forceLeftovers, untracked)
	require.NoError(t, err)
	assert.Equal(t, 3, out.Len())
}

func TestResolveForceLeftovers_ForceOnly_NoFlag(t *testing.T) {
	forceEntity := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceEntity})
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	uninstalls, err := entity.NewEntities(nil)
	require.NoError(t, err)

	_, err = resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallNone, uninstalls,
	)
	require.Error(t, err)
	assert.True(t, errors.ErrAborted.MatchesError(err), "should abort with ErrAborted when no flag is provided")
	assert.Contains(t, err.Error(), "--force", "abort message should hint at --force flag")
	assert.Contains(t, err.Error(), "--keep", "abort message should hint at --keep flag")
}

func TestResolveForceLeftovers_ForceOnly_NoFlag_LogsStrongUninstallOwnership(t *testing.T) {
	forceEntity := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceEntity})
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	uninstalls, err := entity.NewEntities(nil)
	require.NoError(t, err)

	output := captureActionLogs(t, func(logger log.Logger) {
		_, err = resolveForceLeftovers(
			logger,
			forceLeftovers, untrackedLeftovers,
			types.ForceUninstallNone, uninstalls,
		)
		require.Error(t, err)
	})

	assert.Contains(t, output, "match priority >= 0 uninstall ownership rules")
	assert.NotContains(t, output, "match uninstall-force rules")
}

func TestResolveForceLeftovers_UntrackedPresent_WithForceFlag(t *testing.T) {
	forceLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	untrackedEntity := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "unknown-cm")
	untrackedLeftovers, err := entity.NewEntities([]entity.Entity{untrackedEntity})
	require.NoError(t, err)

	uninstalls, err := entity.NewEntities(nil)
	require.NoError(t, err)

	_, err = resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForce, uninstalls,
	)
	require.Error(t, err)
	assert.True(t, errors.ErrAborted.MatchesError(err), "untracked leftovers always block uninstallation")
	assert.Contains(t, err.Error(), "selected app")
	assert.Contains(t, err.Error(), "priority >= 0 uninstall ownership rules")
	assert.Contains(t, err.Error(), "clone rules")
}

func TestResolveForceLeftovers_MixedLeftovers_WithForceFlag(t *testing.T) {
	forceEntity := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceEntity})
	require.NoError(t, err)

	untrackedEntity := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "unknown-cm")
	untrackedLeftovers, err := entity.NewEntities([]entity.Entity{untrackedEntity})
	require.NoError(t, err)

	uninstalls, err := entity.NewEntities(nil)
	require.NoError(t, err)

	_, err = resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForce, uninstalls,
	)
	require.Error(t, err)
	assert.True(t, errors.ErrAborted.MatchesError(err), "untracked leftovers block even when --force is used")
	assert.Contains(t, err.Error(), "selected app")
	assert.Contains(t, err.Error(), "priority >= 0 uninstall ownership rules")
	assert.Contains(t, err.Error(), "clone rules")
}

func captureActionLogs(t *testing.T, fn func(logger log.Logger)) string {
	t.Helper()

	var buf bytes.Buffer
	oldDefault := slog.Default()
	oldLogger := log.Default()

	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	formattedHandler := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})
	slog.SetDefault(slog.New(formattedHandler))
	logger := log.NewLogger()
	log.SetDefault(logger)

	defer func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldDefault)
	}()

	fn(logger)
	return buf.String()
}

func withTemplateUnstructured(e entity.Entity) entity.Entity {
	metadata := map[string]interface{}{
		"name": mustName(e),
	}
	ns, _ := e.Namespace()
	if ns != "" {
		metadata["namespace"] = string(ns)
	}
	group, _ := e.Group()
	version, _ := e.Version()
	kind, _ := e.Kind()
	apiVersion := string(version)
	if group != "" {
		apiVersion = string(group) + "/" + apiVersion
	}
	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       string(kind),
			"metadata":   metadata,
		},
	}
	result, err := e.Modify(func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyTemplateEntity, u)
	})
	if err != nil {
		panic(err)
	}
	return result
}

func TestFilterPrimaryUninstallSelection_KeepsTemplateAssignedAndSafeSelectedResources(t *testing.T) {
	t.Parallel()

	templateDep := withTemplateUnstructured(makeTestEntity("apps", "v1", "Deployment", "deployments", "cert-manager", "cert-manager"))
	selectedSecret := withClusterUnstructured(makeTestEntity("", "v1", "Secret", "secrets", "cert-manager", "webhook-tls"), nil, nil)
	safePodMetrics := withClusterUnstructured(makeTestEntity("metrics.k8s.io", "v1beta1", "PodMetrics", "pods", "cert-manager", "cert-manager-abc"), nil, nil)
	foreignServiceAccount := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "dex", "dex"), nil, nil)
	selectedClusterRole := withClusterUnstructured(makeTestEntity("rbac.authorization.k8s.io", "v1", "ClusterRole", "clusterroles", "", "cert-manager-controller"), nil, nil)
	foreignClusterRole := withClusterUnstructured(makeTestEntity("rbac.authorization.k8s.io", "v1", "ClusterRole", "clusterroles", "", "dex"), nil, nil)
	outsideNamespace := withClusterUnstructured(makeTestEntity("", "v1", "Secret", "secrets", "other", "ignored"), nil, nil)

	selected, err := entity.NewEntities([]entity.Entity{
		templateDep,
		selectedSecret,
		safePodMetrics,
		foreignServiceAccount,
		selectedClusterRole,
		foreignClusterRole,
		outsideNamespace,
	})
	require.NoError(t, err)

	selectedAssigned := sets.New[types.Id]()
	selectedSecretID, err := selectedSecret.Id()
	require.NoError(t, err)
	selectedAssigned.Insert(selectedSecretID)
	selectedClusterRoleID, err := selectedClusterRole.Id()
	require.NoError(t, err)
	selectedAssigned.Insert(selectedClusterRoleID)
	outsideNamespaceID, err := outsideNamespace.Id()
	require.NoError(t, err)
	selectedAssigned.Insert(outsideNamespaceID)
	safeSelected := sets.New[types.Id]()
	safePodMetricsID, err := safePodMetrics.Id()
	require.NoError(t, err)
	safeSelected.Insert(safePodMetricsID)

	uninstallNamespaces := sets.New(types.Namespace("cert-manager"), types.Namespace("dex"))
	out, err := filterPrimaryUninstallSelection(selected, uninstallNamespaces, selectedAssigned, safeSelected, sets.New[types.Id]())
	require.NoError(t, err)

	ids := make([]types.Id, 0, out.Len())
	for _, item := range out.Items {
		id, idErr := item.Id()
		require.NoError(t, idErr)
		ids = append(ids, id)
	}

	templateDepID, err := templateDep.Id()
	require.NoError(t, err)
	foreignServiceAccountID, err := foreignServiceAccount.Id()
	require.NoError(t, err)
	foreignClusterRoleID, err := foreignClusterRole.Id()
	require.NoError(t, err)

	assert.Contains(t, ids, templateDepID)
	assert.Contains(t, ids, selectedSecretID)
	assert.Contains(t, ids, selectedClusterRoleID)
	assert.Contains(t, ids, safePodMetricsID)
	assert.NotContains(t, ids, foreignServiceAccountID)
	assert.NotContains(t, ids, foreignClusterRoleID)
	assert.NotContains(t, ids, outsideNamespaceID)
}

func TestFilterPrimaryUninstallSelection_KeepsRuntimeAutoSelectedNamespaceObjects(t *testing.T) {
	t.Parallel()

	namespace := withClusterUnstructured(makeTestEntity("", "v1", "Namespace", "namespaces", "", "cert-manager"), nil, nil)
	defaultSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "cert-manager", "default"), nil, nil)

	selected, err := entity.NewEntities([]entity.Entity{namespace, defaultSA})
	require.NoError(t, err)

	namespaceID, err := namespace.Id()
	require.NoError(t, err)
	defaultSAID, err := defaultSA.Id()
	require.NoError(t, err)

	out, err := filterPrimaryUninstallSelection(
		selected,
		sets.New(types.Namespace("cert-manager")),
		sets.New[types.Id](),
		sets.New[types.Id](),
		sets.New(namespaceID, defaultSAID),
	)
	require.NoError(t, err)

	ids := make([]types.Id, 0, out.Len())
	for _, item := range out.Items {
		id, idErr := item.Id()
		require.NoError(t, idErr)
		ids = append(ids, id)
	}

	assert.Contains(t, ids, namespaceID)
	assert.Contains(t, ids, defaultSAID)
}

func TestMarkUninstallNamespaceRuntimeObjects_ReturnsAutoSelectedIDs(t *testing.T) {
	t.Parallel()

	namespace := withClusterUnstructured(makeTestEntity("", "v1", "Namespace", "namespaces", "", "cert-manager"), nil, nil)
	defaultSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "cert-manager", "default"), nil, nil)
	otherSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "cert-manager", "custom"), nil, nil)

	entities, err := entity.NewEntities([]entity.Entity{namespace, defaultSA, otherSA})
	require.NoError(t, err)

	selected, autoSelectedIDs, err := markUninstallNamespaceRuntimeObjects(entities, sets.New(types.Namespace("cert-manager")))
	require.NoError(t, err)

	namespaceID, err := namespace.Id()
	require.NoError(t, err)
	defaultSAID, err := defaultSA.Id()
	require.NoError(t, err)
	otherSAID, err := otherSA.Id()
	require.NoError(t, err)

	require.True(t, autoSelectedIDs.Has(namespaceID))
	require.True(t, autoSelectedIDs.Has(defaultSAID))
	require.False(t, autoSelectedIDs.Has(otherSAID))

	selectedEntities, err := selected.Selected()
	require.NoError(t, err)
	selectedIDs := make([]types.Id, 0, selectedEntities.Len())
	for _, item := range selectedEntities.Items {
		id, idErr := item.Id()
		require.NoError(t, idErr)
		selectedIDs = append(selectedIDs, id)
	}
	assert.Contains(t, selectedIDs, namespaceID)
	assert.Contains(t, selectedIDs, defaultSAID)
	assert.NotContains(t, selectedIDs, otherSAID)
}

func TestMarkSelectedByIdSets_SelectsAssignedAndSafeIds(t *testing.T) {
	t.Parallel()

	assigned := withClusterUnstructured(makeTestEntity("", "v1", "Secret", "secrets", "cert-manager", "assigned"), nil, nil)
	safe := withClusterUnstructured(makeTestEntity("metrics.k8s.io", "v1beta1", "PodMetrics", "pods", "cert-manager", "safe"), nil, nil)
	other := withClusterUnstructured(makeTestEntity("", "v1", "ConfigMap", "configmaps", "cert-manager", "other"), nil, nil)

	entities, err := entity.NewEntities([]entity.Entity{assigned, safe, other})
	require.NoError(t, err)

	assignedID, err := assigned.Id()
	require.NoError(t, err)
	safeID, err := safe.Id()
	require.NoError(t, err)

	out, err := markSelectedByIdSets(entities, sets.New(assignedID), sets.New(safeID))
	require.NoError(t, err)

	selected, err := out.Selected()
	require.NoError(t, err)
	require.Len(t, selected.Items, 2)

	selectedIDs := make([]types.Id, 0, len(selected.Items))
	for _, item := range selected.Items {
		id, idErr := item.Id()
		require.NoError(t, idErr)
		selectedIDs = append(selectedIDs, id)
	}

	assert.Contains(t, selectedIDs, assignedID)
	assert.Contains(t, selectedIDs, safeID)
}

func TestLogUninstallNamespaces_PrintsSortedList(t *testing.T) {
	t.Parallel()

	output := captureActionLogs(t, func(logger log.Logger) {
		logUninstallNamespaces(logger, sets.New(types.Namespace("dex"), types.Namespace("cert-manager")))
	})

	assert.Contains(t, output, "uninstall namespaces: cert-manager, dex")
}

func TestFilterSelectedWorkloadScopedLive_KeepsOnlySelectedAssignments(t *testing.T) {
	t.Parallel()

	selectedNamespaced := withClusterUnstructured(makeTestEntity("", "v1", "Service", "services", "cert-manager", "cert-manager"), nil, nil)
	foreignSameNamespace := withClusterUnstructured(makeTestEntity("", "v1", "Service", "services", "cert-manager", "external-dns"), nil, nil)
	foreignOtherNamespace := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "dex", "dex"), nil, nil)
	selectedClusterScoped := withClusterUnstructured(makeTestEntity("rbac.authorization.k8s.io", "v1", "ClusterRole", "clusterroles", "", "cert-manager-controller"), nil, nil)

	workloadScoped, err := entity.NewEntities([]entity.Entity{
		selectedNamespaced,
		foreignSameNamespace,
		foreignOtherNamespace,
		selectedClusterScoped,
	})
	require.NoError(t, err)

	selectedAssigned := sets.New[types.Id]()
	selectedNamespacedID, err := selectedNamespaced.Id()
	require.NoError(t, err)
	selectedAssigned.Insert(selectedNamespacedID)
	selectedClusterScopedID, err := selectedClusterScoped.Id()
	require.NoError(t, err)
	selectedAssigned.Insert(selectedClusterScopedID)

	out, err := filterSelectedWorkloadScopedLive(workloadScoped, selectedAssigned)
	require.NoError(t, err)

	ids := make([]types.Id, 0, out.Len())
	for _, item := range out.Items {
		id, idErr := item.Id()
		require.NoError(t, idErr)
		ids = append(ids, id)
	}

	foreignSameNamespaceID, err := foreignSameNamespace.Id()
	require.NoError(t, err)
	foreignOtherNamespaceID, err := foreignOtherNamespace.Id()
	require.NoError(t, err)

	assert.Contains(t, ids, selectedNamespacedID)
	assert.Contains(t, ids, selectedClusterScopedID)
	assert.NotContains(t, ids, foreignSameNamespaceID)
	assert.NotContains(t, ids, foreignOtherNamespaceID)
}

func TestShouldAutoSelectUninstallNamespaceRuntimeObject(t *testing.T) {
	t.Parallel()

	uninstallNamespaces := sets.New(types.Namespace("cert-manager"))

	namespace := withClusterUnstructured(makeTestEntity("", "v1", "Namespace", "namespaces", "", "cert-manager"), nil, nil)
	keep, err := shouldAutoSelectUninstallNamespaceRuntimeObject(namespace, uninstallNamespaces)
	require.NoError(t, err)
	assert.True(t, keep)

	defaultSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "cert-manager", "default"), nil, nil)
	keep, err = shouldAutoSelectUninstallNamespaceRuntimeObject(defaultSA, uninstallNamespaces)
	require.NoError(t, err)
	assert.True(t, keep)

	foreignSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "dex", "default"), nil, nil)
	keep, err = shouldAutoSelectUninstallNamespaceRuntimeObject(foreignSA, uninstallNamespaces)
	require.NoError(t, err)
	assert.False(t, keep)

	nonDefaultSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "cert-manager", "cert-manager"), nil, nil)
	keep, err = shouldAutoSelectUninstallNamespaceRuntimeObject(nonDefaultSA, uninstallNamespaces)
	require.NoError(t, err)
	assert.False(t, keep)
}

func TestShouldAutoSelectUninstallNamespaceRuntimeObject_SkipsKubernetesSystemNamespaces(t *testing.T) {
	t.Parallel()

	uninstallNamespaces := sets.New(types.Namespace("kube-system"))

	namespace := withClusterUnstructured(makeTestEntity("", "v1", "Namespace", "namespaces", "", "kube-system"), nil, nil)
	keep, err := shouldAutoSelectUninstallNamespaceRuntimeObject(namespace, uninstallNamespaces)
	require.NoError(t, err)
	assert.False(t, keep)

	defaultSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "kube-system", "default"), nil, nil)
	keep, err = shouldAutoSelectUninstallNamespaceRuntimeObject(defaultSA, uninstallNamespaces)
	require.NoError(t, err)
	assert.False(t, keep)
}

func TestSkipProtectedNamespaceDeletes_SkipsKubernetesSystemNamespace(t *testing.T) {
	t.Parallel()

	ns := withClusterUnstructured(makeTestEntity("", "v1", "Namespace", "namespaces", "", "kube-system"), nil, nil)
	deploy := withClusterUnstructured(makeTestEntity("apps", "v1", "Deployment", "deployments", "kube-system", "addon"), nil, nil)

	uninstalls, err := entity.NewEntities([]entity.Entity{ns, deploy})
	require.NoError(t, err)
	clusterLive, err := entity.NewEntities([]entity.Entity{ns, deploy})
	require.NoError(t, err)

	out, err := skipProtectedNamespaceDeletes(testLogger(), uninstalls, clusterLive)
	require.NoError(t, err)

	assert.False(t, out.IdSet.Has(types.Id("v1/Namespace//kube-system")))
	assert.True(t, out.IdSet.Has(types.Id("apps/v1/Deployment/kube-system/addon")))
}

func TestSkipProtectedNamespaceDeletes_SkipsNamespaceWithUntrackedLiveResources(t *testing.T) {
	t.Parallel()

	ns := withClusterUnstructured(makeTestEntity("", "v1", "Namespace", "namespaces", "", "demo"), nil, nil)
	plannedDeploy := withClusterUnstructured(makeTestEntity("apps", "v1", "Deployment", "deployments", "demo", "app"), nil, nil)
	untrackedSecret := withClusterUnstructured(makeTestEntity("", "v1", "Secret", "secrets", "demo", "left-behind"), nil, nil)

	uninstalls, err := entity.NewEntities([]entity.Entity{ns, plannedDeploy})
	require.NoError(t, err)
	clusterLive, err := entity.NewEntities([]entity.Entity{ns, plannedDeploy, untrackedSecret})
	require.NoError(t, err)

	out, err := skipProtectedNamespaceDeletes(testLogger(), uninstalls, clusterLive)
	require.NoError(t, err)

	assert.False(t, out.IdSet.Has(types.Id("v1/Namespace//demo")))
	assert.True(t, out.IdSet.Has(types.Id("apps/v1/Deployment/demo/app")))
}

func TestSkipProtectedNamespaceDeletes_IgnoresNamespaceBuiltins(t *testing.T) {
	t.Parallel()

	ns := withClusterUnstructured(makeTestEntity("", "v1", "Namespace", "namespaces", "", "demo"), nil, nil)
	plannedDeploy := withClusterUnstructured(makeTestEntity("apps", "v1", "Deployment", "deployments", "demo", "app"), nil, nil)
	defaultSA := withClusterUnstructured(makeTestEntity("", "v1", "ServiceAccount", "serviceaccounts", "demo", "default"), nil, nil)
	rootCA := withClusterUnstructured(makeTestEntity("", "v1", "ConfigMap", "configmaps", "demo", "kube-root-ca.crt"), nil, nil)

	uninstalls, err := entity.NewEntities([]entity.Entity{ns, plannedDeploy})
	require.NoError(t, err)
	clusterLive, err := entity.NewEntities([]entity.Entity{ns, plannedDeploy, defaultSA, rootCA})
	require.NoError(t, err)

	out, err := skipProtectedNamespaceDeletes(testLogger(), uninstalls, clusterLive)
	require.NoError(t, err)

	assert.True(t, out.IdSet.Has(types.Id("v1/Namespace//demo")))
	assert.True(t, out.IdSet.Has(types.Id("apps/v1/Deployment/demo/app")))
}

func TestResolveForceLeftovers_KeepWithoutForceLeftovers(t *testing.T) {
	forceLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	existingEntity := makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "my-app")
	uninstalls, err := entity.NewEntities([]entity.Entity{existingEntity})
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallKeep, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "existing uninstalls should be preserved")
}

func TestResolveForceLeftovers_NoLeftovers(t *testing.T) {
	flags := []struct {
		name string
		flag types.ForceUninstall
	}{
		{"None", types.ForceUninstallNone},
		{"Force", types.ForceUninstallForce},
		{"Keep", types.ForceUninstallKeep},
	}

	for _, tt := range flags {
		t.Run(tt.name, func(t *testing.T) {
			forceLeftovers, err := entity.NewEntities(nil)
			require.NoError(t, err)

			untrackedLeftovers, err := entity.NewEntities(nil)
			require.NoError(t, err)

			uninstalls, err := entity.NewEntities(nil)
			require.NoError(t, err)

			result, err := resolveForceLeftovers(
				testLogger(),
				forceLeftovers, untrackedLeftovers,
				tt.flag, uninstalls,
			)
			require.NoError(t, err)
			assert.Equal(t, 0, result.Len())
		})
	}
}

func TestResolveForceLeftovers_ForceFlag_EntitiesMerged(t *testing.T) {
	existingEntity := makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "my-app")
	uninstalls, err := entity.NewEntities([]entity.Entity{existingEntity})
	require.NoError(t, err)

	forceSecret := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forcePVC := makeTestEntity("", "v1", "PersistentVolumeClaim", "persistentvolumeclaims", "default", "data-vol")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceSecret, forcePVC})
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForce, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Len(), "result should contain original uninstalls + force entities")

	ids := make([]types.Id, 0, result.Len())
	for _, e := range result.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	assert.Contains(t, ids, types.Id("apps/v1/Deployment/default/my-app"))
	assert.Contains(t, ids, types.Id("v1/Secret/default/tls-cert"))
	assert.Contains(t, ids, types.Id("v1/PersistentVolumeClaim/default/data-vol"))
}

func TestResolveForceLeftovers_ForceAll_UntrackedOnly(t *testing.T) {
	forceLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	untrackedCM := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "unknown-cm")
	untrackedSecret := makeTestEntity("", "v1", "Secret", "secrets", "default", "stale-secret")
	untrackedLeftovers, err := entity.NewEntities([]entity.Entity{untrackedCM, untrackedSecret})
	require.NoError(t, err)

	uninstalls, err := entity.NewEntities(nil)
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForceAll, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Len(), "all untracked entities should be added to uninstalls")

	ids := make([]types.Id, 0, result.Len())
	for _, e := range result.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	assert.Contains(t, ids, types.Id("v1/ConfigMap/default/unknown-cm"))
	assert.Contains(t, ids, types.Id("v1/Secret/default/stale-secret"))
}

func TestResolveForceLeftovers_ForceAll_MixedLeftovers(t *testing.T) {
	existingEntity := makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "my-app")
	uninstalls, err := entity.NewEntities([]entity.Entity{existingEntity})
	require.NoError(t, err)

	forceSecret := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceSecret})
	require.NoError(t, err)

	untrackedCM := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "unknown-cm")
	untrackedLeftovers, err := entity.NewEntities([]entity.Entity{untrackedCM})
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForceAll, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Len(), "result should contain existing uninstalls + force + untracked entities")

	ids := make([]types.Id, 0, result.Len())
	for _, e := range result.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	assert.Contains(t, ids, types.Id("apps/v1/Deployment/default/my-app"))
	assert.Contains(t, ids, types.Id("v1/Secret/default/tls-cert"))
	assert.Contains(t, ids, types.Id("v1/ConfigMap/default/unknown-cm"))
}

func TestResolveForceLeftovers_ForceAll_NoLeftovers(t *testing.T) {
	forceLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	existingEntity := makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "my-app")
	uninstalls, err := entity.NewEntities([]entity.Entity{existingEntity})
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForceAll, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "only existing uninstalls should remain when there are no leftovers")
}

func TestResolveForceLeftovers_ForceAll_ForceOnlyLeftovers(t *testing.T) {
	existingEntity := makeTestEntity("apps", "v1", "Deployment", "deployments", "default", "my-app")
	uninstalls, err := entity.NewEntities([]entity.Entity{existingEntity})
	require.NoError(t, err)

	forceSecret := makeTestEntity("", "v1", "Secret", "secrets", "default", "tls-cert")
	forcePVC := makeTestEntity("", "v1", "PersistentVolumeClaim", "persistentvolumeclaims", "default", "data-vol")
	forceLeftovers, err := entity.NewEntities([]entity.Entity{forceSecret, forcePVC})
	require.NoError(t, err)

	untrackedLeftovers, err := entity.NewEntities(nil)
	require.NoError(t, err)

	result, err := resolveForceLeftovers(
		testLogger(),
		forceLeftovers, untrackedLeftovers,
		types.ForceUninstallForceAll, uninstalls,
	)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Len(), "result should contain existing uninstalls + force entities")

	ids := make([]types.Id, 0, result.Len())
	for _, e := range result.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	assert.Contains(t, ids, types.Id("apps/v1/Deployment/default/my-app"))
	assert.Contains(t, ids, types.Id("v1/Secret/default/tls-cert"))
	assert.Contains(t, ids, types.Id("v1/PersistentVolumeClaim/default/data-vol"))
}

func TestFilterLeftoversByClusterDefaultsPresets(t *testing.T) {
	t.Parallel()
	match := makeTestEntity("", "v1", "Secret", "secrets", "ns1", "match-me")
	other := makeTestEntity("", "v1", "Secret", "secrets", "ns1", "other")
	envInv, err := entity.NewEntities([]entity.Entity{match, other})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(envInv)
	require.NoError(t, err)

	effective := []hydra.ClusterDefaultsPresetEffective{
		{
			ID:      "t",
			Enabled: true,
			Predicates: map[string]hydra.ClusterDefaultsPredicateEffective{
				"p": {Enabled: true, Ids: []hydra.ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/match-me"}}},
			},
		},
	}

	forceIn, err := entity.NewEntities([]entity.Entity{match, other})
	require.NoError(t, err)

	out, err := filterLeftoversByClusterDefaultsPresets(forceIn, effective, env, 99, workloadclosure.EmptyMatchInput(types.KeyClusterEntity))
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	id, err := out.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/Secret/ns1/other"), id)

	passThrough, err := filterLeftoversByClusterDefaultsPresets(forceIn, nil, env, 99, workloadclosure.EmptyMatchInput(types.KeyClusterEntity))
	require.NoError(t, err)
	assert.Equal(t, forceIn.Len(), passThrough.Len())
}

func TestFilterLeftoversByClusterDefaultsPresets_DropsGenericK3sServiceLBDaemonSetPodAndPodMetrics(t *testing.T) {
	t.Parallel()

	effective := effectivePresetsForUninstallTest(t, "k3s")
	daemonSet := makeDaemonSetForUninstallTest(
		"svclb-demo-service-a1ba7efd",
		map[string]any{
			"svccontroller.k3s.cattle.io/svcname":      "demo-service",
			"svccontroller.k3s.cattle.io/svcnamespace": "demo-namespace",
		},
	)
	pod := makePodOwnedByForUninstallTest(
		"svclb-demo-service-a1ba7efd-hnttj",
		"uid-svclb-demo-service-a1ba7efd",
		"apps/v1",
		"DaemonSet",
		"svclb-demo-service-a1ba7efd",
	)
	podMetrics := makePodMetricsForUninstallTest("svclb-demo-service-a1ba7efd-hnttj")
	leftovers, err := entity.NewEntities([]entity.Entity{daemonSet, pod, podMetrics})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(leftovers)
	require.NoError(t, err)

	model, err := commands.BuildResourceModel(commands.ResourceModelInput{
		ClusterEntities: &leftovers,
		KubernetesMinor: 99,
		NetworkMode:     types.HelmNetworkModeOffline,
		Bootstrap:       types.BootstrapNo,
	}, false)
	require.NoError(t, err)
	inv := model.InventoryForGraph()
	defer inv.Close()
	mergedRefs, err := inv.RefsMerged()
	require.NoError(t, err)
	presetClosure, err := presetWorkloadClosureForClusterDefaultsFilters(nil, leftovers, entity.Entities{}, mergedRefs)
	require.NoError(t, err)

	out, err := filterLeftoversByClusterDefaultsPresets(leftovers, effective, env, 99, presetClosure)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len(), "generic k3s ServiceLB DaemonSet must anchor implicit filtering of its Pod and PodMetrics")
}

func TestFilterLeftoversByClusterDefaultsPresets_DropsPresetOnlyDeploymentPodEventsAndMetricsFromMergedInventory(t *testing.T) {
	t.Parallel()

	effective := effectivePresetsForUninstallTest(t, "local-path-provisioner", "metrics-server")
	presetTemplates, err := commands.PresetTemplateEntities(types.InCluster, effective, 99)
	require.NoError(t, err)
	renderedAll := mergePresetTemplateEntitiesForUninstallTest(t, presetTemplates)
	env, err := cel.NewEnvWithEntityInventory(renderedAll)
	require.NoError(t, err)

	localPathEvent := makePodRegardingEventForUninstallTest(
		"local-path-provisioner-6bc6568469-chlbm.18abd958c90f2e30",
		"local-path-provisioner-6bc6568469-chlbm",
	)
	metricsServerEvent := makePodRegardingEventForUninstallTest(
		"metrics-server-786d997795-s6q95.18abd958c980c15f",
		"metrics-server-786d997795-s6q95",
	)
	localPathPodMetrics := makePodMetricsForUninstallTest("local-path-provisioner-6bc6568469-chlbm")
	leftovers, err := entity.NewEntities([]entity.Entity{localPathEvent, metricsServerEvent, localPathPodMetrics})
	require.NoError(t, err)

	model, err := commands.BuildResourceModel(commands.ResourceModelInput{
		TemplateEntities: &renderedAll,
		ClusterEntities:  &leftovers,
		KubernetesMinor:  99,
		NetworkMode:      types.HelmNetworkModeOffline,
		Bootstrap:        types.BootstrapNo,
	}, false)
	require.NoError(t, err)
	inv := model.InventoryForGraph()
	defer inv.Close()
	mergedRefs, err := inv.RefsMerged()
	require.NoError(t, err)
	presetClosure, err := presetWorkloadClosureForClusterDefaultsFilters(nil, leftovers, renderedAll, mergedRefs)
	require.NoError(t, err)

	out, err := filterLeftoversByClusterDefaultsPresets(leftovers, effective, env, 99, presetClosure)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len(), "cluster-only pod events and PodMetrics must be filtered as belonging to their preset-only Deployment anchors")
}

func effectivePresetsForUninstallTest(t *testing.T, ids ...string) []hydra.ClusterDefaultsPresetEffective {
	t.Helper()
	effs, err := hydra.EffectiveClusterDefaultsPresets(nil)
	require.NoError(t, err)
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	for i := range effs {
		_, effs[i].Enabled = wanted[effs[i].ID]
	}
	return effs
}

func mergePresetTemplateEntitiesForUninstallTest(t *testing.T, perApp map[types.AppId]entity.Entities) entity.Entities {
	t.Helper()
	var items []entity.Entity
	for _, ents := range perApp {
		items = append(items, ents.Items...)
	}
	out, err := entity.NewEntities(items)
	require.NoError(t, err)
	return out
}

func makePodRegardingEventForUninstallTest(eventName, podName string) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "events.k8s.io/v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"name":      eventName,
			"namespace": "kube-system",
			"uid":       "uid-" + eventName,
		},
		"regarding": map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       podName,
			"namespace":  "kube-system",
		},
		"reason":              "Pulled",
		"action":              "Pulling",
		"reportingController": "kubelet",
		"reportingInstance":   "node-1",
		"type":                "Normal",
		"note":                "Pulled image",
	}}
	return mustBuildDiff(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("events.k8s.io"), types.Version("v1"), types.Kind("Event"))).
		WithResource(types.Resource("events")).
		WithName(types.Name(eventName)).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func makePodMetricsForUninstallTest(name string) entity.Entity {
	return makePodMetricsWithLabelsForUninstallTest(name, nil)
}

func makePodMetricsWithLabelsForUninstallTest(name string, labels map[string]any) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "PodMetrics",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "kube-system",
			"uid":       "uid-" + name,
			"labels":    labels,
		},
		"timestamp": "2021-01-01T00:00:00Z",
		"window":    "30s",
		"containers": []any{
			map[string]any{"name": "c", "usage": map[string]any{}},
		},
	}}
	return mustBuildDiff(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("metrics.k8s.io"), types.Version("v1beta1"), types.Kind("PodMetrics"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func makeDaemonSetForUninstallTest(name string, labels map[string]any) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "DaemonSet",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "kube-system",
			"uid":       "uid-" + name,
			"labels":    labels,
		},
	}}
	return mustBuildDiff(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("DaemonSet"))).
		WithResource(types.Resource("daemonsets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func makePodOwnedByForUninstallTest(name, ownerUID, ownerAPIVersion, ownerKind, ownerName string) entity.Entity {
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "kube-system",
			"uid":       "uid-" + name,
			"ownerReferences": []any{
				map[string]any{
					"apiVersion": ownerAPIVersion,
					"kind":       ownerKind,
					"name":       ownerName,
					"uid":        ownerUID,
				},
			},
		},
	}}
	return mustBuildDiff(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace("kube-system")).
		WithUnstructured(types.KeyClusterEntity, u))
}
