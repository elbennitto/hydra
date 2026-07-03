package commands

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	hlog "hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func reviewRefsTestRenderedSelectedAppEntities(
	t *testing.T,
	data string,
	appNamespace types.AppNamespace,
	appIdsByName map[string][]types.AppId,
) entity.Entities {
	t.Helper()

	entities, err := entity.NewEntitiesFromYaml(hlog.Default(), types.YamlString(data), types.KeyTemplateEntity)
	require.NoError(t, err)
	entities, err = entities.WithAppNamespace(appNamespace)
	require.NoError(t, err)

	var updated []entity.Entity
	for _, item := range entities.Items {
		name, err := item.Name()
		require.NoError(t, err)
		if appIds, ok := appIdsByName[string(name)]; ok {
			item = withAppIds(item, appIds)
		}
		updated = append(updated, item)
	}

	result, err := entity.NewEntities(updated)
	require.NoError(t, err)

	result, err = ApplyScopeInfoMap(types.CrdModeKeepUnknown, result, DefaultScopeInfoMap(), types.KeyTemplateEntity)
	require.NoError(t, err)
	return result
}

func reviewRefsRequireEntityByName(t *testing.T, entities entity.Entities, expectedName types.Name) entity.Entity {
	t.Helper()

	for _, item := range entities.Items {
		name, err := item.Name()
		require.NoError(t, err)
		if name == expectedName {
			return item
		}
	}

	t.Fatalf("entity with name %q not found", expectedName)
	return entity.Entity{}
}

func reviewRefsAssertNamespacedTemplateEntity(
	t *testing.T,
	item entity.Entity,
	expectedId types.Id,
	expectedNamespace types.Namespace,
) {
	t.Helper()

	id, err := item.Id()
	require.NoError(t, err)
	assert.Equal(t, expectedId, id)

	namespace, err := item.Namespace()
	require.NoError(t, err)
	assert.Equal(t, expectedNamespace, namespace)

	u, err := item.UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, string(expectedNamespace), u.GetNamespace())
}

// TestReviewRefsEntitiesWithTargets_TemplateWithoutMetadataNamespaceMatchesClusterTargetInAppNamespace
// reproduces cluster-style review: rendered templates omit metadata.namespace while live targets use the app namespace.
// Ref endpoints must normalize to that namespace so we do not report a false "missing target resource" for v1/ConfigMap//name.
func TestReviewRefsEntitiesWithTargets_TemplateWithoutMetadataNamespaceMatchesClusterTargetInAppNamespace(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-selected": {"prod.platform.consumer-selected"},
	})
	reviewRefsAssertNamespacedTemplateEntity(
		t,
		reviewRefsRequireEntityByName(t, sourceEntities, types.Name("consumer-selected")),
		types.Id("apps/v1/Deployment/demo/consumer-selected"),
		types.Namespace("demo"),
	)

	targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  SPRING_PROFILE: prod
`, types.KeyClusterEntity, map[string][]types.AppId{
		"shared-config": {"prod.platform.provider"},
	})

	findings, err := ReviewRefsEntitiesWithTargets(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.consumer-selected")),
	)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestReviewRefsEntities_TemplateWithoutMetadataNamespaceMatchesTargetInSameRenderSet(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
data:
  SPRING_PROFILE: prod
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-selected": {"prod.platform.consumer-selected"},
		"shared-config":     {"prod.platform.provider"},
	})

	reviewRefsAssertNamespacedTemplateEntity(
		t,
		reviewRefsRequireEntityByName(t, entities, types.Name("consumer-selected")),
		types.Id("apps/v1/Deployment/demo/consumer-selected"),
		types.Namespace("demo"),
	)
	reviewRefsAssertNamespacedTemplateEntity(
		t,
		reviewRefsRequireEntityByName(t, entities, types.Name("shared-config")),
		types.Id("v1/ConfigMap/demo/shared-config"),
		types.Namespace("demo"),
	)

	findings, err := ReviewRefsEntities(hlog.Default(), entities, sets.New(types.AppId("prod.platform.consumer-selected")))
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestSelectedAppRenderPipelineNormalizesNamespacedResourcesWithoutMetadataNamespace(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: api
          image: nginx:1.27
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  KEY: value
`, types.AppNamespace("prod-ns"), map[string][]types.AppId{
		"web":        {"prod.platform.web"},
		"app-config": {"prod.platform.web"},
	})

	reviewRefsAssertNamespacedTemplateEntity(
		t,
		reviewRefsRequireEntityByName(t, entities, types.Name("web")),
		types.Id("apps/v1/Deployment/prod-ns/web"),
		types.Namespace("prod-ns"),
	)
	reviewRefsAssertNamespacedTemplateEntity(
		t,
		reviewRefsRequireEntityByName(t, entities, types.Name("app-config")),
		types.Id("v1/ConfigMap/prod-ns/app-config"),
		types.Namespace("prod-ns"),
	)
}

func TestReviewRefsEntitiesAllowsExistingCrossAppTarget(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-api
  template:
    metadata:
      labels:
        app: consumer-api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  SPRING_PROFILE: prod
`, map[string][]types.AppId{
		"consumer-api":  {"prod.platform.consumer"},
		"shared-config": {"prod.platform.provider"},
	})

	findings, err := ReviewRefsEntities(hlog.Default(), entities, sets.New(types.AppId("prod.platform.consumer")))
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestReviewRefsEntitiesGroupsRepeatedMissingKeyFindings(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-a
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-a
  template:
    metadata:
      labels:
        app: consumer-a
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-b
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-b
  template:
    metadata:
      labels:
        app: consumer-b
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  OTHER_KEY: value
`, map[string][]types.AppId{
		"consumer-a":    {"prod.platform.consumer-a"},
		"consumer-b":    {"prod.platform.consumer-b"},
		"shared-config": {"prod.platform.provider"},
	})

	findings, err := ReviewRefsEntities(hlog.Default(), entities, sets.New(
		types.AppId("prod.platform.consumer-a"),
		types.AppId("prod.platform.consumer-b"),
	))
	require.NoError(t, err)
	require.Len(t, findings, 1)

	assert.Equal(t, types.Id("v1/ConfigMap/demo/shared-config"), findings[0].Target)
	assert.ElementsMatch(t, []types.Id{
		"apps/v1/Deployment/demo/consumer-a",
		"apps/v1/Deployment/demo/consumer-b",
	}, findings[0].Sources)
	assert.Contains(t, findings[0].Message, "SPRING_PROFILE")
}

func TestReviewRefsEntitiesTreatsEnvFromAsExistenceOnly(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-envfrom
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-envfrom
  template:
    metadata:
      labels:
        app: consumer-envfrom
    spec:
      containers:
        - name: api
          image: nginx:1.27
          envFrom:
            - configMapRef:
                name: shared-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
`, map[string][]types.AppId{
		"consumer-envfrom": {"prod.platform.consumer"},
		"shared-config":    {"prod.platform.provider"},
	})

	findings, err := ReviewRefsEntities(hlog.Default(), entities, sets.New(types.AppId("prod.platform.consumer")))
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestReviewRefsEntitiesReportsMissingTargetResource(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-missing
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-missing
  template:
    metadata:
      labels:
        app: consumer-missing
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: missing-secret
                  key: PASSWORD
`, map[string][]types.AppId{
		"consumer-missing": {"prod.platform.consumer"},
	})

	findings, err := ReviewRefsEntities(hlog.Default(), entities, sets.New(types.AppId("prod.platform.consumer")))
	require.NoError(t, err)
	require.Len(t, findings, 1)

	assert.Equal(t, types.Id("v1/Secret/demo/missing-secret"), findings[0].Target)
	assert.ElementsMatch(t, []types.Id{
		"apps/v1/Deployment/demo/consumer-missing",
	}, findings[0].Sources)
	assert.Equal(t, "missing target resource", findings[0].Message)
}

