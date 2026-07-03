package action

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yaml"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func clusterSystemTestSecretEntity(t *testing.T, name, ns string) entity.Entity {
	t.Helper()
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
			},
		},
	}
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyClusterEntity, u).
		Build()
	require.NoError(t, err)
	return e
}

func clusterSystemTestConfigMapEntity(t *testing.T, name, ns string) entity.Entity {
	t.Helper()
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
			},
		},
	}
	e, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(ns)).
		WithUnstructured(types.KeyTemplateEntity, u).
		Build()
	require.NoError(t, err)
	return e
}

func TestBuildClusterSystemReport_stubInventoryMatchesCel(t *testing.T) {
	t.Parallel()

	secA := clusterSystemTestSecretEntity(t, "match-me", "ns1")
	secB := clusterSystemTestSecretEntity(t, "other", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{secA, secB})
	require.NoError(t, err)

	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	celLine := `gvk == "v1/Secret" && ns == "ns1" && name == "match-me"`
	eff := hydra.ClusterDefaultsPresetEffectiveTestFixture("stub-preset", true, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"pick-one": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: celLine, Optional: false}}},
	})

	report, err := buildClusterSystemReport("test-cluster", []hydra.ClusterDefaultsPresetEffective{eff}, env, clusterEnts, 99, true, nil)
	require.NoError(t, err)

	assert.Equal(t, "test-cluster", report.Cluster)
	assert.Equal(t, 1, report.MatchCount)
	assert.Equal(t, 0, report.MissingCount)
	assert.Empty(t, report.MissingIds)
	require.Len(t, report.Presets, 1)
	p := report.Presets[0]
	assert.Equal(t, "stub-preset", p.ID)
	assert.True(t, p.BuiltinDefaultEnabled)
	assert.True(t, p.EffectiveEnabled)
	assert.Equal(t, 1, p.MatchCount)
	assert.Equal(t, 0, p.MissingCount)
	assert.Empty(t, p.MissingIds)
	require.Len(t, p.Predicates, 1)
	pr := p.Predicates[0]
	assert.Equal(t, "pick-one", pr.Name)
	assert.Equal(t, 1, pr.MatchCount)
	assert.Equal(t, 0, pr.MissingCount)
	assert.Empty(t, pr.MissingIds)
	require.Len(t, pr.CelLines, 1)
	line := pr.CelLines[0]
	assert.Equal(t, 0, line.Index)
	assert.Equal(t, celLine, line.Expression)
	assert.Equal(t, "[pick-one] Secret (match-me)", line.Label)
	assert.Equal(t, 1, line.MatchCount)
	require.Len(t, line.MatchIds, 1)
	assert.Equal(t, "v1/Secret/ns1/match-me", line.MatchIds[0])
}

func TestBuildClusterSystemReport_explicitIdsSorted(t *testing.T) {
	t.Parallel()

	sec := clusterSystemTestSecretEntity(t, "present", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	idLater := "v1/Secret/ns1/zebra"
	idEarlier := "v1/Secret/ns1/aaa"
	eff := hydra.ClusterDefaultsPresetEffectiveTestFixture("stub", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"p": {
			Enabled: true,
			Ids:     []hydra.ClusterDefaultsIdLine{{Id: idLater}, {Id: idEarlier}},
		},
	})
	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{eff}, env, clusterEnts, 99, true, nil)
	require.NoError(t, err)

	pr := report.Presets[0].Predicates[0]
	require.Len(t, pr.Ids, 2)
	assert.Equal(t, idEarlier, pr.Ids[0].Id, "explicit ids should be sorted lexicographically within the predicate")
	assert.Equal(t, idLater, pr.Ids[1].Id)
}

