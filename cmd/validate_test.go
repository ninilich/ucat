package cmd

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addTableFile writes a table YAML file under tmpDir at tables/<schema>/<name>.yml.
func addTableFile(t *testing.T, tmpDir, schema, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(tmpDir, "tables", schema, name+".yml"), content)
}

// --- Layer 1: local validation (no backend needed) ---

// TestValidateLocal_Valid verifies that a valid project passes local checks.
func TestValidateLocal_Valid(t *testing.T) {
	tmpDir := setupTestProject(t)

	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")

	out, err := executeCmd("validate")
	require.NoError(t, err)
	assert.Contains(t, out, "all local checks passed")
}

// TestValidateLocal_MissingTableName verifies that a table YAML without a name is rejected.
func TestValidateLocal_MissingTableName(t *testing.T) {
	tmpDir := setupTestProject(t)

	addTableFile(t, tmpDir, "silver", "unnamed",
		"schema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")

	out, err := executeCmd("validate")
	require.Error(t, err)
	assert.Contains(t, out, "table name is required")
}

// TestValidateLocal_RenameSameAsName verifies that rename_from equal to name produces
// a warning (exit 0, not an error).
func TestValidateLocal_RenameSameAsName(t *testing.T) {
	tmpDir := setupTestProject(t)

	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n    rename_from: id\n")

	out, err := executeCmd("validate")
	assert.NoError(t, err, "rename_from same as name should be a warning, not a hard error")
	assert.Contains(t, out, "rename_from")
}

// --- Layer 2: live validation (with mock backend) ---

// TestValidateLive_AllPass verifies that a valid local project with an accessible
// schema passes both local and live checks.
func TestValidateLive_AllPass(t *testing.T) {
	tmpDir := setupTestProject(t)

	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, "all local checks passed")
	assert.Contains(t, out, "dev.silver")
}

// TestValidateLive_SchemaNotFound verifies that a ListTables error for the schema
// is reported as a live-check failure.
func TestValidateLive_SchemaNotFound(t *testing.T) {
	tmpDir := setupTestProject(t)

	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")

	mlb := newSpy(nil)
	mlb.listErr = errors.New("SCHEMA_DOES_NOT_EXIST")
	useBackend(t, mlb)

	out, err := executeCmd("validate", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, out, "dev.silver")
	assert.Contains(t, out, "SCHEMA_DOES_NOT_EXIST")
}

// TestValidateLive_LocalErrorPreventsLive verifies that when local validation fails,
// the command exits immediately and does not call the backend.
func TestValidateLive_LocalErrorPreventsLive(t *testing.T) {
	tmpDir := setupTestProject(t)

	// Table references a schema that is not declared in any catalog.
	addTableFile(t, tmpDir, "nonexistent", "orders",
		"name: orders\nschema: nonexistent\ncolumns:\n  - name: id\n    type: BIGINT\n")

	mlb := newSpy(nil)
	useBackend(t, mlb)

	_, err := executeCmd("validate", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 0, mlb.listCalls, "ListTables should not be called when local validation fails")
}

// TestValidateLocal_RenameFromDefinedTable verifies that rename_from pointing at a
// table that is also defined in the same schema fails validate.
func TestValidateLocal_RenameFromDefinedTable(t *testing.T) {
	tmpDir := setupTestProject(t)

	addTableFile(t, tmpDir, "silver", "a",
		"name: a\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")
	addTableFile(t, tmpDir, "silver", "b",
		"name: b\nschema: silver\nrename_from: a\ncolumns:\n  - name: id\n    type: BIGINT\n")

	out, err := executeCmd("validate")
	require.Error(t, err)
	assert.Contains(t, out, `table "b": rename_from "a" refers to a table that is also defined in`)
	assert.Contains(t, out, "remove one of them")
}

// TestValidateLocal_RenameFromUndefinedTable verifies the normal rename still passes.
func TestValidateLocal_RenameFromUndefinedTable(t *testing.T) {
	tmpDir := setupTestProject(t)

	addTableFile(t, tmpDir, "silver", "b",
		"name: b\nschema: silver\nrename_from: a\ncolumns:\n  - name: id\n    type: BIGINT\n")

	_, err := executeCmd("validate")
	require.NoError(t, err)
}

// TestValidate_MissingTarget_NoAbsolutePath: a read error shows the project-relative
// path and the OS reason, never the absolute path of the project.
func TestValidate_MissingTarget_NoAbsolutePath(t *testing.T) {
	tmpDir := setupTestProject(t)

	out, err := executeCmd("validate", "-t", "nope")
	require.Error(t, err)
	all := out + err.Error()
	assert.Contains(t, all, "targets/nope.yml")
	assert.Regexp(t, `no such file or directory|cannot find the file specified`, all)
	assertNoAbsolutePath(t, tmpDir, all)
}
