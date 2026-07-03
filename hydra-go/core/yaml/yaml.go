package yaml

import (
	"fmt"
	"math"
	"strconv"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "sigs.k8s.io/yaml"
)

// ToYaml converts a value to YAML, ensuring whole-number floats are written
// as plain integers (e.g. 10485760) instead of scientific notation (e.g. 1.048576e+07).
func ToYaml(data any) (types.YamlString, error) {
	data = preserveLeadingNewlines(data)
	var doc yaml.Node
	if err := doc.Encode(data); err != nil {
		return "", err
	}
	normalizeFloatNodes(&doc)
	yamlBytes, err := yaml.Marshal(&doc)
	if err != nil {
		return "", err
	}
	return types.YamlString(yamlBytes), nil
}

// FromYaml converts a YAML string to the specified type T.
// Uses yaml.Node to rewrite !!float scalars that represent whole numbers to
// !!int before decoding, so the result contains int instead of float64.
func FromYaml[T any](yamlString types.YamlString) (T, error) {
	var doc yaml.Node
	var result T
	if err := yaml.Unmarshal([]byte(yamlString), &doc); err != nil {
		return result, err
	}
	normalizeFloatNodes(&doc)
	err := doc.Decode(&result)
	return result, err
}

// normalizeFloatNodes walks the yaml.Node tree and converts !!float scalar
// nodes that represent whole numbers to !!int so that Decode produces int
// instead of float64 (avoids scientific notation on re-marshal).
func normalizeFloatNodes(node *yaml.Node) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!float" {
		f, err := strconv.ParseFloat(node.Value, 64)
		if err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) &&
			f == math.Trunc(f) && f >= math.MinInt64 && f <= math.MaxInt64 {
			node.Tag = "!!int"
			node.Value = strconv.FormatInt(int64(f), 10)
		}
	}
	for _, child := range node.Content {
		normalizeFloatNodes(child)
	}
}

func YamlToUnstructured(yamlString types.YamlString) (*unstructured.Unstructured, error) {
	var result map[string]any
	if err := kyaml.Unmarshal([]byte(yamlString), &result); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: result}, nil
}

// Lookup traverses a map using variadic keys and returns the value nil-safe.
// Example: Lookup(m, "global", "hydra", "values-cleanup")
func Lookup(m map[string]any, keys ...string) any {
	current := any(m)

	for _, key := range keys {
		// Check if current is a map
		if mapVal, ok := current.(map[string]any); !ok {
			return nil
		} else {
			// Safe retrieval from map
			current, ok = mapVal[key]
			if !ok {
				return nil
			}
		}
	}

	return current
}

func LookupMap(m map[string]any, keys ...string) (map[string]any, error) {
	if len(keys) == 0 {
		return m, nil
	}
	value := Lookup(m, keys[0])
	if value == nil {
		return nil, nil
	}

	if result, ok := value.(map[string]any); ok {
		return LookupMap(result, keys[1:]...)
	}
	return nil, log.CreateError(
		errors.ErrInternalError,
		"value at keys {keys} is not a map[string]any, it is {type}",
		log.String("keys", fmt.Sprintf("%v", keys)),
		log.String("type", fmt.Sprintf("%T", value)))
}