func TestBuildClusterSystemReport_missingExplicitIds(t *testing.T) {
	t.Parallel()

	sec := clusterSystemTestSecretEntity(t, "present", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	missingID := "v1/ConfigMap/ns1/not-there"
	eff := hydra.ClusterDefaultsPresetEffectiveTestFixture("stub", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"p": {
			Enabled: true,
			Ids:     []hydra.ClusterDefaultsIdLine{{Id: missingID}, {Id: "v1/Secret/ns1/present"}},
		},
	})
	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{eff}, env, clusterEnts, 99, true, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, report.MatchCount)
	assert.Equal(t, 1, report.MissingCount)
	assert.Equal(t, []string{missingID}, report.MissingIds)

	p := report.Presets[0]
	assert.Equal(t, 1, p.MatchCount)
	assert.Equal(t, 1, p.MissingCount)
	assert.Equal(t, []string{missingID}, p.MissingIds)

	pr := p.Predicates[0]
	assert.Equal(t, 1, pr.MatchCount)
	assert.Equal(t, 1, pr.MissingCount)
	assert.Equal(t, []string{missingID}, pr.MissingIds)
	require.Len(t, pr.Ids, 2)
	assert.Equal(t, missingID, pr.Ids[0].Id)
	assert.Equal(t, 0, pr.Ids[0].MatchCount)
	assert.Equal(t, 1, pr.Ids[0].MissingCount)
	assert.Equal(t, "v1/Secret/ns1/present", pr.Ids[1].Id)
	assert.Equal(t, 1, pr.Ids[1].MatchCount)
	assert.Equal(t, 0, pr.Ids[1].MissingCount)
}

func TestBuildClusterSystemReport_minorExcludedPresentIsUnexpected(t *testing.T) {
	t.Parallel()

	onlyID := "v1/Secret/ns1/extra"
	sec := clusterSystemTestSecretEntity(t, "extra", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	eff := hydra.ClusterDefaultsPresetEffectiveTestFixture("stub", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"future": {
			Enabled:            true,
			KubernetesMinorMin: 99,
			Ids:                []hydra.ClusterDefaultsIdLine{{Id: onlyID}},
		},
	})
	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{eff}, env, clusterEnts, 35, true, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, report.MissingCount)
	assert.Equal(t, 1, report.UnexpectedCount)

	pr := report.Presets[0].Predicates[0]
	require.Len(t, pr.Ids, 1)
	assert.True(t, pr.Ids[0].Unexpected)
	assert.Equal(t, onlyID, pr.Ids[0].Id)
	assert.Equal(t, 1, pr.UnexpectedCount)
}

func TestBuildClusterSystemReport_minorExcludedExplicitAndCelSameIdOneUnexpectedCount(t *testing.T) {
	t.Parallel()

	id := "v1/Secret/ns1/orphan"
	sec := clusterSystemTestSecretEntity(t, "orphan", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	eff := hydra.ClusterDefaultsPresetEffectiveTestFixture("stub", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"gate": {
			Enabled:            true,
			KubernetesMinorMin: 99,
			Ids:                []hydra.ClusterDefaultsIdLine{{Id: id}},
		},
		"rbac": {
			Enabled:  true,
			CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `id == "` + id + `"`, Optional: false}},
		},
	})
	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{eff}, env, clusterEnts, 35, true, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, report.UnexpectedCount)
}

func TestBuildClusterSystemReport_minorExcludedAbsentOmitted(t *testing.T) {
	t.Parallel()

	clusterEnts, err := entity.NewEntities(nil)
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	eff := hydra.ClusterDefaultsPresetEffectiveTestFixture("stub", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"future": {
			Enabled:            true,
			KubernetesMinorMin: 99,
			Ids:                []hydra.ClusterDefaultsIdLine{{Id: "v1/Secret/ns1/absent"}},
		},
	})
	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{eff}, env, clusterEnts, 35, true, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, report.UnexpectedCount)
	pr := report.Presets[0].Predicates[0]
	assert.Empty(t, pr.Ids)
}

