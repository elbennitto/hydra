package hydra

import (
	"cmp"
	"embed"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

//go:embed embed/presets/*.yaml
var clusterDefaultsPresetBuiltinYAML embed.FS

const (
	ClusterDefaultsPresetIDCoredns                      = "coredns"
	ClusterDefaultsPresetIDKubernetes                   = "kubernetes"
	ClusterDefaultsPresetIDKubernetesDRA                = "kubernetes-dynamic-resource-allocation"
	ClusterDefaultsPresetIDKubernetesVolumeAttrsClass   = "kubernetes-volume-attributes-class"
	ClusterDefaultsPresetIDKubeControllerManager        = "kube-controller-manager"
	ClusterDefaultsPresetIDKubeScheduler                = "kube-scheduler"
	ClusterDefaultsPresetIDKubeProxy                    = "kube-proxy"
	ClusterDefaultsPresetIDFlannel                      = "flannel"
	ClusterDefaultsPresetIDCalico                       = "calico"
	ClusterDefaultsPresetIDCanal                        = "canal"
	ClusterDefaultsPresetIDKubermatic                   = "kubermatic"
	ClusterDefaultsPresetIDGardener                     = "gardener"
	ClusterDefaultsPresetIDMonex                        = "monex"
	ClusterDefaultsPresetIDCloudPoc                     = "cloud-poc"
	ClusterDefaultsPresetIDSyseleven                    = "syseleven"
	ClusterDefaultsPresetIDMetakube                     = "metakube"
	ClusterDefaultsPresetIDSyselevenNodeProblemDetector = "syseleven-node-problem-detector"
	ClusterDefaultsPresetIDQuobyte                      = "quobyte"
	ClusterDefaultsPresetIDCloudinit                    = "cloudinit"
	ClusterDefaultsPresetIDCinder                       = "cinder"
	ClusterDefaultsPresetIDCinderController             = "cinder-controller"
	ClusterDefaultsPresetIDMetricsServer                = "metrics-server"
	ClusterDefaultsPresetIDLocalPathProvisioner         = "local-path-provisioner"
	ClusterDefaultsPresetIDK3s                          = "k3s"
	ClusterDefaultsPresetIDK3d                          = "k3d"
	ClusterDefaultsPresetIDTalos                        = "talos"
)

// clusterDefaultsBuiltinPresetIDsOrdered is the deterministic merge/effective order for embedded
// built-in cluster-defaults presets.
func clusterDefaultsBuiltinPresetIDsOrdered() []string {
	_, _ = loadBuiltinClusterDefaultsPresets()
	return append([]string(nil), builtinClusterDefaultsPresetIDs...)
}

type builtinClusterDefaultsPresetFile struct {
	ID             string                           `yaml:"id"`
	DefaultEnabled bool                             `yaml:"defaultEnabled"`
	Activates      types.PresetActivateList         `yaml:"activates,omitempty"`
	Predicates     map[string]builtinPredicateEntry `yaml:"predicates"`
}

type builtinPredicateEntry struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	// Cel is a sequence of CEL strings and/or {cel, optional: true} maps.
	Cel types.PresetCelList `yaml:"cel,omitempty"`
	// Ids is a sequence of resource id strings and/or {id, optional: true} maps.
	Ids types.PresetIdList `yaml:"ids,omitempty"`
	// 0 = unset (no gate), same convention as bootstrap RBAC minKubernetesMinor.
	KubernetesMinorMin int `yaml:"kubernetesMinorMin,omitempty"`
	KubernetesMinorMax int `yaml:"kubernetesMinorMax,omitempty"`
}

func stringLinesToPresetCelList(strs []string) types.PresetCelList {
	if len(strs) == 0 {
		return nil
	}
	out := make(types.PresetCelList, len(strs))
	for i, s := range strs {
		out[i] = types.PresetCelItem{Expr: s, Optional: false}
	}
	return out
}

var (
	builtinClusterDefaultsPresetsOnce sync.Once
	builtinClusterDefaultsPresets     map[string]builtinClusterDefaultsPresetFile
	builtinClusterDefaultsPresetIDs   []string
	builtinClusterDefaultsPresetsErr  error
)

