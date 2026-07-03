package action

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// discardActionLogger discards output; use for findChangedEntities in tests (debug lines are not asserted).
var discardActionLogger = log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

func builtinDiffIgnorePipeline(t *testing.T) *commands.DiffIgnorePipeline {
	t.Helper()
	p, err := commands.NewDiffIgnorePipeline(hydra.BuiltinDiffIgnoreRuleEntries())
	require.NoError(t, err)
	return p
}

func mustBuildApply(b entity.EntityBuilder) entity.Entity {
	e, err := b.Build()
	if err != nil {
		panic(err)
	}
	return e
}

func mustModifyApply(e entity.Entity, fn func(entity.EntityBuilder) entity.EntityBuilder) entity.Entity {
	result, err := e.Modify(fn)
	if err != nil {
		panic(err)
	}
	return result
}

func makeEntity(group, version, kind, namespace, name string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	return mustBuildApply(b)
}

func makeApplyTestSopsSecretEntity(namespace, name, templateName string, backup bool, appIds ...types.AppId) entity.Entity {
	gvk := types.NewGVK(types.Group("isindir.github.com"), types.Version("v1alpha3"), types.Kind("SopsSecret"))
	annotations := map[string]any{}
	if backup {
		annotations[hydra.AnnotationHydraBackup] = "true"
	}

	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithUnstructured(types.KeyTemplateEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "isindir.github.com/v1alpha3",
				"kind":       "SopsSecret",
				"metadata": map[string]any{
					"name":        name,
					"namespace":   namespace,
					"annotations": annotations,
				},
				"spec": map[string]any{
					"secretTemplates": []any{
						map[string]any{
							"name":       templateName,
							"stringData": map[string]any{"password": "ENC[AES256_GCM,data:test]"},
						},
					},
				},
			},
		})

	for _, appId := range appIds {
		var err error
		b, err = b.AddAppId(appId)
		if err != nil {
			panic(err)
		}
	}

	return mustBuildApply(b)
}

func decryptedApplyTestSopsSecretYaml(namespace, name, templateName string) types.YamlString {
	return types.YamlString(fmt.Sprintf(`apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: %s
  namespace: %s
spec:
  secretTemplates:
    - name: %s
      stringData:
        password: plain-text
`, name, namespace, templateName))
}

func bootstrapLaterApplySetForTest(t *testing.T, items ...entity.Entity) entity.Entities {
	t.Helper()

	entities, err := entity.NewEntities(items)
	require.NoError(t, err)

	rendered, err := commands.ConvertSopsSecretsToSecrets(
		log.Default(),
		entities,
		types.KeyTemplateEntity,
		func(types.YamlString) (types.YamlString, error) {
			return decryptedApplyTestSopsSecretYaml("selected-ns", "selected-sops", "selected-secret"), nil
		},
	)
	require.NoError(t, err)

	_, nonCrds, err := splitCRDs(rendered)
	require.NoError(t, err)

	_, nonNamespaces, err := splitNamespaces(nonCrds)
	require.NoError(t, err)

	nonNamespaces, err = excludeBootstrapOutOfScopeBackupResources(nonNamespaces)
	require.NoError(t, err)

	_, laterApplySet, err := splitWebhooks(nonNamespaces)
	require.NoError(t, err)

	return laterApplySet
}

func entityIds(t *testing.T, entities entity.Entities) []types.Id {
	t.Helper()

	ids := make([]types.Id, 0, entities.Len())
	for _, e := range entities.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return ids
}

