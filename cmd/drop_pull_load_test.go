package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shuffledBackend returns a fresh copy of the tables in a fixed non-sorted order on every call,
// so a sort done by the caller on the returned slice cannot leak into later runs.
type shuffledBackend struct {
	backendtest.MockUCBackend
	names []string
}

func (b *shuffledBackend) ListTables(context.Context, backend.SchemaRef) ([]*backend.LiveTable, error) {
	tables := make([]*backend.LiveTable, 0, len(b.names))
	for _, n := range b.names {
		tables = append(tables, backendtest.Live("", "", n, backendtest.Col("id", "BIGINT")))
	}
	return tables, nil
}

// --- no -t outside a project ---

func TestDropPull_NoTargetOutsideProject(t *testing.T) {
	emptyDir(t)
	cb := useSpy(t, newSpy(nil))

	_, _, err := executeCmdSplit("drop", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drop requires -t")

	_, _, err = executeCmdSplit("pull")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pull requires -t")

	assert.Zero(t, cb.factoryCalls)
	assert.Empty(t, cb.RecordedCalls())
}

// --- in a project with -t ---

func TestDropPull_InProjectWithTarget(t *testing.T) {
	tmpDir := setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useBackend(t, mock)

	stdout, _, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, stdout, "created")
	assert.FileExists(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"))

	stdout, _, err = executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.NoError(t, err)
	assert.Contains(t, stdout, "dropped `dev`.`silver`.`orders`")
	require.Len(t, mock.RecordedCalls(), 1)
	assert.Equal(t, change.DropTable, mock.RecordedCalls()[0].Kind)
}

// --- drop does not read tables; pull does ---

func TestDrop_IgnoresBrokenTableFile(t *testing.T) {
	tmpDir := setupTestProject(t)
	dir := filepath.Join(tmpDir, "tables", "silver")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.yml"), []byte("name: [unclosed\n"), 0644))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "t", "--auto-approve")
	require.NoError(t, err, stderr)
	assert.NotContains(t, stderr, "broken.yml")
	require.Len(t, mock.RecordedCalls(), 1)
}

// --- load diagnostics go to stderr ---

func TestDropPull_LoadWarningsToStderr(t *testing.T) {
	tmpDir := setupTestProject(t)
	// A catalog file whose name: differs from the filename (a warning).
	writeFile(t, filepath.Join(tmpDir, "catalogs", "extra.yml"), "name: other\nuc_name: other_uc\nschemas:\n  gold:\n    uc_name: gold\n")
	useBackend(t, &backendtest.MockUCBackend{})

	stdout, stderr, err := executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "t", "--auto-approve")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "warning: catalog name")
	assert.Contains(t, stderr, "extra.yml")
	assert.NotContains(t, stdout, "warning")

	stdout, stderr, err = executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "warning: catalog name")
	assert.Contains(t, stderr, "extra.yml")
	assert.NotContains(t, stdout, "warning")
}

func TestDropPull_LoadErrors(t *testing.T) {
	cases := map[string]struct{ file, content string }{
		"broken target":  {filepath.Join("targets", "dev.yml"), "workspace: [unclosed\n"},
		"broken catalog": {filepath.Join("catalogs", "bad.yml"), "name: [unclosed\n"},
	}
	for name, tc := range cases {
		for _, args := range [][]string{
			{cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, "t", flagAutoApprove},
			{cmdPull, "-t", litDev},
		} {
			t.Run(name+"/"+args[0], func(t *testing.T) {
				tmpDir := setupTestProject(t)
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, tc.file), []byte(tc.content), 0644))
				cb := useSpy(t, newSpy(nil))

				_, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				assert.Contains(t, stderr, "error:")
				assert.Contains(t, stderr, filepath.Base(tc.file))
				assert.Zero(t, cb.factoryCalls, "backend must not be created on load error")
				assert.Empty(t, cb.RecordedCalls())
			})
		}
	}
}

// --- pull order ---

func TestPull_OrderIsSortedByName(t *testing.T) {
	var first string
	for i := 0; i < 20; i++ {
		setupTestProject(t)
		useBackend(t, &shuffledBackend{names: []string{"c", "a", "b"}})

		stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
		require.NoError(t, err, stderr)

		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		require.Len(t, lines, 3, stdout)
		for j, n := range []string{"a", "b", "c"} {
			assert.Contains(t, lines[j], "silver/"+n+".yml", "line %d", j)
		}
		if i == 0 {
			first = stdout
		}
		assert.Equal(t, first, stdout, "run %d differs", i)
	}
}
