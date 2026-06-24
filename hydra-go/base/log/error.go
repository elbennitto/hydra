package log

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime"
	"strings"
	"time"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
)

type lazyError struct {
	id      errors.ErrorId
	message string
	params  map[string]any
	Log     func()
}

var _ error = (*lazyError)(nil)
var _ errors.Error = (*lazyError)(nil)

func (e lazyError) Error() string {
	return e.message
}

func (e lazyError) ErrorId() errors.ErrorId {
	return e.id
}

func (e lazyError) ErrorTemplateParams() map[string]any {
	if len(e.params) == 0 {
		return nil
	}
	return maps.Clone(e.params)
}

// CreateError returns a lazily logged error value.
// The returned error can later be emitted via [LogLazy].
// It replaces {key} placeholders in the message with slog attribute values.
// Usage: return nil, log.CreateError("failed to process: {err}", slog.Any("err", err))
func CreateError(id errors.ErrorId, msg string, args ...any) error {
	return logWithCaller(slog.LevelError, id, msg, args...)
}

// CreateWarn returns a lazily logged warning value.
func CreateWarn(id errors.ErrorId, msg string, args ...any) error {
	return logWithCaller(slog.LevelWarn, id, msg, args...)
}

// CreateInfo returns a lazily logged info value.
func CreateInfo(id errors.ErrorId, msg string, args ...any) error {
	return logWithCaller(slog.LevelInfo, id, msg, args...)
}

// captureStack captures the call stack, skipping the given number of frames.
// Returns the caller PC and a formatted stack trace string.
func captureStack(skip int) (uintptr, string) {
	var pcs [32]uintptr
	n := runtime.Callers(skip, pcs[:])
	if n == 0 {
		return 0, ""
	}
	callerPC := pcs[0]

	// Build stack trace string
	frames := runtime.CallersFrames(pcs[:n])
	var sb strings.Builder
	for {
		frame, more := frames.Next()
		if frame.File == "" {
			if !more {
				break
			}
			continue
		}
		fmt.Fprintf(&sb, "    %s\n        %s:%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
	}
	return callerPC, sb.String()
}

func logWithCaller(level slog.Level, id errors.ErrorId, msg string, args ...any) error {
	// Replace placeholders in message (no color codes for error logging)
	message := ReplacePlaceholdersWithArgs(msg, args, "", "").Message
	params := attrsToParamMap(args)
	logArgs := argsForLogger(args)

	// Capture the real caller PC and stack trace now (skip: Callers, captureStack, logWithCaller, CreateError/CreateWarn/CreateInfo)
	callerPC, stack := captureStack(4)

	return &lazyError{
		id:      id,
		message: message,
		params:  params,
		Log: func() {
			// Build a slog.Record manually with the real caller's PC
			r := slog.NewRecord(time.Now(), level, msg, callerPC)
			if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
				r.Add(slog.String("logId", string(id)))
			}
			r.Add(logArgs...)
			if level >= slog.LevelError && stack != "" && slog.Default().Enabled(context.Background(), slog.LevelDebug) {
				r.Add(slog.String("stack", "\n"+stack))
			}
			_ = slog.Default().Handler().Handle(context.Background(), r)
		},
	}
}

func attrsToParamMap(args []any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := map[string]any{}
	for i := range args {
		switch arg := args[i].(type) {
		case slog.Attr:
			if arg.Key == "" {
				continue
			}
			out[arg.Key] = arg.Value.Any()
		case extendedHelpArg:
			if arg.key == "" {
				continue
			}
			// ExtendedHelp is markdown-only and can intentionally override string attrs.
			out[arg.key] = arg.value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func argsForLogger(args []any) []any {
	if len(args) == 0 {
		return nil
	}
	out := make([]any, 0, len(args))
	for i := range args {
		if _, ok := args[i].(extendedHelpArg); ok {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// returnedErrorAlreadyEmitted wraps an error whose causes were already emitted via [LogLazy]
// (e.g. batched ref-ownership conflicts). [LogLazy] on this wrapper is a no-op and returns true
// so the CLI does not print the same failure again when handling the return value.
type returnedErrorAlreadyEmitted struct {
	err error
}

func (e *returnedErrorAlreadyEmitted) Error() string {
	return e.err.Error()
}

func (e *returnedErrorAlreadyEmitted) Unwrap() error {
	return e.err
}

func (e *returnedErrorAlreadyEmitted) ErrorId() errors.ErrorId {
	if he, ok := e.err.(errors.Error); ok {
		return he.ErrorId()
	}
	return errors.ErrUnknown
}

func (e *returnedErrorAlreadyEmitted) ErrorTemplateParams() map[string]any {
	if withParams, ok := e.err.(errors.ErrorTemplateParamsProvider); ok {
		return withParams.ErrorTemplateParams()
	}
	return nil
}

var _ errors.Error = (*returnedErrorAlreadyEmitted)(nil)

// ReturnedErrorAlreadyEmitted returns err wrapped so [LogLazy] treats the value as already logged.
// Pass nil to get nil.
func ReturnedErrorAlreadyEmitted(err error) error {
	if err == nil {
		return nil
	}
	return &returnedErrorAlreadyEmitted{err: err}
}

func LogLazy(err error) bool {
	var already *returnedErrorAlreadyEmitted
	if stderrors.As(err, &already) {
		return true
	}
	lazy, ok := err.(*lazyError)
	if ok {
		lazy.Log()
	}
	return ok
}