func TestSplitNamespaces_MixedEntities(t *testing.T) {
	ns1 := makeEntity("", "v1", "Namespace", "", "kube-system")
	ns2 := makeEntity("", "v1", "Namespace", "", "default")
	crd := makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "foos.example.com")
	deploy := makeEntity("apps", "v1", "Deployment", "default", "my-app")
	configMap := makeEntity("", "v1", "ConfigMap", "default", "my-config")

	all, err := entity.NewEntities([]entity.Entity{ns1, crd, ns2, deploy, configMap})
	require.NoError(t, err)

	namespaces, rest, err := splitNamespaces(all)
	require.NoError(t, err)

	assert.Equal(t, 2, namespaces.Len(), "should extract both Namespace entities")
	assert.Equal(t, 3, rest.Len(), "CRD, Deployment, and ConfigMap should remain in rest")

	for _, e := range namespaces.Items {
		gvk, gErr := e.GVKString()
		require.NoError(t, gErr)
		assert.Equal(t, types.GVKString(types.KubernetesGvkV1Namespace), gvk)
	}

	for _, e := range rest.Items {
		gvk, gErr := e.GVKString()
		require.NoError(t, gErr)
		assert.NotEqual(t, types.GVKString(types.KubernetesGvkV1Namespace), gvk)
	}
}

func TestSplitNamespaces_NoNamespaceEntities(t *testing.T) {
	deploy := makeEntity("apps", "v1", "Deployment", "default", "my-app")
	svc := makeEntity("", "v1", "Service", "default", "my-svc")

	all, err := entity.NewEntities([]entity.Entity{deploy, svc})
	require.NoError(t, err)

	namespaces, rest, err := splitNamespaces(all)
	require.NoError(t, err)

	assert.Equal(t, 0, namespaces.Len(), "no Namespace entities expected")
	assert.Equal(t, 2, rest.Len(), "all entities should be in rest")
}

func TestSplitNamespaces_OnlyNamespaceEntities(t *testing.T) {
	ns1 := makeEntity("", "v1", "Namespace", "", "production")
	ns2 := makeEntity("", "v1", "Namespace", "", "staging")
	ns3 := makeEntity("", "v1", "Namespace", "", "development")

	all, err := entity.NewEntities([]entity.Entity{ns1, ns2, ns3})
	require.NoError(t, err)

	namespaces, rest, err := splitNamespaces(all)
	require.NoError(t, err)

	assert.Equal(t, 3, namespaces.Len(), "all entities should be namespaces")
	assert.Equal(t, 0, rest.Len(), "rest should be empty")
}

func TestSplitNamespaces_PreparesNamespacedSecretsForRestoreFollowUp(t *testing.T) {
	namespace := makeEntity("", "v1", "Namespace", "", "cert-manager")
	secret := makeEntity("", "v1", "Secret", "cert-manager", "wildcard-tls")
	deployment := makeEntity("apps", "v1", "Deployment", "cert-manager", "cert-manager")

	all, err := entity.NewEntities([]entity.Entity{secret, namespace, deployment})
	require.NoError(t, err)

	namespaces, rest, err := splitNamespaces(all)
	require.NoError(t, err)

	assert.Equal(t, 1, namespaces.Len(), "namespace creation should stay isolated for early preparation")
	assert.Equal(t, 2, rest.Len(), "namespaced follow-up resources should remain for later phases")

	namespaceName, err := namespaces.Items[0].Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("cert-manager"), namespaceName)

	foundSecret := false
	for _, e := range rest.Items {
		gvk, gErr := e.GVKString()
		require.NoError(t, gErr)
		assert.NotEqual(t, types.GVKString(types.KubernetesGvkV1Namespace), gvk)

		kind, kErr := e.Kind()
		require.NoError(t, kErr)
		if kind != types.Kind("Secret") {
			continue
		}

		ns, nErr := e.Namespace()
		require.NoError(t, nErr)
		name, nameErr := e.Name()
		require.NoError(t, nameErr)

		assert.Equal(t, types.Namespace("cert-manager"), ns)
		assert.Equal(t, types.Name("wildcard-tls"), name)
		foundSecret = true
	}

	assert.True(t, foundSecret, "Secret should stay in the non-namespace set for later restore/apply phases")
}

