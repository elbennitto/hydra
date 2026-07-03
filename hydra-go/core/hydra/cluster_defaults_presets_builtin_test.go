package hydra

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestEffectiveClusterDefaultsPresets_OverrideReplacesPredicateCel(t *testing.T) {
	t.Parallel()
	custom := `id == "v1/Secret/kube-system/replaced-only"`
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDCoredns: {
			Predicates: map[string]types.HydraPresetPredicateEntry{
				"deployment": {Cel: types.PresetCelList{{Expr: custom, Optional: false}}},
			},
		},
	}
	effs, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	var coredns *ClusterDefaultsPresetEffective
	for i := range effs {
		if effs[i].ID == ClusterDefaultsPresetIDCoredns {
			coredns = &effs[i]
			break
		}
	}
	require.NotNil(t, coredns)
	deploy, ok := coredns.Predicates["deployment"]
	require.True(t, ok)
	assert.Equal(t, []ClusterDefaultsCelLine{{Expr: custom, Optional: false}}, deploy.CelLines)
}

func TestEffectiveClusterDefaultsPresets_DisabledNamedPredicateDropsMatch(t *testing.T) {
	t.Parallel()
	disabled := false
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDKubernetes: {
			Predicates: map[string]types.HydraPresetPredicateEntry{
				"default-namespace-injected": {Enabled: &disabled},
			},
		},
	}
	effs, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	var kube *ClusterDefaultsPresetEffective
	for i := range effs {
		if effs[i].ID == ClusterDefaultsPresetIDKubernetes {
			kube = &effs[i]
			break
		}
	}
	require.NotNil(t, kube)
	pe, ok := kube.Predicates["default-namespace-injected"]
	require.True(t, ok)
	assert.False(t, pe.Enabled)
}

