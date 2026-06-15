package record

import corerecord "hydra-gitops.org/hydra/hydra-go/core/record"

type RecordFile = corerecord.RecordFile
type RecordStep = corerecord.RecordStep
type RecordSpec = corerecord.RecordSpec

func Discover(specDir string) ([]RecordSpec, error) {
	return corerecord.Discover(specDir)
}

func Load(path string) (RecordFile, error) {
	return corerecord.Load(path)
}

func Find(specDir, slug string) (RecordSpec, error) {
	return corerecord.Find(specDir, slug)
}

func Resolve(specDir, input string) (RecordSpec, error) {
	return corerecord.Resolve(specDir, input)
}