func TestSplitNamespaces_PreparesAllNamespacesButKeepsFollowUpResourcesForLaterPhases(t *testing.T) {
	certManagerNamespace := makeEntity("", "v1", "Namespace", "", "cert-manager")
	monitoringNamespace := makeEntity("", "v1", "Namespace", "", "monitoring")
	secret := makeEntity("", "v1", "Secret", "cert-manager", "wildcard-tls")
	service := makeEntity("", "v1", "Service", "monitoring", "prometheus")
	clusterRole := makeEntity("rbac.authorization.k8s.io", "v1", "ClusterRole", "", "monitoring-reader")

	all, err := entity.NewEntities([]entity.Entity{
		clusterRole,
		service,
		certManagerNamespace,
		secret,
		monitoringNamespace,
	})
	require.NoError(t, err)

	namespaces, rest, err := splitNamespaces(all)
	require.NoError(t, err)

	assert.Equal(t, 2, namespaces.Len(), "all namespace preparation entities should be isolated up front")
	assert.Equal(t, 3, rest.Len(), "namespaced and cluster-scoped follow-up resources should remain for later phases")

	preparedNamespaces := map[types.Name]bool{}
	for _, e := range namespaces.Items {
		kind, kindErr := e.Kind()
		require.NoError(t, kindErr)
		assert.Equal(t, types.Kind("Namespace"), kind)

		name, nameErr := e.Name()
		require.NoError(t, nameErr)
		preparedNamespaces[name] = true
	}

	assert.True(t, preparedNamespaces[types.Name("cert-manager")])
	assert.True(t, preparedNamespaces[types.Name("monitoring")])

	foundSecret := false
	foundService := false
	foundClusterRole := false
	for _, e := range rest.Items {
		kind, kindErr := e.Kind()
		require.NoError(t, kindErr)

		switch kind {
		case types.Kind("Secret"):
			ns, nsErr := e.Namespace()
			require.NoError(t, nsErr)
			name, nameErr := e.Name()
			require.NoError(t, nameErr)
			assert.Equal(t, types.Namespace("cert-manager"), ns)
			assert.Equal(t, types.Name("wildcard-tls"), name)
			foundSecret = true
		case types.Kind("Service"):
			ns, nsErr := e.Namespace()
			require.NoError(t, nsErr)
			name, nameErr := e.Name()
			require.NoError(t, nameErr)
			assert.Equal(t, types.Namespace("monitoring"), ns)
			assert.Equal(t, types.Name("prometheus"), name)
			foundService = true
		case types.Kind("ClusterRole"):
			name, nameErr := e.Name()
			require.NoError(t, nameErr)
			assert.Equal(t, types.Name("monitoring-reader"), name)
			foundClusterRole = true
		}
	}

	assert.True(t, foundSecret, "restore/apply follow-up secrets should remain after namespace preparation")
	assert.True(t, foundService, "namespaced workloads should remain after namespace preparation")
	assert.True(t, foundClusterRole, "cluster-scoped non-namespace resources should remain in later phases")
}

func TestBootstrapApplyPipeline_KeepsSelectedOrdinarySopsSecretsInLaterApplySet(t *testing.T) {
	selectedApp := types.NewChildAppId(types.InCluster, types.RootAppName("argocd"), types.ChildAppName("selected"))

	selectedNamespaceBuilder, err := makeEntity("", "v1", "Namespace", "", "selected-ns").ToBuilder().AddAppId(selectedApp)
	require.NoError(t, err)
	selectedNamespace := mustBuildApply(selectedNamespaceBuilder)

	selectedOrdinarySops := mustModifyApply(
		makeApplyTestSopsSecretEntity("selected-ns", "selected-sops", "selected-secret", false, selectedApp),
		func(b entity.EntityBuilder) entity.EntityBuilder {
			return b.WithAppNamespace(types.AppNamespace("selected-ns"))
		},
	)
	laterApplySet := bootstrapLaterApplySetForTest(t, selectedNamespace, selectedOrdinarySops)

	assert.Contains(
		t,
		entityIds(t, laterApplySet),
		types.Id("isindir.github.com/v1alpha3/SopsSecret/selected-ns/selected-sops"),
		"ordinary selected-app SopsSecrets must still be part of the later bootstrap apply set",
	)
}