func TestReviewRefsEntitiesSkipsOptionalTaggedRefs(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-optional-missing
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-optional-missing
  template:
    metadata:
      labels:
        app: consumer-optional-missing
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: missing-secret
                  key: PASSWORD
                  optional: true
`, map[string][]types.AppId{
		"consumer-optional-missing": {"prod.platform.consumer"},
	})

	findings, err := ReviewRefsEntities(hlog.Default(), entities, sets.New(types.AppId("prod.platform.consumer")))
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestReviewRefsEntitiesWithTargetsUsesLiveTargetsOutsideRenderSet(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-unselected
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-unselected
  template:
    metadata:
      labels:
        app: consumer-unselected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
`, map[string][]types.AppId{
		"consumer-selected":   {"prod.platform.consumer-selected"},
		"consumer-unselected": {"prod.platform.consumer-unselected"},
	})
	targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  SPRING_PROFILE: prod
`, types.KeyClusterEntity, nil)

	findings, err := ReviewRefsEntitiesWithTargets(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.consumer-selected")),
	)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestReviewRefsEntitiesWithTargetsReportsMissingLiveTargetResource(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: missing-secret
                  key: PASSWORD
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-selected": {"prod.platform.consumer-selected"},
	})
	reviewRefsAssertNamespacedTemplateEntity(
		t,
		reviewRefsRequireEntityByName(t, sourceEntities, types.Name("consumer-selected")),
		types.Id("apps/v1/Deployment/demo/consumer-selected"),
		types.Namespace("demo"),
	)

	findings, err := ReviewRefsEntitiesWithTargets(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		entity.Entities{},
		types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.consumer-selected")),
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, types.Id("v1/Secret/demo/missing-secret"), findings[0].Target)
	assert.Equal(t, "missing target resource", findings[0].Message)
	assert.ElementsMatch(t, []types.Id{
		"apps/v1/Deployment/demo/consumer-selected",
	}, findings[0].Sources)
}

func TestReviewRefsEntitiesWithTargetsGroupsLiveTargetKeyFindingsForSelectedSources(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-a
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-a
  template:
    metadata:
      labels:
        app: consumer-a
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-b
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-b
  template:
    metadata:
      labels:
        app: consumer-b
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-unselected
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-unselected
  template:
    metadata:
      labels:
        app: consumer-unselected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
`, map[string][]types.AppId{
		"consumer-a":          {"prod.platform.consumer-a"},
		"consumer-b":          {"prod.platform.consumer-b"},
		"consumer-unselected": {"prod.platform.consumer-unselected"},
	})
	targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  OTHER_KEY: value
`, types.KeyClusterEntity, nil)

	findings, err := ReviewRefsEntitiesWithTargets(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		types.KeyClusterEntity,
		sets.New(
			types.AppId("prod.platform.consumer-a"),
			types.AppId("prod.platform.consumer-b"),
		),
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	assert.Equal(t, types.Id("v1/ConfigMap/demo/shared-config"), findings[0].Target)
	assert.ElementsMatch(t, []types.Id{
		"apps/v1/Deployment/demo/consumer-a",
		"apps/v1/Deployment/demo/consumer-b",
	}, findings[0].Sources)
	assert.Equal(t, `missing referenced key "SPRING_PROFILE"`, findings[0].Message)
}

func TestReviewRefsEntitiesScopesUnselectedTargetValidationToSelectedSources(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-unselected
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-unselected
  template:
    metadata:
      labels:
        app: consumer-unselected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  OTHER_KEY: value
`, map[string][]types.AppId{
		"consumer-selected":   {"prod.platform.consumer-selected"},
		"consumer-unselected": {"prod.platform.consumer-unselected"},
	})

	findings, err := ReviewRefsEntities(hlog.Default(), entities, sets.New(types.AppId("prod.platform.consumer-selected")))
	require.NoError(t, err)
	require.Len(t, findings, 1)

	assert.Equal(t, types.Id("v1/ConfigMap/demo/shared-config"), findings[0].Target)
	assert.ElementsMatch(t, []types.Id{
		"apps/v1/Deployment/demo/consumer-selected",
	}, findings[0].Sources)
	assert.Equal(t, `missing referenced key "SPRING_PROFILE"`, findings[0].Message)
}

func TestReviewRefsEntitiesCallbackEmitsSortedFindings(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-multi
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-multi
  template:
    metadata:
      labels:
        app: consumer-multi
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PROFILE
              valueFrom:
                configMapKeyRef:
                  name: missing-config
                  key: SPRING_PROFILE
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: missing-secret
                  key: PASSWORD
`, map[string][]types.AppId{
		"consumer-multi": {"prod.platform.consumer"},
	})

	var emitted []ReviewFinding
	count, err := ReviewRefsEntitiesCallback(
		hlog.Default(),
		entities,
		sets.New(types.AppId("prod.platform.consumer")),
		func(finding ReviewFinding) error {
			emitted = append(emitted, finding)
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	require.Len(t, emitted, 2)
	assert.Equal(t, types.Id("v1/ConfigMap/demo/missing-config"), emitted[0].Target)
	assert.Equal(t, types.Id("v1/Secret/demo/missing-secret"), emitted[1].Target)
}

func TestReviewRefsEntitiesCallbackSkipsInvocationWhenNoFindings(t *testing.T) {
	configureReviewRefsTestLogging()

	entities := reviewRefsTestEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-ok
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-ok
  template:
    metadata:
      labels:
        app: consumer-ok
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: demo
data:
  SPRING_PROFILE: prod
`, map[string][]types.AppId{
		"consumer-ok":   {"prod.platform.consumer"},
		"shared-config": {"prod.platform.provider"},
	})

	callbackCalls := 0
	count, err := ReviewRefsEntitiesCallback(
		hlog.Default(),
		entities,
		sets.New(types.AppId("prod.platform.consumer")),
		func(finding ReviewFinding) error {
			callbackCalls++
			return nil
		},
	)
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Zero(t, callbackCalls)
}

func reviewRefsTestEntities(t *testing.T, data string, appIdsByName map[string][]types.AppId) entity.Entities {
	t.Helper()
	return reviewRefsTestEntitiesWithKey(t, data, types.KeyTemplateEntity, appIdsByName)
}

func reviewRefsTestEntitiesWithKey(
	t *testing.T,
	data string,
	key types.EntityKeyUnstructured,
	appIdsByName map[string][]types.AppId,
) entity.Entities {
	t.Helper()

	entities, err := entity.NewEntitiesFromYaml(hlog.Default(), types.YamlString(data), key)
	require.NoError(t, err)

	var updated []entity.Entity
	for _, item := range entities.Items {
		name, err := item.Name()
		require.NoError(t, err)
		if appIds, ok := appIdsByName[string(name)]; ok {
			item = withAppIds(item, appIds)
		}
		updated = append(updated, item)
	}

	result, err := entity.NewEntities(updated)
	require.NoError(t, err)
	return result
}

