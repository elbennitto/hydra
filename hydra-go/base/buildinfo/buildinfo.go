package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is the Hydra build version.
//
// It defaults to "dev" for local builds and tests and can be overridden via:
//
//	go build -ldflags "-X hydra-gitops.org/hydra/hydra-go/base/buildinfo.Version=v1.0.0"
var Version = "dev"

// TagSHA is the Git SHA of the release tag/commit.
//
// It can be overridden via:
//
//	go build -ldflags "-X hydra-gitops.org/hydra/hydra-go/base/buildinfo.TagSHA=<sha>"
var TagSHA = ""

var readBuildInfo = debug.ReadBuildInfo

// String returns the effective Hydra build version used across the CLI,
// logging, and artifact metadata.
func String() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		return "dev"
	}
	return v
}

// SHA returns the effective Git SHA associated with the build.
func SHA() string {
	if sha := normalizeSHA(TagSHA); sha != "" {
		return sha
	}

	if info, ok := readBuildInfo(); ok && info != nil {
		for _, setting := range info.Settings {
			if setting.Key != "vcs.revision" {
				continue
			}
			if sha := normalizeSHA(setting.Value); sha != "" {
				return sha
			}
		}
	}

	return "unknown"
}

func normalizeSHA(in string) string {
	value := strings.TrimSpace(in)
	if value == "" || strings.EqualFold(value, "unknown") {
		return ""
	}
	return value
}

// CLIString returns the canonical single-line CLI representation.
func CLIString() string {
	version := String()
	if version == "dev" {
		return "hydra " + version
	}
	return "hydra " + version + " " + SHA()
}
