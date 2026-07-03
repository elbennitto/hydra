// Cluster preset match gold tests: each testdata/cluster_preset_matches/<case>.given.yaml
// lists inventory entity ids, optional presetSection (Helm-style global.hydra.presets), and optional presetIds
// ("preset" forces enable + asserts on, "!preset" forces disable + asserts off). Omit kubernetes/coredns from
// positive entries (on by default); use "!kubernetes" / "!coredns" to disable. Omit presets only implied via activates.
//
// Expected file (<case>.expected.yaml) uses schema v2:
//   - status: ok — matches maps each fixture entity id to its single matching preset (1:1).
//   - status: invalid — documents entities with no match or multiple matches; matches uses "(missing)"
//     for missing entities and comma-separated preset IDs for ambiguous entities. Normal test run always fails
//     until presets/fixtures are fixed and gold is regenerated with status ok.
//
// Regenerate all expected files (from hydra/hydra-go, same as ./update_testdata.sh):
//
//	go test -count=1 -run TestClusterPresetMatchesGolden ./core/hydra -update
package hydra

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
)

var updateGolden = flag.Bool("update", false, "update golden files")

//go:embed testdata/cluster_preset_matches/*.given.yaml
var clusterPresetMatchGivenFS embed.FS

const clusterPresetGoldenFileHeader = "# Cluster preset match gold file (schema v2)\n" +
	"# Regenerate: go test -count=1 -run TestClusterPresetMatchesGolden ./core/hydra -update\n" +
	"# (from repo: hydra/hydra-go — or run ./update_testdata.sh)\n"

// clusterPresetMatchFixture is shared input for gold tests: inventory ids plus documented preset config.
// Add more files under testdata/cluster_preset_matches/<name>.given.yaml for other clusters.
type clusterPresetMatchFixture struct {
	SchemaVersion int `yaml:"schemaVersion"`
	// KubernetesMinor is the apiserver minor used for preset minor gates and CEL compilation.
	KubernetesMinor int    `yaml:"kubernetesMinor"`
	Cluster         string `yaml:"cluster"`
	Description     string `yaml:"description,omitempty"`
	// PresetSection is merged like Helm global.hydra.presets (omit or use {} for builtins only).
	PresetSection *types.HydraPresetsSection `yaml:"presetSection,omitempty"`
	// PresetIds: "preset" forces enabled (merged after presetSection) and asserts on; "!preset" forces disabled and asserts off.
	PresetIds []string `yaml:"presetIds,omitempty"`
	EntityIds []string `yaml:"entityIds"`
}

type ambiguousEntityMatch struct {
	EntityID string   `yaml:"entityId"`
	Presets  []string `yaml:"presets"`
}

const clusterPresetMatchMissingValue = "(missing)"

// clusterPresetMatchGoldenFile is the on-disk expected format (schema v2).
// Older v1 files are a flat map of entityId -> presetId (no status / matches keys); they parse as status ok.
type clusterPresetMatchGoldenFile struct {
	Status                 string                 `yaml:"status,omitempty"`
	Matches                map[string]string      `yaml:"matches,omitempty"`
	MissingEntityIDs       []string               `yaml:"missingEntityIds,omitempty"`
	AmbiguousEntityMatches []ambiguousEntityMatch `yaml:"ambiguousEntityMatches,omitempty"`
}

func parseClusterPresetMatchGolden(data []byte) (clusterPresetMatchGoldenFile, error) {
	var top map[string]interface{}
	if err := yaml.Unmarshal(data, &top); err != nil {
		return clusterPresetMatchGoldenFile{}, err
	}
	if top == nil {
		return clusterPresetMatchGoldenFile{}, fmt.Errorf("empty golden file")
	}
	newFormat := false
	for _, k := range []string{"status", "matches", "missingEntityIds", "ambiguousEntityMatches"} {
		if _, ok := top[k]; ok {
			newFormat = true
			break
		}
	}
	if !newFormat {
		out := clusterPresetMatchGoldenFile{Status: "ok", Matches: make(map[string]string, len(top))}
		for k, v := range top {
			s, ok := v.(string)
			if !ok {
				return clusterPresetMatchGoldenFile{}, fmt.Errorf("older golden: value for %q is not a string", k)
			}
			out.Matches[k] = s
		}
		return out, nil
	}
	var g clusterPresetMatchGoldenFile
	if err := yaml.Unmarshal(data, &g); err != nil {
		return clusterPresetMatchGoldenFile{}, err
	}
	if g.Status != "ok" && g.Status != "invalid" {
		return clusterPresetMatchGoldenFile{}, fmt.Errorf("golden status must be ok or invalid, got %q", g.Status)
	}
	if g.Matches == nil {
		g.Matches = map[string]string{}
	}
	return g, nil
}

