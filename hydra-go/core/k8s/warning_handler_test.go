package k8s

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

func TestKubernetesAPICtxWarningHandler_Info_LogsSourceIdAndMessage(t *testing.T) {
	var buf bytes.Buffer
	oldDefault := slog.Default()
	oldLogger := log.Default()

	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	formatted := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})
	slog.SetDefault(slog.New(formatted))
	logger := log.NewLogger()
	log.SetDefault(logger)

	t.Cleanup(func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldDefault)
	})

	logID := log.Hydra().Child("test")

	KubernetesAPICtxWarningHandler{
		Logger: logger,
		LogID:  logID,
		Source: KubernetesAPIWarningSourceServerSideDiff,
		Debug:  false,
	}.HandleWarningHeaderWithContext(
		context.Background(),
		299,
		"apiserver",
		`would violate PodSecurity "restricted:latest"`,
	)

	out := buf.String()
	assert.Contains(t, out, "server-side diff")
	assert.Contains(t, out, "status code")
	assert.Contains(t, out, "299")
	assert.Contains(t, out, "for id")
	assert.Contains(t, out, "unknown")
	assert.Contains(t, out, "would violate PodSecurity")
	assert.Contains(t, out, "restricted:latest")
}

func TestKubernetesAPICtxWarningHandler_Debug(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	formatted := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})
	slog.SetDefault(slog.New(formatted))
	logger := log.NewLogger()
	old := log.Default()
	log.SetDefault(logger)
	t.Cleanup(func() { log.SetDefault(old) })

	logID := log.Hydra().Child("test")

	KubernetesAPICtxWarningHandler{
		Logger: logger,
		LogID:  logID,
		Source: KubernetesAPIWarningSourceClusterScale,
		Debug:  true,
	}.HandleWarningHeaderWithContext(
		ContextWithResourceID(context.Background(), "apps/v1/Deployment/ns/foo"),
		299,
		"apiserver",
		"some admission warning",
	)

	out := buf.String()
	assert.Contains(t, out, "level=DEBUG")
	assert.Contains(t, out, "cluster scale")
	assert.Contains(t, out, "status code")
	assert.Contains(t, out, "299")
	assert.Contains(t, out, "for id")
	assert.Contains(t, out, "apps/v1/Deployment/ns/foo")
	assert.Contains(t, out, "some admission warning")
}

func TestKubernetesAPICtxWarningHandler_DefaultSourceWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	formatted := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})
	slog.SetDefault(slog.New(formatted))
	logger := log.NewLogger()
	old := log.Default()
	log.SetDefault(logger)
	t.Cleanup(func() { log.SetDefault(old) })

	logID := log.Hydra().Child("test")

	KubernetesAPICtxWarningHandler{
		Logger: logger,
		LogID:  logID,
		Source: "",
		Debug:  false,
	}.HandleWarningHeaderWithContext(context.Background(), 299, "", "x")

	assert.Contains(t, buf.String(), "Kubernetes API")
}

func TestKubernetesAPICtxWarningHandler_IgnoresNon299(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	formatted := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})
	slog.SetDefault(slog.New(formatted))
	logger := log.NewLogger()
	old := log.Default()
	log.SetDefault(logger)
	t.Cleanup(func() { log.SetDefault(old) })

	logID := log.Hydra().Child("test")

	KubernetesAPICtxWarningHandler{
		Logger: logger,
		LogID:  logID,
		Source: "test",
		Debug:  false,
	}.HandleWarningHeaderWithContext(context.Background(), 400, "", "nope")

	assert.Empty(t, buf.String())
}
