package hydra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestDefaultPredicateContextsByTag_UninstallSafeIncludesPodMetrics(t *testing.T) {
	t.Parallel()

	contexts, err := defaultPredicateContextsByTag("uninstall-safe")
	require.NoError(t, err)
	require.NotEmpty(t, contexts)

	matched := false
	for _, ctx := range contexts {
		if strings.Contains(ctx.Predicate, "metrics.k8s.io/v1beta1/PodMetrics") {
			matched = true
			break
		}
	}
	assert.True(t, matched, "embedded uninstall-safe predicates should include PodMetrics")
}

func TestDefaultRefOwnershipPredicateLinePriorityBands_UninstallSafeIncludesPodMetrics(t *testing.T) {
	t.Parallel()

	_, negativePriority := defaultRefOwnershipPredicateLinePriorityBands(true)
	require.NotEmpty(t, negativePriority)

	matched := false
	for _, line := range negativePriority {
		if strings.Contains(line.Cel, "metrics.k8s.io/v1beta1/PodMetrics") {
			matched = true
			assert.Contains(t, line.Cel, `templateEntity != null`)
			break
		}
	}
	assert.True(t, matched, "embedded negative-priority ownership predicates should include PodMetrics")
}

func TestDefaultRefOwnershipPredicateLinePriorityBands_PickLinesKeepParserScope(t *testing.T) {
	t.Parallel()

	_, negativePriority := defaultRefOwnershipPredicateLinePriorityBands(true)
	require.NotEmpty(t, negativePriority)

	matched := false
	for _, line := range negativePriority {
		if !strings.Contains(line.Cel, `id('metrics.k8s.io/v1beta1/PodMetrics', ns, name)`) {
			continue
		}
		matched = true
		assert.Contains(t, line.Cel, `templateEntity != null`)
		assert.Contains(t, line.Cel, `(version == "v1" && kind == "Pod")`)
		assert.Contains(t, line.Cel, `.filter(ref, ref.hasEndpoint(string(id)))`)
	}
	assert.True(t, matched, "pod ownership pick lines should retain the source parser scope")
}

func TestHydraAppUninstallSafePredicates_UsesCentralNegativePriorityOwnershipLines(t *testing.T) {
	t.Parallel()

	rendered := entity.Entities{}
	appIds := sets.New(types.AppId("in-cluster.cluster-infra.cert-manager"))

	predicates, err := HydraAppUninstallSafePredicates(nil, appIds, types.HelmNetworkModeOffline, rendered)
	require.NoError(t, err)
	require.NotEmpty(t, predicates)

	_, negativePriority, err := HydraAppRefOwnershipUninstallPredicateLinePriorityBands(nil, appIds, types.HelmNetworkModeOffline, rendered, true)
	require.NoError(t, err)
	require.NotEmpty(t, negativePriority[types.AppId("in-cluster.cluster-infra.cert-manager")])

	matched := false
	for _, predicate := range predicates {
		if strings.Contains(predicate, "metrics.k8s.io/v1beta1/PodMetrics") {
			matched = true
			break
		}
	}
	assert.True(t, matched, "uninstall-safe predicates should be derived from the central negative-priority ownership model")
	assert.Subset(t, predicates, refOwnershipLinesToCelStrings(negativePriority[types.AppId("in-cluster.cluster-infra.cert-manager")]))
}

