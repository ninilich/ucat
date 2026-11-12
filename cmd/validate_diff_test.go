package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	silverRef = backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}
	goldRef   = backend.SchemaRef{UCCatalog: litDev, UCSchema: litGold}
)

// liveTable builds a live table with a single BIGINT id column.
func liveTable(schema, name string) *backend.LiveTable {
	return backendtest.Live("dev", schema, name, backendtest.Col("id", "BIGINT"))
}

func tableYAML(schema, name, extra string) string {
	return "name: " + name + "\nschema: " + schema + "\n" + extra + "columns:\n  - name: id\n    type: BIGINT\n"
}

// addGoldSchema rewrites catalogs/main.yml to declare both silver and gold schemas.
func addGoldSchema(t *testing.T, tmpDir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "catalogs", "main.yml"),
		[]byte("name: main\nuc_name: dev\nschemas:\n  silver:\n    uc_name: silver\n  gold:\n    uc_name: gold\n"),
		0644,
	))
}

func TestValidateLive_TableRenameOldLive_NoWarning(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "b", tableYAML("silver", "b", "rename_from: a\n"))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "a")})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.NotContains(t, out, "can be removed after apply")
	assert.NotContains(t, out, "ignored: both")
	assert.NotContains(t, out, "new table will be created")
}

func TestValidateLive_TableRenameDone_Warns(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "b", tableYAML("silver", "b", "rename_from: a\n"))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "b")})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, `table "silver.b": rename_from "a" can be removed after apply`)
	assert.Contains(t, out, "warning: ")
}

func TestValidateLive_TableRenameBothAbsent_Warns(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "b", tableYAML("silver", "b", "rename_from: a\n"))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, "not found in UC schema - a new table will be created")
}

func TestValidateLive_TableRenameBothLive_Warns(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "b", tableYAML("silver", "b", "rename_from: a\n"))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "a"), liveTable("silver", "b")})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, "ignored: both")
}

func TestValidateLive_ColumnRenameDone_Warns(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n    rename_from: old_id\n")

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "orders")})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, `column "id": rename_from "old_id" can be removed after apply`)
}

func TestValidateLive_NoPlanOutputAndNoApply(t *testing.T) {
	tmpDir := setupTestProject(t)
	// Desired differs from live: new table, plus a column/comment change on an existing one.
	addTableFile(t, tmpDir, "silver", "fresh", tableYAML("silver", "fresh", ""))
	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncomment: changed\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: extra\n    type: STRING\n")

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "orders")})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.NotContains(t, out, "Plan:")
	assert.NotContains(t, out, "applied")
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, p := range []string{"+ ", "~ ", "- "} {
			assert.Falsef(t, strings.HasPrefix(trimmed, p), "unexpected change line: %q", line)
		}
	}
	assert.Empty(t, mock.RecordedCalls(), "validate must not call ApplyChange")
}

func TestValidateLive_TwoSchemas_DeterministicOrder(t *testing.T) {
	tmpDir := setupTestProject(t)
	addGoldSchema(t, tmpDir)
	addTableFile(t, tmpDir, "silver", "sb", tableYAML("silver", "sb", "rename_from: sa\n"))
	addTableFile(t, tmpDir, "gold", "gb", tableYAML("gold", "gb", "rename_from: ga\n"))

	for i := 0; i < 5; i++ {
		mock := &backendtest.MockUCBackend{}
		mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "sb")})
		mock.SetFixture(goldRef, []*backend.LiveTable{liveTable("gold", "gb")})
		useBackend(t, mock)

		out, err := executeCmd("validate", "-t", "dev")
		require.NoError(t, err)
		gi := strings.Index(out, `table "gold.gb": rename_from "ga" can be removed after apply`)
		si := strings.Index(out, `table "silver.sb": rename_from "sa" can be removed after apply`)
		require.GreaterOrEqual(t, gi, 0, "gold warning missing: %s", out)
		require.GreaterOrEqual(t, si, 0, "silver warning missing: %s", out)
		assert.Less(t, gi, si, "dev.gold must come before dev.silver")
	}
}

func TestValidateLive_ListFailureSkipsDiffOtherSchemasWarn(t *testing.T) {
	tmpDir := setupTestProject(t)
	addGoldSchema(t, tmpDir)
	addTableFile(t, tmpDir, "silver", "sb", tableYAML("silver", "sb", "rename_from: sa\n"))
	addTableFile(t, tmpDir, "gold", "gb", tableYAML("gold", "gb", "rename_from: ga\n"))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "sb")})
	mock.SetFixture(goldRef, []*backend.LiveTable{liveTable("gold", "gb")})
	spy := newSpy(mock)
	spy.failList = &goldRef
	useBackend(t, spy)

	out, err := executeCmd("validate", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, out, "✗ schema dev.gold")
	assert.NotContains(t, out, `table "gold.gb"`, "diff must not run for a schema whose listing failed")
	assert.Contains(t, out, `table "silver.sb": rename_from "sa" can be removed after apply`)
	assert.EqualError(t, err, "live validation failed for schema dev.gold")
}

func TestValidateLocal_NoDiffWarningsNoBackendCalls(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "b", tableYAML("silver", "b", "rename_from: a\n"))

	useBackend(t, newSpy(nil).forbid(t))

	out, err := executeCmd("validate")
	require.NoError(t, err)
	assert.NotContains(t, out, "can be removed after apply")
	assert.NotContains(t, out, "new table will be created")
	assert.NotContains(t, out, "ignored: both")
}

func TestValidateLive_DropMissingInLive_WarnsExitZero(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "gone", tableYAML("silver", "gone", "drop: true\n"))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, "drop: true but table not in UC")
	assert.Empty(t, mock.RecordedCalls())
}

func TestValidateLive_NoDesiredTables_NoNoise(t *testing.T) {
	setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "unmanaged")})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	// The test project has a warehouse_id and no tables: the empty-load warning is the only one.
	assert.Equal(t, 1, strings.Count(out, "warning:"))
	assert.Contains(t, out, "tables_dir \"tables\" not found")
	assert.NotContains(t, out, "error:")
	assert.NotContains(t, out, "✗")
	assert.NotContains(t, out, "unmanaged")
}