func marshalClusterPresetMatchGolden(g clusterPresetMatchGoldenFile) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(clusterPresetGoldenFileHeader)
	buf.WriteByte('\n')
	fmt.Fprintf(&buf, "status: %s\n", g.Status)

	if len(g.MissingEntityIDs) > 0 {
		b, err := yaml.Marshal(map[string]any{"missingEntityIds": g.MissingEntityIDs})
		if err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimSuffix(b, []byte("\n")))
		buf.WriteByte('\n')
	}
	if len(g.AmbiguousEntityMatches) > 0 {
		b, err := yaml.Marshal(map[string]any{"ambiguousEntityMatches": g.AmbiguousEntityMatches})
		if err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimSuffix(b, []byte("\n")))
		buf.WriteByte('\n')
	}

	keys := slices.Sorted(maps.Keys(g.Matches))
	if len(keys) == 0 {
		buf.WriteString("matches: {}\n")
		return buf.Bytes(), nil
	}
	buf.WriteString("matches:\n")
	one := make(map[string]string, 1)
	for _, k := range keys {
		one[k] = g.Matches[k]
		chunk, err := yaml.Marshal(one)
		if err != nil {
			return nil, err
		}
		for _, line := range bytes.Split(bytes.TrimSpace(chunk), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			buf.WriteString("  ")
			buf.Write(line)
			buf.WriteByte('\n')
		}
		delete(one, k)
	}
	return buf.Bytes(), nil
}

func collectExclusivePresetMatch(
	cache *ClusterDefaultsPresetEvalCache,
	entities []entity.Entity,
	presetClosure workloadclosure.MatchInput,
) (
	matches map[string]string,
	missing []string,
	ambiguous []ambiguousEntityMatch,
	err error,
) {
	matches = make(map[string]string, len(entities))
	for _, e := range entities {
		eid, err := e.Id()
		if err != nil {
			return nil, nil, nil, err
		}
		id := string(eid)
		pids, err := cache.MatchingPresetIDsWithRegarding(e, presetClosure, nil)
		if err != nil {
			return nil, nil, nil, err
		}
		switch len(pids) {
		case 0:
			missing = append(missing, id)
		case 1:
			matches[id] = pids[0]
		default:
			slices.Sort(pids)
			ambiguous = append(ambiguous, ambiguousEntityMatch{EntityID: id, Presets: pids})
		}
	}
	slices.Sort(missing)
	slices.SortFunc(ambiguous, func(a, b ambiguousEntityMatch) int {
		return strings.Compare(a.EntityID, b.EntityID)
	})
	return matches, missing, ambiguous, nil
}

var clusterPresetCELSingleIdEquals = regexp.MustCompile(`^\s*id\s*==\s*"([^"]+)"\s*$`)

