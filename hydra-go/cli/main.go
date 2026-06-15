package main

import (
	"fmt"
	"os"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/cmd"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		if !log.HasWrittenRecord() {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		// Best-effort flush for short-lived TTY failures so the final log/error line
		// is not lost when the process exits immediately afterward.
		_ = os.Stderr.Sync()
		log.SyncStdoutBestEffort()
		if code, ok := exitcode.As(err); ok {
			os.Exit(code)
		}
		os.Exit(1)
	}
}
