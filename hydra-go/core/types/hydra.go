package types

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

type HydraGlobal struct {
	Global *struct {
		Hydra *HydraValues `yaml:"hydra"`
	} `yaml:"global"`
}

type HydraValues struct {
	KubernetesVersion     KubernetesVersion                 `yaml:"kubernetesVersion"`
	AdditionalSourceRepos []string                          `yaml:"additionalSourceRepos"`
	Cluster               string                            `yaml:"cluster"`
	Path                  string                            `yaml:"path"`
	Repository            string                            `yaml:"repository"`
	Revision              string                            `yaml:"revision"`
	Stage                 string                            `yaml:"stage"`
	KubeCtl               HydraKubectl                      `yaml:"kubectl"`
	Refs                  map[string]HydraRefGroup          `yaml:"refs"`
	Clones                map[string]HydraCloneRule         `yaml:"clones,omitempty"`
	OwnerNamespaces       []string                          `yaml:"ownerNamespaces,omitempty"`
	UninstallFinalizer    []string                          `yaml:"uninstall-finalizer"`
	Scale                 map[string]HydraScaleGroup        `yaml:"scale"`
	Ready                 map[string]HydraReadyGroup        `yaml:"ready,omitempty"`
	Diff                  *HydraDiffSection                 `yaml:"diff,omitempty"`
	TemplatePatches       map[string]HydraTemplatePatchRule `yaml:"templatePatches,omitempty"`
	TemplateFiles         *HydraTemplateFiles               `yaml:"templateFiles,omitempty"`
	// Presets configures built-in CEL groups for cluster infrastructure matching.
	Presets *HydraPresetsSection `yaml:"presets,omitempty"`
	// Scope matches Hydra ConfigMap data.hydra only (hydra-gitops.org/hydra-config). Helm chart values must not set scope — see Validate().
	Scope any `yaml:"scope,omitempty"`
}

// HydraTemplateFiles configures pre-render chart template file mutations under global.hydra.templateFiles.
// Paths use loaded-chart names such as templates/deployment.yaml or charts/subchart/templates/job.yaml.
type HydraTemplateFiles struct {
	Move   []HydraTemplateFileMove `yaml:"move,omitempty"`
	Delete []string                `yaml:"delete,omitempty"`
}

type HydraTemplateFileMove struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// HydraPresetsSection holds optional overrides keyed by built-in cluster-defaults preset id.
type HydraPresetsSection map[string]*HydraPresetGroupOverride

// HydraPresetGroupOverride merges with embedded defaults for one preset id.
type HydraPresetGroupOverride struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	// Activates lists other builtin cluster-defaults preset ids: plain ids are force-enabled when
	// this preset is effectively enabled; entries prefixed with "!" declare exclusions (the
	// excluded preset must not be enabled together with this preset after activation resolution).
	// Entries can also be maps with preset, kubernetesMinorMin, and/or kubernetesMinorMax.
	Activates  PresetActivateList                   `yaml:"activates,omitempty"`
	Predicates map[string]HydraPresetPredicateEntry `yaml:"predicates,omitempty"`
}

type PresetActivateItem struct {
	Preset             string `yaml:"preset,omitempty"`
	Exclude            bool   `yaml:"exclude,omitempty"`
	KubernetesMinorMin int    `yaml:"kubernetesMinorMin,omitempty"`
	KubernetesMinorMax int    `yaml:"kubernetesMinorMax,omitempty"`
}

type PresetActivateList []PresetActivateItem

func (p *PresetActivateItem) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var raw string
		if err := n.Decode(&raw); err != nil {
			return err
		}
		exclude, target, err := ParseHydraPresetActivateRef(raw)
		if err != nil {
			return err
		}
		*p = PresetActivateItem{Preset: target, Exclude: exclude}
		return nil
	case yaml.MappingNode:
		type plain PresetActivateItem
		var aux plain
		if err := n.Decode(&aux); err != nil {
			return err
		}
		if strings.HasPrefix(strings.TrimSpace(aux.Preset), "!") {
			exclude, target, err := ParseHydraPresetActivateRef(aux.Preset)
			if err != nil {
				return err
			}
			aux.Preset = target
			aux.Exclude = aux.Exclude || exclude
		}
		*p = PresetActivateItem(aux)
		return nil
	default:
		return fmt.Errorf("activates entry must be a string or map")
	}
}

