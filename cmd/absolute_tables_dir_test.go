package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An absolute tables_dir (any OS spelling, checked as a string on every OS) is a load error
// with exit code 1 for every command that loads the project; no backend call, no "not found" warning.
var absoluteTablesDirs = []string{
	"/abs/tables", `C:\x`, `\\server\share\t`,
}

func TestAbsoluteTablesDir_LoadErrorOnAllCommands(t *testing.T) {
	cmds := [][]string{
		{cmdValidate},
		{cmdValidate, "-t", litDev},
		{cmdPlan, "-t", litDev},
		{cmdApply, "-t", litDev},
		{cmdPull, "-t", litDev},
		{cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, "x", flagAutoApprove},
	}
	for _, v := range absoluteTablesDirs {
		for _, args := range cmds {
			t.Run(v+"/"+args[0], func(t *testing.T) {
				emptyTablesProject(t, v)
				cb := useSpy(t, newSpy(nil))
				_, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				assert.Equal(t, 1, codeOf(err))
				assert.Contains(t, stderr, `error: tables_dir must be relative to the project root (got "`+v+`")`)
				assert.Contains(t, stderr, "ucat.yml:2")
				assert.Equal(t, 1, strings.Count(stderr, "tables_dir must be relative"), "reported once: %s", stderr)
				assert.NotContains(t, stderr, "not found")
				assert.Zero(t, cb.factoryCalls, "backend must not be created")
			})
		}
	}
}

func TestRelativeTablesDir_NotAffected(t *testing.T) {
	// "" is the default (tables_dir not set) and is not an error either.
	for _, v := range []string{"", litTables, pathTables, "a/b", `tables\prod`, `..\tables`, "../tables"} {
		t.Run("value="+v, func(t *testing.T) {
			emptyTablesProject(t, v)
			_, stderr, err := executeCmdSplit("validate")
			require.NoError(t, err)
			assert.NotContains(t, stderr, "must be relative")
		})
	}
}
