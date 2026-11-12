package cmd

// edge cases beyond init_errors_test.go: pre-existing directory, last step, rerun, --json.

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const partialSuffix = "; remove them or fix the cause and rerun init in an empty directory\n"

// catalogs/ existed before init: it is not listed, the file created inside it is. The failure is on the
// last step (mkdir tables): targets/ and targets/dev.yml are listed, in creation order.
func TestInit_Partial_PreexistingCatalogsDir_LastStep(t *testing.T) {
	emptyDir(t)
	require.NoError(t, os.MkdirAll("catalogs", 0o755))
	require.NoError(t, os.WriteFile("tables", []byte("x"), 0o644))
	useStdin(t, initFullAnswers)
	stdout, stderr, err := executeCmdSplit("init")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Equal(t, "project was created partially: ucat.yml, catalogs/main.yml, targets/, targets/dev.yml"+partialSuffix+"Error: "+err.Error()+"\n", stderr)
	assert.Equal(t, 0, strings.Count(stdout, "partially"), "the addition goes to stderr")
	assert.Equal(t, 1, strings.Count(stderr, err.Error()))
	// the line comes first, the error after it
	assert.Less(t, strings.Index(stderr, "project was created partially"), strings.Index(stderr, "Error: "))

	// no rollback: a rerun still refuses on the existing files
	useStdin(t, initFullAnswers)
	_, stderr2, err2 := executeCmdSplit("init")
	require.Error(t, err2)
	assert.Contains(t, err2.Error(), "ucat.yml already exists")
	assert.NotContains(t, stderr2, "partially", "nothing is created by the refused rerun")
}

// The failing step is mkdir tables (a file is in the way): the created files are listed, the "Created" lines are on stdout.
func TestInit_Partial_TablesIsAFile(t *testing.T) {
	emptyDir(t)
	require.NoError(t, os.WriteFile("tables", []byte("x"), 0o644))
	useStdin(t, initFullAnswers)
	stdout, stderr, err := executeCmdSplit("init")
	require.Error(t, err)
	assert.Equal(t, "project was created partially: ucat.yml, catalogs/, catalogs/main.yml, targets/, targets/dev.yml"+partialSuffix+"Error: "+err.Error()+"\n", stderr)
	assert.Contains(t, stdout, "✓ Created ucat.yml")
}
