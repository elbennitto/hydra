package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func webhookEntry(name, svcName, svcNamespace string) map[string]any {
	return map[string]any{
		"name": name,
		"clientConfig": map[string]any{
			"service": map[string]any{
				"name":      svcName,
				"namespace": svcNamespace,
			},
		},
	}
}

func webhookEntryURL(name, url string) map[string]any {
	return map[string]any{
		"name": name,
		"clientConfig": map[string]any{
			"url": url,
		},
	}
}

func webhooksToAny(items []map[string]any) []any {
	out := make([]any, len(items))
	for i, v := range items {
		out[i] = v
	}
	return out
}

func makeValidatingWebhookConfig(name string, webhooks []map[string]any) entity.Entity {
	e := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", name)
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1",
		"kind":       "ValidatingWebhookConfiguration",
		"metadata": map[string]any{
			"name": name,
		},
		"webhooks": webhooksToAny(webhooks),
	}}
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

func makeMutatingWebhookConfig(name string, webhooks []map[string]any) entity.Entity {
	e := makeEntity("admissionregistration.k8s.io", "v1", "MutatingWebhookConfiguration", "", name)
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1",
		"kind":       "MutatingWebhookConfiguration",
		"metadata": map[string]any{
			"name": name,
		},
		"webhooks": webhooksToAny(webhooks),
	}}
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

func makeServiceWithSelector(namespace, name string, selector map[string]string) entity.Entity {
	e := makeEntity("", "v1", "Service", namespace, name)
	selectorAny := make(map[string]any, len(selector))
	for k, v := range selector {
		selectorAny[k] = v
	}
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"selector": selectorAny,
		},
	}}
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

func makeServiceWithoutSelector(namespace, name string) entity.Entity {
	e := makeEntity("", "v1", "Service", namespace, name)
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"ports": []any{
				map[string]any{"port": int64(443)},
			},
		},
	}}
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

func makeDeploymentWithLabels(namespace, name string, labels map[string]string) entity.Entity {
	e := makeEntity("apps", "v1", "Deployment", namespace, name)
	labelsAny := make(map[string]any, len(labels))
	for k, v := range labels {
		labelsAny[k] = v
	}
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labelsAny,
				},
			},
		},
	}}
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

// --- ResolveWebhookProviders tests ---

func TestResolveWebhookProviders_SingleWebhook(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("webhook.cert-manager.io", []map[string]any{
		webhookEntry("webhook.cert-manager.io", "cert-manager-webhook", "cert-manager"),
	})
	svc := makeServiceWithSelector("cert-manager", "cert-manager-webhook", map[string]string{
		"app": "cert-manager-webhook",
	})
	deploy := makeDeploymentWithLabels("cert-manager", "cert-manager-webhook", map[string]string{
		"app": "cert-manager-webhook",
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc, deploy})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, providers, 1)

	assert.Equal(t, types.Name("cert-manager-webhook"), providers[0].ServiceName)
	assert.Equal(t, types.Namespace("cert-manager"), providers[0].ServiceNs)

	workloadName, err := providers[0].Workload.Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("cert-manager-webhook"), workloadName)
}

func TestResolveWebhookProviders_DeduplicateSameService(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("webhook.cert-manager.io", []map[string]any{
		webhookEntry("webhook-a.cert-manager.io", "cert-manager-webhook", "cert-manager"),
		webhookEntry("webhook-b.cert-manager.io", "cert-manager-webhook", "cert-manager"),
	})
	svc := makeServiceWithSelector("cert-manager", "cert-manager-webhook", map[string]string{
		"app": "cert-manager-webhook",
	})
	deploy := makeDeploymentWithLabels("cert-manager", "cert-manager-webhook", map[string]string{
		"app": "cert-manager-webhook",
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc, deploy})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, providers, 1, "duplicate service references should be deduplicated")

	assert.Equal(t, types.Name("cert-manager-webhook"), providers[0].ServiceName)
	assert.Equal(t, types.Namespace("cert-manager"), providers[0].ServiceNs)
}