// HydraPresetPredicateEntry overrides or extends a named predicate (enabled defaults to true; cel and ids are optional in overrides).
// Each `cel` item is a CEL string or a map {cel: "...", optional: true}. Each `id` is a resource id or {id: "...", optional: true}.
type HydraPresetPredicateEntry struct {
	Enabled *bool         `yaml:"enabled,omitempty"`
	Cel     PresetCelList `yaml:"cel,omitempty"`
	// Deprecated: use cel items with {cel: "...", optional: true}. When present with an empty `cel` list, lines are merged as optional. When `cel` is set, they are appended after the required lines (legacy overlay behavior).
	OptionalCel []string `yaml:"optionalCel,omitempty"`
	// Ids lists resource ids; OR with cel. Non-optional ids contribute to the blanket bootstrap audit expected-id union.
	Ids PresetIdList `yaml:"ids,omitempty"`
	// KubernetesMinorMin/Max gate the predicate group when set (nil = inherit builtin / no extra gate).
	KubernetesMinorMin *int `yaml:"kubernetesMinorMin,omitempty"`
	KubernetesMinorMax *int `yaml:"kubernetesMinorMax,omitempty"`
}

// UnmarshalYAML supports legacy `optionalCel` by merging it into `cel` as optional items.
func (h *HydraPresetPredicateEntry) UnmarshalYAML(n *yaml.Node) error {
	type plain HydraPresetPredicateEntry
	var aux plain
	if err := n.Decode(&aux); err != nil {
		return err
	}
	// If optionalCel is present, convert/append its strings into Cel as PresetCelItem with Optional=true.
	if len(aux.OptionalCel) > 0 {
		if len(aux.Cel) == 0 {
			aux.Cel = make(PresetCelList, 0, len(aux.OptionalCel))
		}
		for _, s := range aux.OptionalCel {
			if strings.TrimSpace(s) == "" {
				continue
			}
			aux.Cel = append(aux.Cel, PresetCelItem{Expr: s, Optional: true})
		}
	}
	*h = HydraPresetPredicateEntry(aux)
	return nil
}

// HydraDiffSection configures comparison normalization for hydra gitops diff and apply under global.hydra.diff.
type HydraDiffSection struct {
	Ignore map[string]HydraDiffIgnoreRule `yaml:"ignore,omitempty"`
}

// HydraDiffIgnoreRule selects resources with a CEL predicate and applies yq patches to each compared side
// before YAML string comparison.
type HydraDiffIgnoreRule struct {
	Predicate string `yaml:"predicate"`
	// Patches are optional when IgnoreWhenMissingInCluster is true (see below).
	Patches []HydraDiffYqPatch `yaml:"patches"`
	// IgnoreWhenMissingInCluster, when true, affects hydra gitops diff only: for resources that appear
	// in rendered templates but not in the cluster (left-only), Hydra prints a short "diff ignored" line
	// instead of a full unified diff. When the resource exists in the cluster, comparison uses the
	// usual diff (including any yq patches on this rule).
	IgnoreWhenMissingInCluster bool `yaml:"ignoreWhenMissingInCluster,omitempty"`
}

// HydraDiffYqPatch is a single mikefarah/yq expression evaluated against the resource document root.
type HydraDiffYqPatch struct {
	Yq string `yaml:"yq"`
}

// DiffIgnoreRuleEntry is a named rule from merged global.hydra (Helm + ConfigMaps) or a built-in rule.
type DiffIgnoreRuleEntry struct {
	Name         string
	DeclaringApp AppId
	Rule         HydraDiffIgnoreRule
}

// HydraTemplatePatchRule selects resources with a CEL predicate and applies yq patches to rendered
// manifests after Helm template and optional clone materialization (hydra local template / hydra gitops apply).
type HydraTemplatePatchRule struct {
	Predicate string             `yaml:"predicate"`
	Patches   []HydraDiffYqPatch `yaml:"patches"`
}

// TemplatePatchRuleEntry is a named rule from merged global.hydra (Helm + ConfigMaps).
type TemplatePatchRuleEntry struct {
	Name         string
	DeclaringApp AppId
	Rule         HydraTemplatePatchRule
}

// HydraReadyGroup defines CEL-based readiness under global.hydra.ready.
// Predicate selects entities. Each expression in Cel must return an empty string when that check passes,
// or a non-empty string describing why the workload is not ready. Null/absent results omit that check.
// List results collect multiple failure lines; boolean results are not accepted (use strings).
type HydraReadyGroup struct {
	Predicate string   `yaml:"predicate"`
	Cel       []string `yaml:"cel"`
}

