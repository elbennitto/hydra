package asciinema

import (
	"os"
	"testing"
	"time"
)

func TestBenchCaptureEchoScript(t *testing.T) {
	script := "#!/bin/bash\nset -euo pipefail\necho 'benchmark help: hydra gitops apply --help'\n"
	require := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.CreateTemp("", "bench-*.sh")
	require(err)
	_, _ = f.WriteString(script)
	require(f.Chmod(0o755))
	require(f.Close())
	defer os.Remove(f.Name())

	t0 := time.Now()
	_, err = captureScriptOutput(f.Name(), recordingEnv(), nil)
	require(err)
	t.Logf("echo capture: %v", time.Since(t0))
}

func TestBenchCaptureHydraScript(t *testing.T) {
	if _, err := os.Stat("/tmp/rec-hydra.sh"); err != nil {
		if os.IsNotExist(err) {
			t.Skip("skipping benchmark helper test: /tmp/rec-hydra.sh not present")
		}
		t.Fatal(err)
	}
	t0 := time.Now()
	_, err := captureScriptOutput("/tmp/rec-hydra.sh", recordingEnv(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("hydra capture: %v", time.Since(t0))
}
