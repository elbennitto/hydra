package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestString_DefaultsToDevWhenEmpty(t *testing.T) {
	old := Version
	Version = "   "
	t.Cleanup(func() {
		Version = old
	})

	if got := String(); got != "dev" {
		t.Fatalf("String() = %q, want %q", got, "dev")
	}
}

func TestCLIString_UsesNormalizedVersion(t *testing.T) {
	old := Version
	oldSha := TagSHA
	Version = " v1.2.3 "
	TagSHA = " abc123 "
	t.Cleanup(func() {
		Version = old
		TagSHA = oldSha
	})

	if got := CLIString(); got != "hydra v1.2.3 abc123" {
		t.Fatalf("CLIString() = %q, want %q", got, "hydra v1.2.3 abc123")
	}
}

func TestCLIString_DevDoesNotIncludeSHA(t *testing.T) {
	old := Version
	oldSha := TagSHA
	Version = "dev"
	TagSHA = "abc123"
	t.Cleanup(func() {
		Version = old
		TagSHA = oldSha
	})

	if got := CLIString(); got != "hydra dev" {
		t.Fatalf("CLIString() = %q, want %q", got, "hydra dev")
	}
}

func TestSHA_FallsBackToBuildInfo(t *testing.T) {
	oldSha := TagSHA
	oldRead := readBuildInfo
	TagSHA = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "deadbeef"}}}, true
	}
	t.Cleanup(func() {
		TagSHA = oldSha
		readBuildInfo = oldRead
	})

	if got := SHA(); got != "deadbeef" {
		t.Fatalf("SHA() = %q, want %q", got, "deadbeef")
	}
}

func TestSHA_DefaultsToUnknown(t *testing.T) {
	oldSha := TagSHA
	oldRead := readBuildInfo
	TagSHA = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}
	t.Cleanup(func() {
		TagSHA = oldSha
		readBuildInfo = oldRead
	})

	if got := SHA(); got != "unknown" {
		t.Fatalf("SHA() = %q, want %q", got, "unknown")
	}
}
