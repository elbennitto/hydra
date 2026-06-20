package helm

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type chartTemplateFile struct {
	displayPath string
	absPath     string
}

// NormalizeTemplateSourcePathPrefix trims space, maps to slash, drops a leading "./", and
// removes a trailing slash (except for "/").
func NormalizeTemplateSourcePathPrefix(p string) string {
	p = strings.TrimSpace(p)
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// TemplateSourcePathMatchesAnyPrefix reports whether displayPath matches at least one normalized
// source path prefix using the same rules as --include-path in hydra local source.
func TemplateSourcePathMatchesAnyPrefix(displayPath string, prefixes []string) bool {
	return displayPathMatchesAnySourcePrefix(displayPath, prefixes)
}

// displayPathMatchesAnySourcePrefix reports whether displayPath should be included for the given
// non-empty prefixes (chart-relative, forward slashes). Matching uses a path boundary after the
// prefix: exact equality, next rune is '/', or the prefix names a single file (full string match).
//
// If that fails, and the prefix contains at least one '/', it also matches when the prefix appears
// as a full segment sequence inside displayPath (preceded by '/'). Helm may prefix template paths
// with the parent chart name (for example
// "kube-prometheus-stack/charts/kube-prometheus-stack/templates/...") while users often pass the
// path fragment that appears under charts/ in the repository.
func displayPathMatchesAnySourcePrefix(displayPath string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	disp := filepath.ToSlash(displayPath)
	for _, pref := range prefixes {
		if pref == "" {
			continue
		}
		if sourcePathPrefixFromChartRoot(disp, pref) {
			return true
		}
		if sourcePathContainedSegment(disp, pref) {
			return true
		}
	}
	return false
}

func sourcePathPrefixFromChartRoot(disp, pref string) bool {
	if !strings.HasPrefix(disp, pref) {
		return false
	}
	if len(disp) == len(pref) {
		return true
	}
	return disp[len(pref)] == '/'
}

// sourcePathContainedSegment is the relaxed match: pref must contain '/' so we never treat a
// single path segment like "deployment" as a suffix of "foo-deployment".
func sourcePathContainedSegment(disp, pref string) bool {
	if !strings.Contains(pref, "/") {
		return false
	}
	if strings.Contains(disp, "/"+pref+"/") {
		return true
	}
	return strings.HasSuffix(disp, "/"+pref)
}

// ChartSourceTemplatesMultidocFromChartDirectory lists template-like files under templates/ and
// charts/*/templates/ (same layout Helm uses), sorted by path, and formats them like helm manifest
// headers. Path separators in "# Source:" are forward slashes for stable output on all platforms.
// If pathPrefixes is non-empty, only files whose display path matches [displayPathMatchesAnySourcePrefix]
// for at least one prefix are included (OR). Prefixes should be normalized with [NormalizeTemplateSourcePathPrefix].
func ChartSourceTemplatesMultidocFromChartDirectory(chartRoot string, pathPrefixes []string) (string, error) {
	var files []chartTemplateFile

	templatesDir := filepath.Join(chartRoot, "templates")
	if err := walkTemplateFiles(templatesDir, "templates", &files); err != nil {
		return "", err
	}

	chartsDir := filepath.Join(chartRoot, "charts")
	subCharts, err := os.ReadDir(chartsDir)
	if err == nil {
		for _, e := range subCharts {
			if !e.IsDir() {
				continue
			}
			subName := e.Name()
			subTemplates := filepath.Join(chartsDir, subName, "templates")
			prefix := pathJoinSlash("charts", subName, "templates")
			if err := walkTemplateFiles(subTemplates, prefix, &files); err != nil {
				return "", err
			}
		}
	}

	slices.SortFunc(files, func(a, b chartTemplateFile) int {
		return strings.Compare(a.displayPath, b.displayPath)
	})

	if len(pathPrefixes) > 0 {
		filtered := files[:0]
		for _, f := range files {
			if displayPathMatchesAnySourcePrefix(f.displayPath, pathPrefixes) {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}

	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f.absPath)
		if err != nil {
			return "", err
		}
		b.WriteString("---\n# Source: ")
		b.WriteString(f.displayPath)
		b.WriteString("\n")
		b.Write(data)
		if len(data) == 0 || data[len(data)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func pathJoinSlash(elem ...string) string {
	return strings.Join(elem, "/")
}

func walkTemplateFiles(absDir, displayPrefix string, out *[]chartTemplateFile) error {
	if _, err := os.Stat(absDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		srcName := pathJoinSlash(displayPrefix, rel)
		*out = append(*out, chartTemplateFile{displayPath: srcName, absPath: path})
		return nil
	})
}