func TestResolveWebhookProviders_MutatingWebhook(t *testing.T) {
	whConfig := makeMutatingWebhookConfig("mutating.kyverno.io", []map[string]any{
		webhookEntry("mutating.kyverno.io", "kyverno-svc", "kyverno"),
	})
	svc := makeServiceWithSelector("kyverno", "kyverno-svc", map[string]string{
		"app.kubernetes.io/name": "kyverno",
	})
	deploy := makeDeploymentWithLabels("kyverno", "kyverno", map[string]string{
		"app.kubernetes.io/name": "kyverno",
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc, deploy})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, providers, 1)

	assert.Equal(t, types.Name("kyverno-svc"), providers[0].ServiceName)
	assert.Equal(t, types.Namespace("kyverno"), providers[0].ServiceNs)

	workloadName, err := providers[0].Workload.Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("kyverno"), workloadName)
}

func TestResolveWebhookProviders_ServiceNotFound(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("webhook.missing.io", []map[string]any{
		webhookEntry("webhook.missing.io", "nonexistent-svc", "some-ns"),
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Empty(t, providers, "should not return providers when service is missing")
}

func TestResolveWebhookProviders_ServiceWithoutSelector(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("webhook.no-selector.io", []map[string]any{
		webhookEntry("webhook.no-selector.io", "no-selector-svc", "default"),
	})
	svc := makeServiceWithoutSelector("default", "no-selector-svc")

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Empty(t, providers, "should skip service without selector")
}

func TestResolveWebhookProviders_NoMatchingWorkload(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("webhook.orphan.io", []map[string]any{
		webhookEntry("webhook.orphan.io", "orphan-svc", "default"),
	})
	svc := makeServiceWithSelector("default", "orphan-svc", map[string]string{
		"app": "orphan-webhook",
	})
	deploy := makeDeploymentWithLabels("default", "some-other-deploy", map[string]string{
		"app": "unrelated-app",
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc, deploy})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Empty(t, providers, "should skip when no workload matches service selector")
}

func TestResolveWebhookProviders_MultipleConfigs(t *testing.T) {
	whConfig1 := makeValidatingWebhookConfig("webhook.cert-manager.io", []map[string]any{
		webhookEntry("webhook.cert-manager.io", "cert-manager-webhook", "cert-manager"),
	})
	whConfig2 := makeMutatingWebhookConfig("mutating.kyverno.io", []map[string]any{
		webhookEntry("mutating.kyverno.io", "kyverno-svc", "kyverno"),
	})

	svc1 := makeServiceWithSelector("cert-manager", "cert-manager-webhook", map[string]string{
		"app": "cert-manager-webhook",
	})
	deploy1 := makeDeploymentWithLabels("cert-manager", "cert-manager-webhook", map[string]string{
		"app": "cert-manager-webhook",
	})

	svc2 := makeServiceWithSelector("kyverno", "kyverno-svc", map[string]string{
		"app": "kyverno",
	})
	deploy2 := makeDeploymentWithLabels("kyverno", "kyverno", map[string]string{
		"app": "kyverno",
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig1, whConfig2})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig1, whConfig2, svc1, deploy1, svc2, deploy2})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, providers, 2)

	serviceNames := []types.Name{providers[0].ServiceName, providers[1].ServiceName}
	assert.Contains(t, serviceNames, types.Name("cert-manager-webhook"))
	assert.Contains(t, serviceNames, types.Name("kyverno-svc"))
}

func TestResolveWebhookProviders_EmptyInput(t *testing.T) {
	webhookEntities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Empty(t, providers)
}