func TestRefOwnershipPredicatesFromHydraValues_UninstallUninstallForceAndBackupTags(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()
	falsePtr := func() *bool { b := false; return &b }()

	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"uninstallGroup": {
				Enabled: truePtr,
				Tag:     []string{"uninstall"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "Secret"`},
				},
			},
			"forceGroup": {
				Enabled: truePtr,
				Tag:     []string{"uninstall-force"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "PersistentVolumeClaim"`},
				},
			},
			"bothTags": {
				Enabled: truePtr,
				Tag:     []string{"uninstall", "uninstall-force"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "ConfigMap"`},
				},
			},
			"safeOnly": {
				Enabled: truePtr,
				Tag:     []string{"uninstall-safe"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "UninstallSafeOnlyRef"`},
				},
			},
			"backupOnly": {
				Enabled: truePtr,
				Tag:     []string{"backup"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `true`},
				},
			},
			"disabledUninstall": {
				Enabled: falsePtr,
				Tag:     []string{"uninstall"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `true`},
				},
			},
			"emptyPredicate": {
				Enabled: truePtr,
				Tag:     []string{"uninstall"},
				RefParsers: []types.HydraRefParser{
					{Predicate: "  "},
				},
			},
		},
	}

	out := refOwnershipPredicatesFromHydraValues(hv, true)
	assert.Len(t, out, 5)
	assert.Contains(t, out, `kind == "Secret"`)
	assert.Contains(t, out, `kind == "PersistentVolumeClaim"`)
	assert.Contains(t, out, `kind == "ConfigMap"`)
	assert.Contains(t, out, `kind == "UninstallSafeOnlyRef"`)
	assert.Contains(t, out, `true`)
}

func TestRefOwnershipPredicatesFromCloneRules_MirrorImagePullSecret(t *testing.T) {
	t.Parallel()

	hv := &types.HydraValues{
		Clones: map[string]types.HydraCloneRule{
			"image-pull-secret-mirror": {
				Desc:      "mirror to managed namespaces",
				Tag:       "bootstrap",
				Predicate: `id == "v1/Secret/sops-secrets-operator/image-pull-secret"`,
				Targets:   types.HydraCloneTargets{CEL: "managedNamespaces()"},
				Exclude:   []string{"sops-secrets-operator", "kube-public", "kube-node-lease"},
			},
		},
	}

	out := refOwnershipPredicatesFromCloneRules(hv)
	require.Len(t, out, 1)
	assert.Equal(t,
		`gvk == "v1/Secret" && name == "image-pull-secret" && string(ns) != "kube-node-lease" && string(ns) != "kube-public" && string(ns) != "sops-secrets-operator" && size(managedNamespaces().filter(n, n == string(ns))) > 0`,
		out[0])
}

func TestRefOwnershipPredicatesFromCloneRulesSplit_BootstrapClonesAreStrong(t *testing.T) {
	t.Parallel()

	hv := &types.HydraValues{
		Clones: map[string]types.HydraCloneRule{
			"image-pull-secret-mirror": {
				Tag:       "bootstrap",
				Predicate: `id == "v1/Secret/sops-secrets-operator/image-pull-secret"`,
				Targets:   types.HydraCloneTargets{CEL: "managedNamespaces()"},
				Exclude:   []string{"sops-secrets-operator"},
			},
			"other-mirror": {
				Tag:       "",
				Predicate: `id == "v1/Secret/default/other"`,
				Targets:   types.HydraCloneTargets{CEL: "managedNamespaces()"},
			},
		},
	}

	nonNegativePriority, negativePriority := refOwnershipPredicatesFromCloneRulesPriorityBands(hv)
	require.Len(t, negativePriority, 0)
	require.Len(t, nonNegativePriority, 2)
	assert.Contains(t, nonNegativePriority[0]+nonNegativePriority[1], "image-pull-secret")
	assert.Contains(t, nonNegativePriority[0]+nonNegativePriority[1], "other")
	combined := refOwnershipPredicatesFromCloneRules(hv)
	assert.Len(t, combined, 2)
}

// HydraAppUninstallForcePredicateLines appends the same CEL strings as refOwnershipPredicatesFromCloneRules
// for each app (see hydra_values.go) so clone targets participate in force-leftover classification.
func TestUninstallForcePredicateLineAppendSourceMatchesCloneOwnership(t *testing.T) {
	t.Parallel()
	hv := &types.HydraValues{
		Clones: map[string]types.HydraCloneRule{
			"image-pull-secret-mirror": {
				Tag:       "bootstrap",
				Predicate: `id == "v1/Secret/sops-secrets-operator/image-pull-secret"`,
				Targets:   types.HydraCloneTargets{CEL: "managedNamespaces()"},
				Exclude:   []string{"sops-secrets-operator", "kube-public", "kube-node-lease"},
			},
		},
	}
	fromClones := refOwnershipPredicatesFromCloneRules(hv)
	require.Len(t, fromClones, 1)
	assert.Contains(t, fromClones[0], `name == "image-pull-secret"`)
	assert.Contains(t, fromClones[0], `managedNamespaces()`)
}

