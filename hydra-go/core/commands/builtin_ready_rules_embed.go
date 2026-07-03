package commands

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

//go:embed ready-rules/*.yaml
var builtinReadyRulesFS embed.FS

// embeddedStaticBuiltinReadyRules is parsed from ready-rules/*.yaml at init (same shape as global.hydra.ready).
var embeddedStaticBuiltinReadyRules []readyRule

func init() {
	rules, err := parseEmbeddedBuiltinReadyRules()
	if err != nil {
		panic("commands: embedded builtin ready rules: " + err.Error())
	}
	embeddedStaticBuiltinReadyRules = rules
}

type builtinReadyYAMLDoc struct {
	Ready map[string]types.HydraReadyGroup `yaml:"ready"`
}

func parseEmbeddedBuiltinReadyRules() ([]readyRule, error) {
	merged := map[string]types.HydraReadyGroup{}
	err := fs.WalkDir(builtinReadyRulesFS, "ready-rules", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(path)
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			return nil
		}
		data, err := builtinReadyRulesFS.ReadFile(path)
		if err != nil {
			return err
		}
		var doc builtinReadyYAMLDoc
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if doc.Ready == nil {
			return nil
		}
		for name, g := range doc.Ready {
			if _, exists := merged[name]; exists {
				return fmt.Errorf("%s: duplicate ready rule name %q (also defined in another embedded file)", path, name)
			}
			merged[name] = g
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	names := slices.Sorted(maps.Keys(merged))
	out := make([]readyRule, 0, len(names))
	for _, name := range names {
		g := merged[name]
		pred := strings.TrimSpace(g.Predicate)
		if pred == "" {
			return nil, fmt.Errorf("embedded ready rule %q: predicate is required", name)
		}
		var cel []string
		for _, c := range g.Cel {
			t := strings.TrimSpace(c)
			if t != "" {
				cel = append(cel, t)
			}
		}
		if len(cel) == 0 {
			return nil, fmt.Errorf("embedded ready rule %q: at least one non-empty cel expression is required", name)
		}
		out = append(out, readyRule{
			name:      name,
			predicate: types.CelPredicate(pred),
			cel:       toCelExpressions(cel),
		})
	}
	return out, nil
}