func TestBuildClusterSystemReport_skipsSynthesizedRbacCelForMinorGatedIds(t *testing.T) {
	t.Parallel()
	effs, err := hydra.EffectiveClusterDefaultsPresets(nil)
	require.NoError(t, err)
	var kube hydra.ClusterDefaultsPresetEffective
	var found bool
	for i := range effs {
		if effs[i].ID == hydra.ClusterDefaultsPresetIDKubernetes {
			kube = effs[i]
			found = true
			break
		}
	}
	require.True(t, found)
	ents, err := entity.NewEntities(nil)
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(ents)
	require.NoError(t, err)
	report, err := buildClusterSystemReport("t", []hydra.ClusterDefaultsPresetEffective{kube}, env, ents, 35, true, nil)
	require.NoError(t, err)
	const dev = "device-taint-eviction-controller"
	for _, p := range report.Presets {
		for _, pr := range p.Predicates {
			for _, cl := range pr.CelLines {
				assert.False(t, strings.Contains(cl.Expression, dev),
					"at kubernetes minor 35, CEL must not reference %q (minor-gated bootstrap RBAC)", dev)
			}
		}
	}
}

func TestClusterSystemTextForEmptyCelMatch_singleIdEquals(t *testing.T) {
	t.Parallel()
	got := clusterSystemTextForEmptyCelMatch("rbac-cluster-role-bindings", `id == "rbac.authorization.k8s.io/v1/ClusterRoleBinding//system:controller:device-taint-eviction-controller"`)
	assert.Equal(t, "rbac.authorization.k8s.io/v1/ClusterRoleBinding//system:controller:device-taint-eviction-controller", got)
}

func TestClusterSystemCelLineHumanLabel_fallbackPredicateBracketOnly(t *testing.T) {
	t.Parallel()
	got := clusterSystemCelLineHumanLabel("servicelb-daemonsets", `name.startsWith("svclb-") && clusterEntity.labels().getOrEmpty("svccontroller.k3s.cattle.io/svcname") != ""`)
	assert.Equal(t, "[servicelb-daemonsets]", got)
}

func TestClusterSystemInferCelLineShortName_kubeSystemEvent(t *testing.T) {
	t.Parallel()
	expr := `ns == "kube-system" && name.matches("^konnectivity-agent.*")`
	assert.Equal(t, "Event (konnectivity-agent…)", clusterSystemInferCelLineShortName(expr))
}

func TestClusterSystemPresetRows_optionalCelAbsent(t *testing.T) {
	t.Parallel()
	p := clusterSystemPresetEntry{
		ID: "kubernetes",
		Predicates: []clusterSystemPredicateEntry{
			{
				Name: "konnectivity-agent",
				CelLines: []clusterSystemCelLineEntry{
					{
						Expression: `ns == "kube-system" && name.matches("^konnectivity-agent.*")`,
						Label:      "[konnectivity-agent] Event (konnectivity-agent…)",
						Optional:   true,
						MatchCount: 0,
						MatchIds:   nil,
					},
				},
			},
		},
	}
	rows := clusterSystemPresetRows(p)
	require.Len(t, rows, 1)
	assert.Equal(t, "optional", rows[0].status)
	assert.Equal(t, "[konnectivity-agent] Event (konnectivity-agent…)", rows[0].text)
}

func TestClusterSystemPresetRows_dedupesExplicitIdAndEmptyCelSameText(t *testing.T) {
	t.Parallel()

	id := "rbac.authorization.k8s.io/v1/ClusterRoleBinding//system:controller:device-taint-eviction-controller"
	p := clusterSystemPresetEntry{
		ID:               "kubernetes",
		EffectiveEnabled: true,
		Predicates: []clusterSystemPredicateEntry{
			{
				Name: "bootstrap-audit-m36",
				Ids: []clusterSystemIdEntry{
					{Id: id, MatchCount: 0, MissingCount: 1},
				},
			},
			{
				Name: "rbac-cluster-role-bindings",
				CelLines: []clusterSystemCelLineEntry{
					{
						Expression: `id == "` + id + `"`,
						MatchIds:   nil,
					},
				},
			},
		},
	}
	rows := clusterSystemPresetRows(p)
	require.Len(t, rows, 1)
	assert.Equal(t, id, rows[0].text)
	assert.Equal(t, "not found", rows[0].status)
}