// HydraCloneTargets holds a CEL expression that returns list(string) of target namespaces.
type HydraCloneTargets struct {
	CEL string `yaml:"cel"`
}

func (c HydraCloneTargets) IsEmpty() bool {
	return c.CEL == ""
}

// HydraCloneRule defines a resource clone rule under global.hydra.clones (Helm values or Hydra ConfigMaps).
// Tag semantics: empty = always active; "bootstrap" = only when bootstrap context is active; other tag values are ignored (treated as always).
type HydraCloneRule struct {
	Desc      string            `yaml:"desc,omitempty"`
	Tag       string            `yaml:"tag,omitempty"`
	Predicate string            `yaml:"predicate"`
	Targets   HydraCloneTargets `yaml:"targets"`
	Exclude   []string          `yaml:"exclude,omitempty"`
}

// HydraCloneRuleEntry is a named clone rule with its declaring app (empty if from global ConfigMap only).
type HydraCloneRuleEntry struct {
	Name         string
	Rule         HydraCloneRule
	DeclaringApp AppId
}

type HydraRefGroup struct {
	Tag        []string             `yaml:"tag"`
	Attributes []RefParserAttribute `yaml:"attributes,omitempty"`
	Desc       string               `yaml:"desc,omitempty"`
	Label      string               `yaml:"label,omitempty"`
	Reverse    bool                 `yaml:"reverse,omitempty"`
	Priority   int                  `yaml:"priority,omitempty"`
	Enabled    *bool                `yaml:"enabled,omitempty"`
	RefParsers []HydraRefParser     `yaml:"ref-parsers"`
}

func (g HydraRefGroup) IsEnabled() bool {
	return g.Enabled == nil || *g.Enabled
}

func (g HydraRefGroup) HasTag(tag string) bool {
	for _, t := range g.Tag {
		if t == tag {
			return true
		}
	}
	return false
}

type RefOwnershipRuleSourceKind string

const (
	RefOwnershipRuleSourceKindHydraRefParser           RefOwnershipRuleSourceKind = "hydra-ref-parser"
	RefOwnershipRuleSourceKindEmbeddedDefaultRefParser RefOwnershipRuleSourceKind = "embedded-default-ref-parser"
	RefOwnershipRuleSourceKindCloneRule                RefOwnershipRuleSourceKind = "clone-rule"
)

type RefOwnershipRuleSource struct {
	Kind      RefOwnershipRuleSourceKind `yaml:"kind,omitempty"`
	GroupName string                     `yaml:"groupName,omitempty"`
	BlockPath string                     `yaml:"blockPath,omitempty"`
	Sources   []string                   `yaml:"sources,omitempty"`
}

// RefOwnershipPredicateLine is one CEL line used for cluster uninstall ownership matching.
type RefOwnershipPredicateLine struct {
	Cel    string                  `yaml:"cel"`
	Source *RefOwnershipRuleSource `yaml:"source,omitempty"`
	// Priority prefers higher values during ownership matching; negative priorities only participate
	// while the evaluated resource is still cluster-only / untracked.
	Priority int `yaml:"priority,omitempty"`
}

type HydraRefParser struct {
	Group      string               `yaml:"group,omitempty"`
	Version    string               `yaml:"version,omitempty"`
	Kind       string               `yaml:"kind,omitempty"`
	ApiVersion string               `yaml:"apiVersion,omitempty"`
	GVK        string               `yaml:"gvk,omitempty"`
	Namespace  string               `yaml:"namespace,omitempty"`
	GVKN       string               `yaml:"gvkn,omitempty"`
	Name       string               `yaml:"name,omitempty"`
	Id         string               `yaml:"id,omitempty"`
	Cel        string               `yaml:"cel,omitempty"`
	Predicate  string               `yaml:"predicate,omitempty"`
	Pick       []HydraRefPick       `yaml:"pick"`
	Attributes []RefParserAttribute `yaml:"attributes,omitempty"`
	Tag        []string             `yaml:"tag,omitempty"`
	Label      string               `yaml:"label,omitempty"`
	Reverse    bool                 `yaml:"reverse,omitempty"`
	Priority   int                  `yaml:"priority,omitempty"`
}

func (p HydraRefParser) SelectorAndCel() (RefSelector, CelPredicate, error) {
	return RefSelectorInput{
		Group:      p.Group,
		Version:    p.Version,
		Kind:       p.Kind,
		ApiVersion: p.ApiVersion,
		GVK:        p.GVK,
		Namespace:  p.Namespace,
		GVKN:       p.GVKN,
		Name:       p.Name,
		Id:         p.Id,
		Cel:        p.Cel,
		Predicate:  p.Predicate,
	}.Normalized()
}