func TestCloneOwnershipPredicatesFromHydraValuesMatchesRefOwnershipPredicatesFromCloneRules(t *testing.T) {
	t.Parallel()
	hv := &types.HydraValues{
		Clones: map[string]types.HydraCloneRule{
			"mirror": {
				Tag:       "bootstrap",
				Predicate: `id == "v1/Secret/sops-secrets-operator/image-pull-secret"`,
				Targets:   types.HydraCloneTargets{CEL: "managedNamespaces()"},
				Exclude:   []string{"sops-secrets-operator"},
			},
		},
	}
	require.Equal(t, refOwnershipPredicatesFromCloneRules(hv), CloneOwnershipPredicatesFromHydraValues(hv))
}

func TestRefOwnershipPredicatesFromHydraValuesPriorityBands(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()

	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"safeOnly": {
				Enabled: truePtr,
				Tag:     []string{"uninstall-safe"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "UninstallSafeOnlyRef"`},
				},
			},
			"uninstallAlso": {
				Enabled: truePtr,
				Tag:     []string{"uninstall", "uninstall-safe"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "BothTagsRef"`},
				},
			},
		},
	}

	negativePriority := refOwnershipPredicatesFromHydraValuesNegativePriority(hv, true)
	assert.Len(t, negativePriority, 1)
	assert.Contains(t, negativePriority[0], `UninstallSafeOnlyRef`)

	nonNegativePriority := refOwnershipPredicatesFromHydraValuesNonNegativePriority(hv, true)
	assert.Len(t, nonNegativePriority, 1)
	assert.Contains(t, nonNegativePriority[0], `BothTagsRef`)
}

func TestRefOwnershipPredicateLinePriorityBands_NegativePriorityMovesDirectOwnershipToNegativeBand(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()
	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"argocd-resources": {
				Enabled: truePtr,
				Tag:     []string{"uninstall", "runtime"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `group == "argoproj.io"`, Priority: -1},
					{Predicate: `id == "v1/Secret/argocd/argocd-redis"`},
				},
			},
		},
	}

	nonNegativePriority := refOwnershipLinesFromHydraValuesNonNegativePriority(hv, true, nil)
	negativePriority := refOwnershipLinesFromHydraValuesNegativePriority(hv, true, nil)

	require.Len(t, nonNegativePriority, 1)
	assert.Equal(t, `id == "v1/Secret/argocd/argocd-redis"`, nonNegativePriority[0].Cel)
	assert.Equal(t, 0, nonNegativePriority[0].Priority)

	require.Len(t, negativePriority, 1)
	assert.Equal(t, `group == "argoproj.io"`, negativePriority[0].Cel)
	assert.Equal(t, -1, negativePriority[0].Priority)
}

func TestRefOwnershipPredicateLinePriorityBands_ExplicitNegativePriorityMovesDirectOwnershipToNegativeBand(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()
	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"kyverno-events": {
				Enabled: truePtr,
				Tag:     []string{"uninstall"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `gvk == "kyverno.io/v1/ClusterPolicy"`, Priority: -1},
					{Predicate: `id == "kyverno.io/v1/ClusterPolicy//clone-image-pull-secret"`},
				},
			},
		},
	}

	nonNegativePriority := refOwnershipLinesFromHydraValuesNonNegativePriority(hv, true, nil)
	negativePriority := refOwnershipLinesFromHydraValuesNegativePriority(hv, true, nil)

	require.Len(t, nonNegativePriority, 1)
	assert.Equal(t, `id == "kyverno.io/v1/ClusterPolicy//clone-image-pull-secret"`, nonNegativePriority[0].Cel)
	assert.Equal(t, 0, nonNegativePriority[0].Priority)

	require.Len(t, negativePriority, 1)
	assert.Equal(t, `gvk == "kyverno.io/v1/ClusterPolicy"`, negativePriority[0].Cel)
	assert.Equal(t, -1, negativePriority[0].Priority)
}