func TestClusterSystemPresetRows_foundWinsOverNotFoundDuplicateText(t *testing.T) {
	t.Parallel()

	id := "v1/Secret/ns1/x"
	p := clusterSystemPresetEntry{
		ID: "p",
		Predicates: []clusterSystemPredicateEntry{
			{
				Name: "a",
				Ids: []clusterSystemIdEntry{
					{Id: id, MatchCount: 1, MissingCount: 0},
				},
				CelLines: []clusterSystemCelLineEntry{
					{Expression: `id == "` + id + `"`, MatchIds: nil},
				},
			},
		},
	}
	rows := clusterSystemPresetRows(p)
	require.Len(t, rows, 1)
	assert.Equal(t, "found", rows[0].status)
}

func TestBuildClusterSystemReport_canalMarkedExcludedWhenFlannelActivatesNotCanal(t *testing.T) {
	t.Parallel()

	sec := clusterSystemTestSecretEntity(t, "x", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	canal := hydra.ClusterDefaultsPresetEffectiveTestFixture("canal", false, false, map[string]hydra.ClusterDefaultsPredicateEffective{
		"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `gvk == "v1/Secret" && ns == "ns1"`, Optional: false}}},
	})
	flannel := hydra.ClusterDefaultsPresetEffectiveTestFixture("flannel", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `gvk == "v1/Secret" && ns == "ns1" && name == "missing"`, Optional: false}}},
	})
	flannel.Activates = types.PresetActivateList{{Preset: "canal", Exclude: true}}

	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{canal, flannel}, env, clusterEnts, 99, true, nil)
	require.NoError(t, err)
	var canalEntry *clusterSystemPresetEntry
	for i := range report.Presets {
		if report.Presets[i].ID == "canal" {
			canalEntry = &report.Presets[i]
			break
		}
	}
	require.NotNil(t, canalEntry)
	assert.True(t, canalEntry.Excluded)
}

func TestBuildClusterSystemReport_omitsDisabledPresetsWhenIncludeDisabledFalse(t *testing.T) {
	t.Parallel()

	sec := clusterSystemTestSecretEntity(t, "x", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	canal := hydra.ClusterDefaultsPresetEffectiveTestFixture("canal", false, false, map[string]hydra.ClusterDefaultsPredicateEffective{
		"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `gvk == "v1/Secret" && ns == "ns1"`, Optional: false}}},
	})
	flannel := hydra.ClusterDefaultsPresetEffectiveTestFixture("flannel", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `gvk == "v1/Secret" && ns == "ns1" && name == "missing"`, Optional: false}}},
	})
	flannel.Activates = types.PresetActivateList{{Preset: "canal", Exclude: true}}

	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{canal, flannel}, env, clusterEnts, 99, false, nil)
	require.NoError(t, err)
	require.Len(t, report.Presets, 1)
	assert.Equal(t, "flannel", report.Presets[0].ID)
	assert.True(t, report.Presets[0].EffectiveEnabled)
}

func TestClusterSystemPresetTitleSuffix_excludedAppendsCommaExcluded(t *testing.T) {
	t.Parallel()
	s := clusterSystemPresetTitleSuffix(clusterSystemPresetEntry{
		BuiltinDefaultEnabled: false,
		EffectiveEnabled:      false,
		Excluded:              true,
	})
	assert.Equal(t, "(disabled, excluded, preset disabled)", s)
}

func TestWriteClusterSystemHumanText_disabledNotExcludedRowsGrayNoTag(t *testing.T) {
	t.Parallel()

	report := clusterSystemReport{
		Cluster: "demo",
		Presets: []clusterSystemPresetEntry{
			{
				ID:                    "cloudinit",
				BuiltinDefaultEnabled: true,
				EffectiveEnabled:      false,
				Excluded:              false,
				Predicates: []clusterSystemPredicateEntry{
					{
						Name: "m",
						Ids: []clusterSystemIdEntry{
							{Id: "v1/Namespace//cloud-init-settings", MatchCount: 0, MissingCount: 1},
						},
					},
				},
			},
		},
	}
	var buf strings.Builder
	require.NoError(t, writeClusterSystemHumanText(&buf, report, types.ColorYes))
	out := buf.String()
	assert.NotContains(t, out, "(excluded)")
	gray := colors.LightGray.String()
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.HasPrefix(line, "  v1/Namespace/") {
			assert.Contains(t, line, gray, "disabled preset row should use gray: %q", line)
			assert.Contains(t, line, "not found")
		}
	}
}

