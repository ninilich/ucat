package cmd

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- name case rules, through the real cmd pipeline ---

func TestPlan_CamelCaseColumnsVsLowercaseLive_NoChanges(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: userId\n    type: BIGINT\n  - name: OrderDate\n    type: DATE\n")

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: "userid", Type: typBIGINT}, {Name: "orderdate", Type: "DATE"}},
	})})
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev", "--detailed-exitcode")
	require.NoError(t, err, "case-only column difference must be a no-op (exit 0 even with --detailed-exitcode); out: %s", out)
	assert.NotContains(t, out, "drop column")
	assert.NotContains(t, out, "add column")
}

func TestValidate_UppercaseTableName_Fails(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders",
		"name: Orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")

	out, err := executeCmd("validate")
	require.Error(t, err)
	assert.Contains(t, out, `table name "Orders" must be lowercase: Unity Catalog stores table names in lowercase`)
}

func TestPlan_UppercaseTableName_FailsBeforeBackend(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders",
		"name: Orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")

	mock := newSpy(nil).forbid(t)
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, out+err.Error(), "must be lowercase: Unity Catalog stores table names in lowercase")
}

func TestValidate_DuplicateColumnDifferingByCase_Fails(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: Id\n    type: BIGINT\n")

	out, err := executeCmd("validate")
	require.Error(t, err)
	assert.Contains(t, out, `duplicate column "Id" (column names are case-insensitive)`)
}