// kubernetesMinorsToTest returns sorted distinct kubernetes server minors to exercise builtin preset
// minor gates like ClusterDefaultsPredicateMinorApplies: every kubernetesMinorMin and kubernetesMinorMax
// from the builtin YAMLs, boundary neighbors (min-1 below a min gate, max+1 above a max gate),
// bootstrap-audit-m{N} predicate names, plus 99 (unknown minor — same normalization as k8sMinor<=0).
func kubernetesMinorsToTest(t *testing.T) []int {
	t.Helper()
	m, err := loadBuiltinClusterDefaultsPresets()
	require.NoError(t, err)
	seen := map[int]struct{}{99: {}}
	add := func(v int) {
		if v > 0 {
			seen[v] = struct{}{}
		}
	}
	for _, fid := range clusterDefaultsBuiltinPresetIDsOrdered() {
		f := m[fid]
		for _, act := range f.Activates {
			if act.KubernetesMinorMin > 0 {
				add(act.KubernetesMinorMin)
				if act.KubernetesMinorMin > 1 {
					add(act.KubernetesMinorMin - 1)
				}
			}
			if act.KubernetesMinorMax > 0 {
				add(act.KubernetesMinorMax)
				add(act.KubernetesMinorMax + 1)
			}
		}
		for pname, pe := range f.Predicates {
			if pe.KubernetesMinorMin > 0 {
				add(pe.KubernetesMinorMin)
				if pe.KubernetesMinorMin > 1 {
					add(pe.KubernetesMinorMin - 1)
				}
			}
			if pe.KubernetesMinorMax > 0 {
				add(pe.KubernetesMinorMax)
				add(pe.KubernetesMinorMax + 1)
			}
			const prefix = "bootstrap-audit-m"
			if strings.HasPrefix(pname, prefix) {
				rest := strings.TrimPrefix(pname, prefix)
				if v, err := strconv.Atoi(rest); err == nil && v > 0 {
					add(v)
				}
			}
		}
	}
	out := make([]int, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// duplicateBuiltinPresetIDsAtKubernetesMinor returns sorted ids that occur more than once when
// concatenating all ids from every enabled builtin preset predicate that applies at k8sMinor
// (same rules as audit expected-id collection: preset enabled, predicate enabled, minor gates).
func duplicateBuiltinPresetIDsAtKubernetesMinor(effs []ClusterDefaultsPresetEffective, k8sMinor int) []string {
	counts := make(map[string]int)
	for _, eff := range effs {
		if !eff.Enabled {
			continue
		}
		for _, pe := range eff.Predicates {
			if !pe.Enabled || !ClusterDefaultsPredicateMinorApplies(k8sMinor, pe) {
				continue
			}
			for _, idl := range pe.Ids {
				counts[idl.Id]++
			}
		}
	}
	var dup []string
	for id, n := range counts {
		if n > 1 {
			dup = append(dup, id)
		}
	}
	slices.Sort(dup)
	return dup
}

// explicitCelOverlap records that an explicit preset id (after minor gating) also matches a CEL line
// when evaluated against the same inventory model as `hydra gitops system` (CEL runs for every enabled
// predicate line regardless of kubernetesMinorMin/Max on that predicate).
type explicitCelOverlap struct {
	Preset            string
	KubernetesMinor   int
	ExplicitPredicate string
	CelPredicate      string
	ID                string
	CelLine           string
}

// testCelLineSingleIdEquals matches synthesized bootstrap RBAC CEL (`id == "…"`), same idea as cli/action/cluster_system.go.
var testCelLineSingleIdEquals = regexp.MustCompile(`^\s*id\s*==\s*"([^"]+)"\s*$`)

// testClusterDefaultsEntityFromHydraID builds a minimal cluster entity whose Id() equals id.
func testClusterDefaultsEntityFromHydraID(id types.Id) (entity.Entity, error) {
	g, ver, k, ns, name, err := id.Components()
	if err != nil {
		return entity.Entity{}, err
	}
	b := entity.NewEntityBuilder().
		WithGroup(g).
		WithVersion(ver).
		WithKind(k).
		WithName(name)
	b = b.WithNamespace(ns)
	if ns == "" {
		b = b.WithNamespaced(types.NamespacedNo)
	} else {
		b = b.WithNamespaced(types.NamespacedYes)
	}
	return b.Build()
}

// builtinPresetExplicitIDCELOverlaps returns every pair (explicit id source predicate, CEL predicate, line)
// where an explicit id at k8sMinor also matches a CEL line from any enabled predicate (same rules as cluster system output).
func builtinPresetExplicitIDCELOverlaps(eff ClusterDefaultsPresetEffective, k8sMinor int) ([]explicitCelOverlap, error) {
	if !eff.Enabled {
		return nil, nil
	}
	explicitPreds := make(map[types.Id][]string)
	for pname, pe := range eff.Predicates {
		if !pe.Enabled || !ClusterDefaultsPredicateMinorApplies(k8sMinor, pe) {
			continue
		}
		for _, idl := range pe.Ids {
			tid := types.Id(idl.Id)
			explicitPreds[tid] = append(explicitPreds[tid], pname)
		}
	}
	if len(explicitPreds) == 0 {
		return nil, nil
	}
	var (
		out            []explicitCelOverlap
		inventoryReady bool
		ents           entity.Entities
		env            cel.Env
	)
	appendOverlap := func(celPname, line string, mid types.Id) {
		for _, explPname := range explicitPreds[mid] {
			out = append(out, explicitCelOverlap{
				Preset:            eff.ID,
				KubernetesMinor:   k8sMinor,
				ExplicitPredicate: explPname,
				CelPredicate:      celPname,
				ID:                string(mid),
				CelLine:           line,
			})
		}
	}
	ensureInventory := func() error {
		if inventoryReady {
			return nil
		}
		entsSlice := make([]entity.Entity, 0, len(explicitPreds))
		for id := range explicitPreds {
			e, err := testClusterDefaultsEntityFromHydraID(id)
			if err != nil {
				return fmt.Errorf("entity for id %q: %w", id, err)
			}
			entsSlice = append(entsSlice, e)
		}
		var err error
		ents, err = entity.NewEntities(entsSlice)
		if err != nil {
			return err
		}
		env, err = cel.NewEnvWithEntityInventory(ents)
		if err != nil {
			return err
		}
		inventoryReady = true
		return nil
	}
	pnames := make([]string, 0, len(eff.Predicates))
	for n := range eff.Predicates {
		pnames = append(pnames, n)
	}
	slices.Sort(pnames)
	for _, celPname := range pnames {
		pe := eff.Predicates[celPname]
		if !pe.Enabled {
			continue
		}
		for _, line := range pe.CelLines {
			celCombined := types.CelPredicate(strings.TrimSpace(line.Expr))
			if !line.Selector.IsZero() {
				if celCombined == "" {
					celCombined = line.Selector.CelPredicate()
				} else {
					celCombined = line.Selector.MatchPredicate(celCombined)
				}
			} else if celCombined == "" {
				continue
			}
			if m := testCelLineSingleIdEquals.FindStringSubmatch(string(celCombined)); len(m) > 1 {
				mid := types.Id(m[1])
				if _, ok := explicitPreds[mid]; ok {
					appendOverlap(celPname, string(celCombined), mid)
				}
				continue
			}
			if err := ensureInventory(); err != nil {
				return nil, err
			}
			matched, err := MatchingEntityIdsForCEL(env, ents, string(celCombined))
			if err != nil {
				return nil, fmt.Errorf("preset %q predicate %q cel: %w", eff.ID, celPname, err)
			}
			for _, mid := range matched {
				if _, ok := explicitPreds[mid]; !ok {
					continue
				}
				appendOverlap(celPname, string(celCombined), mid)
			}
		}
	}
	slices.SortFunc(out, func(a, b explicitCelOverlap) int {
		if c := strings.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		if c := strings.Compare(a.ExplicitPredicate, b.ExplicitPredicate); c != 0 {
			return c
		}
		if c := strings.Compare(a.CelPredicate, b.CelPredicate); c != 0 {
			return c
		}
		return strings.Compare(a.CelLine, b.CelLine)
	})
	return out, nil
}

func TestBuiltinClusterDefaultsPresets_NoDuplicateIDsGlobally_PerKubernetesMinor(t *testing.T) {
	t.Parallel()
	minors := kubernetesMinorsToTest(t)
	effs, err := EffectiveClusterDefaultsPresets(nil)
	require.NoError(t, err)
	for _, minor := range minors {
		t.Run(fmt.Sprintf("kubernetes_minor_%d", minor), func(t *testing.T) {
			t.Parallel()
			dup := duplicateBuiltinPresetIDsAtKubernetesMinor(effs, minor)
			if len(dup) == 0 {
				return
			}
			t.Fatalf("duplicate ids across all builtin presets at kubernetes minor %d: %v", minor, dup)
		})
	}
}

// kubernetesExplicitIDCELOverlapExpected documents known intentional double-counting in `hydra gitops system`:
// bootstrap-audit* explicit ids are mirrored as synthesized rbac-cluster-* CEL lines, and kube-root-ca
// ConfigMaps are listed both under bootstrap-audit ids and the kube-root-ca-configmap CEL predicate.
func kubernetesExplicitIDCELOverlapExpected(o explicitCelOverlap) bool {
	switch o.CelPredicate {
	case "rbac-cluster-roles", "rbac-cluster-role-bindings":
		return strings.HasPrefix(o.ExplicitPredicate, "bootstrap-audit")
	case "kube-root-ca-configmap":
		return o.ExplicitPredicate == "bootstrap-audit" || o.ExplicitPredicate == "default-namespace-injected"
	default:
		return false
	}
}

func TestBuiltinClusterDefaultsPresets_ExplicitIDsVsCelLines_KnownOverlapsOnly(t *testing.T) {
	t.Parallel()
	minors := kubernetesMinorsToTest(t)
	effs, err := EffectiveClusterDefaultsPresets(nil)
	require.NoError(t, err)
	for _, minor := range minors {
		t.Run(fmt.Sprintf("kubernetes_minor_%d", minor), func(t *testing.T) {
			t.Parallel()
			for _, eff := range effs {
				overlaps, err := builtinPresetExplicitIDCELOverlaps(eff, minor)
				require.NoError(t, err)
				for _, o := range overlaps {
					require.True(t, kubernetesExplicitIDCELOverlapExpected(o),
						"unexpected explicit id also matching CEL (would duplicate rows in hydra gitops system): preset=%s minor=%d id=%q explicitPredicate=%q celPredicate=%q celLine=%q",
						o.Preset, o.KubernetesMinor, o.ID, o.ExplicitPredicate, o.CelPredicate, o.CelLine)
				}
			}
		})
	}
}

func TestEffectiveClusterDefaultsPresets_SyselevenActivatesRespectsExplicitDisabled(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDSyseleven:  {Enabled: &enabled},
		ClusterDefaultsPresetIDCloudinit:  {Enabled: &disabled},
		ClusterDefaultsPresetIDCinder:     {Enabled: &disabled},
		ClusterDefaultsPresetIDKubermatic: {Enabled: &disabled},
	}
	effs, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	byID := make(map[string]ClusterDefaultsPresetEffective, len(effs))
	for i := range effs {
		byID[effs[i].ID] = effs[i]
	}
	require.True(t, byID[ClusterDefaultsPresetIDSyseleven].Enabled)
	require.Contains(t, clusterDefaultsPositiveActivateTargets(byID[ClusterDefaultsPresetIDSyseleven].Activates), ClusterDefaultsPresetIDMetakube)
	require.Contains(t, clusterDefaultsPositiveActivateTargets(byID[ClusterDefaultsPresetIDSyseleven].Activates), ClusterDefaultsPresetIDSyselevenNodeProblemDetector)
	require.Contains(t, clusterDefaultsPositiveActivateTargets(byID[ClusterDefaultsPresetIDSyseleven].Activates), ClusterDefaultsPresetIDQuobyte)
	require.Contains(t, clusterDefaultsPositiveActivateTargets(byID[ClusterDefaultsPresetIDSyseleven].Activates), ClusterDefaultsPresetIDMetricsServer)
	require.True(t, byID[ClusterDefaultsPresetIDMetakube].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDSyselevenNodeProblemDetector].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDQuobyte].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDCloudinit].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDCinder].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDCinderController].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDKubermatic].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDKubeProxy].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDKubeControllerManager].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDKubeScheduler].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDMetricsServer].Enabled)
}