func TestResolveWebhookProviders_URLBasedWebhook(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("webhook.url-based.io", []map[string]any{
		webhookEntryURL("webhook.url-based.io", "https://external.example.com/validate"),
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Empty(t, providers, "URL-based webhooks should be skipped")
}

func TestResolveWebhookProviders_AmbiguousWorkloads(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("webhook.ambiguous.io", []map[string]any{
		webhookEntry("webhook.ambiguous.io", "ambiguous-svc", "default"),
	})
	svc := makeServiceWithSelector("default", "ambiguous-svc", map[string]string{
		"app": "ambiguous",
	})
	deploy1 := makeDeploymentWithLabels("default", "ambiguous-deploy-1", map[string]string{
		"app": "ambiguous",
	})
	deploy2 := makeDeploymentWithLabels("default", "ambiguous-deploy-2", map[string]string{
		"app": "ambiguous",
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc, deploy1, deploy2})
	require.NoError(t, err)

	providers, err := ResolveWebhookProviders(log.Default(), webhookEntities, allEntities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, providers, 1, "should use first matching workload when ambiguous")

	workloadName, err := providers[0].Workload.Name()
	require.NoError(t, err)
	assert.Contains(t, []types.Name{"ambiguous-deploy-1", "ambiguous-deploy-2"}, workloadName)
}

// --- Webhook rule helpers ---

func webhookEntryWithRules(name, svcName, svcNamespace string, rules []map[string]any) map[string]any {
	entry := webhookEntry(name, svcName, svcNamespace)
	entry["rules"] = webhooksToAny(rules)
	return entry
}

func admissionRule(apiGroups, resources, operations []string) map[string]any {
	return map[string]any{
		"apiGroups":  stringsToAny(apiGroups),
		"resources":  stringsToAny(resources),
		"operations": stringsToAny(operations),
	}
}

func stringsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func makeEntityWithResource(group, version, kind, resource, namespace, name string) entity.Entity {
	return withResource(makeEntity(group, version, kind, namespace, name), types.Resource(resource))
}

// --- ExtractWebhookRules tests ---

func TestExtractWebhookRules_SingleRule(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("test-webhook", []map[string]any{
		webhookEntryWithRules("test", "svc", "ns", []map[string]any{
			admissionRule([]string{"monitoring.coreos.com"}, []string{"prometheusrules"}, []string{"CREATE", "UPDATE"}),
		}),
	})

	rules := ExtractWebhookRules(whConfig, types.KeyTemplateEntity)
	require.Len(t, rules, 1)
	assert.Equal(t, []string{"monitoring.coreos.com"}, rules[0].ApiGroups)
	assert.Equal(t, []string{"prometheusrules"}, rules[0].Resources)
	assert.Equal(t, []string{"CREATE", "UPDATE"}, rules[0].Operations)
}

func TestExtractWebhookRules_MultipleWebhooksAndRules(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("multi-webhook", []map[string]any{
		webhookEntryWithRules("wh-a", "svc", "ns", []map[string]any{
			admissionRule([]string{"cert-manager.io"}, []string{"certificates"}, []string{"CREATE"}),
		}),
		webhookEntryWithRules("wh-b", "svc", "ns", []map[string]any{
			admissionRule([]string{"apps"}, []string{"deployments"}, []string{"UPDATE"}),
			admissionRule([]string{""}, []string{"configmaps"}, []string{"CREATE", "DELETE"}),
		}),
	})

	rules := ExtractWebhookRules(whConfig, types.KeyTemplateEntity)
	require.Len(t, rules, 3)
}

func TestExtractWebhookRules_Wildcards(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("wildcard-webhook", []map[string]any{
		webhookEntryWithRules("wh", "svc", "ns", []map[string]any{
			admissionRule([]string{"cert-manager.io", "acme.cert-manager.io"}, []string{"*/*"}, []string{"CREATE", "UPDATE"}),
		}),
	})

	rules := ExtractWebhookRules(whConfig, types.KeyTemplateEntity)
	require.Len(t, rules, 1)
	assert.Equal(t, []string{"cert-manager.io", "acme.cert-manager.io"}, rules[0].ApiGroups)
	assert.Equal(t, []string{"*/*"}, rules[0].Resources)
}

func TestExtractWebhookRules_NoRules(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("no-rules", []map[string]any{
		webhookEntry("wh", "svc", "ns"),
	})

	rules := ExtractWebhookRules(whConfig, types.KeyTemplateEntity)
	assert.Empty(t, rules)
}

func TestExtractWebhookRules_NoUnstructured(t *testing.T) {
	e := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "test")
	rules := ExtractWebhookRules(e, types.KeyTemplateEntity)
	assert.Nil(t, rules)
}

// --- WebhookMatchesEntities tests ---

func TestWebhookMatchesEntities_ExactMatch(t *testing.T) {
	rules := []WebhookRule{
		{ApiGroups: []string{"monitoring.coreos.com"}, Resources: []string{"prometheusrules"}, Operations: []string{"CREATE", "UPDATE"}},
	}
	entities, err := entity.NewEntities([]entity.Entity{
		makeEntityWithResource("monitoring.coreos.com", "v1", "PrometheusRule", "prometheusrules", "monitoring", "my-rule"),
	})
	require.NoError(t, err)

	matches, err := WebhookMatchesEntities(rules, entities)
	require.NoError(t, err)
	assert.True(t, matches)
}

func TestWebhookMatchesEntities_WildcardGroup(t *testing.T) {
	rules := []WebhookRule{
		{ApiGroups: []string{"*"}, Resources: []string{"deployments"}, Operations: []string{"CREATE"}},
	}
	entities, err := entity.NewEntities([]entity.Entity{
		makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy"),
	})
	require.NoError(t, err)

	matches, err := WebhookMatchesEntities(rules, entities)
	require.NoError(t, err)
	assert.True(t, matches)
}

func TestWebhookMatchesEntities_WildcardResource(t *testing.T) {
	rules := []WebhookRule{
		{ApiGroups: []string{"cert-manager.io"}, Resources: []string{"*/*"}, Operations: []string{"CREATE"}},
	}
	entities, err := entity.NewEntities([]entity.Entity{
		makeEntityWithResource("cert-manager.io", "v1", "Certificate", "certificates", "default", "my-cert"),
	})
	require.NoError(t, err)

	matches, err := WebhookMatchesEntities(rules, entities)
	require.NoError(t, err)
	assert.True(t, matches)
}

func TestWebhookMatchesEntities_NoMatch(t *testing.T) {
	rules := []WebhookRule{
		{ApiGroups: []string{"monitoring.coreos.com"}, Resources: []string{"prometheusrules"}, Operations: []string{"CREATE"}},
	}
	entities, err := entity.NewEntities([]entity.Entity{
		makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy"),
	})
	require.NoError(t, err)

	matches, err := WebhookMatchesEntities(rules, entities)
	require.NoError(t, err)
	assert.False(t, matches)
}

func TestWebhookMatchesEntities_OnlyDeleteOperations(t *testing.T) {
	rules := []WebhookRule{
		{ApiGroups: []string{"apps"}, Resources: []string{"deployments"}, Operations: []string{"DELETE"}},
	}
	entities, err := entity.NewEntities([]entity.Entity{
		makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy"),
	})
	require.NoError(t, err)

	matches, err := WebhookMatchesEntities(rules, entities)
	require.NoError(t, err)
	assert.False(t, matches, "DELETE-only rules should not match during apply")
}

func TestWebhookMatchesEntities_EmptyOperations(t *testing.T) {
	rules := []WebhookRule{
		{ApiGroups: []string{"apps"}, Resources: []string{"deployments"}, Operations: nil},
	}
	entities, err := entity.NewEntities([]entity.Entity{
		makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy"),
	})
	require.NoError(t, err)

	matches, err := WebhookMatchesEntities(rules, entities)
	require.NoError(t, err)
	assert.True(t, matches, "empty operations should match all")
}

func TestWebhookMatchesEntities_EmptyRules(t *testing.T) {
	entities, err := entity.NewEntities([]entity.Entity{
		makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy"),
	})
	require.NoError(t, err)

	matches, err := WebhookMatchesEntities(nil, entities)
	require.NoError(t, err)
	assert.False(t, matches)
}

// --- FilterWebhooksToDisable tests ---

func TestFilterWebhooksToDisable_ReadyProviderNotDisabled(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("test-webhook", []map[string]any{
		webhookEntryWithRules("wh", "svc", "ns", []map[string]any{
			admissionRule([]string{"cert-manager.io"}, []string{"*/*"}, []string{"CREATE"}),
		}),
	})
	svc := makeServiceWithSelector("ns", "svc", map[string]string{"app": "webhook"})
	deploy := makeDeploymentWithLabels("ns", "webhook-deploy", map[string]string{"app": "webhook"})
	resource := makeEntityWithResource("cert-manager.io", "v1", "Certificate", "certificates", "default", "my-cert")

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	nonWebhookEntities, err := entity.NewEntities([]entity.Entity{resource})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc, deploy, resource})
	require.NoError(t, err)

	alwaysReady := func(_ WebhookProvider) (bool, error) { return true, nil }

	toDisable, toKeep, err := FilterWebhooksToDisable(log.Default(), webhookEntities, nonWebhookEntities, allEntities, types.KeyTemplateEntity, alwaysReady)
	require.NoError(t, err)
	assert.Equal(t, 0, toDisable.Len())
	assert.Equal(t, 1, toKeep.Len())
}

