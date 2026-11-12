package cmd

// apply_test.go covers: apply must return error via RunE, not call os.Exit(1).

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- apply returns error instead of os.Exit ---

// TestApply_NoChanges_ReturnsNil verifies that apply with no changes returns nil
// (no os.Exit needed; this tests the happy path).
func TestApply_NoChanges_ReturnsNil(t *testing.T) {
	setupPlanProject(t)

	mock := &backendtest.MockUCBackend{}
	// Live state matches desired exactly → no changes
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litDev,
			ResolvedSchema:  litSilver,
			Columns:         []config.Column{{Name: "id", Type: typBIGINT}},
		}),
	})
	useBackend(t, mock)

	out, err := executeCmd("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err, "apply with no changes must return nil (exit 0)")
	assert.Contains(t, out, "up-to-date",
		"apply with no changes must print 'up-to-date'")
}

// TestApply_BackendConnectError_ReturnsError verifies that a backend connection failure
// is returned as an error (not os.Exit). This tests the pipeline layer, not executor.
func TestApply_BackendConnectError_ReturnsError(t *testing.T) {
	setupPlanProject(t)

	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		return nil, errors.New("connection refused")
	})

	_, err := executeCmd("apply", "-t", "dev")
	require.Error(t, err, "apply must return error when backend connection fails")
	assert.Contains(t, err.Error(), "connection refused",
		"error must propagate the connection failure message")
}

// TestApply_NoWarehouse_FailsBeforePrompt verifies that apply with a non-empty plan and no
// warehouse_id fails before the confirmation prompt, applies nothing and names no Go method.
func TestApply_NoWarehouse_FailsBeforePrompt(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), "workspace:\n  host: https://dev.example.com\n")
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")

	// Live state has no table: CreateTable change requires a warehouse.
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{})
	useBackend(t, mock)
	useStdin(t, "yes\n")

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Equal(t, "apply requires a SQL warehouse: set workspace.warehouse_id in targets/dev.yml (or ucat.yml), or DATABRICKS_WAREHOUSE_ID", err.Error())
	assert.NotContains(t, stdout+stderr, "ApplyChange")
	assert.NotContains(t, stdout+stderr, "Proceed")
	assert.Zero(t, countApplied(mock))
}

// An empty plan needs no warehouse: no error without warehouse_id.
func TestApply_NoWarehouse_EmptyPlan_OK(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), "workspace:\n  host: https://dev.example.com\n")
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useBackend(t, mock)

	stdout, _, err := executeCmdSplit("apply", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No changes")
}

// TestApply_WithChanges_AutoApprove_Success verifies that apply with changes and
// --auto-approve succeeds and reports applied changes.
func TestApply_WithChanges_AutoApprove_Success(t *testing.T) {
	setupPlanProject(t)

	// Live: table exists but is missing the "id" column → AddColumn change
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litDev,
			ResolvedSchema:  litSilver,
			Columns:         []config.Column{}, // no columns
		}),
	})
	useBackend(t, mock)

	out, err := executeCmd("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err, "apply with auto-approve must succeed")
	assert.Contains(t, out, "applied",
		"apply output must confirm applied changes; got: %q", out)
}

// TestApply_AutoApprove_OutputFormat_IndexAndTable verifies that each applied line has
// the "[i/N] applied in <time>: <table> - <describe>" format.
func TestApply_AutoApprove_OutputFormat_IndexAndTable(t *testing.T) {
	setupPlanProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litDev,
			ResolvedSchema:  litSilver,
			Columns:         []config.Column{},
		}),
	})
	useBackend(t, mock)

	out, err := executeCmd("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)

	calls := mock.RecordedCalls()
	require.NotEmpty(t, calls)
	n := len(calls)
	for i, c := range calls {
		assert.Contains(t, out, fmt.Sprintf("  [%d/%d] applied in 1.5s: `dev`.`silver`.`orders` - %s", i+1, n, c.Describe))
	}
}
