package cmd

import (
	"io"
	"os"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"
)

// writeCommandResult flushes any active footer UI, writes the result to stdout line-by-line,
// then appends the extra trailing newline that fmt.Println-style command output always adds.
func writeCommandResult(result string) error {
	log.FlushProgressForStdout()

	for _, segment := range strings.SplitAfter(result, "\n") {
		if segment == "" {
			continue
		}
		if _, err := io.WriteString(os.Stdout, segment); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(os.Stdout, "\n"); err != nil {
		return err
	}

	log.SyncStdoutBestEffort()
	return nil
}