func TestFilterWebhooksToDisable_NotReadyProviderDisabled(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("test-webhook", []map[string]any{
		webhookEntryWithRules("wh", "svc", "ns", []map[string]any{
			admissionRule([]string{"cert-manager.io"}, []string{"*/*"}, []string{"CREATE"}),
		}),
	})
	svc := makeServiceWithSelector("ns", "svc", map[string]string{"app": "webhook"})
	deploy := makeDeploymentWithLabels("ns", "webhook-deploy", map[string]string{"app": "webhook"})
	resource := makeEntityWithResource("cert-manager.io", "v1", "Certificate", "certificates", "default", "my-cert")

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	nonWebhookEntities, err := entity.NewEntities([]entity.Entity{resource})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, svc, deploy, resource})
	require.NoError(t, err)

	neverReady := func(_ WebhookProvider) (bool, error) { return false, nil }

	toDisable, toKeep, err := FilterWebhooksToDisable(log.Default(), webhookEntities, nonWebhookEntities, allEntities, types.KeyTemplateEntity, neverReady)
	require.NoError(t, err)
	assert.Equal(t, 1, toDisable.Len())
	assert.Equal(t, 0, toKeep.Len())
}

func TestFilterWebhooksToDisable_NoMatchingRules(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("test-webhook", []map[string]any{
		webhookEntryWithRules("wh", "svc", "ns", []map[string]any{
			admissionRule([]string{"monitoring.coreos.com"}, []string{"prometheusrules"}, []string{"CREATE"}),
		}),
	})
	resource := makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy")

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	nonWebhookEntities, err := entity.NewEntities([]entity.Entity{resource})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, resource})
	require.NoError(t, err)

	neverReady := func(_ WebhookProvider) (bool, error) { return false, nil }

	toDisable, toKeep, err := FilterWebhooksToDisable(log.Default(), webhookEntities, nonWebhookEntities, allEntities, types.KeyTemplateEntity, neverReady)
	require.NoError(t, err)
	assert.Equal(t, 0, toDisable.Len(), "webhook should not be disabled if rules don't match applied resources")
	assert.Equal(t, 1, toKeep.Len())
}

