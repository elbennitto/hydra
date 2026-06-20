package record

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type RecordFile struct {
	Name  string       `yaml:"name"`
	Steps []RecordStep `yaml:"steps"`
}

type RecordStep struct {
	Kind string

	SleepSeconds float64
	Color        *RecordColor

	Write string
	Speed *float64

	Run              string
	Input            *bool
	Output           *bool
	ExpectedExitCode *int

	CD string
	Marker string
	ExportToDirectory string

	Env []RecordEnvEntry

	Assert RecordAssert
}

type RecordColor struct {
	Reset bool
	FG    string
	BG    string
	Bold  bool
}

type RecordEnvEntry struct {
	Name  string
	Value *string
	CEL   *string
	Exec  *string
}

type RecordAssert struct {
	Stdout string
	Stderr string
	Cast   string
}

type RecordSpec struct {
	Path        string
	DisplayPath string
	OutputBase  string
	File        RecordFile
}

type rawRecordFile struct {
	Name  string                   `yaml:"name"`
	Steps []map[string]interface{} `yaml:"steps"`
}

func Discover(specDir string) ([]RecordSpec, error) {
	var out []RecordSpec
	err := filepath.WalkDir(specDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !isRecordSpecFilename(name) {
			return nil
		}
		spec, err := Load(path)
		if err != nil {
			return err
		}
		out = append(out, RecordSpec{
			Path:        path,
			DisplayPath: outputBaseFromSpecPath(specDir, path),
			OutputBase:  outputBaseFromSpecPath(specDir, path),
			File:        spec,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk record spec dir: %w", err)
	}

	slices.SortFunc(out, func(a, b RecordSpec) int {
		return strings.Compare(a.Path, b.Path)
	})
	return out, nil
}

func Load(path string) (RecordFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RecordFile{}, fmt.Errorf("read record spec %q: %w", path, err)
	}

	var raw rawRecordFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return RecordFile{}, fmt.Errorf("parse record spec %q: %w", path, err)
	}

	spec, err := parseRecordFile(path, raw)
	if err != nil {
		return RecordFile{}, err
	}
	return spec, nil
}

func Find(specDir, name string) (RecordSpec, error) {
	specs, err := Discover(specDir)
	if err != nil {
		return RecordSpec{}, err
	}
	for _, spec := range specs {
		if spec.DisplayPath == name || spec.OutputBase == name {
			return spec, nil
		}
	}
	return RecordSpec{}, fmt.Errorf("record %q not found in %q", name, specDir)
}

func Resolve(specDir, input string) (RecordSpec, error) {
	if strings.TrimSpace(input) == "" {
		return RecordSpec{}, fmt.Errorf("record file path is empty")
	}

	path := input
	if !filepath.IsAbs(path) {
		path = filepath.Clean(path)
		absPath, err := filepath.Abs(path)
		if err != nil {
			return RecordSpec{}, fmt.Errorf("resolve absolute record file path %q: %w", input, err)
		}
		path = absPath
	}
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if info.IsDir() {
			return RecordSpec{}, fmt.Errorf("record file not found: %q is a directory", input)
		}
	case os.IsNotExist(err):
		return RecordSpec{}, fmt.Errorf("record file not found: %q", input)
	default:
		return RecordSpec{}, fmt.Errorf("stat record file %q: %w", input, err)
	}

	spec, err := Load(path)
	if err != nil {
		return RecordSpec{}, err
	}

	return RecordSpec{
		Path:        path,
		DisplayPath: filepath.ToSlash(strings.TrimSpace(input)),
		OutputBase:  outputBaseFromSpecPath(specDir, path),
		File:        spec,
	}, nil
}

func outputBaseFromSpecPath(specDir, path string) string {
	rel, err := filepath.Rel(specDir, path)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		rel = filepath.Base(path)
	}
	rel = TrimRecordSpecExtension(rel)
	return filepath.ToSlash(rel)
}

func TrimRecordSpecExtension(path string) string {
	switch {
	case strings.HasSuffix(path, ".cast.yaml"):
		return strings.TrimSuffix(path, ".cast.yaml")
	case strings.HasSuffix(path, ".cast.yml"):
		return strings.TrimSuffix(path, ".cast.yml")
	default:
		ext := filepath.Ext(path)
		if ext == "" {
			return path
		}
		return strings.TrimSuffix(path, ext)
	}
}

func isRecordSpecFilename(name string) bool {
	return strings.HasSuffix(name, ".cast.yaml") ||
		strings.HasSuffix(name, ".cast.yml") ||
		strings.HasSuffix(name, ".yaml") ||
		strings.HasSuffix(name, ".yml")
}

func parseRecordFile(path string, raw rawRecordFile) (RecordFile, error) {
	spec := RecordFile{Name: strings.TrimSpace(raw.Name)}
	if spec.Name == "" {
		spec.Name = outputBaseFromSpecPath(filepath.Dir(path), path)
	}
	if len(raw.Steps) == 0 {
		return RecordFile{}, fmt.Errorf("record spec %q: at least one step is required", path)
	}

	steps := make([]RecordStep, 0, len(raw.Steps))
	for i, stepMap := range raw.Steps {
		step, err := parseRecordStep(path, i, stepMap)
		if err != nil {
			return RecordFile{}, err
		}
		steps = append(steps, step)
	}
	spec.Steps = steps
	return spec, nil
}