func configureReviewRefsTestLogging() {
	hlog.Configure(hlog.Config{
		Level:      slog.LevelWarn,
		Timestamps: false,
	})
}

func reviewRefsSopsSecretRefParsers(t *testing.T) []types.RefParser {
	t.Helper()
	const yaml = `ref-parsers:
  - predicate: 'gvk == "isindir.github.com/v1alpha3/SopsSecret"'
    attributes:
      - "origin:generated": controller
    pick:
      - cel: 'entity.spec.secretTemplates.map(t, refBuilder().outgoing(id("v1/Secret", ns, t.name)))'
        label: sops
        reverse: true
`
	parsers, err := references.ParseRefParsers([]byte(yaml))
	require.NoError(t, err)
	return parsers
}

func reviewRefsCollectWithTargetsAndParsers(
	t *testing.T,
	sourceEntities entity.Entities,
	targetEntities entity.Entities,
	targetKey types.EntityKeyUnstructured,
	selectedAppIds sets.Set[types.AppId],
	extraParsers []types.RefParser,
) []ReviewFinding {
	t.Helper()
	var findings []ReviewFinding
	_, err := reviewRefsEntitiesWithTargetsCallback(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		targetKey,
		selectedAppIds,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		extraParsers,
	)
	require.NoError(t, err)
	return findings
}

// TestReviewRefsEntitiesWithTargets_LocalTargetIndexIncludesPeerEnabledAppTemplates documents
// hydra local review: targets are templates of all effectively enabled apps on the cluster,
// so a reference from a selected app can resolve to objects owned by another enabled app.
func TestReviewRefsEntitiesWithTargets_LocalTargetIndexIncludesPeerEnabledAppTemplates(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SPRING_PROFILE
              valueFrom:
                configMapKeyRef:
                  name: shared-config
                  key: SPRING_PROFILE
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-selected": {"prod.platform.consumer-selected"},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
data:
  SPRING_PROFILE: prod
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"shared-config": {"prod.platform.peer-provider"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(types.AppId("prod.platform.consumer-selected")),
		nil,
	)
	assert.Empty(t, findings)
}

// TestReviewRefsEntitiesWithTargets_ExcludeAppDoesNotShrinkTemplateTargetSet documents that
// --exclude-app only shrinks the source app set; the local target template index must still
// contain resources from apps that were excluded from the source selection.
func TestReviewRefsEntitiesWithTargets_ExcludeAppDoesNotShrinkTemplateTargetSet(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-only-selected
spec:
  selector:
    matchLabels:
      app: consumer-only-selected
  template:
    metadata:
      labels:
        app: consumer-only-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: CFG
              valueFrom:
                configMapKeyRef:
                  name: excluded-app-cm
                  key: CFG
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-only-selected": {"prod.platform.consumer"},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: excluded-app-cm
data:
  CFG: "1"
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"excluded-app-cm": {"prod.platform.excluded-from-sources"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		nil,
	)
	assert.Empty(t, findings)
}

// TestReviewRefsEntitiesWithTargets_ClusterReviewReducedSourcesAndLiveTargets documents hydra
// cluster review (KeyClusterEntity targets): (1) a reduced source app set must not shrink the
// live target inventory; (2) selected sources may resolve against live objects tied to apps outside
// that source set; (3) findings still only cite sources from the selected app IDs.
func TestReviewRefsEntitiesWithTargets_ClusterReviewReducedSourcesAndLiveTargets(t *testing.T) {
	configureReviewRefsTestLogging()

	t.Run("live_targets_not_shrunk_selected_resolves_against_excluded_app_owned_object", func(t *testing.T) {
		sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-only-selected
spec:
  selector:
    matchLabels:
      app: consumer-only-selected
  template:
    metadata:
      labels:
        app: consumer-only-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: CFG
              valueFrom:
                configMapKeyRef:
                  name: live-excluded-app-cm
                  key: CFG
`, types.AppNamespace("demo"), map[string][]types.AppId{
			"consumer-only-selected": {"prod.platform.consumer"},
		})

		targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: live-excluded-app-cm
  namespace: demo
data:
  CFG: "1"
`, types.KeyClusterEntity, map[string][]types.AppId{
			"live-excluded-app-cm": {"prod.platform.excluded-from-sources"},
		})

		findings := reviewRefsCollectWithTargetsAndParsers(
			t, sourceEntities, targetEntities, types.KeyClusterEntity,
			sets.New(types.AppId("prod.platform.consumer")),
			nil,
		)
		assert.Empty(t, findings)
	})

	t.Run("findings_only_from_selected_apps_when_extra_manifests_present", func(t *testing.T) {
		sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: CFG
              valueFrom:
                configMapKeyRef:
                  name: live-shared-cm
                  key: CFG
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: other-unselected
spec:
  selector:
    matchLabels:
      app: other-unselected
  template:
    metadata:
      labels:
        app: other-unselected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: X
              valueFrom:
                secretKeyRef:
                  name: missing-only-for-unselected
                  key: X
`, types.AppNamespace("demo"), map[string][]types.AppId{
			"consumer-selected": {"prod.platform.consumer"},
			"other-unselected":  {"prod.platform.other-unselected"},
		})

		targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: live-shared-cm
  namespace: demo
data:
  CFG: "1"
`, types.KeyClusterEntity, map[string][]types.AppId{
			"live-shared-cm": {"prod.platform.peer-live"},
		})

		findings := reviewRefsCollectWithTargetsAndParsers(
			t, sourceEntities, targetEntities, types.KeyClusterEntity,
			sets.New(types.AppId("prod.platform.consumer")),
			nil,
		)
		assert.Empty(t, findings)
	})
}

// TestReviewRefsEntitiesWithTargets_ClusterModeAcceptsCrossNamespaceLiveTargets documents
// hydra gitops review: the live inventory spans all namespaces, so references that point
// across namespaces (here RoleBinding subject ServiceAccount in another namespace) resolve
// against cluster targets even when the source app lives in a different namespace.
func TestReviewRefsEntitiesWithTargets_ClusterModeAcceptsCrossNamespaceLiveTargets(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: bind-remote-sa
subjects:
  - kind: ServiceAccount
    name: remote-sa
    namespace: other-ns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: demo-role
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"bind-remote-sa": {"prod.platform.consumer-selected"},
	})

	targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: demo-role
  namespace: demo
rules: []
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: remote-sa
  namespace: other-ns
