package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newProject and emptyDir change the working directory for the test only: after the test ends it is the
// directory the test started in, so no test depends on where an earlier one left the process.
func TestNewProject_RestoresWorkingDirectory(t *testing.T) {
	start, err := os.Getwd()
	require.NoError(t, err)

	var inside string
	t.Run("project", func(t *testing.T) {
		dir := newProject(t, projectOpts{})
		cwd, gerr := os.Getwd()
		require.NoError(t, gerr)
		assert.Equal(t, dirKey(t, dir), dirKey(t, cwd), "newProject must make the project the working directory")
		inside = cwd
	})
	t.Run("empty", func(t *testing.T) {
		emptyDir(t)
	})

	after, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, start, after, "the working directory must be restored after the tests that changed it")
	assert.NotEqual(t, inside, after)
}

// dirKey resolves symlinks (macOS temp directories) so two spellings of one directory compare equal.
func dirKey(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	require.NoError(t, err)
	return r
}
