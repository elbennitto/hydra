package cmd

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/buildinfo"
	"hydra-gitops.org/hydra/hydra-go/base/utils"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yaml"
	"k8s.io/klog/v2"
)

// mockRootCommand holds captured flags from mock function calls
type mockRootCommand struct {
	// Captured flags - initially nil
	FindFlags          *action.FindFlags
	ClusterDumpFlags   *action.ClusterDumpFlags
	ConfigFlags        *action.ConfigFlags
	TemplateFlags      *action.TemplateFlags
	SourceFlags        *action.SourceFlags
	ValuesFlags        *action.ValuesFlags
	ReviewRefsFlags    *action.ReviewRefsFlags
	ExportContextFlags *action.ClusterViewContextFlags
}

// newMockRootCommand creates a new mock with all flags initially nil
func newMockRootCommand() *mockRootCommand {
	return &mockRootCommand{}
}

// rootCommandParams returns mock params that capture all flag objects.
func (m *mockRootCommand) rootCommandParams() *RootCommandParams {
	return &RootCommandParams{
		Find: func(flags action.FindFlags) (hydra.Hydra, string, error) {
			m.FindFlags = &flags
			return nil, "mock find", nil
		},
		Config: func(flags action.ConfigFlags) (hydra.Hydra, string, error) {
			m.ConfigFlags = &flags
			return nil, "mock config", nil
		},
		Template: func(flags action.TemplateFlags) (hydra.Hydra, string, error) {
			m.TemplateFlags = &flags
			return nil, "mock template", nil
		},
		Source: func(flags action.SourceFlags) (hydra.Hydra, string, error) {
			m.SourceFlags = &flags
			return nil, "mock source", nil
		},
		Values: func(flags action.ValuesFlags) (hydra.Hydra, string, error) {
			m.ValuesFlags = &flags
			return nil, "mock values", nil
		},
		Review: ReviewCommandParams{
			ReviewRefs: func(flags action.ReviewRefsFlags) error {
				m.ReviewRefsFlags = &flags
				return nil
			},
		},
		ExportContext: func(flags action.ClusterViewContextFlags) error {
			m.ExportContextFlags = &flags
			return nil
		},
	}
}

