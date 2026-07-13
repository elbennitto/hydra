package record

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var updateRecordFileGolden = flag.Bool("update", false, "update record file golden files")

var pointerAddressRegexp = regexp.MustCompile(`0x[0-9a-fA-F]+`)

func TestRecordFileGolden(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	pkgDir := filepath.Dir(thisFile)
	goldenRoot := filepath.Join(pkgDir, "testdata", "record_file")

	givenFiles, err := listRecordFileGoldenCases(goldenRoot)
	require.NoError(t, err)
	require.NotEmpty(t, givenFiles, "no *.given.yaml files under %s", goldenRoot)

	for _, givenPath := range givenFiles {
		relPath, err := filepath.Rel(goldenRoot, givenPath)
		require.NoError(t, err)
		caseName := strings.TrimSuffix(relPath, ".given.yaml")

		t.Run(caseName, func(t *testing.T) {
			givenBytes, err := os.ReadFile(givenPath)
			require.NoError(t, err)

			specDir := t.TempDir()
			outDir := t.TempDir()
			specPath := filepath.Join(specDir, caseName+".cast.yaml")
			require.NoError(t, os.MkdirAll(filepath.Dir(specPath), 0o755))
			require.NoError(t, os.WriteFile(specPath, givenBytes, 0o644))

			require.NoError(t, RecordOne(specPath, RecordOptions{
				HydraBin:     "/bin/echo",
				SpecDir:      specDir,
				OutputDir:    outDir,
				MirrorOutput: false,
			}))

			gotPath := filepath.Join(outDir, caseName+".cast")
			gotBytes, err := os.ReadFile(gotPath)
			require.NoError(t, err)

			expectedPath := filepath.Join(goldenRoot, caseName+".expected.cast")
			if *updateRecordFileGolden {
				require.NoError(t, os.MkdirAll(filepath.Dir(expectedPath), 0o755))
				require.NoError(t, os.WriteFile(expectedPath, normalizeCastHeaderCommand(gotBytes, caseName), 0o644))
			}

			expectedBytes, err := os.ReadFile(expectedPath)
			require.NoError(t, err, "missing golden file; run: go test ./core/record -run TestRecordFileGolden -update")
			require.Equal(t, string(normalizeCastHeaderCommand(expectedBytes, caseName)), string(normalizeCastHeaderCommand(gotBytes, caseName)))

			combinedOut, runErr := captureCombinedOutput(func() error {
				return RecordOne(specPath, RecordOptions{
					HydraBin:     "/bin/echo",
					SpecDir:      specDir,
					OutputDir:    t.TempDir(),
					MirrorOutput: true,
				})
			})
			require.NoError(t, runErr)

			if bytes.Contains(givenBytes, []byte("OUT_STDOUT")) {
				require.Contains(t, string(combinedOut), "OUT_STDOUT")
			}
			if bytes.Contains(givenBytes, []byte("ERR_STDERR")) {
				require.Contains(t, string(combinedOut), "ERR_STDERR")
			}
		})
	}
}

func TestTutorialRecordingsGolden(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	pkgDir := filepath.Dir(thisFile)
	moduleRoot := filepath.Clean(filepath.Join(pkgDir, "..", ".."))
	repoRoot := filepath.Clean(filepath.Join(moduleRoot, ".."))
	tutorialRoot := filepath.Join(repoRoot, "docs", "manual")

	specs, err := Discover(tutorialRoot)
	require.NoError(t, err)
	require.NotEmpty(t, specs, "no tutorial specs found under %s", tutorialRoot)

	hydraBin := buildHydraCLIBinary(t, moduleRoot)
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	for _, spec := range specs {
		spec := spec
		t.Run(spec.OutputBase, func(t *testing.T) {
			outDir := t.TempDir()
			inputPath, err := filepath.Rel(repoRoot, spec.Path)
			require.NoError(t, err)

			require.NoError(t, RecordOne(inputPath, RecordOptions{
				HydraBin:        hydraBin,
				HydraGlobalArgs: []string{"--no-timestamps"},
				SpecDir:         tutorialRoot,
				OutputDir:       outDir,
				MirrorOutput:    false,
			}))

			gotPath := filepath.Join(outDir, spec.OutputBase+".cast")
			gotBytes, err := os.ReadFile(gotPath)
			require.NoError(t, err)
			normalizedGotBytes := normalizePointerAddresses(gotBytes)

			expectedPath := TrimRecordSpecExtension(spec.Path) + ".cast"
			if *updateRecordFileGolden {
				require.NoError(t, os.WriteFile(expectedPath, normalizedGotBytes, 0o644))
			}

			expectedBytes, err := os.ReadFile(expectedPath)
			require.NoError(t, err, "missing golden cast; run: go test ./core/record -run TestTutorialRecordingsGolden -update")
			normalizedExpectedBytes := normalizePointerAddresses(expectedBytes)
			require.Equal(t, string(normalizedExpectedBytes), string(normalizedGotBytes))
		})
	}
}

func buildHydraCLIBinary(t *testing.T, moduleRoot string) string {
	t.Helper()

	hydraBin := filepath.Join(t.TempDir(), "hydra")
	cmd := exec.Command("go", "build", "-o", hydraBin, "./cli")
	cmd.Dir = moduleRoot
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build hydra cli binary: %s", string(out))
	return hydraBin
}

func captureCombinedOutput(run func() error) ([]byte, error) {
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = r.Close()
		_ = w.Close()
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	}()

	os.Stdout = w
	os.Stderr = w

	runErr := run()
	if closeErr := w.Close(); closeErr != nil && runErr == nil {
		runErr = closeErr
	}

	out, readErr := io.ReadAll(r)
	if readErr != nil && runErr == nil {
		runErr = readErr
	}

	return out, runErr
}

func normalizeCastHeaderCommand(cast []byte, caseName string) []byte {
	lines := bytes.Split(cast, []byte("\n"))
	if len(lines) == 0 || len(lines[0]) == 0 {
		return cast
	}

	var header map[string]any
	if err := json.Unmarshal(lines[0], &header); err != nil {
		return cast
	}
	header["command"] = "hydra record file " + caseName
	normalizedHeader, err := json.Marshal(header)
	if err != nil {
		return cast
	}

	lines[0] = normalizedHeader
	return bytes.Join(lines, []byte("\n"))
}

func normalizePointerAddresses(cast []byte) []byte {
	return pointerAddressRegexp.ReplaceAll(cast, []byte("0xPTR"))
}

func listRecordFileGoldenCases(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".given.yaml") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