func TestEffectiveClusterDefaultsPresets_ExclusionBlocksTransitiveActivation(t *testing.T) {
	t.Parallel()
	enabled := true
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDCloudPoc: {Enabled: &enabled},
	}
	effs, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	byID := make(map[string]ClusterDefaultsPresetEffective, len(effs))
	for i := range effs {
		byID[effs[i].ID] = effs[i]
	}
	require.True(t, byID[ClusterDefaultsPresetIDCloudPoc].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDGardener].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDMonex].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDCinder].Enabled)
	require.Contains(t, clusterDefaultsPositiveActivateTargets(byID[ClusterDefaultsPresetIDCinder].Activates), ClusterDefaultsPresetIDCinderController)
	require.False(t, byID[ClusterDefaultsPresetIDCinderController].Enabled)
}

func TestValidateClusterDefaultsActivatesGraphAcyclic_acceptsDAG(t *testing.T) {
	t.Parallel()
	err := validateClusterDefaultsActivatesGraphAcyclic(map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": nil,
	})
	require.NoError(t, err)
}

func TestValidateClusterDefaultsActivatesGraphAcyclic_rejectsCycle(t *testing.T) {
	t.Parallel()
	err := validateClusterDefaultsActivatesGraphAcyclic(map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	})
	require.Error(t, err)
}

