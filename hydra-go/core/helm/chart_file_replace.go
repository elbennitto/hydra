package helm

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/common"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
)

// ChartFileReplacement replaces all chart files whose normalized path matches Path.
// Path is interpreted after the chart has been loaded, so it targets loaded chart file names
// such as templates/deployment.yaml or charts/subchart/templates/configmap.yaml.
type ChartFileReplacement struct {
	Path string
	Data []byte
}

type ChartFileMove struct {
	From string
	To   string
}

type ChartFileOperations struct {
	Replacements []ChartFileReplacement
	Moves        []ChartFileMove
	Deletes      []string
}

func (ops ChartFileOperations) Empty() bool {
	return len(ops.Replacements) == 0 && len(ops.Moves) == 0 && len(ops.Deletes) == 0
}

func NormalizeChartFileReplacementPath(path string) string {
	return filepath.ToSlash(strings.TrimSpace(path))
}

// ReplaceChartFiles clones a chart and replaces matching file bodies before rendering.
func ReplaceChartFiles(charter chart.Charter, replacements []ChartFileReplacement) (chart.Charter, error) {
	return ApplyChartFileOperations(charter, ChartFileOperations{Replacements: replacements})
}

func ApplyChartFileOperations(charter chart.Charter, ops ChartFileOperations) (chart.Charter, error) {
	if ops.Empty() {
		return charter, nil
	}

	c, err := convertToV2Chart(charter)
	if err != nil {
		return nil, err
	}
	cloned, err := CloneChart(c)
	if err != nil {
		return nil, err
	}
	if err := applyChartFileOperationsV2(cloned, ops); err != nil {
		return nil, err
	}
	return cloned, nil
}

func applyChartFileOperationsV2(chart *v2chart.Chart, ops ChartFileOperations) error {
	if chart == nil {
		return fmt.Errorf("chart cannot be nil")
	}

	replacements := make([]ChartFileReplacement, 0, len(ops.Replacements))
	for _, replacement := range ops.Replacements {
		path := NormalizeChartFileReplacementPath(replacement.Path)
		if path == "" {
			return fmt.Errorf("chart file replacement path cannot be empty")
		}
		replacements = append(replacements, ChartFileReplacement{
			Path: path,
			Data: slices.Clone(replacement.Data),
		})
	}
	moves := make([]ChartFileMove, 0, len(ops.Moves))
	for _, move := range ops.Moves {
		from := NormalizeChartFileReplacementPath(move.From)
		to := NormalizeChartFileReplacementPath(move.To)
		if from == "" {
			return fmt.Errorf("chart file move source path cannot be empty")
		}
		if to == "" {
			return fmt.Errorf("chart file move destination path cannot be empty")
		}
		if from == to {
			return fmt.Errorf("chart file move source and destination must differ: %s", from)
		}
		moves = append(moves, ChartFileMove{From: from, To: to})
	}
	deletePatterns := make([]*regexp.Regexp, 0, len(ops.Deletes))
	for _, path := range ops.Deletes {
		path = NormalizeChartFileReplacementPath(path)
		if path == "" {
			return fmt.Errorf("chart file delete path cannot be empty")
		}
		re, err := regexp.Compile(path)
		if err != nil {
			return fmt.Errorf("chart file delete regex is invalid %q: %w", path, err)
		}
		deletePatterns = append(deletePatterns, re)
	}

	findData := func(path string) ([]byte, bool) {
		var found []byte
		var ok bool
		var scan func(*v2chart.Chart)
		scanFiles := func(files []*common.File) {
			for _, file := range files {
				if file == nil || NormalizeChartFileReplacementPath(file.Name) != path {
					continue
				}
				found = slices.Clone(file.Data)
				ok = true
				return
			}
		}
		scan = func(c *v2chart.Chart) {
			if c == nil || ok {
				return
			}
			scanFiles(c.Raw)
			scanFiles(c.Templates)
			scanFiles(c.Files)
			for _, dep := range c.Dependencies() {
				scan(dep)
				if ok {
					return
				}
			}
		}
		scan(chart)
		return found, ok
	}

	replaceDestinationData := func(path string, data []byte) int {
		replaced := 0
		replaceFiles := func(files []*common.File) {
			for _, file := range files {
				if file == nil || NormalizeChartFileReplacementPath(file.Name) != path {
					continue
				}
				file.Data = slices.Clone(data)
				replaced++
			}
		}
		var walk func(*v2chart.Chart)
		walk = func(c *v2chart.Chart) {
			if c == nil {
				return
			}
			replaceFiles(c.Raw)
			replaceFiles(c.Templates)
			replaceFiles(c.Files)
			for _, dep := range c.Dependencies() {
				walk(dep)
			}
		}
		walk(chart)
		return replaced
	}

	deleteMatchingPaths := func(match func(string) bool) int {
		removed := 0
		removeFromSlice := func(files []*common.File) ([]*common.File, int) {
			if len(files) == 0 {
				return files, 0
			}
			out := files[:0]
			localRemoved := 0
			for _, file := range files {
				if file != nil && match(NormalizeChartFileReplacementPath(file.Name)) {
					localRemoved++
					continue
				}
				out = append(out, file)
			}
			return out, localRemoved
		}
		var walkDelete func(*v2chart.Chart)
		walkDelete = func(c *v2chart.Chart) {
			if c == nil {
				return
			}
			accumulateRemoved := func(files []*common.File) []*common.File {
				updated, removedInSlice := removeFromSlice(files)
				removed += removedInSlice
				return updated
			}
			c.Raw = accumulateRemoved(c.Raw)
			c.Templates = accumulateRemoved(c.Templates)
			c.Files = accumulateRemoved(c.Files)
			for _, dep := range c.Dependencies() {
				walkDelete(dep)
			}
		}
		walkDelete(chart)
		return removed
	}

	for _, move := range moves {
		data, ok := findData(move.From)
		if !ok {
			return fmt.Errorf("chart file move source path not found: %s", move.From)
		}
		if replaced := replaceDestinationData(move.To, data); replaced == 0 {
			return fmt.Errorf("chart file move destination path not found: %s", move.To)
		}
		if removed := deleteMatchingPaths(func(path string) bool { return path == move.From }); removed == 0 {
			return fmt.Errorf("chart file move source path not found during delete: %s", move.From)
		}
	}

	for _, pattern := range deletePatterns {
		if removed := deleteMatchingPaths(pattern.MatchString); removed == 0 {
			return fmt.Errorf("chart file delete regex matched no paths: %s", pattern.String())
		}
	}

	replaced := map[string]int{}
	replaceFiles := func(files []*common.File) {
		for _, file := range files {
			if file == nil {
				continue
			}
			name := NormalizeChartFileReplacementPath(file.Name)
			for _, replacement := range replacements {
				if replacement.Path != name {
					continue
				}
				file.Data = slices.Clone(replacement.Data)
				replaced[replacement.Path]++
			}
		}
	}
	var walk func(*v2chart.Chart)
	walk = func(c *v2chart.Chart) {
		if c == nil {
			return
		}
		replaceFiles(c.Raw)
		replaceFiles(c.Templates)
		replaceFiles(c.Files)
		for _, dep := range c.Dependencies() {
			walk(dep)
		}
	}
	walk(chart)

	for _, replacement := range replacements {
		if replaced[replacement.Path] == 0 {
			return fmt.Errorf("chart file replacement path not found: %s", replacement.Path)
		}
	}

	return nil
}
