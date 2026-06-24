package log

import "log/slog"

type extendedHelpArg struct {
	key   string
	value any
}

// Attr is an alias for slog.Attr
type Attr = slog.Attr

// Level is an alias for slog.Level
type Level = slog.Level

// Level constants
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// String returns an Attr for a string value.
func String(key, value string) Attr {
	return slog.String(key, value)
}

// Int returns an Attr for an int value.
func Int(key string, value int) Attr {
	return slog.Int(key, value)
}

// Int64 returns an Attr for an int64 value.
func Int64(key string, value int64) Attr {
	return slog.Int64(key, value)
}

// Bool returns an Attr for a bool value.
func Bool(key string, value bool) Attr {
	return slog.Bool(key, value)
}

// Any returns an Attr for any value.
func Any(key string, value any) Attr {
	return slog.Any(key, value)
}

// Err returns an Attr for an error value with key "err".
func Err(err error) Attr {
	return slog.Any("err", err)
}

// ExtendedHelp passes structured data to error markdown templates.
// This argument is not emitted to the logger output.
func ExtendedHelp(key string, value any) any {
	return extendedHelpArg{key: key, value: value}
}