func TestBootstrapApplyPipeline_ExcludesBackupResourcesOfOtherAppsFromLaterApplySet(t *testing.T) {
	selectedApp := types.NewChildAppId(types.InCluster, types.RootAppName("argocd"), types.ChildAppName("selected"))

	selectedNamespaceBuilder, err := makeEntity("", "v1", "Namespace", "", "selected-ns").
		ToBuilder().AddAppId(selectedApp)
	require.NoError(t, err)
	selectedNamespace := mustBuildApply(selectedNamespaceBuilder)

	selectedBackupSops := mustModifyApply(
		makeApplyTestSopsSecretEntity("selected-ns", "selected-backup", "selected-secret", true, selectedApp),
		func(b entity.EntityBuilder) entity.EntityBuilder {
			return b.WithAppNamespace(types.AppNamespace("selected-ns"))
		},
	)
	foreignBackupSops := mustModifyApply(
		makeApplyTestSopsSecretEntity("argocd", "foreign-backup", "foreign-secret", true, selectedApp),
		func(b entity.EntityBuilder) entity.EntityBuilder {
			return b.WithAppNamespace(types.AppNamespace("selected-ns"))
		},
	)

	laterApplySet := bootstrapLaterApplySetForTest(
		t,
		selectedNamespace,
		selectedBackupSops,
		foreignBackupSops,
	)

	assert.Contains(
		t,
		entityIds(t, laterApplySet),
		types.Id("isindir.github.com/v1alpha3/SopsSecret/selected-ns/selected-backup"),
		"backup resources selected by app-scoped backup discovery must remain in the later bootstrap apply set",
	)
	assert.NotContains(
		t,
		entityIds(t, laterApplySet),
		types.Id("isindir.github.com/v1alpha3/SopsSecret/argocd/foreign-backup"),
		"backup resources whose target namespace does not belong to the selected app must not continue into the later normal bootstrap apply set",
	)
}

func TestSplitWebhooks_MixedEntities(t *testing.T) {
	vwc := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "validate-policy")
	mwc := makeEntity("admissionregistration.k8s.io", "v1", "MutatingWebhookConfiguration", "", "inject-sidecar")
	deploy := makeEntity("apps", "v1", "Deployment", "default", "my-app")
	svc := makeEntity("", "v1", "Service", "default", "my-svc")

	all, err := entity.NewEntities([]entity.Entity{vwc, mwc, deploy, svc})
	require.NoError(t, err)

	webhooks, rest, err := splitWebhooks(all)
	require.NoError(t, err)

	assert.Equal(t, 2, webhooks.Len(), "should extract both webhook configurations")
	assert.Equal(t, 2, rest.Len(), "Deployment and Service should remain in rest")

	for _, e := range webhooks.Items {
		gvk, gErr := e.GVKString()
		require.NoError(t, gErr)
		assert.True(t,
			gvk == types.KubernetesGvkAdmissionregistrationK8sIoV1ValidatingWebhookConfiguration ||
				gvk == types.KubernetesGvkAdmissionregistrationK8sIoV1MutatingWebhookConfiguration,
			"expected webhook GVK, got %s", gvk)
	}

	for _, e := range rest.Items {
		gvk, gErr := e.GVKString()
		require.NoError(t, gErr)
		assert.NotEqual(t, types.GVKString(types.KubernetesGvkAdmissionregistrationK8sIoV1ValidatingWebhookConfiguration), gvk)
		assert.NotEqual(t, types.GVKString(types.KubernetesGvkAdmissionregistrationK8sIoV1MutatingWebhookConfiguration), gvk)
	}
}

func TestSplitWebhooks_NoWebhookEntities(t *testing.T) {
	deploy := makeEntity("apps", "v1", "Deployment", "default", "my-app")
	svc := makeEntity("", "v1", "Service", "default", "my-svc")

	all, err := entity.NewEntities([]entity.Entity{deploy, svc})
	require.NoError(t, err)

	webhooks, rest, err := splitWebhooks(all)
	require.NoError(t, err)

	assert.Equal(t, 0, webhooks.Len(), "no webhook entities expected")
	assert.Equal(t, 2, rest.Len(), "all entities should be in rest")
}