func collectEnabledPresetCompletenessIssues(
	effective []ClusterDefaultsPresetEffective,
	env cel.Env,
	ents entity.Entities,
	k8sMinor int,
) (missingRequiredIDs []string, missingRequiredCEL []string, err error) {
	for _, eff := range effective {
		if !eff.Enabled {
			continue
		}
		omittedCelExplicitIDs := OmittedExplicitIdsForKubernetesMinor(eff, k8sMinor)
		predicateNames := slices.Sorted(maps.Keys(eff.Predicates))
		for _, predicateName := range predicateNames {
			pe := eff.Predicates[predicateName]
			if !pe.Enabled || !ClusterDefaultsPredicateMinorApplies(k8sMinor, pe) {
				continue
			}
			for _, idl := range pe.Ids {
				if idl.Optional || ents.IdSet.Has(types.Id(idl.Id)) {
					continue
				}
				missingRequiredIDs = append(missingRequiredIDs, fmt.Sprintf("%s/%s: %s", eff.ID, predicateName, idl.Id))
			}
			for i, line := range pe.CelLines {
				if line.Optional {
					continue
				}
				if m := clusterPresetCELSingleIdEquals.FindStringSubmatch(line.Expr); len(m) > 1 {
					if omittedCelExplicitIDs.Has(types.Id(m[1])) {
						continue
					}
				}
				var matched []types.Id
				var matchErr error
				if strings.TrimSpace(line.Expr) == "" {
					if line.Selector.IsZero() {
						continue
					}
					pred, cerr := env.CompileSelectedPredicate(line.Selector)
					if cerr != nil {
						matchErr = cerr
					} else {
						_, matchedEnts, selErr := pred.Select(ents)
						if selErr != nil {
							matchErr = selErr
						} else {
							matched = make([]types.Id, 0, matchedEnts.Len())
							for _, e := range matchedEnts.Items {
								id, idErr := e.Id()
								if idErr != nil {
									matchErr = idErr
									break
								}
								matched = append(matched, id)
							}
						}
					}
				} else {
					celRun := strings.TrimSpace(line.Expr)
					if !line.Selector.IsZero() {
						celRun = string(line.Selector.MatchPredicate(types.CelPredicate(celRun)))
					}
					matched, matchErr = MatchingEntityIdsForCEL(env, ents, celRun)
				}
				if matchErr != nil {
					return nil, nil, fmt.Errorf("preset %s predicate %s cel[%d]: %w", eff.ID, predicateName, i, matchErr)
				}
				if len(matched) == 0 {
					detail := line.Expr
					if strings.TrimSpace(detail) == "" {
						detail = string(line.Selector.CelPredicate())
					}
					missingRequiredCEL = append(missingRequiredCEL,
						fmt.Sprintf("%s/%s cel[%d]: %s", eff.ID, predicateName, i, detail))
				}
			}
		}
	}
	slices.Sort(missingRequiredIDs)
	slices.Sort(missingRequiredCEL)
	return missingRequiredIDs, missingRequiredCEL, nil
}

func entityIDFromMissingRequiredPresetID(detail string) string {
	_, id, ok := strings.Cut(detail, ": ")
	if !ok {
		return detail
	}
	return id
}

func clusterPresetMatchGoldenMatches(
	gotMatches map[string]string,
	missing []string,
	ambiguous []ambiguousEntityMatch,
	missingRequiredIDs []string,
) map[string]string {
	out := maps.Clone(gotMatches)
	for _, id := range missing {
		out[id] = clusterPresetMatchMissingValue
	}
	for _, detail := range missingRequiredIDs {
		out[entityIDFromMissingRequiredPresetID(detail)] = clusterPresetMatchMissingValue
	}
	for _, match := range ambiguous {
		out[match.EntityID] = strings.Join(match.Presets, ", ")
	}
	return out
}

func cloneHydraPresetsSection(p *types.HydraPresetsSection) *types.HydraPresetsSection {
	if p == nil {
		return &types.HydraPresetsSection{}
	}
	raw, err := yaml.Marshal(p)
	if err != nil {
		panic(err)
	}
	var out types.HydraPresetsSection
	if err := yaml.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return &out
}

func clusterFixturePresetIDValid(presetID string) error {
	presetID = strings.TrimSpace(presetID)
	if presetID == "" {
		return fmt.Errorf("empty preset id")
	}
	if _, err := BuiltinClusterDefaultsPresetFile(presetID); err != nil {
		return fmt.Errorf("unknown preset id %q", presetID)
	}
	return nil
}

func TestClusterFixturePresetIds_BangDisablesCoredns(t *testing.T) {
	t.Parallel()
	merged := cloneHydraPresetsSection(nil)
	require.NoError(t, applyClusterFixturePresetIDs(merged, []string{"!coredns"}))
	eff, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	for _, e := range eff {
		if e.ID == ClusterDefaultsPresetIDCoredns {
			require.False(t, e.Enabled)
			return
		}
	}
	t.Fatal("coredns preset missing from EffectiveClusterDefaultsPresets")
}