// synthesizeKubernetesRbacCelFromBootstrapAuditIds fills rbac-cluster-roles and rbac-cluster-role-bindings
// CEL from bootstrap-audit* predicate ids when those predicates are not already defined in YAML.
func synthesizeKubernetesRbacCelFromBootstrapAuditIds(f *builtinClusterDefaultsPresetFile) {
	if f.ID != ClusterDefaultsPresetIDKubernetes || f.Predicates == nil {
		return
	}
	var crLines, crbLines []string
	pnames := make([]string, 0, len(f.Predicates))
	for n := range f.Predicates {
		pnames = append(pnames, n)
	}
	slices.Sort(pnames)
	for _, pname := range pnames {
		if !strings.HasPrefix(pname, "bootstrap-audit") {
			continue
		}
		for _, idl := range f.Predicates[pname].Ids {
			id := idl.Id
			switch {
			case strings.Contains(id, "/v1/ClusterRoleBinding//"):
				crbLines = append(crbLines, fmt.Sprintf(`id == %q`, id))
			case strings.Contains(id, "/v1/ClusterRole//"):
				crLines = append(crLines, fmt.Sprintf(`id == %q`, id))
			}
		}
	}
	slices.Sort(crLines)
	slices.Sort(crbLines)
	if len(crLines) > 0 {
		if _, ok := f.Predicates["rbac-cluster-roles"]; !ok {
			f.Predicates["rbac-cluster-roles"] = builtinPredicateEntry{Cel: stringLinesToPresetCelList(crLines)}
		}
	}
	if len(crbLines) > 0 {
		if _, ok := f.Predicates["rbac-cluster-role-bindings"]; !ok {
			f.Predicates["rbac-cluster-role-bindings"] = builtinPredicateEntry{Cel: stringLinesToPresetCelList(crbLines)}
		}
	}
}

func loadBuiltinClusterDefaultsPresets() (map[string]builtinClusterDefaultsPresetFile, error) {
	builtinClusterDefaultsPresetsOnce.Do(func() {
		const dir = "embed/presets"
		entries, err := clusterDefaultsPresetBuiltinYAML.ReadDir(dir)
		if err != nil {
			builtinClusterDefaultsPresetsErr = err
			return
		}
		out := make(map[string]builtinClusterDefaultsPresetFile, len(entries))
		orderedIDs := make([]string, 0, len(entries))
		for _, ent := range entries {
			if ent.IsDir() || filepath.Ext(ent.Name()) != ".yaml" {
				continue
			}
			data, err := clusterDefaultsPresetBuiltinYAML.ReadFile(dir + "/" + ent.Name())
			if err != nil {
				builtinClusterDefaultsPresetsErr = err
				return
			}
			var f builtinClusterDefaultsPresetFile
			if err := yaml.Unmarshal(data, &f); err != nil {
				builtinClusterDefaultsPresetsErr = err
				return
			}
			synthesizeKubernetesRbacCelFromBootstrapAuditIds(&f)
			if f.ID == "" {
				builtinClusterDefaultsPresetsErr = log.CreateError(errors.ErrHydraConfigError,
					"builtin cluster defaults preset file {file} has empty id", log.String("file", ent.Name()))
				return
			}
			predCount := 0
			if f.Predicates != nil {
				predCount = len(f.Predicates)
			}
			if predCount == 0 && len(f.Activates) == 0 {
				builtinClusterDefaultsPresetsErr = log.CreateError(errors.ErrHydraConfigError,
					"builtin cluster defaults preset {id} must define non-empty predicates and/or activates",
					log.String("id", f.ID))
				return
			}
			for pname, pe := range f.Predicates {
				if len(pe.Cel) == 0 && len(pe.Ids) == 0 {
					builtinClusterDefaultsPresetsErr = log.CreateError(errors.ErrHydraConfigError,
						"builtin cluster defaults preset {id} predicate {name} must set cel and/or ids",
						log.String("id", f.ID), log.String("name", pname))
					return
				}
				for i, line := range pe.Cel {
					if strings.TrimSpace(line.Expr) == "" && line.Selector.IsZero() {
						builtinClusterDefaultsPresetsErr = log.CreateError(errors.ErrHydraConfigError,
							"builtin cluster defaults preset {id} predicate {name} cel[{i}] is empty (set cel or a non-empty gvk/group/kind/… selector)",
							log.String("id", f.ID), log.String("name", pname), log.Int("i", i))
						return
					}
				}
				for i, idl := range pe.Ids {
					if strings.TrimSpace(idl.Id) == "" {
						builtinClusterDefaultsPresetsErr = log.CreateError(errors.ErrHydraConfigError,
							"builtin cluster defaults preset {id} predicate {name} ids[{i}] is empty",
							log.String("id", f.ID), log.String("name", pname), log.Int("i", i))
						return
					}
				}
			}
			if _, exists := out[f.ID]; exists {
				builtinClusterDefaultsPresetsErr = log.CreateError(errors.ErrHydraConfigError,
					"duplicate builtin cluster defaults preset id {id}", log.String("id", f.ID))
				return
			}
			out[f.ID] = f
			orderedIDs = append(orderedIDs, f.ID)
		}
		for id, f := range out {
			if err := types.ValidateHydraPresetActivates(id, f.Activates); err != nil {
				builtinClusterDefaultsPresetsErr = err
				return
			}
			if err := validateClusterDefaultsActivateTargetsExist(id, f.Activates, out); err != nil {
				builtinClusterDefaultsPresetsErr = err
				return
			}
		}
		adj := make(map[string][]string, len(out))
		for id, f := range out {
			adj[id] = clusterDefaultsPositiveActivateTargets(f.Activates)
		}
		if err := validateClusterDefaultsActivatesGraphAcyclic(adj); err != nil {
			builtinClusterDefaultsPresetsErr = err
			return
		}
		slices.Sort(orderedIDs)
		builtinClusterDefaultsPresets = out
		builtinClusterDefaultsPresetIDs = orderedIDs
	})
	return builtinClusterDefaultsPresets, builtinClusterDefaultsPresetsErr
}

