package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installOrdersMock installs a mock backend with one live table "orders" in dev.silver.
func installOrdersMock(t *testing.T) *backendtest.MockUCBackend {
	t.Helper()
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useBackend(t, mock)
	return mock
}

// --- local validation runs before the backend is created ---

func TestDropPull_LocalValidationFailsBeforeBackend(t *testing.T) {
	for _, args := range [][]string{
		{cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove},
		{cmdPull, "-t", litDev},
	} {
		t.Run(args[0], func(t *testing.T) {
			tmpDir := setupTestProject(t)
			// the logical schema "silver" is now declared in two catalogs
			writeFile(t, filepath.Join(tmpDir, "catalogs", "second.yml"),
				"name: second\nuc_name: dev2\nschemas:\n  silver:\n    uc_name: silver\n")
			cb := useSpy(t, newSpy(nil))

			_, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "local validation failed")
			assert.Contains(t, stderr, "silver")
			assert.Zero(t, cb.factoryCalls, "backend must not be created")
			assert.Empty(t, cb.RecordedCalls())
		})
	}
}

// --- pull loads tables ---

func TestPull_BrokenTableFileFails(t *testing.T) {
	tmpDir := setupTestProject(t)
	writeFile(t, filepath.Join(tmpDir, "tables", "silver", "broken.yml"), "name: [unclosed\n")
	cb := useSpy(t, newSpy(nil))

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, stderr, "broken.yml")
	assert.Zero(t, cb.factoryCalls)
	assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"))
}

func TestPull_NewTableGoesToCanonicalPath(t *testing.T) {
	tmpDir := setupTestProject(t)
	// an unrelated defined table must not affect the new one
	writeFile(t, filepath.Join(tmpDir, "tables", "silver", "other.yml"),
		"name: other\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")
	installOrdersMock(t)

	stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err, stderr)
	rel := filepath.Join("tables", "silver", "orders.yml")
	assert.FileExists(t, filepath.Join(tmpDir, rel))
	assert.Contains(t, stdout, "created")
	// ucat prints paths with "/" on every OS, while rel is built with the OS separator
	assert.Contains(t, stdout, filepath.ToSlash(rel))
}

func TestPull_DefinedInNonCanonicalFile(t *testing.T) {
	const flat = "tables/flat_orders.yml"
	content := "name: orders\nschema: silver\ncomment: hand-written\ncolumns:\n  - name: id\n    type: BIGINT\n"

	t.Run("without force skips", func(t *testing.T) {
		tmpDir := setupTestProject(t)
		path := writeFile(t, filepath.Join(tmpDir, flat), content)
		installOrdersMock(t)

		stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
		require.NoError(t, err, stderr)
		assert.Contains(t, stdout, "skipped")
		assert.Contains(t, stdout, flat)
		assert.NotContains(t, stdout, "created")
		assert.NotContains(t, stdout, "updated")
		assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, content, string(got))
	})

	t.Run("with force rewrites defining file", func(t *testing.T) {
		tmpDir := setupTestProject(t)
		path := writeFile(t, filepath.Join(tmpDir, flat), content)
		installOrdersMock(t)

		stdout, stderr, err := executeCmdSplit("pull", "-t", "dev", "--force")
		require.NoError(t, err, stderr)
		assert.Contains(t, stdout, "updated")
		assert.Contains(t, stdout, flat)
		assert.NotContains(t, stdout, "created")
		assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(got), "hand-written", "live state must overwrite the file")
		assert.Contains(t, string(got), "name: orders")
	})
}

func TestPull_DropFlaggedTableCountsAsDefined(t *testing.T) {
	tmpDir := setupTestProject(t)
	writeFile(t, filepath.Join(tmpDir, "tables/flat_orders.yml"),
		"name: orders\nschema: silver\ndrop: true\ncolumns:\n  - name: id\n    type: BIGINT\n")
	installOrdersMock(t)

	stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "skipped")
	assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"))
}

func TestPull_NeverDeletes(t *testing.T) {
	tmpDir := setupTestProject(t)
	p := writeFile(t, filepath.Join(tmpDir, "tables/silver/gone.yml"),
		"name: gone\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")
	installOrdersMock(t)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--force")
	require.NoError(t, err, stderr)
	assert.FileExists(t, p)
}

// A live table that is the rename_from source of a defined table must not be pulled as a new file.
func TestPull_SkipsRenameFromSource(t *testing.T) {
	for _, force := range []bool{false, true} {
		tmpDir := setupTestProject(t)
		writeFile(t, filepath.Join(tmpDir, "tables", "silver", "orders_new.yml"),
			"name: orders_new\nschema: silver\nrename_from: orders\ncolumns:\n  - name: id\n    type: BIGINT\n")
		installOrdersMock(t)

		args := []string{cmdPull, "-t", litDev}
		if force {
			args = append(args, "--force")
		}
		stdout, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, stderr)
		assert.Contains(t, stdout, `rename_from source of "orders_new"`)
		assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"))
	}
}

// The canonical path may hold another table's definition (file name is only a convention).
func TestPull_DoesNotOverwriteFileDefiningAnotherTable(t *testing.T) {
	for _, force := range []bool{false, true} {
		tmpDir := setupTestProject(t)
		content := "name: bar\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n"
		p := writeFile(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"), content)
		installOrdersMock(t)

		args := []string{cmdPull, "-t", litDev}
		if force {
			args = append(args, "--force")
		}
		stdout, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, stderr)
		assert.Contains(t, stdout, `defines table "bar"`)
		got, readErr := os.ReadFile(p)
		require.NoError(t, readErr)
		assert.Equal(t, content, string(got))
	}
}

// uc_name may live only in a target: validate without -t passes, with -t it is an error until the target sets it.
func TestValidate_EmptyCatalogUCNameNeedsTarget(t *testing.T) {
	tmpDir := setupTestProject(t)
	writeFile(t, filepath.Join(tmpDir, "catalogs", "main.yml"), "name: main\nschemas:\n  silver:\n    uc_name: silver\n")
	installOrdersMock(t)

	_, stderr, err := executeCmdSplit("validate")
	require.NoError(t, err, stderr)

	_, stderr, err = executeCmdSplit("validate", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, stderr, "uc_name is not set")

	writeFile(t, filepath.Join(tmpDir, "targets", "dev.yml"), "catalogs:\n  main:\n    uc_name: dev\n")
	_, stderr, err = executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err, stderr)
}