func TestClusterFixturePresetIds_BangBlocksActivationFromEnabledPreset(t *testing.T) {
	t.Parallel()
	merged := cloneHydraPresetsSection(nil)
	require.NoError(t, applyClusterFixturePresetIDs(merged, []string{"k3s", "!k3s-addon-traefik"}))
	eff, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	byID := make(map[string]ClusterDefaultsPresetEffective, len(eff))
	for _, e := range eff {
		byID[e.ID] = e
	}
	require.True(t, byID[ClusterDefaultsPresetIDK3s].Enabled)
	require.False(t, byID["k3s-addon-traefik"].Enabled)
}

func TestEffectiveClusterDefaultsPresets_ExplicitFalseBlocksActivation(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false
	merged := &types.HydraPresetsSection{
		ClusterDefaultsPresetIDK3s: {
			Enabled: &enabled,
		},
		"k3s-addon-traefik": {
			Enabled: &disabled,
		},
	}
	eff, err := EffectiveClusterDefaultsPresets(merged)
	require.NoError(t, err)
	byID := make(map[string]ClusterDefaultsPresetEffective, len(eff))
	for _, e := range eff {
		byID[e.ID] = e
	}
	require.True(t, byID[ClusterDefaultsPresetIDK3s].Enabled)
	require.False(t, byID["k3s-addon-traefik"].Enabled)
	require.True(t, byID["k3s-addon-traefik"].ExplicitlyDisabled)
}

func applyClusterFixturePresetIDs(s *types.HydraPresetsSection, presetIDs []string) error {
	var enableIDs []string
	var disableIDs []string
	for _, raw := range presetIDs {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		disable := strings.HasPrefix(line, "!")
		id := strings.TrimSpace(strings.TrimPrefix(line, "!"))
		if id == "" {
			return fmt.Errorf("empty preset id in presetIds entry %q", raw)
		}
		if err := clusterFixturePresetIDValid(id); err != nil {
			return err
		}
		on := !disable
		over := s.BuiltinClusterDefaultsOverride(id)
		if over == nil {
			over = &types.HydraPresetGroupOverride{}
		}
		over.Enabled = &on
		s.SetBuiltinClusterDefaultsOverride(id, over)
		if disable {
			disableIDs = append(disableIDs, id)
		} else {
			enableIDs = append(enableIDs, id)
		}
	}
	for _, enableID := range enableIDs {
		over := s.BuiltinClusterDefaultsOverride(enableID)
		if over == nil {
			continue
		}
		if over.Activates == nil {
			builtin, err := BuiltinClusterDefaultsPresetFile(enableID)
			if err != nil {
				return err
			}
			over.Activates = append(types.PresetActivateList(nil), builtin.Activates...)
		}
		for _, disableID := range disableIDs {
			over.Activates = append(over.Activates, types.PresetActivateItem{
				Preset:  disableID,
				Exclude: true,
			})
		}
	}
	return nil
}

func presetWorkloadClosureForGolden(ents entity.Entities) (workloadclosure.MatchInput, error) {
	if ents.Len() == 0 {
		return workloadclosure.EmptyMatchInput(types.KeyClusterEntity), nil
	}
	silent := log.NewLoggerWithHandler(slog.DiscardHandler)
	refs, err := references.Refs(silent, ents, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil)
	if err != nil {
		return workloadclosure.MatchInput{}, err
	}
	return workloadclosure.NewMatchInput(refs, ents, types.KeyClusterEntity)
}

func entityFromInventoryEntityID(id types.Id) (entity.Entity, error) {
	g, ver, k, ns, name, err := id.Components()
	if err != nil {
		return entity.Entity{}, err
	}
	gvk := types.NewGVK(g, ver, k)
	b := entity.NewEntityBuilder().WithGVK(gvk).WithNamespace(ns).WithName(name)
	return b.Build()
}

