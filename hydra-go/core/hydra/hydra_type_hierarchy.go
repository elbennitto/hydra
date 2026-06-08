package hydra

import (
	"os"
	"path/filepath"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

const (
	hydraTypeGroup    = "group"
	hydraTypeContext  = "context"
	hydraTypeCluster  = "cluster"
	hydraTypeRootApp  = "root-app"
	hydraTypeChildApp = "child-app"
)

type hydraTypeDirective struct {
	Path      string
	HasType   bool
	Type      string
	Parent    bool
	HasParent bool
}

func nextHydraType(level string) (string, bool) {
	switch level {
	case hydraTypeGroup:
		return hydraTypeContext, true
	case hydraTypeContext:
		return hydraTypeCluster, true
	case hydraTypeCluster:
		return hydraTypeRootApp, true
	case hydraTypeRootApp:
		return hydraTypeChildApp, true
	default:
		return "", false
	}
}

func deriveHydraTypeFromAncestor(ancestorType string, stepsDown int) (string, bool) {
	resolved := ancestorType
	for range stepsDown {
		next, ok := nextHydraType(resolved)
		if !ok {
			return "", false
		}
		resolved = next
	}
	return resolved, true
}

func isAllowedHydraType(level string) bool {
	switch level {
	case hydraTypeGroup, hydraTypeContext, hydraTypeCluster, hydraTypeRootApp, hydraTypeChildApp:
		return true
	default:
		return false
	}
}

func loadHydraTypeDirective(l log.Logger, dirPath string) (hydraTypeDirective, error) {
	valuesPath := filepath.Join(dirPath, "values.yaml")
	vals, err := values.LoadValuesFile(l, valuesPath)
	if err != nil {
		return hydraTypeDirective{}, err
	}

	d := hydraTypeDirective{Path: dirPath}
	typeAny := values.Lookup(vals, "global", "hydra", "type")
	if t, ok := typeAny.(string); ok {
		t = strings.TrimSpace(t)
		if t != "" {
			if !isAllowedHydraType(t) {
				return hydraTypeDirective{}, log.CreateError(
					errors.ErrInvalidHydraStructure,
					"invalid global.hydra.type '{type}' in '{valuesPath}': allowed values are group/context/cluster/root-app/child-app",
					log.String("type", t),
					log.String("valuesPath", valuesPath),
				)
			}
			d.HasType = true
			d.Type = t
		}
	}

	parentAny := values.Lookup(vals, "global", "hydra", "parent")
	if parentAny != nil {
		p, ok := parentAny.(bool)
		if !ok {
			return hydraTypeDirective{}, log.CreateError(
				errors.ErrInvalidHydraStructure,
				"invalid global.hydra.parent in '{valuesPath}': expected boolean",
				log.String("valuesPath", valuesPath),
			)
		}
		d.HasParent = true
		d.Parent = p
	} else {
		// Default parent lookup is enabled. Group defaults to false because it has no logical parent level.
		d.Parent = !(d.HasType && d.Type == hydraTypeGroup)
	}

	return d, nil
}

// resolveHydraTypeAtPath resolves the effective global.hydra.type for dirPath by combining explicit
// declarations on dirPath and eligible parent directories according to global.hydra.parent.
func resolveHydraTypeAtPath(l log.Logger, dirPath string) (string, bool, error) {
	chain := make([]hydraTypeDirective, 0, 8)
	current := filepath.Clean(dirPath)
	for {
		d, err := loadHydraTypeDirective(l, current)
		if err != nil {
			return "", false, err
		}
		chain = append(chain, d)

		if !d.Parent {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	resolved := ""
	hasResolved := false
	for idx, d := range chain {
		if !d.HasType {
			continue
		}
		candidate, ok := deriveHydraTypeFromAncestor(d.Type, idx)
		if !ok {
			return "", false, log.CreateError(
				errors.ErrInvalidHydraStructure,
				"cannot derive hydra type for '{path}' from global.hydra.type '{type}' at '{sourcePath}'",
				log.String("path", dirPath),
				log.String("type", d.Type),
				log.String("sourcePath", d.Path),
			)
		}
		if !hasResolved {
			resolved = candidate
			hasResolved = true
			continue
		}
		if resolved != candidate {
			return "", false, log.CreateError(
				errors.ErrInvalidHydraStructure,
				"conflicting global.hydra.type resolution for '{path}': resolved '{resolved}' but '{sourcePath}' implies '{candidate}'",
				log.String("path", dirPath),
				log.String("resolved", resolved),
				log.String("sourcePath", d.Path),
				log.String("candidate", candidate),
			)
		}
	}

	return resolved, hasResolved, nil
}

func parentLookupEnabledAtPath(l log.Logger, dirPath string) (bool, error) {
	d, err := loadHydraTypeDirective(l, dirPath)
	if err != nil {
		return false, err
	}
	return d.Parent, nil
}

func ensureHydraTypeInValues(vals types.ValuesMap, expected string, level string) (types.ValuesMap, error) {
	if vals == nil {
		vals = types.ValuesMap{}
	}

	globalAny, ok := vals["global"]
	if !ok || globalAny == nil {
		vals["global"] = map[string]any{}
		globalAny = vals["global"]
	}
	globalMap, ok := globalAny.(map[string]any)
	if !ok {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"invalid {level} values: global must be a map to apply global.hydra.type",
			log.String("level", level),
		)
	}

	hydraAny, ok := globalMap["hydra"]
	if !ok || hydraAny == nil {
		globalMap["hydra"] = map[string]any{}
		hydraAny = globalMap["hydra"]
	}
	hydraMap, ok := hydraAny.(map[string]any)
	if !ok {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"invalid {level} values: global.hydra must be a map to apply global.hydra.type",
			log.String("level", level),
		)
	}

	if currentAny, ok := hydraMap["type"]; ok && currentAny != nil {
		if _, ok := currentAny.(string); !ok {
			return nil, log.CreateError(
				errors.ErrInvalidHydraStructure,
				"invalid {level} values: global.hydra.type must be a string",
				log.String("level", level),
			)
		}
	}

	hydraMap["type"] = expected
	return vals, nil
}

func validateLevelType(l log.Logger, dirPath string, expected string, level string) error {
	resolved, hasType, err := resolveHydraTypeAtPath(l, dirPath)
	if err != nil {
		return err
	}
	if !hasType {
		return log.CreateError(
			errors.ErrInvalidHydraStructure,
			"missing required global.hydra.type for {level} at '{path}' (set type to group/context/cluster/root-app/child-app on at least one hierarchy level)",
			log.String("level", level),
			log.String("path", dirPath),
		)
	}
	if resolved != expected {
		return log.CreateError(
			errors.ErrInvalidHydraStructure,
			"invalid {level} at '{path}': resolved global.hydra.type is '{actual}', expected '{expected}'",
			log.String("level", level),
			log.String("path", dirPath),
			log.String("actual", resolved),
			log.String("expected", expected),
		)
	}
	current := filepath.Clean(dirPath)
	for {
		hydraYamlPath := filepath.Join(current, clusterRootHydraFileName)
		if _, statErr := os.Stat(hydraYamlPath); statErr == nil {
			currentResolved, currentHasType, currentErr := resolveHydraTypeAtPath(l, current)
			if currentErr != nil {
				return currentErr
			}
			if !currentHasType || currentResolved != hydraTypeCluster {
				return log.CreateError(
					errors.ErrInvalidHydraStructure,
					"invalid {level} at '{path}': hydra.yaml is only allowed in cluster root directories",
					log.String("level", level),
					log.String("path", current),
				)
			}
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return log.CreateError(
				errors.ErrInvalidHydraStructure,
				"failed to stat hydra.yaml at '{path}': {err}",
				log.String("path", hydraYamlPath),
				log.Err(statErr),
			)
		}
		parentEnabled, parentErr := parentLookupEnabledAtPath(l, current)
		if parentErr != nil {
			return parentErr
		}
		if !parentEnabled {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}