func TestSplitWebhooks_OnlyWebhookEntities(t *testing.T) {
	vwc1 := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "validate-policy")
	mwc1 := makeEntity("admissionregistration.k8s.io", "v1", "MutatingWebhookConfiguration", "", "inject-sidecar")
	vwc2 := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "validate-resources")

	all, err := entity.NewEntities([]entity.Entity{vwc1, mwc1, vwc2})
	require.NoError(t, err)

	webhooks, rest, err := splitWebhooks(all)
	require.NoError(t, err)

	assert.Equal(t, 3, webhooks.Len(), "all entities should be webhooks")
	assert.Equal(t, 0, rest.Len(), "rest should be empty")
}

func TestSplitWebhooks_BothWebhookTypes(t *testing.T) {
	vwc := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "validate-policy")
	mwc := makeEntity("admissionregistration.k8s.io", "v1", "MutatingWebhookConfiguration", "", "inject-sidecar")

	all, err := entity.NewEntities([]entity.Entity{vwc, mwc})
	require.NoError(t, err)

	webhooks, rest, err := splitWebhooks(all)
	require.NoError(t, err)

	assert.Equal(t, 2, webhooks.Len(), "both webhook types should be extracted")
	assert.Equal(t, 0, rest.Len(), "rest should be empty")

	gvkSet := map[types.GVKString]bool{}
	for _, e := range webhooks.Items {
		gvk, gErr := e.GVKString()
		require.NoError(t, gErr)
		gvkSet[gvk] = true
	}
	assert.True(t, gvkSet[types.KubernetesGvkAdmissionregistrationK8sIoV1ValidatingWebhookConfiguration],
		"ValidatingWebhookConfiguration should be present")
	assert.True(t, gvkSet[types.KubernetesGvkAdmissionregistrationK8sIoV1MutatingWebhookConfiguration],
		"MutatingWebhookConfiguration should be present")
}

func TestSplitWebhooks_CertManagerScenario(t *testing.T) {
	// Reproduces the bug where applying cert-manager resources in standard mode
	// would apply the ValidatingWebhookConfiguration before the cert-manager
	// deployment was running, causing Issuer validation to fail with
	// "connection refused" because the webhook pod has zero replicas.
	certManagerDeploy := makeEntity("apps", "v1", "Deployment", "cert-manager", "cert-manager-webhook")
	certManagerSvc := makeEntity("", "v1", "Service", "cert-manager", "cert-manager-webhook")
	certManagerVWC := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "cert-manager-webhook")
	certManagerIssuer := makeEntity("cert-manager.io", "v1", "Issuer", "cert-manager", "cert-manager-webhook-hetzner-ca")
	configMap := makeEntity("", "v1", "ConfigMap", "cert-manager", "cert-manager-config")

	all, err := entity.NewEntities([]entity.Entity{
		certManagerDeploy, certManagerSvc, certManagerVWC, certManagerIssuer, configMap,
	})
	require.NoError(t, err)

	webhooks, rest, err := splitWebhooks(all)
	require.NoError(t, err)

	assert.Equal(t, 1, webhooks.Len(), "only the ValidatingWebhookConfiguration should be split out")
	assert.Equal(t, 4, rest.Len(), "Deployment, Service, Issuer, and ConfigMap must be in the non-webhook set")

	webhookName, err := webhooks.Items[0].Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("cert-manager-webhook"), webhookName)

	// The Issuer must NOT be in the webhook set — it must be applied before
	// the webhook is registered so validation cannot block it.
	for _, e := range rest.Items {
		gvk, gErr := e.GVKString()
		require.NoError(t, gErr)
		assert.NotEqual(t, types.KubernetesGvkAdmissionregistrationK8sIoV1ValidatingWebhookConfiguration, gvk,
			"ValidatingWebhookConfiguration must not be in the non-webhook set")
		assert.NotEqual(t, types.KubernetesGvkAdmissionregistrationK8sIoV1MutatingWebhookConfiguration, gvk,
			"MutatingWebhookConfiguration must not be in the non-webhook set")
	}
}

