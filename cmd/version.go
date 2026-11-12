package cmd

import (
	"fmt"
	"regexp"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Build information; the release build sets it with -ldflags (see .goreleaser.yml, Makefile).
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// versionText is the text of `ucat version` and `ucat --version` (one format for both).
func versionText() string {
	bi, ok := debug.ReadBuildInfo()
	return formatVersion(version, commit, buildDate, bi, ok)
}

// releaseTagRe is a module version that is a real tag: v1.2.3, optionally with a pre-release (v1.2.3-rc.1);
// no build metadata ("+dirty") and not a pseudo-version (a ...-<14 digits>-<12 hex> tail).
var (
	releaseTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	pseudoTailRe = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)
)

// formatVersion renders "ucat <version> (commit: <commit>, built: <date>)". A build without
// ldflags (go install, go run: version "dev" and commit "none") takes the version of the module from
// the build info when it is a clean tag (go install ...@v1.0.1); "(devel)", a pseudo-version, "+dirty"
// (a build from a checkout) or no build info stays "dev".
func formatVersion(ver, commit, date string, bi *debug.BuildInfo, ok bool) string {
	if ver == "dev" && commit == "none" && ok && bi != nil && releaseTagRe.MatchString(bi.Main.Version) && !pseudoTailRe.MatchString(bi.Main.Version) {
		ver = bi.Main.Version
	}
	return fmt.Sprintf("ucat %s (commit: %s, built: %s)", ver, commit, date)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long: `Print the version, the commit and the build date.
"ucat --version" prints the same text. A binary built by "go install <module>@vX.Y.Z" reports
that tag; any other build without release flags (from a checkout, a pseudo-version, a dirty tree)
reports "dev".`,
		Example: "  ucat version",
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), versionText())
		},
	}
}
