package types

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ClusterRootHydraFile is the optional hydra.yaml supported only at cluster root level.
type ClusterRootHydraFile struct {
	Hydra *ClusterRootHydraSection `yaml:"hydra,omitempty"`
}

type ClusterRootHydraSection struct {
	Overrides ClusterRootHydraOverrides `yaml:"overrides,omitempty"`
}

type ClusterRootHydraOverrides map[string]ClusterRootAppOverrideList

// ClusterRootAppOverrideItem assigns or excludes live resources for one cluster-scoped Hydra app key.
// YAML supports:
//   - a plain string (treated like id)
//   - a mapping with id/cel/predicate and the same selector shorthand accepted elsewhere
//     (group, version, kind, apiVersion, gvk, namespace, gvkn, name)
type ClusterRootAppOverrideItem struct {
	Selector RefSelector
	Cel      CelPredicate
	Exclude  bool
}

func (i *ClusterRootAppOverrideItem) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		raw := strings.TrimSpace(n.Value)
		if raw == "" {
			return fmt.Errorf("override item must not be empty")
		}
		selector, celExpr, err := RefSelectorInput{Id: raw}.Normalized()
		if err != nil {
			return err
		}
		i.Selector = selector
		i.Cel = celExpr
		i.Exclude = false
		return nil
	case yaml.MappingNode:
		var wire struct {
			Group      string `yaml:"group,omitempty"`
			Version    string `yaml:"version,omitempty"`
			Kind       string `yaml:"kind,omitempty"`
			ApiVersion string `yaml:"apiVersion,omitempty"`
			GVK        string `yaml:"gvk,omitempty"`
			Namespace  string `yaml:"namespace,omitempty"`
			GVKN       string `yaml:"gvkn,omitempty"`
			Name       string `yaml:"name,omitempty"`
			Id         string `yaml:"id,omitempty"`
			Cel        string `yaml:"cel,omitempty"`
			Predicate  string `yaml:"predicate,omitempty"`
			Exclude    bool   `yaml:"exclude,omitempty"`
		}
		if err := n.Decode(&wire); err != nil {
			return err
		}
		selector, celExpr, err := RefSelectorInput{
			Group:      wire.Group,
			Version:    wire.Version,
			Kind:       wire.Kind,
			ApiVersion: wire.ApiVersion,
			GVK:        wire.GVK,
			Namespace:  wire.Namespace,
			GVKN:       wire.GVKN,
			Name:       wire.Name,
			Id:         wire.Id,
			Cel:        wire.Cel,
			Predicate:  wire.Predicate,
		}.Normalized()
		if err != nil {
			return err
		}
		i.Selector = selector
		i.Cel = celExpr
		i.Exclude = wire.Exclude
		return nil
	default:
		return fmt.Errorf("override item: expected string or map, got kind %v", n.Kind)
	}
}

type ClusterRootAppOverrideList []ClusterRootAppOverrideItem

func (l *ClusterRootAppOverrideList) UnmarshalYAML(n *yaml.Node) error {
	if l == nil {
		return fmt.Errorf("ClusterRootAppOverrideList: nil receiver")
	}
	seq := n
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) != 1 {
			return fmt.Errorf("overrides: document must have one root")
		}
		seq = n.Content[0]
	}
	if seq.Kind != yaml.SequenceNode {
		return fmt.Errorf("overrides: must be a list, got %v", seq.Kind)
	}
	out := make(ClusterRootAppOverrideList, 0, len(seq.Content))
	for i, ch := range seq.Content {
		var item ClusterRootAppOverrideItem
		if err := ch.Decode(&item); err != nil {
			return fmt.Errorf("override item %d: %w", i, err)
		}
		out = append(out, item)
	}
	*l = out
	return nil
}

func ParseClusterLocalOverrideTarget(cluster ClusterName, raw string) (AppId, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("override target must not be empty")
	}
	parts := strings.Split(raw, ".")
	switch len(parts) {
	case 1:
		return NewRootAppId(cluster, RootAppName(parts[0])), nil
	case 2:
		if parts[0] == "presets" {
			return NewPresetAppId(cluster, parts[1])
		}
		if parts[1] == "root" {
			return NewRootAppId(cluster, RootAppName(parts[0])), nil
		}
		return NewChildAppId(cluster, RootAppName(parts[0]), ChildAppName(parts[1])), nil
	default:
		return "", fmt.Errorf("override target %q must be <root>, <root>.root, <root>.<child>, or presets.<preset>", raw)
	}
}
