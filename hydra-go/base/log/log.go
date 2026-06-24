package log

import (
	"io"
	"log/slog"
	"os"
)

type Config struct {
	Level      slog.Level
	Json       bool
	Timestamps bool
	Colors     *ColorHandlerColors
	// ProgressBars, when non-nil, is used as the slog output [io.Writer] so log lines render above mpb bars.
	ProgressBars ProgressBars
}

// lastLogConfig is the most recent [Configure] argument, used to restore plain stderr logging after [CloseActiveProgressBars].
var lastLogConfig Config

// ColorEnabled reports whether the current log configuration uses ANSI colors.
func ColorEnabled() bool {
	return lastLogConfig.Colors != nil
}

func Configure(config Config) {
	lastLogConfig = config
	activeProgressBars = config.ProgressBars
	resetWriteState()

	opts := &slog.HandlerOptions{
		AddSource: config.Level <= slog.LevelDebug,
		Level:     config.Level,
	}

	// Create format handler options
	formatOpts := FormatOptions{
		RemoveUsedAttrs: true,
		AddTemplate:     config.Json,
	}

	out := io.Writer(os.Stderr)
	if config.ProgressBars != nil {
		out = config.ProgressBars
	}

	// Choose base handler based on Json config
	var baseHandler slog.Handler
	if config.Json {
		baseHandler = slog.NewJSONHandler(out, opts)
	} else {
		if config.Colors != nil {
			formatOpts.MessageColor = config.Colors.Message.String()
			formatOpts.MessageValueColor = config.Colors.MessageValue.String()
		}
		baseHandler = NewColorHandler(&ColorHandlerOptions{
			HandlerOpts: opts,
			Colors:      config.Colors,
			Timestamps:  config.Timestamps,
			Output:      out,
		})
	}

	formatHandler := NewFormatHandler(baseHandler, formatOpts)
	transformedHandler := &transformerHandler{handler: formatHandler, debug: config.Level == slog.LevelDebug}
	// dynamicHandler must wrap the configured chain so [WithoutDebugN] can suppress debug
	// without replacing [slog.Default] (which would drop Hydra formatting and break Helm's
	// slog.Info calls, e.g. symlink warnings in helm.sh/helm/v4/internal/sympath).
	defaultLogger := slog.New(&dynamicHandler{base: transformedHandler})

	slog.SetDefault(defaultLogger)
	// Re-bind the default logger so Logger methods use the configured handler
	// (not the pre-Configure default).
	SetDefault(NewLogger())
}
