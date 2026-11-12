package cmd

// version_build_test.go covers the Makefile build: commit and build date reach `ucat version`.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // cmd package dir during go test
	require.NoError(t, err)
	return filepath.Dir(wd)
}

// makeBuildLdflags returns the ldflags value of the `go build` line printed by `make -n build`
// (nothing is built). It skips the test when make is unavailable.
func makeBuildLdflags(t *testing.T, root string) (line, ldflags string) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil { // on Windows make is usually absent: skip
		t.Skip("make is not installed")
	}
	cmd := exec.Command("make", "-n", "build")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	m := regexp.MustCompile(`-ldflags "([^"]*)"`).FindStringSubmatch(string(out))
	require.NotNil(t, m, "no -ldflags \"...\" in `make -n build` output: %s", out)
	return string(out), m[1]
}

func TestMakefileBuild_PassesCommitAndBuildDate(t *testing.T) {
	out, ldflags := makeBuildLdflags(t, repoRoot(t))
	assert.Contains(t, ldflags, "-X github.com/ninilich/ucat/cmd.commit=")
	assert.Contains(t, ldflags, "-X github.com/ninilich/ucat/cmd.buildDate=")
	assert.NotContains(t, out, "cmd.version=")
}

// Builds the binary with the Makefile ldflags and checks that `version` shows a real commit and date.
func TestMakefileBuild_BinaryVersionHasCommitAndDate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped with -short: builds a binary")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := repoRoot(t)
	if out, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").CombinedOutput(); err != nil {
		t.Skipf("%s is not a git repository with a commit: %s", root, out)
	}
	_, ldflags := makeBuildLdflags(t, root)

	binName := "ucat"
	if runtime.GOOS == litWindows {
		binName = "ucat.exe"
	}
	bin := filepath.Join(t.TempDir(), binName)
	b := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, ".")
	b.Dir = root
	bo, err := b.CombinedOutput()
	require.NoError(t, err, string(bo))
	vo, err := exec.Command(bin, "version").CombinedOutput()
	require.NoError(t, err)
	assert.Regexp(t, `^ucat dev \(commit: [0-9a-f]{7,}(-dirty)?, built: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\)`, string(vo))
}