// --- helpers for findChangedEntities tests ---

func makeUnstructured(group, version, kind, namespace, name string, extraFields map[string]any) unstructured.Unstructured {
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}
	obj := map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}
	for k, v := range extraFields {
		obj[k] = v
	}
	return unstructured.Unstructured{Object: obj}
}

func makeEntityWithClusterAndDryRun(group, version, kind, namespace, name string, clusterFields, dryRunFields map[string]any) entity.Entity {
	e := makeEntity(group, version, kind, namespace, name)
	clusterU := makeUnstructured(group, version, kind, namespace, name, clusterFields)
	dryRunU := makeUnstructured(group, version, kind, namespace, name, dryRunFields)
	return mustModifyApply(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.
			WithUnstructured(types.KeyClusterEntity, clusterU).
			WithUnstructured(types.KeyDryRunEntity, dryRunU)
	})
}

// --- findChangedEntities tests ---

func TestFindChangedEntities_UnchangedConfigMap(t *testing.T) {
	fields := map[string]any{
		"data": map[string]any{"key": "value"},
	}
	cm := makeEntityWithClusterAndDryRun("", "v1", "ConfigMap", "default", "my-config", fields, fields)

	entities, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "identical ConfigMap should not appear in result")
}

func TestFindChangedEntities_DebugLogContainsIdAndResult(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := log.NewLoggerWithHandler(h)

	fields := map[string]any{"data": map[string]any{"k": "v"}}
	cm := makeEntityWithClusterAndDryRun("", "v1", "ConfigMap", "default", "logged-cm", fields, fields)
	entities, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	_, err = findChangedEntities(l, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "v1/ConfigMap/default/logged-cm")
	assert.Contains(t, out, "unchanged")
	assert.Contains(t, out, logIdApplyDryRunDiff.String())
}

func TestFindChangedEntities_ChangedConfigMap(t *testing.T) {
	clusterFields := map[string]any{
		"data": map[string]any{"key": "old"},
	}
	dryRunFields := map[string]any{
		"data": map[string]any{"key": "new"},
	}
	cm := makeEntityWithClusterAndDryRun("", "v1", "ConfigMap", "default", "my-config", clusterFields, dryRunFields)

	entities, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "changed ConfigMap should be in result")
}

func TestFindChangedEntities_DeploymentOnlyReplicaDifference(t *testing.T) {
	clusterFields := map[string]any{
		"spec": map[string]any{"replicas": int64(5)},
	}
	dryRunFields := map[string]any{
		"spec": map[string]any{"replicas": int64(3)},
	}
	dep := makeEntityWithClusterAndDryRun("apps", "v1", "Deployment", "default", "web-app", clusterFields, dryRunFields)

	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "Deployment with only replica difference should not appear in result")
}

func TestFindChangedEntities_DeploymentScaledToZeroVsTemplateReplicas(t *testing.T) {
	clusterFields := map[string]any{
		"spec": map[string]any{"replicas": int64(0)},
	}
	dryRunFields := map[string]any{
		"spec": map[string]any{"replicas": int64(1)},
	}
	dep := makeEntityWithClusterAndDryRun("apps", "v1", "Deployment", "monitoring", "kube-prometheus-stack-operator", clusterFields, dryRunFields)

	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "Deployment scaled to 0 while template expects >0 must be considered changed")
}