func TestMockRootCommand(t *testing.T) {
	t.Run("mock captures all flags initially nil", func(t *testing.T) {
		mock := newMockRootCommand()

		assert.Nil(t, mock.ConfigFlags)
		assert.Nil(t, mock.TemplateFlags)
		assert.Nil(t, mock.SourceFlags)
		assert.Nil(t, mock.ValuesFlags)
		assert.Nil(t, mock.ReviewRefsFlags)
		assert.Nil(t, mock.ExportContextFlags)
	})

	t.Run("mock captures find flags when called", func(t *testing.T) {
		mock := newMockRootCommand()
		params := mock.rootCommandParams()

		findFlags := action.FindFlags{
			ContextFlag:    flags.ContextFlag{HydraContext: "test"},
			ColorFlag:      flags.ColorFlag{Color: true},
			PickFlag:       flags.PickFlag{Pick: `appIds[0]`},
			UniqFlag:       flags.UniqFlag{Uniq: true},
			AppIdPatterns:  []types.AppIdPattern{"test.*.*"},
			PredicatesFlag: flags.PredicatesFlag{Predicates: []types.CelPredicate{`kind == "KafkaUser"`}},
		}
		_, _, err := params.Find(findFlags)

		require.NoError(t, err)
		require.NotNil(t, mock.FindFlags)
		assert.Equal(t, findFlags.Pick, mock.FindFlags.Pick)
	})

	t.Run("mock captures config flags when called", func(t *testing.T) {
		mock := newMockRootCommand()
		params := mock.rootCommandParams()

		configFlags := action.ConfigFlags{
			ContextFlag: flags.ContextFlag{HydraContext: "test"},
			AppIdFlag:   flags.AppIdFlag{AppId: "test.app"},
			ColorFlag:   flags.ColorFlag{Color: true},
		}
		_, _, err := params.Config(configFlags)

		require.NoError(t, err)
		require.NotNil(t, mock.ConfigFlags)
		assert.Equal(t, configFlags.AppId, mock.ConfigFlags.AppId)
	})

	t.Run("mock captures template flags when called", func(t *testing.T) {
		mock := newMockRootCommand()
		params := mock.rootCommandParams()

		templateFlags := action.TemplateFlags{
			ContextFlag: flags.ContextFlag{HydraContext: "test"},
			AppId:       "test.app",
			ColorFlag:   flags.ColorFlag{Color: true},
		}
		_, _, err := params.Template(templateFlags)

		require.NoError(t, err)
		require.NotNil(t, mock.TemplateFlags)
		assert.Equal(t, templateFlags.AppId, mock.TemplateFlags.AppId)
	})

	t.Run("mock captures values flags when called", func(t *testing.T) {
		mock := newMockRootCommand()
		params := mock.rootCommandParams()

		valuesFlags := action.ValuesFlags{
			ContextFlag:         flags.ContextFlag{HydraContext: "test"},
			AppIdFlag:           flags.AppIdFlag{AppId: "test.app"},
			ColorFlag:           flags.ColorFlag{Color: true},
			HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
		}
		_, _, err := params.Values(valuesFlags)

		require.NoError(t, err)
		require.NotNil(t, mock.ValuesFlags)
		assert.Equal(t, valuesFlags.AppId, mock.ValuesFlags.AppId)
	})

	t.Run("mock can be used with newRootCommand", func(t *testing.T) {
		mock := newMockRootCommand()
		rootCmd, flags := newRootCommand(mock.rootCommandParams())

		require.NotNil(t, rootCmd)
		require.NotNil(t, flags)
		assert.ElementsMatch(t, []string{"argocd", "ci", "cluster", "cosign", "gitops", "helm", "local", "message", "record", "version", "yq"}, commandUseNames(rootCmd.Commands()))

		// All flags should still be nil until mock functions are called
		assert.Nil(t, mock.FindFlags)
		assert.Nil(t, mock.ClusterDumpFlags)
		assert.Nil(t, mock.ConfigFlags)
		assert.Nil(t, mock.TemplateFlags)
		assert.Nil(t, mock.SourceFlags)
		assert.Nil(t, mock.ValuesFlags)
		assert.Nil(t, mock.ReviewRefsFlags)
	})

	t.Run("export subcommand exists", func(t *testing.T) {
		mock := newMockRootCommand()
		rootCmd, _ := newRootCommand(mock.rootCommandParams())

		require.NotNil(t, rootCmd)

		localCmd := childCommandWithUsePrefix(rootCmd, "local")
		require.NotNil(t, localCmd, "expected hydra local to exist")

		exportCmd := childCommandWithUsePrefix(localCmd, "export")
		require.NotNil(t, exportCmd, "expected hydra local export to exist")
	})

	t.Run("mock captures export context flags when called", func(t *testing.T) {
		mock := newMockRootCommand()
		params := mock.rootCommandParams()

		exportFlags := action.ClusterViewContextFlags{
			ClusterViewFlags: action.ClusterViewFlags{
				ContextFlag: flags.ContextFlag{HydraContext: "test"},
				ColorFlag:   flags.ColorFlag{Color: true},
			},
			OutputDir: "/tmp/export-test",
		}
		err := params.ExportContext(exportFlags)

		require.NoError(t, err)
		require.NotNil(t, mock.ExportContextFlags)
		assert.Equal(t, exportFlags.OutputDir, mock.ExportContextFlags.OutputDir)
	})

	t.Run("mock captures review refs flags when called", func(t *testing.T) {
		mock := newMockRootCommand()
		params := mock.rootCommandParams()

		reviewFlags := action.ReviewRefsFlags{
			ContextFlag:         flags.ContextFlag{HydraContext: "test"},
			ColorFlag:           flags.ColorFlag{Color: true},
			HelmNetworkModeFlag: flags.HelmNetworkModeFlag{HelmNetworkMode: types.HelmNetworkModeLocal},
			ExcludeAppFlag:      flags.ExcludeAppFlag{ExcludeAppPatterns: []types.AppIdPattern{"test.skip"}},
			AppIdPatterns:       []types.AppIdPattern{"test.*.*"},
		}
		err := params.Review.ReviewRefs(reviewFlags)

		require.NoError(t, err)
		require.NotNil(t, mock.ReviewRefsFlags)
		assert.Equal(t, reviewFlags.AppIdPatterns, mock.ReviewRefsFlags.AppIdPatterns)
		assert.Equal(t, reviewFlags.Color, mock.ReviewRefsFlags.Color)
	})
}

