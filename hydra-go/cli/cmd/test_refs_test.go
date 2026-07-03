package cmd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
)

func TestTestRefsCommand_SilencesUsageOnExitCodeError(t *testing.T) {
	cmd := newTestRefsCommand(func(f action.TestRefsFlags) error {
		return exitcode.New(1, "1 test(s) failed")
	})

	cmd.SetArgs([]string{"dev.infra.myapp", "--hydra-context", "/tmp/ctx"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.True(t, cmd.SilenceUsage, "exitcode failures must not print CLI usage")
}

func TestTestRefsCommand_SilencesUsageOnPlainError(t *testing.T) {
	cmd := newTestRefsCommand(func(f action.TestRefsFlags) error {
		return fmt.Errorf("unexpected failure")
	})

	cmd.SetArgs([]string{"dev.infra.myapp", "--hydra-context", "/tmp/ctx"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.True(t, cmd.SilenceUsage, "plain errors must not print CLI usage")
}

func TestTestRefsCommand_ShowsUsageWhenWrappedWithShowUsage(t *testing.T) {
	cmd := newTestRefsCommand(func(f action.TestRefsFlags) error {
		return exitcode.WithShowUsage(fmt.Errorf("need help"))
	})

	cmd.SetArgs([]string{"dev.infra.myapp", "--hydra-context", "/tmp/ctx"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.False(t, cmd.SilenceUsage, "WithShowUsage must allow Cobra to print usage")
}