`, types.KeyClusterEntity, map[string][]types.AppId{
		"demo-role": {"prod.platform.consumer-selected"},
		"remote-sa": {"prod.platform.other-team"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.consumer-selected")),
		nil,
	)
	assert.Empty(t, findings)
}

// TestReviewRefsEntitiesWithTargets_DisabledAppNotInLocalTargetIndex documents hydra local
// review refs: templates from enabled: false apps are not part of the target set, so references
// that would only be satisfied by such an app stay unresolved.
func TestReviewRefsEntitiesWithTargets_DisabledAppNotInLocalTargetIndex(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-selected
spec:
  selector:
    matchLabels:
      app: consumer-selected
  template:
    metadata:
      labels:
        app: consumer-selected
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: X
              valueFrom:
                configMapKeyRef:
                  name: only-from-disabled-app
                  key: X
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-selected": {"prod.platform.consumer"},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: other-enabled-cm
data:
  OTHER: "1"
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"other-enabled-cm": {"prod.platform.other-enabled"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		nil,
	)
	require.Len(t, findings, 1)
	assert.Equal(t, types.Id("v1/ConfigMap/demo/only-from-disabled-app"), findings[0].Target)
	assert.Equal(t, missingTargetResourceFinding, findings[0].Message)
	assert.ElementsMatch(t, []types.Id{"apps/v1/Deployment/demo/consumer-selected"}, findings[0].Sources)
}

// TestReviewRefsEntitiesWithTargets_SopsSecretSecretTemplatesPreventMissingTarget documents the
// shared rule: a referenced Secret counts as present when a SopsSecret in the target set defines
// it via spec.secretTemplates (no plain v1/Secret required).
func TestReviewRefsEntitiesWithTargets_SopsSecretSecretTemplatesPreventMissingTarget(t *testing.T) {
	configureReviewRefsTestLogging()

	sopsAndConsumer := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-api
spec:
  selector:
    matchLabels:
      app: consumer-api
  template:
    metadata:
      labels:
        app: consumer-api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: generated-db
                  key: PASSWORD
---
apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: db-sops
spec:
  secretTemplates:
    - name: generated-db
      stringData:
        PASSWORD: "encrypted"
`
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, sopsAndConsumer, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-api": {"prod.platform.consumer"},
		"db-sops":      {"prod.platform.consumer"},
	})
	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, sopsAndConsumer, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-api": {"prod.platform.consumer"},
		"db-sops":      {"prod.platform.consumer"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		reviewRefsSopsSecretRefParsers(t),
	)
	assert.Empty(t, findings)
}

// TestReviewRefsEntitiesWithTargets_ClusterKeyGeneratorBackedSecret documents cluster review
// with types.KeyClusterEntity targets: a referenced v1/Secret need not exist as a live object when
// the cluster target set contains a SopsSecret whose secretTemplates define that Secret.
func TestReviewRefsEntitiesWithTargets_ClusterKeyGeneratorBackedSecret(t *testing.T) {
	configureReviewRefsTestLogging()
	parsers := reviewRefsSopsSecretRefParsers(t)

	sourceYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-api
spec:
  selector:
    matchLabels:
      app: consumer-api
  template:
    metadata:
      labels:
        app: consumer-api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: cluster-generated-db
                  key: PASSWORD
---
apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: cluster-db-sops
spec:
  secretTemplates:
    - name: cluster-generated-db
      stringData:
        PASSWORD: "encrypted"
`
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, sourceYAML, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-api":    {"prod.platform.consumer"},
		"cluster-db-sops": {"prod.platform.sops-peer"},
	})

	t.Run("sopssecret_in_cluster_targets_prevents_missing_secret_without_live_secret", func(t *testing.T) {
		targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: cluster-db-sops
  namespace: demo
spec:
  secretTemplates:
    - name: cluster-generated-db
      stringData:
        PASSWORD: "encrypted"
`, types.KeyClusterEntity, map[string][]types.AppId{
			"cluster-db-sops": {"prod.platform.sops-peer"},
		})

		findings := reviewRefsCollectWithTargetsAndParsers(
			t, sourceEntities, targetEntities, types.KeyClusterEntity,
			sets.New(types.AppId("prod.platform.consumer")),
			parsers,
		)
		assert.Empty(t, findings)
	})

	t.Run("no_live_secret_and_no_matching_generator_yields_missing_target", func(t *testing.T) {
		emptyTargets, err := entity.NewEntities(nil)
		require.NoError(t, err)

		findings := reviewRefsCollectWithTargetsAndParsers(
			t, sourceEntities, emptyTargets, types.KeyClusterEntity,
			sets.New(types.AppId("prod.platform.consumer")),
			parsers,
		)
		require.Len(t, findings, 1)
		assert.Equal(t, types.Id("v1/Secret/demo/cluster-generated-db"), findings[0].Target)
		assert.Equal(t, missingTargetResourceFinding, findings[0].Message)
		assert.ElementsMatch(t, []types.Id{"apps/v1/Deployment/demo/consumer-api"}, findings[0].Sources)
	})
}

// TestReviewRefsEntitiesWithTargets_PeerSopsSecretOnlyInTargetsResolvesReference documents that a
// SopsSecret owned by another enabled app must still satisfy references when it appears only in
// the target template set (not in the selected source render). This requires the orchestration
// layer to include generator-bearing resources from the full target index when building refs
// or generator metadata for review.
func TestReviewRefsEntitiesWithTargets_PeerSopsSecretOnlyInTargetsResolvesReference(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-api
spec:
  selector:
    matchLabels:
      app: consumer-api
  template:
    metadata:
      labels:
        app: consumer-api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: TOKEN
              valueFrom:
                secretKeyRef:
                  name: peer-generated
                  key: TOKEN
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-api": {"prod.platform.consumer"},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: peer-sops
spec:
  secretTemplates:
    - name: peer-generated
      stringData:
        TOKEN: "x"
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"peer-sops": {"prod.platform.peer"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		reviewRefsSopsSecretRefParsers(t),
	)
	assert.Empty(t, findings)
}

func reviewRefsGeneratorJobToSecretParser(t *testing.T) []types.RefParser {
	t.Helper()
	const yaml = `ref-parsers:
  - predicate: 'gvk == "batch/v1/Job" && name == "hydra-review-generator-test-job"'
    attributes:
      - "origin:generated": job
    pick:
      - cel: '[refBuilder().outgoing(id("v1/Secret", ns, "gen-secret"))]'
`
	parsers, err := references.ParseRefParsers([]byte(yaml))
	require.NoError(t, err)
	return parsers
}