func TestFilterWebhooksToDisable_NoBackingWorkload(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("test-webhook", []map[string]any{
		webhookEntryWithRules("wh", "missing-svc", "ns", []map[string]any{
			admissionRule([]string{"apps"}, []string{"deployments"}, []string{"CREATE"}),
		}),
	})
	resource := makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy")

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	nonWebhookEntities, err := entity.NewEntities([]entity.Entity{resource})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, resource})
	require.NoError(t, err)

	neverReady := func(_ WebhookProvider) (bool, error) { return false, nil }

	toDisable, toKeep, err := FilterWebhooksToDisable(log.Default(), webhookEntities, nonWebhookEntities, allEntities, types.KeyTemplateEntity, neverReady)
	require.NoError(t, err)
	assert.Equal(t, 1, toDisable.Len(), "webhook with no backing workload should be disabled")
	assert.Equal(t, 0, toKeep.Len())
}

func TestFilterWebhooksToDisable_URLBasedWebhookKept(t *testing.T) {
	e := makeEntity("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "url-webhook")
	u := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1",
		"kind":       "ValidatingWebhookConfiguration",
		"metadata":   map[string]any{"name": "url-webhook"},
		"webhooks": []any{
			map[string]any{
				"name":         "wh",
				"clientConfig": map[string]any{"url": "https://external.example.com/validate"},
				"rules": []any{
					admissionRule([]string{"apps"}, []string{"deployments"}, []string{"CREATE"}),
				},
			},
		},
	}}
	whConfig := withUnstructured(e, types.KeyTemplateEntity, u)

	resource := makeEntityWithResource("apps", "v1", "Deployment", "deployments", "default", "my-deploy")

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)
	nonWebhookEntities, err := entity.NewEntities([]entity.Entity{resource})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{whConfig, resource})
	require.NoError(t, err)

	neverReady := func(_ WebhookProvider) (bool, error) { return false, nil }

	toDisable, toKeep, err := FilterWebhooksToDisable(log.Default(), webhookEntities, nonWebhookEntities, allEntities, types.KeyTemplateEntity, neverReady)
	require.NoError(t, err)
	assert.Equal(t, 0, toDisable.Len(), "URL-based webhook should be kept")
	assert.Equal(t, 1, toKeep.Len())
}

