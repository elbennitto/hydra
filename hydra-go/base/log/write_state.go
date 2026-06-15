package log

import "sync/atomic"

var wroteRecord atomic.Bool

func resetWriteState() {
	wroteRecord.Store(false)
}

func markRecordWritten() {
	wroteRecord.Store(true)
}

// HasWrittenRecord reports whether the currently configured logger emitted at
// least one record since the last Configure call.
func HasWrittenRecord() bool {
	return wroteRecord.Load()
}