func reviewRefsRecursiveProvisionedMirrorParsers() []types.RefParser {
	return []types.RefParser{
		{
			Selector: types.RefSelector{
				Group:     "isindir.github.com",
				Version:   "v1alpha3",
				Kind:      "SopsSecret",
				Namespace: "sops-secrets-operator",
				Name:      "image-pull-secret",
			},
			Pick: []types.RefPicker{{
				Cel:     types.CelExpression(`entity.spec.secretTemplates.map(t, refBuilder().outgoing(id("v1/Secret", ns, t.name)))`),
				Label:   "sops",
				Reverse: true,
			}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.cluster-infra.sops-secrets-operator"},
				{Type: types.RefAttributeOriginWorkload, Value: "sops-secrets-operator"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
		{
			Selector: types.RefSelector{
				Version:   "v1",
				Kind:      "Secret",
				Namespace: "sops-secrets-operator",
				Name:      "image-pull-secret",
			},
			Pick: []types.RefPicker{{
				Cel:   types.CelExpression(`[refBuilder().outgoing(id("v1/Secret", "demo", "image-pull-secret"))]`),
				Label: "clone-target",
			}},
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "prod.cluster-infra.kyverno"},
				{Type: types.RefAttributeOriginWorkload, Value: "kyverno-admission-controller"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}
}

// TestReviewRefsEntitiesWithTargets_IncomingGeneratorRefSuppressesMissingTarget documents that a
// missing Secret referenced from a selected source does not produce "missing target resource"
// when another ref (e.g. from a Job in the full target index) points to the same Secret with tag "generator".
func TestReviewRefsEntitiesWithTargets_IncomingGeneratorRefSuppressesMissingTarget(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-api
spec:
  selector:
    matchLabels:
      app: consumer-api
  template:
    metadata:
      labels:
        app: consumer-api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: TOKEN
              valueFrom:
                secretKeyRef:
                  name: gen-secret
                  key: TOKEN
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-api": {"prod.platform.consumer"},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: batch/v1
kind: Job
metadata:
  name: hydra-review-generator-test-job
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: gen
          image: busybox
          command: ["true"]
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"hydra-review-generator-test-job": {"prod.platform.peer-generator"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		reviewRefsGeneratorJobToSecretParser(t),
	)
	assert.Empty(t, findings)
}

// TestReviewRefsEntitiesWithTargets_LocalRecursiveProvisionedChainResolvesMissingSecret documents
// hydra local review with a multi-hop virtual chain:
// SopsSecret -> virtual Secret in sops-secrets-operator -> virtual mirrored Secret in demo.
func TestReviewRefsEntitiesWithTargets_LocalRecursiveProvisionedChainResolvesMissingSecret(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-api
spec:
  selector:
    matchLabels:
      app: consumer-api
  template:
    metadata:
      labels:
        app: consumer-api
    spec:
      imagePullSecrets:
        - name: image-pull-secret
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-api": {"prod.apps.consumer"},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: image-pull-secret
  namespace: sops-secrets-operator
spec:
  secretTemplates:
    - name: image-pull-secret
      stringData:
        .dockerconfigjson: "{}"
`, types.AppNamespace("sops-secrets-operator"), map[string][]types.AppId{
		"image-pull-secret": {"prod.cluster-infra.sops-secrets-operator"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(types.AppId("prod.apps.consumer")),
		reviewRefsRecursiveProvisionedMirrorParsers(),
	)
	assert.Empty(t, findings)
}

// filterSourceEntitiesToClusterExisting mirrors the source filtering that ReviewClusterRefsCallback
// performs: only source entities whose IDs also exist in the cluster entity set are kept.
func filterSourceEntitiesToClusterExisting(t *testing.T, sourceEntities entity.Entities, clusterEntities entity.Entities) entity.Entities {
	t.Helper()
	clusterIds, err := CollectEntityIds(clusterEntities)
	require.NoError(t, err)
	var filtered []entity.Entity
	for _, item := range sourceEntities.Items {
		id, err := item.Id()
		require.NoError(t, err)
		if clusterIds.Has(id) {
			filtered = append(filtered, item)
		}
	}
	result, err := entity.NewEntities(filtered)
	require.NoError(t, err)
	return result
}

// TestReviewRefsClusterReview_SourceNotOnClusterIsExcluded documents that a rendered source entity
// from a selected app that is NOT present on the live cluster is excluded from cluster review.
// Even if its refs point to missing targets, no findings are produced because the source itself
// does not exist on the cluster.
func TestReviewRefsClusterReview_SourceNotOnClusterIsExcluded(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-not-on-cluster
spec:
  selector:
    matchLabels:
      app: deploy-not-on-cluster
  template:
    metadata:
      labels:
        app: deploy-not-on-cluster
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: missing-secret
                  key: PASSWORD
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"deploy-not-on-cluster": {"prod.platform.consumer"},
	})

	// Cluster targets: empty — the Deployment does not exist on the live cluster.
	clusterTargets, err := entity.NewEntities(nil)
	require.NoError(t, err)

	// Filter sources to only those present on the cluster (simulates ReviewClusterRefsCallback).
	filteredSources := filterSourceEntitiesToClusterExisting(t, sourceEntities, clusterTargets)

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, filteredSources, clusterTargets, types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		nil,
	)
	assert.Empty(t, findings)
}

// TestReviewRefsClusterReview_SourceOnClusterReportsMissingRef documents that a rendered source
// entity from a selected app that IS present on the live cluster is reviewed. When its refs point
// to a target that does not exist in the cluster target set, a "missing target resource" finding
// is reported.
func TestReviewRefsClusterReview_SourceOnClusterReportsMissingRef(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-on-cluster
spec:
  selector:
    matchLabels:
      app: deploy-on-cluster
  template:
    metadata:
      labels:
        app: deploy-on-cluster
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: missing-secret
                  key: PASSWORD
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"deploy-on-cluster": {"prod.platform.consumer"},
	})

	// Cluster targets: the Deployment exists on the live cluster but the Secret does not.
	clusterTargets := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-on-cluster
  namespace: demo
spec:
  selector:
    matchLabels:
      app: deploy-on-cluster
  template:
    metadata:
      labels:
        app: deploy-on-cluster
    spec:
      containers:
        - name: api
          image: nginx:1.27
`, types.KeyClusterEntity, nil)

	filteredSources := filterSourceEntitiesToClusterExisting(t, sourceEntities, clusterTargets)

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, filteredSources, clusterTargets, types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		nil,
	)
	require.Len(t, findings, 1)
	assert.Equal(t, types.Id("v1/Secret/demo/missing-secret"), findings[0].Target)
	assert.Equal(t, missingTargetResourceFinding, findings[0].Message)
	assert.ElementsMatch(t, []types.Id{"apps/v1/Deployment/demo/deploy-on-cluster"}, findings[0].Sources)
}

// TestReviewRefsClusterReview_CloneMaterializationsInSourceCandidateSet documents that clone
// materializations are included in the source candidate set before filtering against the live
// cluster. Only clone-materialized sources whose IDs also exist on the live cluster produce
// findings; those absent from the cluster are excluded.
func TestReviewRefsClusterReview_CloneMaterializationsInSourceCandidateSet(t *testing.T) {
	configureReviewRefsTestLogging()

	// Two clone-materialized Deployments with template key, each referencing a missing Secret.
	cloneSources := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: clone-on-cluster
spec:
  selector:
    matchLabels:
      app: clone-on-cluster
  template:
    metadata:
      labels:
        app: clone-on-cluster
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: TOKEN
              valueFrom:
                secretKeyRef:
                  name: missing-clone-secret
                  key: TOKEN
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: clone-not-on-cluster
spec:
  selector:
    matchLabels:
      app: clone-not-on-cluster
  template:
    metadata:
      labels:
        app: clone-not-on-cluster
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: TOKEN
              valueFrom:
                secretKeyRef:
                  name: missing-clone-secret
                  key: TOKEN
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"clone-on-cluster":     {"prod.platform.clone-app"},
		"clone-not-on-cluster": {"prod.platform.clone-app"},
	})

	// Only clone-on-cluster exists on the live cluster.
	clusterTargets := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: clone-on-cluster
  namespace: demo
spec:
  selector:
    matchLabels:
      app: clone-on-cluster
  template:
    metadata:
      labels:
        app: clone-on-cluster
    spec:
      containers:
        - name: api
          image: nginx:1.27
`, types.KeyClusterEntity, nil)

	filteredSources := filterSourceEntitiesToClusterExisting(t, cloneSources, clusterTargets)

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, filteredSources, clusterTargets, types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.clone-app")),
		nil,
	)
	require.Len(t, findings, 1)
	assert.Equal(t, types.Id("v1/Secret/demo/missing-clone-secret"), findings[0].Target)
	assert.Equal(t, missingTargetResourceFinding, findings[0].Message)
	assert.ElementsMatch(t, []types.Id{"apps/v1/Deployment/demo/clone-on-cluster"}, findings[0].Sources)
}

// TestReviewRefsClusterReview_TargetsExcludeCloneMaterializations documents that the cluster
// review target set consists only of live cluster entities (KeyClusterEntity) and does NOT include
// clone materializations. A Secret that would exist as a clone materialization but is absent from
// the live cluster still produces a "missing target resource" finding.
func TestReviewRefsClusterReview_TargetsExcludeCloneMaterializations(t *testing.T) {
	configureReviewRefsTestLogging()

	// Source: a Deployment referencing a Secret. The Deployment exists on the cluster so it IS reviewed.
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-needs-clone-secret
spec:
  selector:
    matchLabels:
      app: consumer-needs-clone-secret
  template:
    metadata:
      labels:
        app: consumer-needs-clone-secret
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: CREDENTIAL
              valueFrom:
                secretKeyRef:
                  name: clone-only-secret
                  key: CREDENTIAL
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-needs-clone-secret": {"prod.platform.consumer"},
	})

	// Cluster targets: the Deployment exists on the cluster but the Secret does NOT.
	// The Secret only exists as a clone materialization (template key), which cluster review
	// must not include in targets.
	clusterTargets := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-needs-clone-secret
  namespace: demo
spec:
  selector:
    matchLabels:
      app: consumer-needs-clone-secret
  template:
    metadata:
      labels:
        app: consumer-needs-clone-secret
    spec:
      containers:
        - name: api
          image: nginx:1.27
`, types.KeyClusterEntity, nil)

	filteredSources := filterSourceEntitiesToClusterExisting(t, sourceEntities, clusterTargets)

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, filteredSources, clusterTargets, types.KeyClusterEntity,
		sets.New(types.AppId("prod.platform.consumer")),
		nil,
	)
	require.Len(t, findings, 1)
	assert.Equal(t, types.Id("v1/Secret/demo/clone-only-secret"), findings[0].Target)
	assert.Equal(t, missingTargetResourceFinding, findings[0].Message)
	assert.ElementsMatch(t, []types.Id{"apps/v1/Deployment/demo/consumer-needs-clone-secret"}, findings[0].Sources)
}

