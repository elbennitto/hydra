package action

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

type TestRefsFlags struct {
	flags.ContextFlag
	flags.HelmNetworkModeFlag
	flags.ExcludeAppFlag
	flags.NoCacheFlag
	AppIdPatterns []types.AppIdPattern
	Update        bool
}

var _ flags.WithContextFlag = (*TestRefsFlags)(nil)

func (f *TestRefsFlags) Flags() flags.Flags {
	return f
}

type testRefsExpected struct {
	RefDefinitions []types.RefDefinition `yaml:"refDefinitions"`
	Refs           []types.Ref           `yaml:"refs"`
}

func TestRefs(f TestRefsFlags) error {
	l := log.Default()

	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo)

	appIds, err := commands.ResolveAppIdsFromConfig(l, f.HydraContext, config, f.AppIdPatterns, f.ExcludeAppPatterns, f.HelmNetworkMode, false)
	if err != nil {
		return err
	}

	var totalTests, totalPassed, totalFailed, totalUpdated int

	for appId := range appIds {
		l.Info(logIdAction, "Testing ref-parsers for app '{appId}'", log.String("appId", string(appId)))

		h, err := hydra.ResolvePathWithAppId(l, f.HydraContext, appId, config)
		if err != nil {
			return err
		}

		extraParsers, err := loadAppRefParsers(h, f.HelmNetworkMode)
		if err != nil {
			return err
		}

		if len(extraParsers) == 0 {
			l.Warn(logIdAction, "No ref-parsers found in global.hydra.refs for app '{appId}', skipping",
				log.String("appId", string(appId)))
			continue
		}

		chartPath, err := resolveChartPath(h)
		if err != nil {
			return err
		}

		testDir := filepath.Join(chartPath, "test", "refs")
		if _, err := os.Stat(testDir); os.IsNotExist(err) {
			l.Warn(logIdAction, "No test/refs/ directory found in chart '{path}', skipping",
				log.String("path", chartPath))
			continue
		}

		entries, err := os.ReadDir(testDir)
		if err != nil {
			return fmt.Errorf("reading test directory %s: %w", testDir, err)
		}

		for _, entry := range entries {
			name := entry.Name()
			base, found := strings.CutSuffix(name, ".given.yaml")
			if !found {
				continue
			}

			testName := fmt.Sprintf("%s/%s", appId, base)
			givenPath := filepath.Join(testDir, name)
			expectedPath := filepath.Join(testDir, base+".expected.yaml")
			totalTests++

			givenBytes, err := os.ReadFile(givenPath)
			if err != nil {
				return fmt.Errorf("reading %s: %w", givenPath, err)
			}

			entities, err := entity.NewEntitiesFromYaml(l, types.YamlString(givenBytes), types.KeyClusterEntity)
			if err != nil {
				return fmt.Errorf("parsing entities from %s: %w", givenPath, err)
			}

			refDefs, err := references.RefDefinitions(entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, extraParsers)
			if err != nil {
				return fmt.Errorf("computing refDefinitions for %s: %w", testName, err)
			}

			refs, err := references.Refs(l, entities, types.KeyClusterEntity, nil, entity.Entities{}, entity.Entities{}, nil, extraParsers)
			if err != nil {
				return fmt.Errorf("computing refs for %s: %w", testName, err)
			}
			refs = references.AnnotateRefsWithSource(refs, types.RefSourceTest)

			actual := testRefsExpected{
				RefDefinitions: refDefs,
				Refs:           refs,
			}

			if f.Update {
				actualBytes, err := yaml.Marshal(actual)
				if err != nil {
					return fmt.Errorf("marshaling expected for %s: %w", testName, err)
				}
				if err := os.WriteFile(expectedPath, actualBytes, 0644); err != nil {
					return fmt.Errorf("writing %s: %w", expectedPath, err)
				}
				l.Info(logIdAction, "  UPDATED {test}", log.String("test", testName))
				totalUpdated++
				continue
			}

			expectedBytes, err := os.ReadFile(expectedPath)
			if err != nil {
				l.Error(logIdAction, "  FAIL {test}: missing expected file {path}",
					log.String("test", testName), log.String("path", expectedPath))
				totalFailed++
				continue
			}

			var expected testRefsExpected
			if err := yaml.Unmarshal(expectedBytes, &expected); err != nil {
				return fmt.Errorf("parsing %s: %w", expectedPath, err)
			}

			if !refsEqual(actual, expected) {
				l.Error(logIdAction, "  FAIL {test}: output does not match expected",
					log.String("test", testName))
				totalFailed++
			} else {
				l.Info(logIdAction, "  PASS {test}", log.String("test", testName))
				totalPassed++
			}
		}
	}

	if f.Update {
		l.Info(logIdAction, "Updated {count} expected files", log.Int("count", totalUpdated))
		return nil
	}

	l.Info(logIdAction, "Test results: {total} tests, {passed} passed, {failed} failed",
		log.Int("total", totalTests), log.Int("passed", totalPassed), log.Int("failed", totalFailed))

	if totalFailed > 0 {
		return exitcode.Newf(1, "%d test(s) failed", totalFailed)
	}
	return nil
}

func loadAppRefParsers(h hydra.HydraApp, networkMode types.HelmNetworkMode) ([]types.RefParser, error) {
	cluster := clusterFromHydraApp(h)
	if cluster == nil {
		return nil, fmt.Errorf("could not resolve cluster for app %s", h.AppId())
	}
	appIds := sets.New(h.AppId())
	rendered, err := commands.RenderClusterSelectedApps(cluster, networkMode, "", appIds, types.KeyTemplateEntity)
	if err != nil {
		return nil, err
	}
	return hydra.HydraAppRefParsers(cluster, appIds, networkMode, rendered)
}

func resolveChartPath(h hydra.HydraApp) (string, error) {
	if childApp := h.AsChildApp(); childApp != nil {
		chartDir, err := childApp.ChildAppPath()
		if err != nil {
			return "", err
		}
		return chartDir.Path(), nil
	}
	if rootApp := h.AsRootApp(); rootApp != nil {
		return rootApp.RootAppPath().Path(), nil
	}
	return "", fmt.Errorf("cannot determine chart path for app %s", h.AppId())
}

func refsEqual(a, b testRefsExpected) bool {
	if len(a.RefDefinitions) != len(b.RefDefinitions) {
		return false
	}
	if len(a.Refs) != len(b.Refs) {
		return false
	}

	aDefBytes, _ := yaml.Marshal(a.RefDefinitions)
	bDefBytes, _ := yaml.Marshal(b.RefDefinitions)
	if string(aDefBytes) != string(bDefBytes) {
		return false
	}

	aRefBytes, _ := yaml.Marshal(a.Refs)
	bRefBytes, _ := yaml.Marshal(b.Refs)
	return string(aRefBytes) == string(bRefBytes)
}