// BuiltinClusterDefaultsPresetFile returns the embedded builtin definition for id (see clusterDefaultsBuiltinPresetIDsOrdered).
func BuiltinClusterDefaultsPresetFile(id string) (builtinClusterDefaultsPresetFile, error) {
	m, err := loadBuiltinClusterDefaultsPresets()
	if err != nil {
		return builtinClusterDefaultsPresetFile{}, err
	}
	f, ok := m[id]
	if !ok {
		return builtinClusterDefaultsPresetFile{}, log.CreateError(errors.ErrHydraConfigError,
			"unknown builtin cluster defaults preset {id}", log.String("id", id))
	}
	return f, nil
}

// MergeHydraPresetsSections merges Helm/ConfigMap preset overrides in deterministic app order, then global docs.
func MergeHydraPresetsSections(overlays []*types.HydraPresetsSection) *types.HydraPresetsSection {
	var out *types.HydraPresetsSection
	for _, o := range overlays {
		if o == nil {
			continue
		}
		out = mergeHydraPresetsSectionPair(out, o)
	}
	return out
}

func mergeHydraPresetsSectionPair(base *types.HydraPresetsSection, over *types.HydraPresetsSection) *types.HydraPresetsSection {
	if over == nil {
		return base
	}
	if base == nil {
		base = &types.HydraPresetsSection{}
	}
	for id, overGroup := range over.BuiltinClusterDefaultsOverrides() {
		base.SetBuiltinClusterDefaultsOverride(id, mergePresetGroupOverride(base.BuiltinClusterDefaultsOverride(id), overGroup))
	}
	return base
}