func TestClusterPresetMatchesGolden(t *testing.T) {
	t.Parallel()
	var bases []string
	err := fs.WalkDir(clusterPresetMatchGivenFS, "testdata/cluster_preset_matches", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if base, ok := strings.CutSuffix(path, ".given.yaml"); ok {
			bases = append(bases, base)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, bases, "no *.given.yaml under testdata/cluster_preset_matches")

	slices.Sort(bases)
	for _, base := range bases {
		name := strings.TrimPrefix(base, "testdata/cluster_preset_matches/")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			givenBytes, err := clusterPresetMatchGivenFS.ReadFile(base + ".given.yaml")
			require.NoError(t, err)
			var fix clusterPresetMatchFixture
			require.NoError(t, yaml.Unmarshal(givenBytes, &fix))
			require.Equal(t, 1, fix.SchemaVersion, "fixture %s: schemaVersion must be 1", name)
			require.NotZero(t, fix.KubernetesMinor, "fixture %s: kubernetesMinor required", name)
			require.NotEmpty(t, fix.EntityIds, "fixture %s: entityIds required", name)

			for _, raw := range fix.PresetIds {
				line := strings.TrimSpace(raw)
				require.NotEmpty(t, line, "fixture %s: empty presetIds entry (raw=%q)", name, raw)
				if !strings.HasPrefix(line, "!") {
					id := line
					require.NotEqualf(t, ClusterDefaultsPresetIDCoredns, id,
						"fixture %s: omit %q from presetIds (on by default; use \"!coredns\" to disable)", name, id)
					require.NotEqualf(t, ClusterDefaultsPresetIDKubernetes, id,
						"fixture %s: omit %q from presetIds (on by default; use \"!kubernetes\" to disable)", name, id)
				}
			}

			merged := cloneHydraPresetsSection(fix.PresetSection)
			require.NoError(t, applyClusterFixturePresetIDs(merged, fix.PresetIds))
			eff, err := EffectiveClusterDefaultsPresetsForKubernetesMinor(merged, fix.KubernetesMinor)
			require.NoError(t, err)

			idx := make(map[string]bool, len(eff))
			for _, e := range eff {
				idx[e.ID] = e.Enabled
			}
			for _, raw := range fix.PresetIds {
				line := strings.TrimSpace(raw)
				if line == "" {
					continue
				}
				disable := strings.HasPrefix(line, "!")
				id := strings.TrimSpace(strings.TrimPrefix(line, "!"))
				if disable {
					require.Falsef(t, idx[id], "fixture %s: preset %q must be disabled after presetSection + presetIds", name, id)
				} else {
					require.Truef(t, idx[id], "fixture %s: preset %q must be enabled after presetSection + presetIds", name, id)
				}
			}

			seen := map[string]struct{}{}
			var entities []entity.Entity
			for _, line := range fix.EntityIds {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				require.NotContains(t, seen, line, "duplicate entity id %q", line)
				seen[line] = struct{}{}
				e, err := entityFromInventoryEntityID(types.Id(line))
				require.NoError(t, err, "id %q", line)
				entities = append(entities, e)
			}
			ents, err := entity.NewEntities(entities)
			require.NoError(t, err)
			env, err := cel.NewEnvWithEntityInventory(ents)
			require.NoError(t, err)
			cache, err := NewClusterDefaultsPresetEvalCache(eff, fix.KubernetesMinor, env)
			require.NoError(t, err)
			missingRequiredIDs, missingRequiredCEL, err := collectEnabledPresetCompletenessIssues(eff, env, ents, fix.KubernetesMinor)
			require.NoError(t, err, "fixture %s: enabled preset completeness", name)

			regarding, err := presetWorkloadClosureForGolden(ents)
			require.NoError(t, err)

			gotMatches, missing, ambiguous, err := collectExclusivePresetMatch(cache, entities, regarding)
			require.NoError(t, err)

			_, thisFile, _, ok := runtime.Caller(0)
			require.True(t, ok)
			pkgDir := filepath.Dir(thisFile)
			expectedFile := filepath.Join(pkgDir, "testdata/cluster_preset_matches", name+".expected.yaml")

			if !*updateGolden {
				require.Empty(t, missingRequiredIDs, "fixture %s: required preset ids missing from entityIds", name)
				require.Empty(t, missingRequiredCEL, "fixture %s: required preset CEL predicates without any entityIds match", name)
			}

			if *updateGolden {
				matches := clusterPresetMatchGoldenMatches(gotMatches, missing, ambiguous, missingRequiredIDs)
				var out clusterPresetMatchGoldenFile
				if len(missing) == 0 && len(ambiguous) == 0 && len(missingRequiredIDs) == 0 && len(missingRequiredCEL) == 0 {
					require.Len(t, gotMatches, len(entities), "fixture %s: each entity id must be unique (duplicate lines map to the same id)", name)
					out = clusterPresetMatchGoldenFile{
						Status:  "ok",
						Matches: matches,
					}
				} else {
					out = clusterPresetMatchGoldenFile{
						Status:                 "invalid",
						Matches:                matches,
						MissingEntityIDs:       missing,
						AmbiguousEntityMatches: ambiguous,
					}
				}
				outBytes, err := marshalClusterPresetMatchGolden(out)
				require.NoError(t, err)
				err = os.WriteFile(expectedFile, outBytes, 0o644)
				require.NoError(t, err)
				t.Logf("updated golden: %s", expectedFile)
				return
			}

			expBytes, err := os.ReadFile(expectedFile)
			if err != nil {
				t.Fatalf("missing %s (from hydra/hydra-go: go test -count=1 -run TestClusterPresetMatchesGolden ./core/hydra -update)", expectedFile)
			}
			golden, err := parseClusterPresetMatchGolden(expBytes)
			require.NoError(t, err)

			if golden.Status == "invalid" {
				t.Fatalf("invalid golden file %s (status=invalid: missingEntityIds=%d ambiguousEntityMatches=%d). "+
					"Fix presets/fixtures and regenerate: go test -count=1 -run TestClusterPresetMatchesGolden ./core/hydra -update",
					expectedFile, len(golden.MissingEntityIDs), len(golden.AmbiguousEntityMatches))
			}

			require.Equal(t, "ok", golden.Status, "fixture %s: golden status", name)
			require.Empty(t, missing, "fixture %s: entities with no matching preset", name)
			require.Empty(t, ambiguous, "fixture %s: entities with multiple matching presets", name)
			require.Len(t, golden.Matches, len(seen), "fixture %s: expected file must list one entry per fixture entity id", name)
			for id := range seen {
				require.Contains(t, golden.Matches, id, "fixture %s: golden missing entity %q", name, id)
			}
			require.Equal(t, golden.Matches, gotMatches, "golden mismatch for %s", name)
		})
	}
}

