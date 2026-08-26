package caltraingateway

import (
	"runtime/debug"
	"strings"
	"sync"
)

// buildVersion and buildRevision are stamped at link time:
//
//	go build -ldflags "-X caltrain-gateway/internal/app/caltrain-gateway.buildVersion=$(git describe --tags)"
//
// They are deliberately left empty by default so a plain `go build` falls back
// to the VCS data the toolchain embeds, rather than reporting a version that
// was never released.
var (
	buildVersion  = ""
	buildRevision = ""
)

// Named separately from the schedule version, which identifies timetable
// content rather than the software itself.
var (
	buildInfoOnce sync.Once
	buildInfoText string
)

// BuildVersion returns a human-readable version of the running binary.
//
// It prefers the value stamped at build time from `git describe --tags`. When
// that is absent, as with a plain `go build`, it falls back to the commit the
// Go toolchain records, so a developer build still identifies itself instead of
// masquerading as a release.
func BuildVersion() string {
	buildInfoOnce.Do(func() {
		buildInfoText = resolveBuildVersion(readVCSInfo())
	})
	return buildInfoText
}

// vcsInfo is the subset of build metadata the toolchain embeds automatically.
type vcsInfo struct {
	revision string
	modified bool
}

// readVCSInfo extracts the VCS stamps Go records when building inside a
// repository. Values are absent when the source is not a checkout.
func readVCSInfo() vcsInfo {
	info := vcsInfo{}
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.revision = setting.Value
		case "vcs.modified":
			info.modified = setting.Value == "true"
		}
	}
	return info
}

// resolveBuildVersion combines the stamped and embedded metadata into a single
// label, for example "v1.5.0", "v1.5.0 (b791e74)" or "dev (b791e74-dirty)".
//
// `git describe` output already embeds the commit, so the hash is not repeated
// in that case; a dirty working tree is still flagged on its own.
func resolveBuildVersion(vcs vcsInfo) string {
	version := strings.TrimSpace(buildVersion)
	if version == "" {
		version = "dev"
	}

	revision := shortRevision(strings.TrimSpace(buildRevision))
	if revision == "" {
		revision = shortRevision(vcs.revision)
	}

	suffix := revision
	if revision != "" && strings.Contains(version, revision) {
		suffix = ""
	}
	if vcs.modified {
		if suffix == "" {
			suffix = "dirty"
		} else {
			suffix += "-dirty"
		}
	}

	if suffix == "" {
		return version
	}
	return version + " (" + suffix + ")"
}

// shortRevision abbreviates a commit hash to its first seven characters.
func shortRevision(revision string) string {
	if len(revision) > 7 {
		return revision[:7]
	}
	return revision
}
