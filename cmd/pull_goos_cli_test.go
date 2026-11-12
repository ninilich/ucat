package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pullTablesIn installs a mock UC with the given tables in dev.<ucSchema>.
func pullTablesIn(t *testing.T, ucSchema string, names ...string) {
	t.Helper()
	mock := &backendtest.MockUCBackend{}
	var tables []*backend.LiveTable
	for _, n := range names {
		tables = append(tables, backendtest.Live("dev", ucSchema, n, backendtest.Col("id", "BIGINT")))
	}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: ucSchema}, tables)
	useBackend(t, mock)
}

const winReserved = "reserved file name on Windows"

// A reserved logical schema name never reaches pull's file name check: the loader rejects it
// (the white list of logical names), so nothing is written.
func TestPull_WindowsReservedSchemaName_RejectedByLoader(t *testing.T) {
	tmpDir := setupProjectWithCatalog(t, "name: main\nuc_name: dev\nschemas:\n  con:\n    uc_name: con\n")
	useGOOS(t, "windows")
	pullTablesIn(t, "con", "orders")

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Contains(t, stderr, `logical schema name "con" `+nameRuleText)
	assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "con", "orders.yml"))
}

// Other reserved names (aux, nul) and a reserved name with an extension-like suffix.
func TestPull_WindowsReservedNames_AllFail(t *testing.T) {
	for _, name := range []string{"aux", "nul", "prn", "com1", "lpt9", "NUL.backup"} {
		t.Run(name, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			useGOOS(t, "windows")
			pullTablesIn(t, "silver", name)

			_, err := executeCmd("pull", "-t", "dev")
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, err.Error(), winReserved)
			assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", name+".yml"))
		})
	}
}

// --force in place for con.yml, which defines table "con", does not run the check.
func TestPull_WindowsForceInPlace_ConYml_NoError(t *testing.T) {
	tmpDir := setupTestProject(t)
	useGOOS(t, "windows")
	addTableFile(t, tmpDir, "silver", "con",
		"name: con\nschema: silver\ncomment: old content\ncolumns:\n  - name: id\n    type: INT\n")
	pullTablesIn(t, "silver", "con")

	out, err := executeCmd("pull", "-t", "dev", "--force")
	require.NoError(t, err)
	assert.Equal(t, 0, codeOf(err))
	assert.Contains(t, out, "updated  tables/silver/con.yml")
	assert.NotContains(t, out, winReserved)
	content, readErr := os.ReadFile(filepath.Join(tmpDir, "tables", "silver", "con.yml"))
	require.NoError(t, readErr)
	assert.NotContains(t, string(content), "old content")
}

// The canonical path is taken by the definition of another table: skipped, no Windows error.
func TestPull_WindowsCanonicalPathTakenByOtherTable_Skipped(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "noforce"
		args := []string{cmdPull, "-t", litDev}
		if force {
			name = litForce
			args = append(args, "--force")
		}
		t.Run(name, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			useGOOS(t, "windows")
			addTableFile(t, tmpDir, "silver", "con",
				"name: other\nschema: silver\ncomment: keep me\ncolumns:\n  - name: id\n    type: INT\n")
			pullTablesIn(t, "silver", "con")

			out, err := executeCmd(args...)
			require.NoError(t, err)
			assert.Contains(t, out, `~ skipped  silver.con (tables/silver/con.yml defines table "other")`)
			assert.NotContains(t, out, winReserved)
			content, readErr := os.ReadFile(filepath.Join(tmpDir, "tables", "silver", "con.yml"))
			require.NoError(t, readErr)
			assert.Contains(t, string(content), "keep me")
		})
	}
}

// The rename_from source of a defined table is skipped before any check.
func TestPull_WindowsRenameSourceReserved_Skipped(t *testing.T) {
	tmpDir := setupTestProject(t)
	useGOOS(t, "windows")
	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\nrename_from: con\ncolumns:\n  - name: id\n    type: BIGINT\n")
	pullTablesIn(t, "silver", "con")

	out, err := executeCmd("pull", "-t", "dev", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, `rename_from source of "orders"`)
	assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "con.yml"))
}

// A name that only looks like a reserved one is allowed and created.
func TestPull_WindowsNearReservedName_Created(t *testing.T) {
	for _, name := range []string{"nul_table", "console", "com10", "auxiliary"} {
		t.Run(name, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			useGOOS(t, "windows")
			pullTablesIn(t, "silver", name)

			out, err := executeCmd("pull", "-t", "dev")
			require.NoError(t, err)
			assert.Contains(t, out, "created  tables/silver/"+name+".yml")
			assert.FileExists(t, filepath.Join(tmpDir, "tables", "silver", name+".yml"))
		})
	}
}

// Wiring regression: the check must run for a new file on windows (con -> error) AND must not
// run for an in-place overwrite on windows; on linux/darwin con is created. All four together
// fail if the check becomes unconditional (in-place case) or is no longer called (new-file case).
func TestPull_WindowsCheck_WiredConditionally(t *testing.T) {
	t.Run("windows/new file is refused", func(t *testing.T) {
		tmpDir := setupTestProject(t)
		useGOOS(t, "windows")
		pullTablesIn(t, "silver", "con")
		_, err := executeCmd("pull", "-t", "dev", "--force") // --force does not exempt a NEW file
		require.Error(t, err)
		assert.Contains(t, err.Error(), winReserved)
		assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "con.yml"))
	})
	t.Run("windows/in place is allowed", func(t *testing.T) {
		tmpDir := setupTestProject(t)
		useGOOS(t, "windows")
		p := writeConTable(t, tmpDir)
		pullTablesIn(t, "silver", "con")
		_, err := executeCmd("pull", "-t", "dev", "--force")
		require.NoError(t, err)
		content, readErr := os.ReadFile(p)
		require.NoError(t, readErr)
		assert.NotContains(t, string(content), "old content")
	})
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos+"/new file is created", func(t *testing.T) {
			tmpDir := setupTestProject(t)
			useGOOS(t, goos)
			pullTablesIn(t, "silver", "con")
			_, err := executeCmd("pull", "-t", "dev")
			require.NoError(t, err)
			assert.FileExists(t, filepath.Join(tmpDir, "tables", "silver", "con.yml"))
		})
	}
}

// The first write error stops pull, and nothing is written after it, including with --force.
func TestPull_WindowsStopsAtFirstError_ForceToo(t *testing.T) {
	tmpDir := setupTestProject(t)
	useGOOS(t, "windows")
	pullTablesIn(t, "silver", "a_first", "con", "zeta")

	_, err := executeCmd("pull", "-t", "dev", "--force")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.FileExists(t, filepath.Join(tmpDir, "tables", "silver", "a_first.yml"))
	assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "zeta.yml"))
}

// conTableYAML is the definition of a table named like the Windows-reserved device "con".
const conTableYAML = "name: con\nschema: silver\ncomment: old content\ncolumns:\n  - name: id\n    type: INT\n"

// writeConTable adds tables/silver/con_table.yml (defining table "con") and returns its path.
func writeConTable(t *testing.T, tmpDir string) string {
	t.Helper()
	return writeFile(t, filepath.Join(tmpDir, "tables", "silver", "con_table.yml"), conTableYAML)
}

// Without --force a defined table is skipped, not checked.
func TestPull_WindowsReservedName_DefinedSkippedNoError(t *testing.T) {
	tmpDir := setupTestProject(t)
	useGOOS(t, "windows")
	p := writeConTable(t, tmpDir)
	pullTablesIn(t, "silver", "con")

	out, err := executeCmd("pull", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, "skipped")
	content, readErr := os.ReadFile(p)
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "old content")
}