// Regression: SSA dry-run can match live while spec.suspend stays true on the cluster; the template
// omits suspend (scheduling allowed). The apply plan must still treat this as an update.
func TestFindChangedEntities_JobLiveSuspendedMatchesDryRunYamlButTemplateRuns(t *testing.T) {
	podTemplate := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "script", "image": "example.local/img:1"},
			},
			"restartPolicy": "Never",
		},
	}
	specSuspended := map[string]any{
		"suspend":     true,
		"completions": int64(1),
		"template":    podTemplate,
	}
	specRunning := map[string]any{
		"completions": int64(1),
		"template":    podTemplate,
	}
	clusterFields := map[string]any{"spec": specSuspended}
	dryRunFields := map[string]any{"spec": specSuspended}
	base := makeEntity("batch", "v1", "Job", "demo", "generator-test")
	templateU := makeUnstructured("batch", "v1", "Job", "demo", "generator-test", map[string]any{"spec": specRunning})
	clusterU := makeUnstructured("batch", "v1", "Job", "demo", "generator-test", clusterFields)
	dryRunU := makeUnstructured("batch", "v1", "Job", "demo", "generator-test", dryRunFields)
	jobEnt := mustModifyApply(base, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyTemplateEntity, templateU).
			WithUnstructured(types.KeyClusterEntity, clusterU).
			WithUnstructured(types.KeyDryRunEntity, dryRunU)
	})
	entities, err := entity.NewEntities([]entity.Entity{jobEnt})
	require.NoError(t, err)
	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "Job suspended in cluster while template omits suspend must be considered changed")
}

func TestEnsureBatchWorkloadUnsuspendForApply_SetsSuspendFalseOnTemplate(t *testing.T) {
	podTemplate := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "script", "image": "example.local/img:1"},
			},
			"restartPolicy": "Never",
		},
	}
	specSuspended := map[string]any{
		"suspend":     true,
		"completions": int64(1),
		"template":    podTemplate,
	}
	specRunning := map[string]any{
		"completions": int64(1),
		"template":    podTemplate,
	}
	base := makeEntity("batch", "v1", "Job", "demo", "generator-test")
	templateU := makeUnstructured("batch", "v1", "Job", "demo", "generator-test", map[string]any{"spec": specRunning})
	clusterU := makeUnstructured("batch", "v1", "Job", "demo", "generator-test", map[string]any{"spec": specSuspended})
	jobEnt := mustModifyApply(base, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyTemplateEntity, templateU).
			WithUnstructured(types.KeyClusterEntity, clusterU)
	})
	entities, err := entity.NewEntities([]entity.Entity{jobEnt})
	require.NoError(t, err)
	out, err := ensureBatchWorkloadUnsuspendForApply(entities)
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	tu, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	require.True(t, ok)
	s, ok, err := unstructured.NestedBool(tu.Object, "spec", "suspend")
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, s)
}

func TestFindChangedEntities_DeploymentReplicaAndOtherDifference(t *testing.T) {
	clusterFields := map[string]any{
		"metadata": map[string]any{
			"name":      "web-app",
			"namespace": "default",
			"labels":    map[string]any{"app": "old"},
		},
		"spec": map[string]any{"replicas": int64(5)},
	}
	dryRunFields := map[string]any{
		"metadata": map[string]any{
			"name":      "web-app",
			"namespace": "default",
			"labels":    map[string]any{"app": "new"},
		},
		"spec": map[string]any{"replicas": int64(3)},
	}
	dep := makeEntityWithClusterAndDryRun("apps", "v1", "Deployment", "default", "web-app", clusterFields, dryRunFields)

	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "Deployment with label and replica differences should be in result")
}

func TestFindChangedEntities_StatefulSetOnlyReplicaDifference(t *testing.T) {
	clusterFields := map[string]any{
		"spec": map[string]any{"replicas": int64(3)},
	}
	dryRunFields := map[string]any{
		"spec": map[string]any{"replicas": int64(1)},
	}
	sts := makeEntityWithClusterAndDryRun("apps", "v1", "StatefulSet", "default", "db", clusterFields, dryRunFields)

	entities, err := entity.NewEntities([]entity.Entity{sts})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "StatefulSet with only replica difference should not appear in result")
}

func TestFindChangedEntities_ReplicaSetOnlyReplicaDifference(t *testing.T) {
	clusterFields := map[string]any{
		"spec": map[string]any{"replicas": int64(4)},
	}
	dryRunFields := map[string]any{
		"spec": map[string]any{"replicas": int64(2)},
	}
	rs := makeEntityWithClusterAndDryRun("apps", "v1", "ReplicaSet", "default", "web-rs", clusterFields, dryRunFields)

	entities, err := entity.NewEntities([]entity.Entity{rs})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "ReplicaSet with only replica difference should not appear in result")
}