// TestRegenerateTalosClusterPresetFromRepoRootTalos writes testdata/cluster_preset_matches/talos.given.yaml
// and talos.expected.yaml from ../../../../talos.txt (workspace root, four levels up from this package), keeping
// only entity ids that map to exactly one enabled **builtin default** preset (coredns + kubernetes; all others
// use their defaultEnabled, typically off).
//
// Run from directory hydra/hydra-go/core:
//
//	HYDRA_REGEN_TALOS_PRESET_GOLD=1 go test -count=1 -run TestRegenerateTalosClusterPresetFromRepoRootTalos ./hydra
func TestRegenerateTalosClusterPresetFromRepoRootTalos(t *testing.T) {
	if os.Getenv("HYDRA_REGEN_TALOS_PRESET_GOLD") == "" {
		t.Skip("set HYDRA_REGEN_TALOS_PRESET_GOLD=1 to write testdata (expects ../../../../talos.txt from package dir)")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pkgDir := filepath.Dir(thisFile)
	talosFile := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", "..", "talos.txt"))
	b, err := os.ReadFile(talosFile)
	require.NoError(t, err, "read %s", talosFile)

	var allIds []string
	for _, line := range bytes.Split(b, []byte("\n")) {
		s := strings.TrimSpace(string(line))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		require.NotContains(t, allIds, s, "duplicate in talos.txt: %q", s)
		allIds = append(allIds, s)
	}
	require.NotEmpty(t, allIds, "no entity ids in %s", talosFile)

	// Aligned with typical 1.33+ clusters that include bootstrap-audit-m33 RBAC.
	const k8sMinor = 35

	merged := cloneHydraPresetsSection(nil)
	require.NoError(t, applyClusterFixturePresetIDs(merged, nil))
	eff, err := EffectiveClusterDefaultsPresetsForKubernetesMinor(merged, k8sMinor)
	require.NoError(t, err)
	allEnts := make([]entity.Entity, 0, len(allIds))
	for _, idstr := range allIds {
		e, err := entityFromInventoryEntityID(types.Id(idstr))
		require.NoError(t, err, "id %q", idstr)
		allEnts = append(allEnts, e)
	}
	inv, err := entity.NewEntities(allEnts)
	require.NoError(t, err)
	fullEnv, err := cel.NewEnvWithEntityInventory(inv)
	require.NoError(t, err)
	fullCache, err := NewClusterDefaultsPresetEvalCache(eff, k8sMinor, fullEnv)
	require.NoError(t, err)
	var oneToOne []string
	var nZero, nMulti int
	for i, e := range allEnts {
		pids, err2 := fullCache.MatchingPresetIDs(e)
		require.NoError(t, err2, "entity %d", i)
		idstr := allIds[i]
		switch len(pids) {
		case 0:
			nZero++
		case 1:
			oneToOne = append(oneToOne, idstr)
		default:
			slices.Sort(pids)
			nMulti++
			t.Logf("skip multi-match: %q -> %v", idstr, pids)
		}
	}
	t.Logf("from %s: total=%d 1:1=%d zero=%d multi=%d", talosFile, len(allIds), len(oneToOne), nZero, nMulti)
	require.NotEmpty(t, oneToOne, "no 1:1 entity ids; relax presets or k8s minor")

	slices.Sort(oneToOne)
	fix := clusterPresetMatchFixture{
		SchemaVersion:   1,
		Cluster:         "talos",
		KubernetesMinor: k8sMinor,
		Description: "Inventory entity ids that match exactly one builtin default preset, derived from " +
			"repo root talos.txt; default presets only (coredns + kubernetes on; e.g. flannel/canal off). " +
			"Re-run: HYDRA_REGEN_TALOS_PRESET_GOLD=1 go test -count=1 -run TestRegenerateTalosClusterPresetFromRepoRootTalos ./hydra (from hydra/hydra-go/core).",
		EntityIds:     oneToOne,
		PresetIds:     nil,
		PresetSection: &types.HydraPresetsSection{},
	}
	givenBytes, err := yaml.Marshal(&fix)
	require.NoError(t, err)
	givenHeader := []byte(
		"# Fixture for TestClusterPresetMatchesGolden/talos (see TestRegenerateTalosClusterPresetFromRepoRootTalos in cluster_preset_matches_golden_test.go).\n" +
			"# Only default (builtin) preset enablement — no extra presetIds.\n\n",
	)
	givenPath := filepath.Join(pkgDir, "testdata/cluster_preset_matches", "talos.given.yaml")
	err = os.WriteFile(givenPath, append(givenHeader, givenBytes...), 0o644)
	require.NoError(t, err)

	byID := make(map[string]entity.Entity, len(allIds))
	for i, e := range allEnts {
		byID[allIds[i]] = e
	}
	ents := make([]entity.Entity, 0, len(oneToOne))
	for _, idstr := range oneToOne {
		ents = append(ents, byID[idstr])
	}
	eAll, err := entity.NewEntities(ents)
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(eAll)
	require.NoError(t, err)
	cache, err := NewClusterDefaultsPresetEvalCache(eff, k8sMinor, env)
	require.NoError(t, err)
	regarding, err := presetWorkloadClosureForGolden(eAll)
	require.NoError(t, err)
	outMap, missing, ambiguous, err := collectExclusivePresetMatch(cache, ents, regarding)
	require.NoError(t, err)
	require.Empty(t, missing, "filtered talos entities must all match exactly one preset")
	require.Empty(t, ambiguous, "filtered talos entities must not be ambiguous")
	golden := clusterPresetMatchGoldenFile{Status: "ok", Matches: outMap}
	outBytes, err := marshalClusterPresetMatchGolden(golden)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(pkgDir, "testdata/cluster_preset_matches", "talos.expected.yaml"), outBytes, 0o644)
	require.NoError(t, err)
	t.Logf("wrote %s and talos.expected.yaml (entities=%d)", givenPath, len(oneToOne))
}
