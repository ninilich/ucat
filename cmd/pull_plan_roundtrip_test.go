package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
)

// liveRich is a live table with different types, NOT NULL columns, comments with an apostrophe,
// a reserved word and a Unicode column name, partitioning, clustering and tblproperties.
func liveRich() *backend.LiveTable {
	return &backend.LiveTable{
		Catalog: litDev, Schema: litSilver, Name: litOrders, Comment: "it's the orders: 100% 'quoted'",
		Columns: []backend.LiveColumn{
			{Name: "id", Type: "bigint", Comment: "it's the key", Nullable: false},
			{Name: "order", Type: litString, Nullable: true},
			{Name: "город", Type: litString, Comment: "ünï", Nullable: true},
			{Name: "amount", Type: typDecimal102, Nullable: false},
			{Name: "tags", Type: "array<string>", Nullable: true},
			{Name: "dt", Type: "date", Nullable: true},
		},
		Clustering: []string{"id", "order"},
		Properties: map[string]string{"owner": "data's", "delta.appendOnly": "true"},
	}
}

// liveEvents is partitioned (a table cannot be both clustered and partitioned).
func liveEvents() *backend.LiveTable {
	return &backend.LiveTable{
		Catalog: litDev, Schema: litSilver, Name: litEvents, Comment: "",
		Columns: []backend.LiveColumn{
			{Name: "dt", Type: "date", Nullable: false},
			{Name: litKind, Type: "string", Nullable: true},
		},
		Partitioning: []string{"dt", "kind"},
	}
}

func pullRich(t *testing.T, args ...string) *backendtest.MockUCBackend {
	t.Helper()
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveRich(), liveEvents()})
	useBackend(t, mock)
	_, err := executeCmd(append([]string{cmdPull, "-t", litDev}, args...)...)
	require.NoError(t, err)
	return mock
}

// pull -> file -> plan: a table with every kind of field comes back with 0 changes (also with
// every tblproperty written), the strict loader reads the file back, and what pull wrote for
// nullable is only nullable: false.
func TestPullThenPlan_RichTable_NoChanges(t *testing.T) {
	for _, tp := range [][]string{nil, {flagTblproperties, litAll}, {flagTblproperties, "owner"}} {
		t.Run(strJoin(tp), func(t *testing.T) {
			tmpDir := setupTestProject(t)
			pullRich(t, tp...)

			raw, err := os.ReadFile(filepath.Join(tmpDir, "tables", "silver", "orders.yml"))
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "nullable: true")
			assert.Contains(t, string(raw), "nullable: false")

			proj, diags := config.LoadProject(tmpDir)
			require.False(t, diags.HasError(), diags)
			tables, diags := config.LoadTables(tmpDir, proj)
			require.Empty(t, diags, diags)
			require.Len(t, tables, 2)
			byName := map[string]config.Table{tables[0].Name: tables[0], tables[1].Name: tables[1]}
			orders := byName["orders"]
			assert.Equal(t, "it's the orders: 100% 'quoted'", orders.Comment)
			assert.Equal(t, "it's the key", orders.Columns[0].Comment)
			assert.Equal(t, "order", orders.Columns[1].Name)
			assert.Equal(t, "город", orders.Columns[2].Name)
			require.NotNil(t, byName["events"].Partitioning)
			assert.Equal(t, []string{"dt", "kind"}, byName["events"].Partitioning.Columns)

			out, err := executeCmd("plan", "-t", "dev")
			require.NoError(t, err, out)
			assert.Contains(t, out, "No changes")
			out, err = executeCmd("validate", "-t", "dev")
			require.NoError(t, err, out)
		})
	}
}

// pull -> edit the file (nullable flipped both ways) -> apply (mock) -> the live
// state after apply -> plan: 0 changes.
func TestPullThenApply_NullableFlips_ThenPlanNoChanges(t *testing.T) {
	tmpDir := setupTestProject(t)
	mock := pullRich(t)

	file := filepath.Join(tmpDir, "tables", "silver", "orders.yml")
	// pulled: id NOT NULL, order nullable; edited: id nullable (no key), order NOT NULL
	desired := `name: orders
schema: silver
comment: "it's the orders: 100% 'quoted'"
columns:
  - name: id
    type: bigint
    comment: "it's the key"
  - name: order
    type: string
    nullable: false
  - name: город
    type: string
    comment: ünï
  - name: amount
    type: decimal(10,2)
    nullable: false
  - name: tags
    type: array<string>
  - name: dt
    type: date
clustering:
  columns: [id, order]
`
	require.NoError(t, os.WriteFile(file, []byte(desired), 0o644))

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err, out)
	assert.Contains(t, out, "id")
	assert.Contains(t, out, "order")
	assert.NotContains(t, out, "No changes")

	out, err = executeCmd("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err, out)
	require.Len(t, mock.RecordedCalls(), 2, "two nullable alters (id, order)")

	// the world after apply
	after := liveRich()
	after.Columns[0].Nullable = true
	after.Columns[1].Nullable = false
	mock.SetFixture(silverRef, []*backend.LiveTable{after, liveEvents()})
	out, err = executeCmd("plan", "-t", "dev")
	require.NoError(t, err, out)
	assert.Contains(t, out, "No changes")
}

func strJoin(a []string) string {
	if len(a) == 0 {
		return "default"
	}
	return a[len(a)-1]
}