func parseRecordStep(path string, index int, stepMap map[string]interface{}) (RecordStep, error) {
	stepType, hasType := optionalString(stepMap, "type")
	_, hasSleep := stepMap["sleep"]
	_, hasWrite := stepMap["write"]
	_, hasRun := stepMap["run"]
	_, hasCD := stepMap["cd"]
	_, hasMarker := stepMap["marker"]
	_, hasAssert := stepMap["assert"]
	_, hasEnv := stepMap["env"]
	_, hasColor := stepMap["color"]
	_, hasExportToDirectory := stepMap["export-to-directory"]

	primary := 0
	if hasType {
		primary++
	}
	if hasSleep {
		primary++
	}
	if hasWrite {
		primary++
	}
	if hasRun {
		primary++
	}
	if hasCD {
		primary++
	}
	if hasMarker {
		primary++
	}
	if hasAssert {
		primary++
	}
	if hasEnv {
		primary++
	}
	if hasColor {
		primary++
	}
	if hasExportToDirectory {
		primary++
	}
	if primary != 1 {
		return RecordStep{}, fmt.Errorf("record spec %q: step %d must define exactly one primary field (type/sleep/write/run/cd/marker/assert/env/color/export-to-directory)", path, index)
	}

	if hasType {
		t := strings.TrimSpace(stepType)
		switch t {
		case "prompt", "newline":
			return RecordStep{Kind: t}, nil
		default:
			return RecordStep{}, fmt.Errorf("record spec %q: step %d has unsupported type %q", path, index, t)
		}
	}

	if hasSleep {
		seconds, ok := asFloat(stepMap["sleep"])
		if !ok || seconds < 0 {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d sleep requires number >= 0", path, index)
		}
		return RecordStep{Kind: "sleep", SleepSeconds: seconds}, nil
	}

	if hasWrite {
		text, ok := asString(stepMap["write"])
		if !ok {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d write requires string", path, index)
		}
		step := RecordStep{Kind: "write", Write: text}
		if v, exists := stepMap["speed"]; exists {
			speed, ok := asFloat(v)
			if !ok || speed < 0 {
				return RecordStep{}, fmt.Errorf("record spec %q: step %d speed requires number >= 0", path, index)
			}
			step.Speed = &speed
		}
		return step, nil
	}

	if hasRun {
		command, ok := asString(stepMap["run"])
		if !ok {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d run requires string", path, index)
		}
		step := RecordStep{Kind: "run", Run: command}
		if v, exists := stepMap["input"]; exists {
			b, ok := asBool(v)
			if !ok {
				return RecordStep{}, fmt.Errorf("record spec %q: step %d input requires bool", path, index)
			}
			step.Input = &b
		}
		if v, exists := stepMap["output"]; exists {
			b, ok := asBool(v)
			if !ok {
				return RecordStep{}, fmt.Errorf("record spec %q: step %d output requires bool", path, index)
			}
			step.Output = &b
		}
		if v, exists := stepMap["expectedExitCode"]; exists {
			n, ok := asInt(v)
			if !ok {
				return RecordStep{}, fmt.Errorf("record spec %q: step %d expectedExitCode requires integer", path, index)
			}
			step.ExpectedExitCode = &n
		}
		if v, exists := stepMap["speed"]; exists {
			speed, ok := asFloat(v)
			if !ok || speed < 0 {
				return RecordStep{}, fmt.Errorf("record spec %q: step %d speed requires number >= 0", path, index)
			}
			step.Speed = &speed
		}
		return step, nil
	}

	if hasCD {
		dir, ok := asString(stepMap["cd"])
		if !ok || strings.TrimSpace(dir) == "" {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d cd requires non-empty string", path, index)
		}
		return RecordStep{Kind: "cd", CD: dir}, nil
	}

	if hasMarker {
		label, ok := asString(stepMap["marker"])
		if !ok || strings.TrimSpace(label) == "" {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d marker requires non-empty string", path, index)
		}
		return RecordStep{Kind: "marker", Marker: strings.TrimSpace(label)}, nil
	}

	if hasAssert {
		m, ok := asMap(stepMap["assert"])
		if !ok {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d assert requires object", path, index)
		}
		cast := strings.TrimSpace(stringValue(m, "cast"))
		if cast == "" {
			cast = strings.TrimSpace(stringValue(m, "both"))
		}
		if cast == "" {
			cast = strings.TrimSpace(stringValue(m, "combined"))
		}
		assert := RecordAssert{
			Stdout: strings.TrimSpace(stringValue(m, "stdout")),
			Stderr: strings.TrimSpace(stringValue(m, "stderr")),
			Cast:   cast,
		}
		if assert.Stdout == "" && assert.Stderr == "" && assert.Cast == "" {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d assert requires stdout/stderr/cast expression", path, index)
		}
		return RecordStep{Kind: "assert", Assert: assert}, nil
	}

	if hasColor {
		color, err := parseRecordColor(path, index, stepMap["color"])
		if err != nil {
			return RecordStep{}, err
		}
		return RecordStep{Kind: "color", Color: &color}, nil
	}

	if hasExportToDirectory {
		dir, ok := asString(stepMap["export-to-directory"])
		if !ok || strings.TrimSpace(dir) == "" {
			return RecordStep{}, fmt.Errorf("record spec %q: step %d export-to-directory requires non-empty string", path, index)
		}
		return RecordStep{Kind: "export-to-directory", ExportToDirectory: strings.TrimSpace(dir)}, nil
	}

	envEntries, err := parseEnvEntries(path, index, stepMap["env"])
	if err != nil {
		return RecordStep{}, err
	}
	return RecordStep{Kind: "env", Env: envEntries}, nil
}

