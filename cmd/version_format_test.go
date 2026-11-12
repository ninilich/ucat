package cmd

// version_format_test.go covers the text of `ucat version` / `ucat --version` and the build-info fallback.

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

const litUnknown = "unknown"

func buildInfo(mainVersion string) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}
}

func TestFormatVersion(t *testing.T) {
	cases := []struct {
		name          string
		ver, com, day string
		bi            *debug.BuildInfo
		ok            bool
		want          string
	}{
		{"ldflags release", "1.2.0", "abc1234", "2026-09-29", buildInfo("v9.9.9"), true,
			"ucat 1.2.0 (commit: abc1234, built: 2026-09-29)"},
		{"make build: commit set, version stays dev", litDev, "abc1234", "2026-09-29", buildInfo("v9.9.9"), true,
			"ucat dev (commit: abc1234, built: 2026-09-29)"},
		{"go install: module version", litDev, litNone, litUnknown, buildInfo("v1.0.1"), true,
			"ucat v1.0.1 (commit: none, built: unknown)"},
		{"go build in a checkout: (devel)", litDev, litNone, litUnknown, buildInfo("(devel)"), true,
			versionDevNone},
		{"pre-release tag", litDev, litNone, litUnknown, buildInfo("v1.0.0-rc.1"), true,
			"ucat v1.0.0-rc.1 (commit: none, built: unknown)"},
		{"pseudo-version", litDev, litNone, litUnknown, buildInfo("v0.6.1-0.20261106075939-b494e5585906"), true, versionDevNone},
		{"pseudo-version without base tag", litDev, litNone, litUnknown, buildInfo("v0.0.0-20261106075939-b494e5585906"), true, versionDevNone},
		{"pseudo-version dirty", litDev, litNone, litUnknown, buildInfo("v0.6.1-0.20261106075939-b494e5585906+dirty"), true, versionDevNone},
		{"tag dirty", litDev, litNone, litUnknown, buildInfo("v1.0.1+dirty"), true, versionDevNone},
		{"incompatible tag", litDev, litNone, litUnknown, buildInfo("v2.0.0+incompatible"), true, versionDevNone},
		{"no v prefix", litDev, litNone, litUnknown, buildInfo("1.0.1"), true, versionDevNone},
		{"empty module version", litDev, litNone, litUnknown, buildInfo(""), true,
			versionDevNone},
		{"no build info", litDev, litNone, litUnknown, nil, false,
			versionDevNone},
		{"ok without info", litDev, litNone, litUnknown, nil, true,
			versionDevNone},
		{"info not ok", litDev, litNone, litUnknown, buildInfo("v1.0.1"), false,
			versionDevNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatVersion(tc.ver, tc.com, tc.day, tc.bi, tc.ok))
		})
	}
}

// Both spellings print exactly versionText() and a newline, for any value of the build variables.
func TestVersion_BothSpellingsPrintVersionText(t *testing.T) {
	origV, origC, origD := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = origV, origC, origD })
	version, commit, buildDate = "3.4.5", "deadbee", "2026-11-06T10:00:00Z"

	want := "ucat 3.4.5 (commit: deadbee, built: 2026-11-06T10:00:00Z)\n"
	for _, args := range [][]string{{"version"}, {"--version"}} {
		out, _, err := executeCmdSplit(args...)
		assert.NoError(t, err)
		assert.Equal(t, want, out, "%v", args)
	}
}
