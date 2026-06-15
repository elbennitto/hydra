package record

import (
	corerecord "hydra-gitops.org/hydra/hydra-go/core/record"
)

type RecordOptions = corerecord.RecordOptions

type RecordFileOutputKind = corerecord.RecordFileOutputKind

const (
	RecordFileOutputText    = corerecord.RecordFileOutputText
	RecordFileOutputControl = corerecord.RecordFileOutputControl
)

type RecordFileOutput = corerecord.RecordFileOutput

func RecordOne(file string, opts RecordOptions) error {
	return corerecord.RecordOne(file, opts)
}

func RecordAll(opts RecordOptions) error {
	return corerecord.RecordAll(opts)
}