// TestReviewRefsEntitiesWithTargets_ClusterRecursiveProvisionedChainResolvesMissingSecret documents
// hydra gitops review with the same multi-hop virtual chain against live target entities.
func TestReviewRefsEntitiesWithTargets_ClusterRecursiveProvisionedChainResolvesMissingSecret(t *testing.T) {
	configureReviewRefsTestLogging()

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer-api
spec:
  selector:
    matchLabels:
      app: consumer-api
  template:
    metadata:
      labels:
        app: consumer-api
    spec:
      imagePullSecrets:
        - name: image-pull-secret
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"consumer-api": {"prod.apps.consumer"},
	})

	targetEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: image-pull-secret
  namespace: sops-secrets-operator
spec:
  secretTemplates:
    - name: image-pull-secret
      stringData:
        .dockerconfigjson: "{}"
`, types.KeyClusterEntity, map[string][]types.AppId{
		"image-pull-secret": {"prod.cluster-infra.sops-secrets-operator"},
	})

	findings := reviewRefsCollectWithTargetsAndParsers(
		t, sourceEntities, targetEntities, types.KeyClusterEntity,
		sets.New(types.AppId("prod.apps.consumer")),
		reviewRefsRecursiveProvisionedMirrorParsers(),
	)
	assert.Empty(t, findings)
}

func reviewRefsCollectWithBuiltinOpts(
	t *testing.T,
	sourceEntities entity.Entities,
	targetEntities entity.Entities,
	targetKey types.EntityKeyUnstructured,
	selectedAppIds sets.Set[types.AppId],
	sourceExtra []types.RefParser,
	targetExtra []types.RefParser,
	preferredVersions map[types.GroupKindKey]types.Version,
	sourceClusterInventoryOverlay entity.Entities,
	builtin *ReviewRefsBuiltinOptions,
) []ReviewFinding {
	t.Helper()
	var findings []ReviewFinding
	_, _, err := reviewRefsEntitiesWithTargetsAndParserSetsCallback(
		hlog.Default(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		targetKey,
		selectedAppIds,
		func(f ReviewFinding) error {
			findings = append(findings, f)
			return nil
		},
		nil,
		entity.Entities{},
		sourceClusterInventoryOverlay,
		sourceExtra,
		targetExtra,
		preferredVersions,
		builtin,
		nil,
	)
	require.NoError(t, err)
	return findings
}

// TestReviewRefs_KubernetesBuiltinMerge_ClusterRoleBindingToView documents local review: bootstrap
// ClusterRole targets are merged into the template target set so bindings to built-in roles do not
// produce missing target findings.
func TestReviewRefs_KubernetesBuiltinMerge_ClusterRoleBindingToView(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.reviewer")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: app-view
subjects:
  - kind: ServiceAccount
    name: sa1
    namespace: demo
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"app-view": {appID},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
data: {}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sa1
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"other": {types.AppId("prod.demo.other")},
		"sa1":   {appID},
	})

	findingsWithout := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{}, nil)
	require.Len(t, findingsWithout, 1)
	assert.Equal(t, kubernetesBuiltinClusterRoleID("view"), findingsWithout[0].Target)
	assert.Equal(t, missingTargetResourceFinding, findingsWithout[0].Message)

	findingsWith := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{
			MergeBuiltinsIntoTemplateTargets: true,
			KubernetesMinor:                  99,
		})
	assert.Empty(t, findingsWith)
}

// TestReviewRefs_KubernetesBuiltinMerge_RoleBindingToExtensionApiserverAuthReader documents local
// review: kube-system bootstrap Roles from bootstrappolicy/namespace_policy.go are merged so
// RoleBindings to extension-apiserver-authentication-reader do not false-positive.
func TestReviewRefs_KubernetesBuiltinMerge_RoleBindingToExtensionApiserverAuthReader(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.reviewer")
	extAuthReaderID := types.Id("rbac.authorization.k8s.io/v1/Role/kube-system/extension-apiserver-authentication-reader")

	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: webhook-authentication-reader
  namespace: kube-system
subjects:
  - kind: ServiceAccount
    name: webhook-sa
    namespace: demo
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: extension-apiserver-authentication-reader
`, types.AppNamespace("kube-system"), map[string][]types.AppId{
		"webhook-authentication-reader": {appID},
	})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
  namespace: demo
data: {}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: webhook-sa
  namespace: demo
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"other":      {types.AppId("prod.demo.other")},
		"webhook-sa": {appID},
	})

	findingsWithout := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{}, nil)
	require.Len(t, findingsWithout, 1)
	assert.Equal(t, extAuthReaderID, findingsWithout[0].Target)
	assert.Equal(t, missingTargetResourceFinding, findingsWithout[0].Message)

	findingsWith := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{
			MergeBuiltinsIntoTemplateTargets: true,
			KubernetesMinor:                  99,
		})
	assert.Empty(t, findingsWith)
}