func (p HydraRefParser) MatchPredicate() (CelPredicate, error) {
	selector, cel, err := p.SelectorAndCel()
	if err != nil {
		return "", err
	}
	return selector.MatchPredicate(cel), nil
}

// HydraRefPick is one pick entry in Helm/ConfigMap ref-parser YAML.
type HydraRefPick struct {
	Cel        string               `yaml:"cel"`
	Attributes []RefParserAttribute `yaml:"attributes,omitempty"`
	Tag        []string             `yaml:"tag,omitempty"`
	Label      string               `yaml:"label,omitempty"`
	Reverse    bool                 `yaml:"reverse,omitempty"`
}

// UnmarshalYAML accepts either a plain CEL string or a mapping with `cel`, `attributes`, etc.
func (p *HydraRefPick) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		p.Cel = s
		return nil
	}
	type pickPlain HydraRefPick
	return value.Decode((*pickPlain)(p))
}

type HydraScaleGroup struct {
	GVK             string   `yaml:"gvk"`
	ReplicaPaths    []string `yaml:"replicaPaths"`
	StatusReadyPath string   `yaml:"statusReadyPath,omitempty"`
}

type ForceUninstall int

const (
	ForceUninstallNone ForceUninstall = iota
	ForceUninstallForce
	ForceUninstallKeep
	ForceUninstallForceAll
)

type ForceScaleDown bool

const (
	ForceScaleDownYes ForceScaleDown = true
	ForceScaleDownNo  ForceScaleDown = false
)

type HydraKubectl struct {
	AllowedContexts []HydraKubectlContext `yaml:"allowedContexts"`
}

type HydraKubectlContext struct {
	Name     string `yaml:"name"`
	Cluster  string `yaml:"cluster"`
	AuthInfo string `yaml:"authInfo"`
}

// Validate validates the Hydra configuration against the JSON schema and business rules.
// It ensures the configuration has all required fields and follows the expected structure.
func (h *HydraValues) Validate() error {
	if h.Scope != nil {
		return log.CreateError(
			errors.ErrHydraConfigError,
			"hydra configuration validation failed: global.hydra.scope is only allowed in Hydra ConfigMap data.hydra (annotation hydra-gitops.org/hydra-config), not in Helm chart values")
	}
	// First validate business rules: required fields must not be empty
	if h.Path == "" {
		return log.CreateError(
			errors.ErrHydraConfigError,
			"hydra configuration validation failed: path is required")
	}

	// Validate scale entries
	seenGVKs := map[string]string{}
	for name, group := range h.Scale {
		if group.GVK == "" {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: scale[{name}].gvk is required",
				log.String("name", name))
		}
		if len(group.ReplicaPaths) == 0 {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: scale[{name}].replicaPaths must not be empty",
				log.String("name", name))
		}
		if prev, exists := seenGVKs[group.GVK]; exists {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: duplicate gvk {gvk} in scale entries {a} and {b}",
				log.String("gvk", group.GVK), log.String("a", prev), log.String("b", name))
		}
		seenGVKs[group.GVK] = name
	}

	for name, rg := range h.Ready {
		if rg.Predicate == "" {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: ready[{name}].predicate is required",
				log.String("name", name))
		}
		if len(rg.Cel) == 0 {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: ready[{name}].cel must not be empty",
				log.String("name", name))
		}
	}

	if len(h.TemplatePatches) > 0 {
		if err := ValidateHydraTemplatePatchRules(h.TemplatePatches); err != nil {
			return err
		}
	}
	if h.TemplateFiles != nil {
		if err := ValidateHydraTemplateFiles(h.TemplateFiles); err != nil {
			return err
		}
	}

	if h.Presets != nil {
		if err := validateHydraPresetsSection(h.Presets); err != nil {
			return err
		}
	}

	if h.Diff != nil && len(h.Diff.Ignore) > 0 {
		for name, rule := range h.Diff.Ignore {
			if strings.TrimSpace(rule.Predicate) == "" {
				return log.CreateError(
					errors.ErrHydraConfigError,
					"hydra configuration validation failed: diff.ignore[{name}].predicate is required",
					log.String("name", name))
			}
			if len(rule.Patches) == 0 && !rule.IgnoreWhenMissingInCluster {
				return log.CreateError(
					errors.ErrHydraConfigError,
					"hydra configuration validation failed: diff.ignore[{name}].patches must not be empty (unless ignoreWhenMissingInCluster is true)",
					log.String("name", name))
			}
			for i, p := range rule.Patches {
				if strings.TrimSpace(p.Yq) == "" {
					return log.CreateError(
						errors.ErrHydraConfigError,
						"hydra configuration validation failed: diff.ignore[{name}].patches[{index}].yq must not be empty",
						log.String("name", name), log.Int("index", i))
				}
			}
		}
	}

	// Validate kubectl context if defined
	if h.KubeCtl.AllowedContexts != nil {
		if len(h.KubeCtl.AllowedContexts) == 0 {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: kubectl.allowedContexts must have at least one entry")
		}
		for i, ctx := range h.KubeCtl.AllowedContexts {
			if ctx.Name == "" && ctx.Cluster == "" && ctx.AuthInfo == "" {
				return log.CreateError(
					errors.ErrHydraConfigError,
					"hydra configuration validation failed: kubectl.allowedContexts[{index}].name is required",
					log.Int("index", i))
			}
		}
	}

	return nil
}