func TestFindChangedEntities_DaemonSetUnchanged(t *testing.T) {
	fields := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"nodeSelector": map[string]any{"app": "web"},
				},
			},
		},
	}
	ds := makeEntityWithClusterAndDryRun("apps", "v1", "DaemonSet", "default", "log-agent", fields, fields)

	entities, err := entity.NewEntities([]entity.Entity{ds})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "unchanged DaemonSet should not appear in result")
}

func TestFindChangedEntities_DaemonSetChanged(t *testing.T) {
	clusterFields := map[string]any{
		"metadata": map[string]any{
			"name":      "log-agent",
			"namespace": "default",
			"labels":    map[string]any{"version": "v1"},
		},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"nodeSelector": map[string]any{"app": "web"},
				},
			},
		},
	}
	dryRunFields := map[string]any{
		"metadata": map[string]any{
			"name":      "log-agent",
			"namespace": "default",
			"labels":    map[string]any{"version": "v2"},
		},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"nodeSelector": map[string]any{"app": "web"},
				},
			},
		},
	}
	ds := makeEntityWithClusterAndDryRun("apps", "v1", "DaemonSet", "default", "log-agent", clusterFields, dryRunFields)

	entities, err := entity.NewEntities([]entity.Entity{ds})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "DaemonSet with label differences should be in result")
}

func TestFindChangedEntities_EntityWithoutDryRunKey(t *testing.T) {
	e := mustModifyApply(
		makeEntity("apps", "v1", "Deployment", "default", "orphan"),
		func(b entity.EntityBuilder) entity.EntityBuilder {
			return b.WithUnstructured(types.KeyClusterEntity, makeUnstructured("apps", "v1", "Deployment", "default", "orphan", map[string]any{
				"spec": map[string]any{"replicas": int64(1)},
			}))
		},
	)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "entity without dryrun key should be treated as changed")
}

func TestFindChangedEntities_EmptyEntities(t *testing.T) {
	entities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "empty input should produce empty result")
}

func TestFindChangedEntities_MixedEntities(t *testing.T) {
	unchangedCMFields := map[string]any{
		"data": map[string]any{"key": "same"},
	}
	unchangedCM := makeEntityWithClusterAndDryRun("", "v1", "ConfigMap", "default", "static-config", unchangedCMFields, unchangedCMFields)

	changedDepCluster := map[string]any{
		"metadata": map[string]any{
			"name":      "api",
			"namespace": "default",
			"labels":    map[string]any{"version": "v1"},
		},
		"spec": map[string]any{"replicas": int64(2)},
	}
	changedDepDryRun := map[string]any{
		"metadata": map[string]any{
			"name":      "api",
			"namespace": "default",
			"labels":    map[string]any{"version": "v2"},
		},
		"spec": map[string]any{"replicas": int64(2)},
	}
	changedDep := makeEntityWithClusterAndDryRun("apps", "v1", "Deployment", "default", "api", changedDepCluster, changedDepDryRun)

	replicaOnlyCluster := map[string]any{
		"spec": map[string]any{"replicas": int64(5)},
	}
	replicaOnlyDryRun := map[string]any{
		"spec": map[string]any{"replicas": int64(1)},
	}
	replicaOnlyDep := makeEntityWithClusterAndDryRun("apps", "v1", "Deployment", "default", "worker", replicaOnlyCluster, replicaOnlyDryRun)

	entities, err := entity.NewEntities([]entity.Entity{unchangedCM, changedDep, replicaOnlyDep})
	require.NoError(t, err)

	result, err := findChangedEntities(discardActionLogger, entities, builtinDiffIgnorePipeline(t))
	require.NoError(t, err)
	assert.Equal(t, 1, result.Len(), "only the Deployment with label change should be in result")

	name, err := result.Items[0].Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("api"), name, "the changed Deployment should be 'api'")
}