func mergePresetGroupOverride(base *types.HydraPresetGroupOverride, over *types.HydraPresetGroupOverride) *types.HydraPresetGroupOverride {
	if over == nil {
		return base
	}
	if base == nil {
		base = &types.HydraPresetGroupOverride{}
	}
	if over.Enabled != nil {
		base.Enabled = over.Enabled
	}
	if len(over.Activates) > 0 {
		base.Activates = append(types.PresetActivateList(nil), over.Activates...)
	}
	if len(over.Predicates) == 0 {
		return base
	}
	if base.Predicates == nil {
		base.Predicates = make(map[string]types.HydraPresetPredicateEntry)
	}
	for name, pe := range over.Predicates {
		cur := base.Predicates[name]
		if pe.Enabled != nil {
			cur.Enabled = pe.Enabled
		}
		if len(pe.Cel) > 0 {
			cur.Cel = append(append(([]types.PresetCelItem)(nil), cur.Cel...), pe.Cel...)
		}
		// Deprecated optionalCel is merged into Cel by YAML unmarshal; rely on pe.Cel only.
		if len(pe.Ids) > 0 {
			cur.Ids = append(append(([]types.PresetIdItem)(nil), cur.Ids...), pe.Ids...)
		}
		if pe.KubernetesMinorMin != nil {
			cur.KubernetesMinorMin = pe.KubernetesMinorMin
		}
		if pe.KubernetesMinorMax != nil {
			cur.KubernetesMinorMax = pe.KubernetesMinorMax
		}
		base.Predicates[name] = cur
	}
	return base
}

// clusterDefaultsPositiveActivateTargets returns only force-enable targets (entries without a leading "!").
func clusterDefaultsPositiveActivateTargets(activates types.PresetActivateList) []string {
	return clusterDefaultsPositiveActivateTargetsForMinor(activates, 0)
}

func clusterDefaultsActivateApplies(k8sMinor int, item types.PresetActivateItem) bool {
	m := k8sMinor
	if m <= 0 {
		m = 99
	}
	if item.KubernetesMinorMin > 0 && m < item.KubernetesMinorMin {
		return false
	}
	if item.KubernetesMinorMax > 0 && m > item.KubernetesMinorMax {
		return false
	}
	return true
}

