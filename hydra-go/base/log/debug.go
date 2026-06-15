package log

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// disabledCount is a process-wide counter of active "disable debug" requests.
// When >0, debug-level records are suppressed by the dynamic handler.
var disabledCount int32

// dynamicHandler wraps the original handler and suppresses debug-level records
// while disabledCount > 0. Multiple copies may be created via WithAttrs/WithGroup
// but they all consult the package-level disabledCount.
type dynamicHandler struct {
	base slog.Handler
}

func (d *dynamicHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if atomic.LoadInt32(&disabledCount) > 0 && level < slog.LevelInfo {
		return false
	}
	return d.base.Enabled(ctx, level)
}

func (d *dynamicHandler) Handle(ctx context.Context, r slog.Record) error {
	if !d.Enabled(ctx, r.Level) {
		return nil
	}
	markRecordWritten()
	return d.base.Handle(ctx, r)
}

func (d *dynamicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dynamicHandler{base: d.base.WithAttrs(attrs)}
}

func (d *dynamicHandler) WithGroup(name string) slog.Handler {
	return &dynamicHandler{base: d.base.WithGroup(name)}
}

// disableDebugLogging increments the disable counter and returns a restore function.
func disableDebugLogging() func() {
	atomic.AddInt32(&disabledCount, 1)
	return func() {
		atomic.AddInt32(&disabledCount, -1)
	}
}

// WithoutDebug0 executes the given function with debug logging disabled.
func WithoutDebug0(fn func()) {
	defer disableDebugLogging()()
	fn()
}

// WithoutDebug1 executes the given function with debug logging disabled.
func WithoutDebug1[T any](fn func() T) T {
	defer disableDebugLogging()()
	return fn()
}

// WithoutDebug2 executes the given function with debug logging disabled.
func WithoutDebug2[A, B any](fn func() (A, B)) (A, B) {
	defer disableDebugLogging()()
	return fn()
}

// WithoutDebug3 executes the given function with debug logging disabled.
func WithoutDebug3[A, B, C any](fn func() (A, B, C)) (A, B, C) {
	defer disableDebugLogging()()
	return fn()
}