// ParseHydraPresetActivateRef parses one legacy string activates entry: "kubernetes" enables
// kubernetes; "!canal" means canal must not be enabled together with the declaring preset.
// Leading/trailing whitespace is trimmed.
func ParseHydraPresetActivateRef(raw string) (exclude bool, targetID string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false, "", fmt.Errorf("empty activates entry")
	}
	if strings.HasPrefix(s, "!") {
		rest := strings.TrimSpace(s[1:])
		if rest == "" {
			return false, "", fmt.Errorf("activates exclusion entry is empty after '!'")
		}
		if strings.HasPrefix(rest, "!") {
			return false, "", fmt.Errorf("activates entry must not use multiple leading '!'")
		}
		return true, rest, nil
	}
	return false, s, nil
}

// ValidateHydraPresetActivates checks activates list syntax for one preset (builtin YAML or Helm/ConfigMap override).
func ValidateHydraPresetActivates(presetKey string, activates PresetActivateList) error {
	if len(activates) == 0 {
		return nil
	}
	pos := make(map[string]struct{}, len(activates))
	neg := make(map[string]struct{}, len(activates))
	for i, item := range activates {
		target := strings.TrimSpace(item.Preset)
		if target == "" {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: presets.{preset}.activates[{index}] must set preset",
				log.String("preset", presetKey), log.Int("index", i))
		}
		if item.KubernetesMinorMin < 0 || item.KubernetesMinorMax < 0 {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: presets.{preset}.activates[{index}] kubernetes minor gates must not be negative",
				log.String("preset", presetKey), log.Int("index", i))
		}
		if item.KubernetesMinorMin > 0 && item.KubernetesMinorMax > 0 && item.KubernetesMinorMin > item.KubernetesMinorMax {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: presets.{preset}.activates[{index}] kubernetesMinorMin must be <= kubernetesMinorMax",
				log.String("preset", presetKey), log.Int("index", i))
		}
		if target == presetKey {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: presets.{preset}.activates must not reference the preset itself",
				log.String("preset", presetKey))
		}
		if item.Exclude {
			if _, dup := neg[target]; dup {
				return log.CreateError(
					errors.ErrHydraConfigError,
					"hydra configuration validation failed: presets.{preset}.activates contains duplicate exclusion {target}",
					log.String("preset", presetKey), log.String("target", target))
			}
			if _, clash := pos[target]; clash {
				return log.CreateError(
					errors.ErrHydraConfigError,
					"hydra configuration validation failed: presets.{preset}.activates must not both enable and exclude preset {target}",
					log.String("preset", presetKey), log.String("target", target))
			}
			neg[target] = struct{}{}
			continue
		}
		if _, dup := pos[target]; dup {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: presets.{preset}.activates contains duplicate {target}",
				log.String("preset", presetKey), log.String("target", target))
		}
		if _, clash := neg[target]; clash {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: presets.{preset}.activates must not both enable and exclude preset {target}",
				log.String("preset", presetKey), log.String("target", target))
		}
		pos[target] = struct{}{}
	}
	return nil
}

func (p *HydraPresetsSection) BuiltinClusterDefaultsOverride(id string) *HydraPresetGroupOverride {
	if p == nil || *p == nil {
		return nil
	}
	return (*p)[id]
}

func (p *HydraPresetsSection) SetBuiltinClusterDefaultsOverride(id string, over *HydraPresetGroupOverride) {
	if p == nil {
		return
	}
	if *p == nil {
		*p = make(HydraPresetsSection)
	}
	(*p)[id] = over
}