func TestFilterWebhooksToDisable_EmptyInput(t *testing.T) {
	webhookEntities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)
	nonWebhookEntities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)

	neverReady := func(_ WebhookProvider) (bool, error) { return false, nil }

	toDisable, toKeep, err := FilterWebhooksToDisable(log.Default(), webhookEntities, nonWebhookEntities, allEntities, types.KeyTemplateEntity, neverReady)
	require.NoError(t, err)
	assert.Equal(t, 0, toDisable.Len())
	assert.Equal(t, 0, toKeep.Len())
}

func TestSetWebhookFailurePolicy_RewritesEntries(t *testing.T) {
	whConfig := makeValidatingWebhookConfig("test-webhook", []map[string]any{
		webhookEntryWithRules("wh-a", "svc", "ns", []map[string]any{
			admissionRule([]string{"apps"}, []string{"deployments"}, []string{"CREATE"}),
		}),
		webhookEntryWithRules("wh-b", "svc", "ns", []map[string]any{
			admissionRule([]string{"apps"}, []string{"deployments"}, []string{"UPDATE"}),
		}),
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{whConfig})
	require.NoError(t, err)

	modified, err := SetWebhookFailurePolicy(webhookEntities, types.KeyTemplateEntity, "Ignore")
	require.NoError(t, err)
	require.Len(t, modified.Items, 1)

	u, err := modified.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	webhooksRaw, ok := u.Object["webhooks"].([]any)
	require.True(t, ok)
	require.Len(t, webhooksRaw, 2)
	for _, whRaw := range webhooksRaw {
		wh, ok := whRaw.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Ignore", wh["failurePolicy"])
	}
}

func TestPlanWebhookApplyOrder_UsesProviderDependencyOrder(t *testing.T) {
	dbDeploy := makeDeploymentWithLabels("ns", "db", map[string]string{"app": "db"})
	apiDeploy := makeDeploymentWithLabels("ns", "api", map[string]string{"app": "api"})
	dbSvc := makeServiceWithSelector("ns", "db-webhook-svc", map[string]string{"app": "db"})
	apiSvc := makeServiceWithSelector("ns", "api-webhook-svc", map[string]string{"app": "api"})
	dbWebhook := makeValidatingWebhookConfig("db-webhook", []map[string]any{
		webhookEntryWithRules("db", "db-webhook-svc", "ns", []map[string]any{
			admissionRule([]string{"apps"}, []string{"deployments"}, []string{"CREATE"}),
		}),
	})
	apiWebhook := makeValidatingWebhookConfig("api-webhook", []map[string]any{
		webhookEntryWithRules("api", "api-webhook-svc", "ns", []map[string]any{
			admissionRule([]string{"apps"}, []string{"deployments"}, []string{"CREATE"}),
		}),
	})

	webhookEntities, err := entity.NewEntities([]entity.Entity{apiWebhook, dbWebhook})
	require.NoError(t, err)
	allEntities, err := entity.NewEntities([]entity.Entity{
		apiWebhook, dbWebhook,
		apiSvc, dbSvc,
		apiDeploy, dbDeploy,
	})
	require.NoError(t, err)

	ordered, err := PlanWebhookApplyOrder(log.Default(), webhookEntities, allEntities, []types.Ref{
		{From: mustID(t, apiDeploy), To: mustID(t, dbDeploy)},
	}, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, ordered.Items, 2)

	firstName, err := ordered.Items[0].Name()
	require.NoError(t, err)
	secondName, err := ordered.Items[1].Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("db-webhook"), firstName)
	assert.Equal(t, types.Name("api-webhook"), secondName)
}

func mustID(t *testing.T, e entity.Entity) types.Id {
	t.Helper()
	id, err := e.Id()
	require.NoError(t, err)
	return id
}
