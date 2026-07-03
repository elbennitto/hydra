package cmd

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
)

type testTimeoutFlags struct {
	flags.ScaleTimeoutFlag
	flags.CrdTimeoutFlag
}

var _ flags.Flags = (*testTimeoutFlags)(nil)
var _ flags.WithScaleTimeoutFlag = (*testTimeoutFlags)(nil)
var _ flags.WithCrdTimeoutFlag = (*testTimeoutFlags)(nil)

func (f *testTimeoutFlags) Flags() flags.Flags {
	return f
}

func newTestTimeoutCommand(f *testTimeoutFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "test-timeout",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	if err := DefineFlags(cmd, f); err != nil {
		panic(err)
	}
	return cmd
}

func TestScaleTimeoutFlag_Default(t *testing.T) {
	f := &testTimeoutFlags{}
	cmd := newTestTimeoutCommand(f)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, 10*time.Minute, f.ScaleTimeout)
}

func TestScaleTimeoutFlag_CustomValue(t *testing.T) {
	f := &testTimeoutFlags{}
	cmd := newTestTimeoutCommand(f)
	cmd.SetArgs([]string{"--scale-timeout", "5m"})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, 5*time.Minute, f.ScaleTimeout)
}

func TestCrdTimeoutFlag_Default(t *testing.T) {
	f := &testTimeoutFlags{}
	cmd := newTestTimeoutCommand(f)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, 60*time.Second, f.CrdTimeout)
}

func TestCrdTimeoutFlag_CustomValue(t *testing.T) {
	f := &testTimeoutFlags{}
	cmd := newTestTimeoutCommand(f)
	cmd.SetArgs([]string{"--crd-timeout", "2m"})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, 2*time.Minute, f.CrdTimeout)
}

func TestTimeoutFlags_BothCustomValues(t *testing.T) {
	f := &testTimeoutFlags{}
	cmd := newTestTimeoutCommand(f)
	cmd.SetArgs([]string{"--scale-timeout", "5m", "--crd-timeout", "90s"})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, 5*time.Minute, f.ScaleTimeout)
	assert.Equal(t, 90*time.Second, f.CrdTimeout)
}