// TestReviewRefs_ClusterBootstrapAudit_SuppressesMissingTargetForBuiltin documents cluster review:
// a missing bootstrap ClusterRole is reported as missing cluster default resource, not missing target resource.
func TestReviewRefs_ClusterBootstrapAudit_SuppressesMissingTargetForBuiltin(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.reviewer")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: app-view
subjects:
  - kind: ServiceAccount
    name: sa1
    namespace: demo
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"app-view": {appID},
	})

	targetClusterEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sa1
  namespace: demo
`, types.KeyClusterEntity, map[string][]types.AppId{
		"sa1": {appID},
	})

	viewID := kubernetesBuiltinClusterRoleID("view")
	liveIds, err := CollectEntityIds(targetClusterEntities)
	require.NoError(t, err)
	for _, id := range KubernetesBuiltinExpectedIDSet(99, nil).UnsortedList() {
		if id != viewID {
			liveIds.Insert(id)
		}
	}

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetClusterEntities, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetClusterEntities,
		&ReviewRefsBuiltinOptions{
			AuditClusterBootstrapMissing: true,
			LiveClusterIds:               liveIds,
			KubernetesMinor:              99,
		})

	var viewFindings []ReviewFinding
	for _, f := range findings {
		if f.Target == viewID {
			viewFindings = append(viewFindings, f)
		}
	}
	require.Len(t, viewFindings, 1)
	assert.Equal(t, missingClusterDefaultResourceFinding, viewFindings[0].Message)
	assert.Empty(t, viewFindings[0].Sources)
	for _, f := range findings {
		if f.Target == viewID {
			continue
		}
		assert.NotEqual(t, missingTargetResourceFinding, f.Message,
			"bootstrap targets should not use missing target resource: %v", f)
	}
}

// TestReviewRefs_ClusterBootstrapBlanketAuditOmittedWithoutAuditFlag documents that the proactive
// scan for every missing catalog ID (empty sources) runs only when AuditClusterBootstrapMissing is
// set — matching `hydra gitops review cluster`, not `hydra gitops review app`.
func TestReviewRefs_ClusterBootstrapBlanketAuditOmittedWithoutAuditFlag(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.reviewer")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: app-view
subjects:
  - kind: ServiceAccount
    name: sa1
    namespace: demo
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"app-view": {appID},
	})

	targetClusterEntities := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sa1
  namespace: demo
`, types.KeyClusterEntity, map[string][]types.AppId{
		"sa1": {appID},
	})

	viewID := kubernetesBuiltinClusterRoleID("view")
	liveIds, err := CollectEntityIds(targetClusterEntities)
	require.NoError(t, err)
	for _, id := range KubernetesBuiltinExpectedIDSet(99, nil).UnsortedList() {
		if id != viewID {
			liveIds.Insert(id)
		}
	}

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetClusterEntities, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetClusterEntities,
		&ReviewRefsBuiltinOptions{
			AuditClusterBootstrapMissing: false,
			LiveClusterIds:               liveIds,
			KubernetesMinor:              99,
		})

	for _, f := range findings {
		assert.NotEqual(t, missingClusterDefaultResourceFinding, f.Message,
			"blanket bootstrap audit must be off when AuditClusterBootstrapMissing is false: %v", f)
	}
}

// reviewRefsTestAuxiliaryNamespaceDefaults builds synthetic kubernetes-defaults targets for
// namespaces present in a minimal template render (single ConfigMap) under appNs.
func reviewRefsTestAuxiliaryNamespaceDefaults(t *testing.T, appNs types.AppNamespace) entity.Entities {
	t.Helper()
	rendered := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: ns-anchor
data: {}
`, appNs, map[string][]types.AppId{
		"ns-anchor": {types.AppId("prod.test.ns-anchor")},
	})
	aux, err := BuildSyntheticNamespaceDefaultTargets(hlog.Default(), rendered, types.KeyClusterEntity)
	require.NoError(t, err)
	return aux
}

func TestReviewRefs_LocalNamespaceDefault_ServiceAccountMissingWithoutMerge(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
data: {}
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"other": {types.AppId("prod.demo.other")},
	})

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{}, nil)
	var sawMissingDefaultSA bool
	for _, f := range findings {
		if f.Message == missingTargetResourceFinding && f.Target == types.Id("v1/ServiceAccount/demo/default") {
			sawMissingDefaultSA = true
		}
	}
	assert.True(t, sawMissingDefaultSA)
}

func TestReviewRefs_LocalNamespaceDefault_ServiceAccountResolvedWithBuiltinMerge(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
data: {}
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"other": {types.AppId("prod.demo.other")},
	})

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{
			MergeBuiltinsIntoTemplateTargets: true,
			KubernetesMinor:                  99,
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ServiceAccount/demo/default"), f.Target,
			"unexpected finding: %+v", f)
	}
}

func TestReviewRefs_LocalNamespaceDefault_ServiceAccountExplicitInChartNoDuplicateFindings(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	yaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: default
`
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, yaml, types.AppNamespace("demo"), map[string][]types.AppId{
		"api":     {appID},
		"default": {appID},
	})
	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, yaml, types.AppNamespace("demo"), map[string][]types.AppId{
		"api":     {appID},
		"default": {appID},
	})

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{
			MergeBuiltinsIntoTemplateTargets: true,
			KubernetesMinor:                  99,
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ServiceAccount/demo/default"), f.Target,
			"unexpected finding: %+v", f)
	}
}

func TestReviewRefs_LocalNamespaceDefault_CrossNamespaceServiceAccountSubjectStillMissing(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: bind
subjects:
  - kind: ServiceAccount
    name: default
    namespace: other-ns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: demo-role
`, types.AppNamespace("demo"), map[string][]types.AppId{"bind": {appID}})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: demo-role
rules: []
`, types.AppNamespace("demo"), map[string][]types.AppId{"demo-role": {appID}})

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{
			MergeBuiltinsIntoTemplateTargets: true,
			KubernetesMinor:                  99,
		})
	var sawMissingOtherNsSA bool
	for _, f := range findings {
		if f.Message == missingTargetResourceFinding && f.Target == types.Id("v1/ServiceAccount/other-ns/default") {
			sawMissingOtherNsSA = true
		}
	}
	assert.True(t, sawMissingOtherNsSA)
}

func TestReviewRefs_LocalNamespaceDefault_KubeRootCAKeySatisfiedBySynthetic(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PEM
              valueFrom:
                configMapKeyRef:
                  name: kube-root-ca.crt
                  key: ca.crt
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
data: {}
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"other": {types.AppId("prod.demo.other")},
	})

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{
			MergeBuiltinsIntoTemplateTargets: true,
			KubernetesMinor:                  99,
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ConfigMap/demo/kube-root-ca.crt"), f.Target,
			"unexpected finding: %+v", f)
	}
}

func TestReviewRefs_LocalNamespaceDefault_KubeRootCAMissingNonStandardKey(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: X
              valueFrom:
                configMapKeyRef:
                  name: kube-root-ca.crt
                  key: not-present
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
data: {}
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"other": {types.AppId("prod.demo.other")},
	})

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetEntities, types.KeyTemplateEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{
			MergeBuiltinsIntoTemplateTargets: true,
			KubernetesMinor:                  99,
		})
	var sawKeyFinding bool
	for _, f := range findings {
		if f.Target == types.Id("v1/ConfigMap/demo/kube-root-ca.crt") &&
			f.Message == `missing referenced key "not-present"` {
			sawKeyFinding = true
		}
	}
	assert.True(t, sawKeyFinding)
}