func TestRefOwnershipPredicateLinePriorityBands_GroupNegativePriorityMovesDirectOwnershipToNegativeBand(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()
	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"kyverno-image-pull-secret": {
				Enabled:  truePtr,
				Tag:      []string{"uninstall-safe"},
				Priority: -2,
				RefParsers: []types.HydraRefParser{
					{Predicate: `gvk == "v1/Secret" && name == "image-pull-secret"`},
				},
			},
		},
	}

	nonNegativePriority := refOwnershipLinesFromHydraValuesNonNegativePriority(hv, true, nil)
	negativePriority := refOwnershipLinesFromHydraValuesNegativePriority(hv, true, nil)

	require.Len(t, nonNegativePriority, 0)
	require.Len(t, negativePriority, 1)
	assert.Equal(t, `gvk == "v1/Secret" && name == "image-pull-secret"`, negativePriority[0].Cel)
	assert.Equal(t, -2, negativePriority[0].Priority)
}

func TestRefOwnershipPredicateLinePriorityBands_ParserPriorityOverridesGroupPriority(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()
	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"mixed-priority": {
				Enabled:  truePtr,
				Tag:      []string{"uninstall"},
				Priority: -2,
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "Secret"`},
					{Predicate: `kind == "ConfigMap"`, Priority: 1},
				},
			},
		},
	}

	nonNegativePriority := refOwnershipLinesFromHydraValuesNonNegativePriority(hv, true, nil)
	negativePriority := refOwnershipLinesFromHydraValuesNegativePriority(hv, true, nil)

	require.Len(t, nonNegativePriority, 1)
	assert.Equal(t, `kind == "ConfigMap"`, nonNegativePriority[0].Cel)
	assert.Equal(t, 1, nonNegativePriority[0].Priority)

	require.Len(t, negativePriority, 1)
	assert.Equal(t, `kind == "Secret"`, negativePriority[0].Cel)
	assert.Equal(t, -2, negativePriority[0].Priority)
}

func TestRefOwnershipPredicatesFromHydraValues_IncludesPickCel(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()

	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"pickOnlyUninstall": {
				Enabled: truePtr,
				Tag:     []string{"uninstall"},
				RefParsers: []types.HydraRefParser{
					{
						Predicate: "",
						Pick: []types.HydraRefPick{
							{Cel: `name == "image-pull-secret" && kind == "Secret"`},
						},
					},
				},
			},
		},
	}

	out := refOwnershipPredicatesFromHydraValues(hv, true)
	require.Len(t, out, 1)
	assert.Equal(t, `size((name == "image-pull-secret" && kind == "Secret").filter(ref, ref.hasEndpoint(string(id)))) > 0`, out[0])
}

func TestRefOwnershipPredicatesFromHydraValues_RuntimeTagLocalVsCluster(t *testing.T) {
	t.Parallel()

	truePtr := func() *bool { b := true; return &b }()

	hv := &types.HydraValues{
		Refs: map[string]types.HydraRefGroup{
			"runtimeUninstall": {
				Enabled: truePtr,
				Tag:     []string{"uninstall", types.RefTagRuntime},
				RefParsers: []types.HydraRefParser{
					{Predicate: `group == "argoproj.io"`},
				},
			},
			"runtimeBackup": {
				Enabled: truePtr,
				Tag:     []string{"backup", types.RefTagRuntime},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "Certificate"`},
				},
			},
			"plainUninstall": {
				Enabled: truePtr,
				Tag:     []string{"uninstall"},
				RefParsers: []types.HydraRefParser{
					{Predicate: `kind == "Secret"`},
				},
			},
		},
	}

	local := refOwnershipPredicatesFromHydraValues(hv, false)
	assert.Len(t, local, 1)
	assert.Contains(t, local, `kind == "Secret"`)

	cluster := refOwnershipPredicatesFromHydraValues(hv, true)
	assert.Len(t, cluster, 3)
	assert.Contains(t, cluster, `group == "argoproj.io"`)
	assert.Contains(t, cluster, `kind == "Certificate"`)
	assert.Contains(t, cluster, `kind == "Secret"`)
}
