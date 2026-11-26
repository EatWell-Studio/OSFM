// Package version reports the OSFM software version.
//
// A release build sets it from the version tag through the linker:
//
//	go build -ldflags "-X github.com/EatWell-Studio/OSFM/internal/version.version=v1.2.3" ./cmd/osfm
//
// The release workflow and deploy/Dockerfile (build argument VERSION) do this. Without it, the version comes from the
// build information the Go toolchain stamps into the binary: the module version for `go install …@v1.2.3`, or, when
// built in a Git checkout, a pseudo-version for the commit (Go 1.24 and later). A build without either, such as one from
// a source tree without .git, reports "dev".
package version

import (
	"runtime/debug"
	"strings"
)

// Dev is the version of a build that carries no version information.
const Dev = "dev"

// version is set by the linker (-X); empty otherwise.
var version string

// String returns the software version without a leading "v", for example "1.2.3", "1.3.0-rc.1",
// "0.0.0-20261126101500-0123456789ab+dirty" for an untagged commit, or [Dev].
func String() string {
	info, _ := debug.ReadBuildInfo()
	return resolve(version, info)
}

// resolve picks the version from the linker-set value, then from the build information.
func resolve(linked string, info *debug.BuildInfo) string {
	if v := strings.TrimSpace(linked); v != "" {
		return trimV(v)
	}
	if info != nil {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return trimV(v)
		}
		if rev := setting(info, "vcs.revision"); rev != "" {
			v := Dev + "+" + rev[:min(len(rev), 12)]
			if setting(info, "vcs.modified") == "true" {
				v += ".dirty"
			}
			return v
		}
	}
	return Dev
}

func setting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// trimV drops the "v" of a tag such as v1.2.3, so that the version reads as a semantic version.
func trimV(v string) string {
	if len(v) > 1 && v[0] == 'v' && v[1] >= '0' && v[1] <= '9' {
		return v[1:]
	}
	return v
}
