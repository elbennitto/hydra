package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/messagebox"
)

func newMessageCommand() *cobra.Command {
	var title string
	var text string

	cmd := &cobra.Command{
		Use:   "message",
		Short: "Print a styled Hydra hint box",
		Long: `Print a styled Hydra hint box.

Use --text for inline content, or pipe/stdin content when no --text value is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := resolveMessageText(cmd.InOrStdin(), text)
			if err != nil {
				return err
			}
			return messagebox.Print(resolveMessageOutput(cmd.OutOrStdout()), title, body)
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Optional box title")
	cmd.Flags().StringVar(&text, "text", "", "Message text; if empty, read from stdin")
	return cmd
}

func resolveMessageOutput(w io.Writer) io.Writer {
	// Hydra root command routes stdout through a debug-only slog writer.
	// Message boxes are user-facing output and must remain visible.
	if _, ok := w.(*log.SlogWriter); ok {
		return os.Stdout
	}
	return w
}

func resolveMessageText(r io.Reader, text string) (string, error) {
	if strings.TrimSpace(text) != "" {
		return strings.ReplaceAll(text, `\n`, "\n"), nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read message text: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("message text is required via --text or stdin")
	}
	return string(data), nil
}
