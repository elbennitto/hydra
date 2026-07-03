package ci

import (
	"bytes"
	"fmt"
	stdlog "log"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

func TestWithCosignHydraLogger_RedirectsOutputToHydraLogger(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	formattedHandler := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})

	oldSlog := slog.Default()
	oldLogger := log.Default()
	slog.SetDefault(slog.New(formattedHandler))
	log.SetDefault(log.NewLoggerWithHandler(formattedHandler))
	t.Cleanup(func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldSlog)
	})

	origStdout := os.Stdout
	origStderr := os.Stderr
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	})

	err := withCosignHydraLogger(func() error {
		fmt.Fprintln(os.Stdout, "payload line")
		fmt.Fprintln(os.Stderr, "stderr line")
		stdlog.Print("stdlib line")
		fmt.Fprintln(os.Stderr, "WARNING: warning line")
		return nil
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "cosign: payload line")
	assert.Contains(t, out, "cosign: stderr line")
	assert.Contains(t, out, "cosign: stdlib line")
	assert.Contains(t, out, "cosign: warning line")
	assert.Contains(t, strings.ToUpper(out), "DEBUG")
}