func TestEffectiveClusterDefaultsPresets_mergedActivatesCycleReturnsError(t *testing.T) {
	t.Parallel()
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDSyseleven: {
			Activates: types.PresetActivateList{{Preset: "kubermatic"}},
		},
		ClusterDefaultsPresetIDKubermatic: {
			Activates: types.PresetActivateList{{Preset: "syseleven"}},
		},
	}
	_, err := EffectiveClusterDefaultsPresets(merged)
	require.Error(t, err)
}

func TestEffectiveClusterDefaultsPresets_flannelAndCanalBothEnabledReturnsError(t *testing.T) {
	t.Parallel()
	on := true
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDFlannel: {Enabled: &on},
		ClusterDefaultsPresetIDCanal:   {Enabled: &on},
	}
	_, err := EffectiveClusterDefaultsPresets(merged)
	require.Error(t, err)
}

func TestEffectiveClusterDefaultsPresets_syselevenAndFlannelExclusionBlocksCanalActivation(t *testing.T) {
	t.Parallel()
	on := true
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDSyseleven: {Enabled: &on},
		ClusterDefaultsPresetIDFlannel:   {Enabled: &on},
	}
	effs, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	byID := make(map[string]ClusterDefaultsPresetEffective, len(effs))
	for i := range effs {
		byID[effs[i].ID] = effs[i]
	}
	require.True(t, byID[ClusterDefaultsPresetIDSyseleven].Enabled)
	require.True(t, byID[ClusterDefaultsPresetIDFlannel].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDCanal].Enabled)
}

func TestEffectiveClusterDefaultsPresets_DisabledSyselevenDoesNotActivateBundled(t *testing.T) {
	t.Parallel()
	disabled := false
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDSyseleven:  {Enabled: &disabled},
		ClusterDefaultsPresetIDCloudinit:  {Enabled: &disabled},
		ClusterDefaultsPresetIDKubermatic: {Enabled: &disabled},
	}
	effs, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	byID := make(map[string]ClusterDefaultsPresetEffective, len(effs))
	for i := range effs {
		byID[effs[i].ID] = effs[i]
	}
	require.False(t, byID[ClusterDefaultsPresetIDSyseleven].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDMetakube].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDSyselevenNodeProblemDetector].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDQuobyte].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDCloudinit].Enabled)
	require.False(t, byID[ClusterDefaultsPresetIDKubermatic].Enabled)
}