func clusterDefaultsPositiveActivateTargetsForMinor(activates types.PresetActivateList, k8sMinor int) []string {
	if len(activates) == 0 {
		return nil
	}
	out := make([]string, 0, len(activates))
	for _, item := range activates {
		if item.Exclude || !clusterDefaultsActivateApplies(k8sMinor, item) {
			continue
		}
		if s := strings.TrimSpace(item.Preset); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func validateClusterDefaultsActivateTargetsExist(
	presetID string,
	activates types.PresetActivateList,
	known map[string]builtinClusterDefaultsPresetFile,
) error {
	for i, item := range activates {
		target := strings.TrimSpace(item.Preset)
		if target == "" {
			continue
		}
		if _, ok := known[target]; !ok {
			return log.CreateError(errors.ErrHydraConfigError,
				"hydra configuration validation failed: presets.{preset}.activates[{index}] must reference a builtin cluster defaults preset id",
				log.String("preset", presetID), log.Int("index", i), log.String("target", target))
		}
	}
	return nil
}

// validateClusterDefaultsActivatesGraphAcyclic rejects directed cycles in the activates graph (edge u→v means preset u activates v).
func validateClusterDefaultsActivatesGraphAcyclic(adj map[string][]string) error {
	nodes := sets.New[string]()
	for u, vs := range adj {
		nodes.Insert(u)
		for _, v := range vs {
			v = strings.TrimSpace(v)
			if v != "" {
				nodes.Insert(v)
			}
		}
	}
	fullAdj := make(map[string][]string, nodes.Len())
	for u := range nodes {
		if vs, ok := adj[u]; ok {
			fullAdj[u] = append([]string(nil), vs...)
		} else {
			fullAdj[u] = nil
		}
	}
	state := make(map[string]uint8) // 0=unvisited, 1=in stack, 2=done
	var visit func(u string) error
	visit = func(u string) error {
		switch state[u] {
		case 1:
			return log.CreateError(errors.ErrHydraConfigError,
				"cluster defaults presets activates graph must be acyclic; cycle involves preset {preset}",
				log.String("preset", u))
		case 2:
			return nil
		}
		state[u] = 1
		for _, v := range fullAdj[u] {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if err := visit(v); err != nil {
				return err
			}
		}
		state[u] = 2
		return nil
	}
	for u := range nodes {
		if state[u] == 0 {
			if err := visit(u); err != nil {
				return err
			}
		}
	}
	return nil
}

// ClusterDefaultsPresetEffective is the builtin + merged Helm/ConfigMap result for one preset id.
type ClusterDefaultsPresetEffective struct {
	ID                 string
	Enabled            bool
	ExplicitlyDisabled bool
	Activates          types.PresetActivateList
	Predicates         map[string]ClusterDefaultsPredicateEffective
	ManualOverrides    []types.ClusterRootAppOverrideItem
	BuiltinFile        builtinClusterDefaultsPresetFile
}

// ClusterDefaultsCelLine is one CEL row in a named cluster-defaults predicate. Optional lines may
// have no inventory match without failing `hydra gitops system` (shown as "optional" when missing).
type ClusterDefaultsCelLine = types.PresetCelItem

// ClusterDefaultsIdLine is one explicit id row (optional ids are not required in cluster inventory).
type ClusterDefaultsIdLine = types.PresetIdItem

// ClusterDefaultsPredicateEffective is one named predicate after merge.
type ClusterDefaultsPredicateEffective struct {
	Enabled  bool
	CelLines []ClusterDefaultsCelLine
	Ids      []ClusterDefaultsIdLine
	// 0 = unset (no gate).
	KubernetesMinorMin int
	KubernetesMinorMax int
}

// EffectiveClusterDefaultsPresets builds merged preset definitions for all builtin cluster-defaults presets (see clusterDefaultsBuiltinPresetIDsOrdered).
func EffectiveClusterDefaultsPresets(merged *types.HydraPresetsSection) ([]ClusterDefaultsPresetEffective, error) {
	return EffectiveClusterDefaultsPresetsForKubernetesMinor(merged, 0)
}

func EffectiveClusterDefaultsPresetsForKubernetesMinor(merged *types.HydraPresetsSection, k8sMinor int) ([]ClusterDefaultsPresetEffective, error) {
	return EffectiveClusterDefaultsPresetsForKubernetesMinorWithOverrides(merged, nil, k8sMinor)
}

func EffectiveClusterDefaultsPresetsForKubernetesMinorWithOverrides(
	merged *types.HydraPresetsSection,
	rootOverrides map[types.AppId]types.ClusterRootAppOverrideList,
	k8sMinor int,
) ([]ClusterDefaultsPresetEffective, error) {
	builtins, err := loadBuiltinClusterDefaultsPresets()
	if err != nil {
		return nil, err
	}
	if merged != nil {
		for id := range merged.BuiltinClusterDefaultsOverrides() {
			if _, ok := builtins[id]; !ok {
				return nil, log.CreateError(errors.ErrHydraConfigError,
					"unknown builtin cluster defaults preset {id}", log.String("id", id))
			}
		}
	}
	var out []ClusterDefaultsPresetEffective
	for _, id := range clusterDefaultsBuiltinPresetIDsOrdered() {
		b := builtins[id]
		var ovr *types.HydraPresetGroupOverride
		if merged != nil {
			ovr = merged.BuiltinClusterDefaultsOverride(id)
		}
		eff := effectiveClusterDefaultsPresetFromBuiltin(b, ovr)
		for appID, items := range rootOverrides {
			if !appID.IsPresetApp() {
				continue
			}
			childName, childErr := appID.ChildAppName()
			if childErr != nil || childName == nil || string(*childName) != id {
				continue
			}
			eff.ManualOverrides = append([]types.ClusterRootAppOverrideItem(nil), items...)
			break
		}
		out = append(out, eff)
	}
	adj := make(map[string][]string, len(out))
	for i := range out {
		adj[out[i].ID] = clusterDefaultsPositiveActivateTargets(out[i].Activates)
		if err := validateClusterDefaultsActivateTargetsExist(out[i].ID, out[i].Activates, builtins); err != nil {
			return nil, err
		}
	}
	if err := validateClusterDefaultsActivatesGraphAcyclic(adj); err != nil {
		return nil, err
	}
	applyClusterDefaultsPresetActivations(out, k8sMinor)
	if err := validateClusterDefaultsActivatesMutualExclusions(out, k8sMinor); err != nil {
		return nil, err
	}
	return out, nil
}

func EffectiveClusterDefaultsPresetsForCluster(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
	k8sMinor int,
) ([]ClusterDefaultsPresetEffective, error) {
	var mergedPresets *types.HydraPresetsSection
	var err error
	if cluster != nil && appIds != nil && appIds.Len() > 0 {
		mergedPresets, err = HydraMergedClusterDefaultsPresetsSection(cluster, appIds, networkMode, rendered)
		if err != nil {
			return nil, err
		}
	}
	rootOverrides, err := ClusterRootAppOverrides(cluster)
	if err != nil {
		return nil, err
	}
	return EffectiveClusterDefaultsPresetsForKubernetesMinorWithOverrides(mergedPresets, rootOverrides, k8sMinor)
}

// applyClusterDefaultsPresetActivations turns on presets listed in Activates for every currently enabled
// preset, in a fixpoint pass (supports transitive activation). Runs after per-preset enabled merges
// (builtin defaultEnabled + Helm/ConfigMap presets.<id>.enabled). Entries prefixed with "!" block
// transitive activation of the excluded target; explicit/default-enabled conflicts are validated afterward.
func applyClusterDefaultsPresetActivations(effs []ClusterDefaultsPresetEffective, k8sMinor int) {
	idx := make(map[string]int, len(effs))
	initiallyEnabled := make([]bool, len(effs))
	explicitlyDisabled := sets.New[string]()
	for i := range effs {
		idx[effs[i].ID] = i
		initiallyEnabled[i] = effs[i].Enabled
		if effs[i].ExplicitlyDisabled {
			explicitlyDisabled.Insert(effs[i].ID)
		}
	}
	posByPreset := make([][]string, len(effs))
	negByPreset := make([][]string, len(effs))
	for i := range effs {
		posByPreset[i] = clusterDefaultsPositiveActivateTargetsForMinor(effs[i].Activates, k8sMinor)
		for _, item := range effs[i].Activates {
			if !item.Exclude || !clusterDefaultsActivateApplies(k8sMinor, item) {
				continue
			}
			if s := strings.TrimSpace(item.Preset); s != "" {
				negByPreset[i] = append(negByPreset[i], s)
			}
		}
	}
	maxRounds := len(effs) + 5
	for round := 0; round < maxRounds; round++ {
		changed := false
		excluded := sets.New[string]()
		for i := range effs {
			if !effs[i].Enabled {
				continue
			}
			excluded.Insert(negByPreset[i]...)
		}
		for target := range excluded {
			j, ok := idx[target]
			if ok && effs[j].Enabled && !initiallyEnabled[j] {
				effs[j].Enabled = false
				changed = true
			}
		}
		for i := range effs {
			if !effs[i].Enabled {
				continue
			}
			for _, tgt := range posByPreset[i] {
				tgt = strings.TrimSpace(tgt)
				if tgt == "" {
					continue
				}
				if excluded.Has(tgt) {
					continue
				}
				if explicitlyDisabled.Has(tgt) {
					continue
				}
				j, ok := idx[tgt]
				if !ok || effs[j].Enabled {
					continue
				}
				effs[j].Enabled = true
				excluded.Insert(negByPreset[j]...)
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// validateClusterDefaultsActivatesMutualExclusions rejects configurations where an enabled preset lists
// "!target" while the target preset is also enabled (after transitive positive activation).
func validateClusterDefaultsActivatesMutualExclusions(effs []ClusterDefaultsPresetEffective, k8sMinor int) error {
	idx := make(map[string]int, len(effs))
	for i := range effs {
		idx[effs[i].ID] = i
	}
	for i := range effs {
		if !effs[i].Enabled {
			continue
		}
		for _, raw := range effs[i].Activates {
			if !raw.Exclude || !clusterDefaultsActivateApplies(k8sMinor, raw) {
				continue
			}
			targetID := raw.Preset
			j, ok := idx[targetID]
			if !ok {
				continue
			}
			if effs[j].Enabled {
				return log.CreateError(errors.ErrHydraConfigError,
					"cluster defaults preset {activator} excludes preset {target}, but {target} is enabled",
					log.String("activator", effs[i].ID), log.String("target", targetID))
			}
		}
	}
	return nil
}

func effectiveClusterDefaultsPresetFromBuiltin(b builtinClusterDefaultsPresetFile, ovr *types.HydraPresetGroupOverride) ClusterDefaultsPresetEffective {
	enabled := b.DefaultEnabled
	explicitlyDisabled := false
	if ovr != nil && ovr.Enabled != nil {
		enabled = *ovr.Enabled
		explicitlyDisabled = !*ovr.Enabled
	}
	activates := append(types.PresetActivateList(nil), b.Activates...)
	if ovr != nil && len(ovr.Activates) > 0 {
		activates = append(types.PresetActivateList(nil), ovr.Activates...)
	}
	preds := make(map[string]ClusterDefaultsPredicateEffective)
	if b.Predicates != nil {
		preds = make(map[string]ClusterDefaultsPredicateEffective, len(b.Predicates))
	}
	for name, be := range b.Predicates {
		lines := make([]ClusterDefaultsCelLine, 0, len(be.Cel))
		for _, it := range be.Cel {
			lines = append(lines, ClusterDefaultsCelLine(it))
		}
		pe := ClusterDefaultsPredicateEffective{
			Enabled:            true,
			CelLines:           lines,
			Ids:                append([]ClusterDefaultsIdLine(nil), be.Ids...),
			KubernetesMinorMin: be.KubernetesMinorMin,
			KubernetesMinorMax: be.KubernetesMinorMax,
		}
		if be.Enabled != nil {
			pe.Enabled = *be.Enabled
		}
		preds[name] = pe
	}
	if ovr != nil {
		for name, oe := range ovr.Predicates {
			cur, ok := preds[name]
			if !ok {
				cur = ClusterDefaultsPredicateEffective{Enabled: true}
			}
			if oe.Enabled != nil {
				cur.Enabled = *oe.Enabled
			}
			// If cel is set, replace the whole CEL list; optionalCel in the same overlay is appended (legacy). If only optionalCel (deprecated) is set, append to builtin CEL.
			if len(oe.Cel) > 0 {
				var ovrLines []ClusterDefaultsCelLine
				for _, it := range oe.Cel {
					ovrLines = append(ovrLines, ClusterDefaultsCelLine(it))
				}
				cur.CelLines = ovrLines
			}
			if len(oe.Ids) > 0 {
				cur.Ids = append([]ClusterDefaultsIdLine(nil), oe.Ids...)
			}
			if oe.KubernetesMinorMin != nil {
				cur.KubernetesMinorMin = *oe.KubernetesMinorMin
			}
			if oe.KubernetesMinorMax != nil {
				cur.KubernetesMinorMax = *oe.KubernetesMinorMax
			}
			preds[name] = cur
		}
	}
	return ClusterDefaultsPresetEffective{
		ID:                 b.ID,
		Enabled:            enabled,
		ExplicitlyDisabled: explicitlyDisabled,
		Activates:          activates,
		Predicates:         preds,
		BuiltinFile:        b,
	}
}

// HydraMergedClusterDefaultsPresetsSection merges global.hydra presets from all apps (Helm + ConfigMaps) like other hydra merges.
func HydraMergedClusterDefaultsPresetsSection(
	cluster *Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	rendered entity.Entities,
) (*types.HydraPresetsSection, error) {
	helmIds := helmChartBackedAppIds(appIds)
	if helmIds.Len() == 0 {
		return nil, nil
	}
	perApp, global, err := PartitionHydraConfigDocumentsByApp(rendered, types.KeyTemplateEntity, helmIds)
	if err != nil {
		return nil, err
	}
	appOrder := helmIds.UnsortedList()
	slices.SortFunc(appOrder, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })

	var overlays []*types.HydraPresetsSection
	for _, appId := range appOrder {
		h, err := cluster.WithApp(appId)
		if err != nil {
			return nil, err
		}
		hv, err := HydraValues(h, networkMode)
		if err != nil {
			return nil, err
		}
		helmMap, err := HelmHydraMapFromValues(hv)
		if err != nil {
			return nil, err
		}
		docs := HydraConfigMapDocumentsForApp(perApp, global, appIds, appId)
		merged := MergeHelmHydraWithConfigMapDocuments(helmMap, docs)
		hv2, err := hydraValuesFromMergedMapLoose(merged)
		if err != nil {
			return nil, err
		}
		if hv2 != nil && hv2.Presets != nil {
			overlays = append(overlays, hv2.Presets)
		}
	}
	return MergeHydraPresetsSections(overlays), nil
}