func TestReviewRefs_ClusterNamespaceDefault_ServiceAccountFromAuxiliaryOnly(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.KeyClusterEntity, map[string][]types.AppId{"api": {appID}})

	aux := reviewRefsTestAuxiliaryNamespaceDefaults(t, types.AppNamespace("demo"))
	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetCluster,
		&ReviewRefsBuiltinOptions{
			AuxiliarySyntheticTargetEntities: aux,
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ServiceAccount/demo/default"), f.Target,
			"unexpected finding: %+v", f)
	}
}

func TestReviewRefs_ClusterNamespaceDefault_ServiceAccountFromLiveOnly(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: default
  namespace: demo
`, types.KeyClusterEntity, map[string][]types.AppId{
		"api":     {appID},
		"default": {appID},
	})

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetCluster,
		&ReviewRefsBuiltinOptions{
			AuxiliarySyntheticTargetEntities: entity.Entities{},
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ServiceAccount/demo/default"), f.Target,
			"unexpected finding: %+v", f)
	}
}

// TestReviewRefs_ClusterReview_TemplateVsClusterDrift_ServiceAccountImagePullSecret documents cluster
// review: template ServiceAccount declares imagePullSecrets; the live object does not — drift reports
// the missing ref edge; live-only missing-target validation runs only on the template∩cluster ref set.
func TestReviewRefs_ClusterReview_TemplateVsClusterDrift_ServiceAccountImagePullSecret(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: default
  namespace: demo
imagePullSecrets:
  - name: regcred
`, types.AppNamespace("demo"), map[string][]types.AppId{
		"default": {appID},
	})

	liveCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: default
  namespace: demo
`, types.KeyClusterEntity, map[string][]types.AppId{
		"default": {appID},
	})

	findingsWithOverlay := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, liveCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, liveCluster,
		&ReviewRefsBuiltinOptions{})
	var sawDrift bool
	for _, f := range findingsWithOverlay {
		if f.Target == types.Id("v1/Secret/demo/regcred") && f.Message == refDriftMissingOnClusterFinding {
			sawDrift = true
		}
	}
	assert.True(t, sawDrift, "expected template-vs-cluster drift for imagePullSecret ref: %+v", findingsWithOverlay)

	findingsNoOverlay := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, liveCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, entity.Entities{},
		&ReviewRefsBuiltinOptions{})
	var sawMissingPullSecret bool
	for _, f := range findingsNoOverlay {
		if f.Target == types.Id("v1/Secret/demo/regcred") && f.Message == missingTargetResourceFinding {
			sawMissingPullSecret = true
		}
	}
	assert.True(t, sawMissingPullSecret, "without dual template/cluster compare, missing target for secret: %+v", findingsNoOverlay)
}

func TestReviewRefs_ClusterNamespaceDefault_ServiceAccountLivePlusAuxiliary(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: default
  namespace: demo
`, types.KeyClusterEntity, map[string][]types.AppId{
		"api":     {appID},
		"default": {appID},
	})

	aux := reviewRefsTestAuxiliaryNamespaceDefaults(t, types.AppNamespace("demo"))
	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetCluster,
		&ReviewRefsBuiltinOptions{
			AuxiliarySyntheticTargetEntities: aux,
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ServiceAccount/demo/default"), f.Target,
			"unexpected finding: %+v", f)
	}
}

func TestReviewRefs_ClusterNamespaceDefault_ServiceAccountMissingWhenAuxiliaryWrongNamespace(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
`, types.KeyClusterEntity, map[string][]types.AppId{"api": {appID}})

	aux := reviewRefsTestAuxiliaryNamespaceDefaults(t, types.AppNamespace("other"))
	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetCluster,
		&ReviewRefsBuiltinOptions{
			AuxiliarySyntheticTargetEntities: aux,
		})
	var sawMissing bool
	for _, f := range findings {
		if f.Target != types.Id("v1/ServiceAccount/demo/default") {
			continue
		}
		if f.Message == missingTargetResourceFinding || f.Message == refDriftMissingOnClusterFinding {
			sawMissing = true
		}
	}
	assert.True(t, sawMissing)
}

func TestReviewRefs_ClusterNamespaceDefault_KubeRootCAFromAuxiliary(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PEM
              valueFrom:
                configMapKeyRef:
                  name: kube-root-ca.crt
                  key: ca.crt
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: PEM
              valueFrom:
                configMapKeyRef:
                  name: kube-root-ca.crt
                  key: ca.crt
`, types.KeyClusterEntity, map[string][]types.AppId{"api": {appID}})

	aux := reviewRefsTestAuxiliaryNamespaceDefaults(t, types.AppNamespace("demo"))
	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetCluster,
		&ReviewRefsBuiltinOptions{
			AuxiliarySyntheticTargetEntities: aux,
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ConfigMap/demo/kube-root-ca.crt"), f.Target,
			"unexpected finding: %+v", f)
	}
}

func TestReviewRefs_ClusterNamespaceDefault_CrossNamespaceRoleBindingNotHealedByAuxiliary(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: bind
  namespace: demo
subjects:
  - kind: ServiceAccount
    name: default
    namespace: other-ns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: demo-role
`, types.AppNamespace("demo"), map[string][]types.AppId{"bind": {appID}})

	targetCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: demo-role
  namespace: demo
rules: []
`, types.KeyClusterEntity, map[string][]types.AppId{"demo-role": {appID}})

	renderedOther := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: x
  namespace: other-ns
data: {}
`, types.AppNamespace("other-ns"), map[string][]types.AppId{
		"x": {types.AppId("prod.other.app")},
	})
	aux, err := BuildSyntheticNamespaceDefaultTargets(hlog.Default(), renderedOther, types.KeyClusterEntity)
	require.NoError(t, err)

	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetCluster,
		&ReviewRefsBuiltinOptions{
			AuxiliarySyntheticTargetEntities: aux,
		})
	var sawMissing bool
	for _, f := range findings {
		if f.Message == missingTargetResourceFinding && f.Target == types.Id("v1/ServiceAccount/other-ns/default") {
			sawMissing = true
		}
	}
	assert.True(t, sawMissing)
}

func TestReviewRefs_ClusterNamespaceDefault_AuxiliaryWithoutBootstrapAudit(t *testing.T) {
	configureReviewRefsTestLogging()

	appID := types.AppId("prod.demo.app")
	sourceEntities := reviewRefsTestRenderedSelectedAppEntities(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.AppNamespace("demo"), map[string][]types.AppId{"api": {appID}})

	targetCluster := reviewRefsTestEntitiesWithKey(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: demo
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      serviceAccountName: default
      containers:
        - name: api
          image: nginx:1.27
`, types.KeyClusterEntity, map[string][]types.AppId{"api": {appID}})

	aux := reviewRefsTestAuxiliaryNamespaceDefaults(t, types.AppNamespace("demo"))
	findings := reviewRefsCollectWithBuiltinOpts(
		t, sourceEntities, targetCluster, types.KeyClusterEntity,
		sets.New(appID), nil, nil, nil, targetCluster,
		&ReviewRefsBuiltinOptions{
			AuxiliarySyntheticTargetEntities: aux,
		})
	for _, f := range findings {
		assert.NotEqual(t, types.Id("v1/ServiceAccount/demo/default"), f.Target,
			"unexpected finding: %+v", f)
	}
}