func (p *HydraPresetsSection) BuiltinClusterDefaultsOverrides() map[string]*HydraPresetGroupOverride {
	if p == nil || *p == nil {
		return nil
	}
	out := make(map[string]*HydraPresetGroupOverride, len(*p))
	for id, over := range *p {
		if over != nil {
			out[id] = over
		}
	}
	return out
}

func validateHydraPresetsSection(p *HydraPresetsSection) error {
	validateGroup := func(presetKey string, g *HydraPresetGroupOverride) error {
		if g == nil {
			return nil
		}
		if err := ValidateHydraPresetActivates(presetKey, g.Activates); err != nil {
			return err
		}
		if len(g.Predicates) == 0 {
			return nil
		}
		for name, pe := range g.Predicates {
			for i, line := range pe.Cel {
				if strings.TrimSpace(line.Expr) == "" {
					return log.CreateError(
						errors.ErrHydraConfigError,
						"hydra configuration validation failed: presets.{preset}.predicates.{name}.cel[{index}] must not be empty",
						log.String("preset", presetKey), log.String("name", name), log.Int("index", i))
				}
			}
			for i, line := range pe.OptionalCel {
				if strings.TrimSpace(line) == "" {
					return log.CreateError(
						errors.ErrHydraConfigError,
						"hydra configuration validation failed: presets.{preset}.predicates.{name}.optionalCel[{index}] must not be empty (deprecated; use a cel list item with optional: true)",
						log.String("preset", presetKey), log.String("name", name), log.Int("index", i))
				}
			}
			for i, id := range pe.Ids {
				if strings.TrimSpace(id.Id) == "" {
					return log.CreateError(
						errors.ErrHydraConfigError,
						"hydra configuration validation failed: presets.{preset}.predicates.{name}.ids[{index}] must not be empty",
						log.String("preset", presetKey), log.String("name", name), log.Int("index", i))
				}
			}
			if len(pe.Cel) == 0 && len(pe.OptionalCel) == 0 && len(pe.Ids) == 0 {
				return log.CreateError(
					errors.ErrHydraConfigError,
					"hydra configuration validation failed: presets.{preset}.predicates.{name} must set cel, optionalCel, and/or ids",
					log.String("preset", presetKey), log.String("name", name))
			}
		}
		return nil
	}
	for presetKey, group := range p.BuiltinClusterDefaultsOverrides() {
		if err := validateGroup(presetKey, group); err != nil {
			return err
		}
	}
	return nil
}

// ValidateHydraTemplatePatchRules validates templatePatches entries (predicate + non-empty yq patches).
func ValidateHydraTemplatePatchRules(rules map[string]HydraTemplatePatchRule) error {
	for name, rule := range rules {
		if strings.TrimSpace(rule.Predicate) == "" {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: templatePatches[{name}].predicate is required",
				log.String("name", name))
		}
		if len(rule.Patches) == 0 {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: templatePatches[{name}].patches must not be empty",
				log.String("name", name))
		}
		for i, p := range rule.Patches {
			if strings.TrimSpace(p.Yq) == "" {
				return log.CreateError(
					errors.ErrHydraConfigError,
					"hydra configuration validation failed: templatePatches[{name}].patches[{index}].yq must not be empty",
					log.String("name", name), log.Int("index", i))
			}
		}
	}
	return nil
}

func ValidateHydraTemplateFiles(files *HydraTemplateFiles) error {
	if files == nil {
		return nil
	}
	for i, move := range files.Move {
		from := strings.TrimSpace(move.From)
		to := strings.TrimSpace(move.To)
		if from == "" {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: templateFiles.move[{index}].from is required",
				log.Int("index", i))
		}
		if to == "" {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: templateFiles.move[{index}].to is required",
				log.Int("index", i))
		}
		if from == to {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: templateFiles.move[{index}] source and destination must differ",
				log.Int("index", i))
		}
	}
	for i, path := range files.Delete {
		path = strings.TrimSpace(path)
		if path == "" {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: templateFiles.delete[{index}] must not be empty",
				log.Int("index", i))
		}
		if _, err := regexp.Compile(path); err != nil {
			return log.CreateError(
				errors.ErrHydraConfigError,
				"hydra configuration validation failed: templateFiles.delete[{index}] must be a valid regex: {err}",
				log.Int("index", i), log.Err(err))
		}
	}
	return nil
}
