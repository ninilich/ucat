package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const evilCatalogYAML = "name: main\nuc_name: dev\nschemas:\n  \"../../evil\":\n    uc_name: silver\n"

// A logical schema name that leaves the project is a load error for validate and for pull,
// and pull writes nothing outside the project root.
func TestUnsafeLogicalSchemaName_ValidateAndPullFail_NothingOutsideRoot(t *testing.T) {
	dir := newProject(t, projectOpts{Catalog: evilCatalogYAML})
	writeFile(t, filepath.Join(dir, "tables", "evil", "t.yml"), tableYAMLOf("t", "../../evil"))

	for _, args := range [][]string{{cmdValidate}, {cmdPull, "-t", litDev}, {cmdPlan, "-t", litDev}} {
		_, stderr, err := executeCmdSplit(args...)
		require.Error(t, err, args)
		assert.Contains(t, stderr, `error: logical schema name "../../evil" must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name (catalogs/main.yml:4)`, args)
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil"))
	assert.True(t, os.IsNotExist(err), "nothing is written above the project root")
}

func TestUnsafeTargetName_Rejected(t *testing.T) {
	newProject(t, projectOpts{})
	for _, name := range []string{"../dev", "..", "a/b", `a\b`} {
		_, stderr, err := executeCmdSplit(cmdPlan, "-t", name)
		require.Error(t, err, name)
		assert.Contains(t, stderr, fmt.Sprintf("error: target name %q must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name", name))
	}
}