func TestWriteClusterSystemHumanText_excludedRowsGrayWithTag(t *testing.T) {
	t.Parallel()

	report := clusterSystemReport{
		Cluster: "demo",
		Presets: []clusterSystemPresetEntry{
			{
				ID:                    "canal",
				BuiltinDefaultEnabled: false,
				EffectiveEnabled:      false,
				Excluded:              true,
				Predicates: []clusterSystemPredicateEntry{
					{
						Name: "m",
						Ids: []clusterSystemIdEntry{
							{Id: "v1/ConfigMap/kube-system/canal-config", MatchCount: 1, MissingCount: 0},
							{Id: "v1/ConfigMap/kube-system/missing", MatchCount: 0, MissingCount: 1},
						},
					},
				},
			},
		},
	}
	var buf strings.Builder
	require.NoError(t, writeClusterSystemHumanText(&buf, report, types.ColorYes))
	out := buf.String()
	assert.Contains(t, out, "(disabled, excluded, preset disabled)")
	gray := colors.LightGray.String()
	assert.Contains(t, out, gray+"(excluded)"+colors.Reset.String())
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.HasPrefix(line, "  v1/ConfigMap/") {
			assert.Contains(t, line, gray, "id column should be gray when excluded: %q", line)
			assert.Contains(t, line, "(excluded)")
		}
	}
}

func TestWriteClusterSystemHumanText_columnsAndStatuses(t *testing.T) {
	t.Parallel()

	report := clusterSystemReport{
		Cluster: "demo",
		Presets: []clusterSystemPresetEntry{
			{
				ID:                    "flannel",
				BuiltinDefaultEnabled: false,
				EffectiveEnabled:      true,
				Predicates: []clusterSystemPredicateEntry{
					{
						Name: "m",
						Ids: []clusterSystemIdEntry{
							{Id: "v1/ConfigMap/kube-system/kube-flannel-cfg", MatchCount: 1, MissingCount: 0},
							{Id: "v1/ConfigMap/kube-system/kube-flannel-cfg2", MatchCount: 0, MissingCount: 1},
						},
					},
				},
			},
		},
	}
	var buf strings.Builder
	require.NoError(t, writeClusterSystemHumanText(&buf, report, types.ColorNo))
	out := buf.String()
	assert.Contains(t, out, "cluster demo")
	assert.Contains(t, out, "flannel (1/2) (enabled, preset disabled)")
	assert.Contains(t, out, "v1/ConfigMap/kube-system/kube-flannel-cfg")
	assert.Contains(t, out, "found")
	assert.Contains(t, out, "not found")
	// Same column width for both id lines; padding aligns the status column.
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 4)
	assert.Contains(t, lines[2], "kube-flannel-cfg")
	assert.Contains(t, lines[2], "found")
	assert.Contains(t, lines[3], "kube-flannel-cfg2")
	assert.Contains(t, lines[3], "not found")
}

