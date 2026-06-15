package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalAppsRecordContextSetupFlow(t *testing.T) {
	t.Run("missing hydra context explains how to provide one", func(t *testing.T) {
		t.Setenv("HYDRA_CONTEXT", "")

		rootCmd, _ := newRootCommand(NewRootCommandParams())

		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs([]string{"--quiet", "local", "apps"})

		err := rootCmd.Execute()
		require.Error(t, err)

		assert.Empty(t, out.String())
		assert.Contains(t, err.Error(), "--hydra-context flag or HYDRA_CONTEXT environment variable is required")
	})

	t.Run("missing context directory returns actionable error", func(t *testing.T) {
		contextPath := filepath.Join(t.TempDir(), "my-first-hydra-context")
		t.Setenv("HYDRA_CONTEXT", contextPath)

		rootCmd, _ := newRootCommand(NewRootCommandParams())

		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs([]string{"--quiet", "local", "apps"})

		err := rootCmd.Execute()
		require.Error(t, err)

		assert.Empty(t, out.String())
		assert.Contains(t, err.Error(), "directory does not exist")
		assert.Contains(t, err.Error(), "HYDRA_CONTEXT")
		assert.Contains(t, err.Error(), contextPath)
	})

	t.Run("empty directory explains how to turn it into a hydra context", func(t *testing.T) {
		contextPath := filepath.Join(t.TempDir(), "my-first-hydra-context")
		require.NoError(t, os.MkdirAll(contextPath, 0o755))
		t.Setenv("HYDRA_CONTEXT", contextPath)

		rootCmd, _ := newRootCommand(NewRootCommandParams())

		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs([]string{"--quiet", "local", "apps"})

		err := rootCmd.Execute()
		require.Error(t, err)

		assert.Empty(t, out.String())
		assert.Contains(t, err.Error(), "could not find a valid Hydra context")
		assert.Contains(t, err.Error(), "global.hydra.type: context")
		assert.Contains(t, err.Error(), filepath.Join(contextPath, "values.yaml"))
	})
}