func TestRootCommandRegistersRootColorFlags(t *testing.T) {
	rootCmd, _ := newRootCommand(NewRootCommandParams())

	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("color"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("no-color"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("color-mode"))
}

func TestRootCommandAcceptsColorBeforeSubcommand(t *testing.T) {
	rootCmd, _ := newRootCommand(NewRootCommandParams())
	rootCmd.SetArgs([]string{"--color", "version"})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestRootCommandDelegatesCosignSubcommand(t *testing.T) {
	rootCmd, _ := newRootCommand(NewRootCommandParams())
	rootCmd.SetArgs([]string{"cosign", "version"})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestRootCommandDelegatesYqSubcommand(t *testing.T) {
	rootCmd, _ := newRootCommand(NewRootCommandParams())
	rootCmd.SetArgs([]string{"yq", "--version"})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestExecuteArgsDelegatesHelmSubcommand(t *testing.T) {
	err := ExecuteArgs([]string{"helm", "version"})
	require.NoError(t, err)
}

func TestExecuteArgsDelegatesHelmAfterHydraFlags(t *testing.T) {
	err := ExecuteArgs([]string{"--quiet", "helm", "version"})
	require.NoError(t, err)
}

func TestNormalizeInvocationArgs(t *testing.T) {
	t.Run("prepends top-level command from executable name", func(t *testing.T) {
		got, err := normalizeInvocationArgs("/usr/local/bin/yq", []string{"--version"})
		require.NoError(t, err)
		assert.Equal(t, []string{"yq", "--version"}, got)
	})

	t.Run("does not duplicate when command already present", func(t *testing.T) {
		got, err := normalizeInvocationArgs("helm", []string{"helm", "version"})
		require.NoError(t, err)
		assert.Equal(t, []string{"helm", "version"}, got)
	})

	t.Run("does not prepend for hydra executable", func(t *testing.T) {
		got, err := normalizeInvocationArgs("hydra", []string{"version"})
		require.NoError(t, err)
		assert.Equal(t, []string{"version"}, got)
	})

	t.Run("does not prepend for unknown executable", func(t *testing.T) {
		got, err := normalizeInvocationArgs("mytool", []string{"version"})
		require.NoError(t, err)
		assert.Equal(t, []string{"version"}, got)
	})

}

func TestExecuteWithArgvUsesInvocationNameForYq(t *testing.T) {
	err := executeWithArgv([]string{"yq", "--version"})
	require.NoError(t, err)
}

func TestExecuteWithArgvUsesInvocationNameForHelm(t *testing.T) {
	err := executeWithArgv([]string{"helm", "version"})
	require.NoError(t, err)
}

func TestRootCommandDelegatesYqDefaultEval(t *testing.T) {
	rootCmd, _ := newRootCommand(NewRootCommandParams())
	rootCmd.SetArgs([]string{"yq", ".a"})

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString("a: hello\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		require.NoError(t, r.Close())
	}()

	oldStdout := os.Stdout
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = outW

	err = rootCmd.Execute()
	require.NoError(t, err)
	require.NoError(t, outW.Close())
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, outR)
	require.NoError(t, err)
	require.NoError(t, outR.Close())

	assert.Contains(t, buf.String(), "hello")
}

func TestCliParameter(t *testing.T) {
	testCases := map[string]*mockRootCommand{}

	for testName, expected := range testCases {
		t.Run(testName, func(t *testing.T) {
			defer utils.EnvWrapper("HYDRA_CONTEXT", "/test")()
			args := strings.Fields(testName)
			given := newMockRootCommand()
			rootCmd, globalFlags := newRootCommand(given.rootCommandParams())
			rootCmd.SetArgs(args)

			require.NotNil(t, rootCmd)
			require.NotNil(t, globalFlags)

			err := rootCmd.Execute()
			if expected == nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				actual, err := yaml.ToYaml(given)
				require.NoError(t, err)

				expected, err := yaml.ToYaml(expected)
				require.NoError(t, err)

				assert.Equal(t, expected, actual)
			}
		})
	}
}

// TestKlogRedirectToSlog verifies that klog output is redirected through slog when
// klog.SetLogger(logr.FromSlogHandler(...)) is used. This tests the mechanism that
// configureLogging() will use to redirect Kubernetes client-go warnings (which use
// klog) through the Hydra slog handler chain.
func TestKlogRedirectToSlog(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := logr.FromSlogHandler(handler)
	klog.SetLogger(logger)
	defer klog.ClearLogger()

	klog.Warning("test message")
	klog.Flush()

	assert.Contains(t, buf.String(), "test message")
}

func TestConfigureLoggingWelcome(t *testing.T) {
	t.Run("prints welcome with version after log init", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stderr = w

		flags := &GlobalFlags{}
		cmd := &cobra.Command{Use: "local"}
		configureLogging(flags, cmd)

		require.NoError(t, w.Close())
		os.Stderr = oldStderr

		var buf bytes.Buffer
		_, err = io.Copy(&buf, r)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		out := buf.String()
		assert.Contains(t, out, "Welcome to Hydra")
		assert.Contains(t, out, buildinfo.String())
	})

	t.Run("skips welcome for version subcommand", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stderr = w

		flags := &GlobalFlags{}
		cmd := &cobra.Command{Use: "version"}
		configureLogging(flags, cmd)

		require.NoError(t, w.Close())
		os.Stderr = oldStderr

		var buf bytes.Buffer
		_, err = io.Copy(&buf, r)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		assert.NotContains(t, buf.String(), "Welcome to Hydra")
	})

	t.Run("skips welcome for message subcommand", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stderr = w

		flags := &GlobalFlags{}
		root := &cobra.Command{Use: "hydra"}
		cmd := &cobra.Command{Use: "message"}
		root.AddCommand(cmd)
		configureLogging(flags, cmd)

		require.NoError(t, w.Close())
		os.Stderr = oldStderr

		var buf bytes.Buffer
		_, err = io.Copy(&buf, r)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		assert.NotContains(t, buf.String(), "Welcome to Hydra")
	})

	t.Run("skips welcome when quiet", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stderr = w

		flags := &GlobalFlags{Quiet: true}
		cmd := &cobra.Command{Use: "local"}
		configureLogging(flags, cmd)

		require.NoError(t, w.Close())
		os.Stderr = oldStderr

		var buf bytes.Buffer
		_, err = io.Copy(&buf, r)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		assert.NotContains(t, buf.String(), "Welcome to Hydra")
	})
}