func TestWriteClusterSystemHumanText_statusColumnAlignedAcrossPresets(t *testing.T) {
	t.Parallel()

	longID := "apps/v1/DaemonSet/kube-system/csi-cinder-nodeplugin"
	report := clusterSystemReport{
		Cluster: "demo",
		Presets: []clusterSystemPresetEntry{
			{
				ID:                    "cloudinit",
				BuiltinDefaultEnabled: true,
				EffectiveEnabled:      false,
				Predicates: []clusterSystemPredicateEntry{
					{
						Name: "x",
						Ids: []clusterSystemIdEntry{
							{Id: "v1/Namespace//cloud-init-settings", MatchCount: 0, MissingCount: 1},
						},
					},
				},
			},
			{
				ID:                    "cinder",
				BuiltinDefaultEnabled: false,
				EffectiveEnabled:      false,
				Predicates: []clusterSystemPredicateEntry{
					{
						Name: "y",
						Ids: []clusterSystemIdEntry{
							{Id: longID, MatchCount: 0, MissingCount: 1},
						},
					},
				},
			},
		},
	}
	var buf strings.Builder
	require.NoError(t, writeClusterSystemHumanText(&buf, report, types.ColorNo))
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	var statusIndices []int
	for _, line := range lines {
		if !strings.HasPrefix(line, "  v1/") && !strings.HasPrefix(line, "  apps/") {
			continue
		}
		idx := strings.Index(line, "not found")
		require.GreaterOrEqual(t, idx, 0, "line should contain status: %q", line)
		statusIndices = append(statusIndices, idx)
	}
	require.Len(t, statusIndices, 2, "expected two id rows")
	assert.Equal(t, statusIndices[0], statusIndices[1], "status column should start at the same offset in every preset block")
}

func TestWriteClusterSystemHumanText_unicodeEllipsisAlignsWithExplicitIds(t *testing.T) {
	t.Parallel()

	// U+2026 HORIZONTAL ELLIPSIS is three UTF-8 bytes but one terminal cell; byte-based padding misaligned status.
	longID := "apiextensions.k8s.io/v1/CustomResourceDefinition//adminnetworkpolicies.policy.networking.k8s.io"
	celLabel := "[kube-system-satellites] Event (canal…)"
	report := clusterSystemReport{
		Cluster: "demo",
		Presets: []clusterSystemPresetEntry{
			{
				ID:                    "canal",
				BuiltinDefaultEnabled: false,
				EffectiveEnabled:      false,
				Predicates: []clusterSystemPredicateEntry{
					{
						Name: "kube-system-satellites",
						CelLines: []clusterSystemCelLineEntry{
							{Label: celLabel, MatchIds: nil},
						},
					},
					{
						Name: "ids",
						Ids: []clusterSystemIdEntry{
							{Id: longID, MatchCount: 0, MissingCount: 1},
						},
					},
				},
			},
		},
	}
	var buf strings.Builder
	require.NoError(t, writeClusterSystemHumanText(&buf, report, types.ColorNo))
	var statusPrefixDisplayWidths []int
	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if !strings.HasPrefix(line, "  [") && !strings.HasPrefix(line, "  apiextensions") {
			continue
		}
		idx := strings.Index(line, "not found")
		require.GreaterOrEqual(t, idx, 0, "line should contain status: %q", line)
		// Byte index differs when the label uses multi-byte runes (e.g. …); terminal columns use display width.
		statusPrefixDisplayWidths = append(statusPrefixDisplayWidths, runewidth.StringWidth(line[:idx]))
	}
	require.Len(t, statusPrefixDisplayWidths, 2)
	assert.Equal(t, statusPrefixDisplayWidths[0], statusPrefixDisplayWidths[1])
}

func TestBuildClusterSystemReport_yamlRoundTripAndYq(t *testing.T) {
	t.Parallel()

	sec := clusterSystemTestSecretEntity(t, "x", "ns1")
	clusterEnts, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	cm := clusterSystemTestConfigMapEntity(t, "cfg", "ns1")
	rendered, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(rendered)
	require.NoError(t, err)

	eff := hydra.ClusterDefaultsPresetEffectiveTestFixture("stub", false, true, map[string]hydra.ClusterDefaultsPredicateEffective{
		"p": {Enabled: true, CelLines: []hydra.ClusterDefaultsCelLine{{Expr: `gvk == "v1/Secret" && ns == "ns1"`, Optional: false}}},
	})
	report, err := buildClusterSystemReport("cl", []hydra.ClusterDefaultsPresetEffective{eff}, env, clusterEnts, 99, true, nil)
	require.NoError(t, err)

	yamlBytes, err := yaml.ToYaml(report)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(yamlBytes), "cluster:"))

	out, err := yq.ToYaml(types.ColorNo, report)
	require.NoError(t, err)
	assert.Contains(t, out, "cluster: cl")
	assert.Contains(t, out, "stub")
}
