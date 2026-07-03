package helm

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"helm.sh/helm/v4/pkg/release/v1/util"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

var sourceRegex = regexp.MustCompile(`(?m)^# Source:\s*(.+)$`)

// SplitManifestMap splits a Helm manifest into a map organized by source file path
func SplitManifestMap(manifest types.YamlString) map[string][]types.YamlString {
	result := make(map[string][]types.YamlString)

	// Split manifests by "---" separator
	manifests := util.SplitManifests(string(manifest))

	// Iterate in sorted key order for deterministic unknown-X numbering
	keys := make([]string, 0, len(manifests))
	for k := range manifests {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	unknownCounter := 0
	for _, k := range keys {
		m := manifests[k]
		path := ""
		content := strings.TrimSpace(m)

		// Try to find "# Source: ..."
		if matches := sourceRegex.FindStringSubmatch(content); len(matches) == 2 {
			path = strings.TrimSpace(matches[1])
			// Remove the Source line from content
			content = sourceRegex.ReplaceAllString(content, "")
			content = strings.TrimSpace(content)
		} else {
			// If no # Source found, fallback to "unknown-x"
			unknownCounter++
			path = fmt.Sprintf("unknown-%d.yaml", unknownCounter)
		}

		if content != "" {
			// Ensure each document ends with a newline so downstream YAML parsers (e.g. for the last
			// literal block in a manifest) see the same trailing-newline semantics as a full stream.
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			result[path] = append(result[path], types.YamlString(content))
		}
	}

	return result
}