func parseRecordColor(path string, index int, raw interface{}) (RecordColor, error) {
	if s, ok := asString(raw); ok {
		if strings.EqualFold(strings.TrimSpace(s), "reset") {
			return RecordColor{Reset: true}, nil
		}
		return RecordColor{}, fmt.Errorf("record spec %q: step %d color string only supports %q", path, index, "reset")
	}

	m, ok := asMap(raw)
	if !ok {
		return RecordColor{}, fmt.Errorf("record spec %q: step %d color requires string or object", path, index)
	}

	color := RecordColor{}
	if v, exists := m["reset"]; exists {
		b, ok := asBool(v)
		if !ok {
			return RecordColor{}, fmt.Errorf("record spec %q: step %d color.reset requires bool", path, index)
		}
		color.Reset = b
	}
	if v, exists := m["fg"]; exists {
		s, ok := asString(v)
		if !ok || strings.TrimSpace(s) == "" {
			return RecordColor{}, fmt.Errorf("record spec %q: step %d color.fg requires non-empty string", path, index)
		}
		color.FG = strings.TrimSpace(s)
	}
	if v, exists := m["bg"]; exists {
		s, ok := asString(v)
		if !ok || strings.TrimSpace(s) == "" {
			return RecordColor{}, fmt.Errorf("record spec %q: step %d color.bg requires non-empty string", path, index)
		}
		color.BG = strings.TrimSpace(s)
	}
	if v, exists := m["bold"]; exists {
		b, ok := asBool(v)
		if !ok {
			return RecordColor{}, fmt.Errorf("record spec %q: step %d color.bold requires bool", path, index)
		}
		color.Bold = b
	}

	if !color.Reset && color.FG == "" && color.BG == "" {
		return RecordColor{}, fmt.Errorf("record spec %q: step %d color requires reset and/or fg/bg", path, index)
	}

	return color, nil
}

func parseEnvEntries(path string, index int, raw interface{}) ([]RecordEnvEntry, error) {
	list, ok := raw.([]interface{})
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("record spec %q: step %d env requires non-empty list", path, index)
	}
	out := make([]RecordEnvEntry, 0, len(list))
	for i, item := range list {
		m, ok := asMap(item)
		if !ok {
			return nil, fmt.Errorf("record spec %q: step %d env[%d] requires object", path, index, i)
		}
		entry, err := parseEnvEntry(path, index, m)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func parseEnvEntry(path string, index int, m map[string]interface{}) (RecordEnvEntry, error) {
	name := strings.TrimSpace(stringValue(m, "name"))
	if name == "" {
		return RecordEnvEntry{}, fmt.Errorf("record spec %q: step %d env entry requires name", path, index)
	}
	_, hasValue := m["value"]
	_, hasCEL := m["cel"]
	_, hasExec := m["exec"]
	count := 0
	if hasValue {
		count++
	}
	if hasCEL {
		count++
	}
	if hasExec {
		count++
	}
	if count != 1 {
		return RecordEnvEntry{}, fmt.Errorf("record spec %q: step %d env %q requires exactly one of value/cel/exec", path, index, name)
	}
	entry := RecordEnvEntry{Name: name}
	if hasValue {
		v, ok := asString(m["value"])
		if !ok {
			return RecordEnvEntry{}, fmt.Errorf("record spec %q: step %d env %q value requires string", path, index, name)
		}
		entry.Value = &v
	}
	if hasCEL {
		v, ok := asString(m["cel"])
		if !ok {
			return RecordEnvEntry{}, fmt.Errorf("record spec %q: step %d env %q cel requires string", path, index, name)
		}
		entry.CEL = &v
	}
	if hasExec {
		v, ok := asString(m["exec"])
		if !ok {
			return RecordEnvEntry{}, fmt.Errorf("record spec %q: step %d env %q exec requires string", path, index, name)
		}
		entry.Exec = &v
	}
	return entry, nil
}

func optionalString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, _ := asString(v)
	return s, true
}

func stringValue(m map[string]interface{}, key string) string {
	s, _ := asString(m[key])
	return s
}

func asMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if ok {
		return m, true
	}
	return nil, false
}

func asString(v interface{}) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func asBool(v interface{}) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func asFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	default:
		return 0, false
	}
}

func asInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if float64(int(n)) == n {
			return int(n), true
		}
		return 0, false
	default:
		return 0, false
	}
}
