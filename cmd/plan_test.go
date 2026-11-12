package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlan_NoWarehouseWarning verifies that plan does not warn about missing warehouse_id.
// A missing warehouse_id is acceptable when there are no SQL changes.
func TestPlan_NoWarehouseWarning(t *testing.T) {
	setupPlanProject(t)

	// targets/dev.yml created by setupTestProject already has no warehouse_id
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{})
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err)
	assert.NotContains(t, out, "warehouse_id not set", "plan must not warn about missing warehouse_id")
}

// --- --show-sql and --verbose removal ---

// TestPlan_ShowSqlFlag verifies that --show-sql causes SQL statements to appear in plan output.
func TestPlan_ShowSqlFlag(t *testing.T) {
	setupPlanProject(t)

	mock := &backendtest.MockUCBackend{}
	// Live state: table exists in UC but is missing the "id" column → plan will AddColumn
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

	out, err := executeCmd("plan", "-t", "dev", "--show-sql")
	require.NoError(t, err)
	// SQL must appear in output (e.g., ALTER TABLE or CREATE TABLE statement)
	hasSQL := strings.Contains(out, "ALTER TABLE") ||
		strings.Contains(out, "CREATE TABLE") ||
		strings.Contains(out, "ADD COLUMN")
	assert.True(t, hasSQL, "--show-sql must include SQL in output, got: %s", out)
}

// TestPlan_VerboseFlagRemoved verifies that --verbose is no longer a valid flag.
func TestPlan_VerboseFlagRemoved(t *testing.T) {
	out, err := executeCmd("plan", "-t", "dev", "--verbose")
	require.Error(t, err, "--verbose must be rejected as an unknown flag")
	combined := out + err.Error()
	assert.Contains(t, combined, "verbose", "error must mention 'verbose'")
}

// --- SilenceErrors ---

// TestPlan_DetailedExitcode_NoErrorPrefix verifies that --detailed-exitcode with changes
// does NOT print "Error:" or "Usage:" in output.
func TestPlan_DetailedExitcode_NoErrorPrefix(t *testing.T) {
	setupPlanProject(t)

	mock := &backendtest.MockUCBackend{}
	// DROP COLUMN requires column mapping (preflight).
	mock.TableProperties = map[string]map[string]string{litDevSilverOrders: {propColumnMapping: litName}}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litDev,
			ResolvedSchema:  litSilver,
			Columns: []config.Column{
				{Name: "id", Type: typBIGINT},
				{Name: "extra", Type: typSTRING},
			},
		}),
	})
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev", "--detailed-exitcode")
	// exit 2 is expected (changes present), but "Error:" and "Usage:" must NOT appear.
	require.Error(t, err, "--detailed-exitcode with changes must return non-zero")
	assert.NotContains(t, out, "Error:",
		"--detailed-exitcode output must not contain 'Error:' (SilenceErrors)")
	assert.NotContains(t, out, "Usage:",
		"--detailed-exitcode output must not contain 'Usage:' (SilenceUsage)")
}

// TestPlan_DetailedExitcode_PlanIsPresent verifies that the plan output IS present
// even when exit code is 2.
func TestPlan_DetailedExitcode_PlanIsPresent(t *testing.T) {
	setupPlanProject(t)

	mock := &backendtest.MockUCBackend{}
	// DROP COLUMN requires column mapping (preflight).
	mock.TableProperties = map[string]map[string]string{litDevSilverOrders: {propColumnMapping: litName}}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litDev,
			ResolvedSchema:  litSilver,
			Columns: []config.Column{
				{Name: "id", Type: typBIGINT},
				{Name: "extra", Type: typSTRING},
			},
		}),
	})
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev", "--detailed-exitcode")
	require.Error(t, err)
	hasPlan := strings.Contains(out, "Plan:") ||
		strings.Contains(out, "drop column") ||
		strings.Contains(out, "to destroy") ||
		strings.Contains(out, "orders")
	assert.True(t, hasPlan,
		"--detailed-exitcode output must include plan info, got: %q", out)
}

// --- Plan grouping ---

// TestPlan_Grouping_SameTable_NameAppearsOnce verifies that when multiple changes
// belong to the same table and are adjacent in the plan, the table name appears once as a header.
func TestPlan_Grouping_SameTable_NameAppearsOnce(t *testing.T) {
	setupPlanProject(t)

	mock := &backendtest.MockUCBackend{}
	// DROP COLUMN requires column mapping (preflight).
	mock.TableProperties = map[string]map[string]string{"dev.silver.orders": {"delta.columnMapping.mode": "name"}}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litDev,
			ResolvedSchema:  litSilver,
			Columns: []config.Column{
				{Name: "extra_col", Type: typSTRING},
			},
		}),
	})
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err)

	assert.Contains(t, out, "orders",
		"table name 'orders' must appear in plan output when it has changes")

	assert.Equal(t, 1, strings.Count(out, "`dev`.`silver`.`orders`"),
		"consecutive changes of one table must share a single header")
}

// TestPlan_Grouping_TwoTables_BothNamesPresent verifies that when changes affect
// two different tables, both table names appear in the plan output.
func TestPlan_Grouping_TwoTables_BothNamesPresent(t *testing.T) {
	setupTwoTableProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{})
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err)

	assert.Contains(t, out, "orders",
		"table 'orders' must appear in plan output")
	assert.Contains(t, out, "customers",
		"table 'customers' must appear in plan output")
}

// TestPlan_Grouping_SameTable_HeaderBeforeChanges verifies the Terraform-style output
// format: table header precedes its indented changes.
func TestPlan_Grouping_SameTable_HeaderBeforeChanges(t *testing.T) {
	setupPlanProject(t)

	mock := &backendtest.MockUCBackend{}
	// DROP COLUMN requires column mapping (preflight).
	mock.TableProperties = map[string]map[string]string{"dev.silver.orders": {"delta.columnMapping.mode": "name"}}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litDev,
			ResolvedSchema:  litSilver,
			Columns:         []config.Column{{Name: "old_col", Type: typSTRING}},
		}),
	})
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err)

	assert.Contains(t, out, "orders",
		"table name must appear in grouped plan output")
	hasSomeChange := strings.Contains(out, "add column") ||
		strings.Contains(out, "drop column") ||
		strings.Contains(out, "column")
	assert.True(t, hasSomeChange,
		"plan output must describe column changes, got: %q", out)
}

// column count in the create-table line is pluralised in text, --json and --show-sql.
func TestPlanCreateTable_ColumnPlural(t *testing.T) {
	cases := map[int]string{1: "(1 column)", 2: "(2 columns)", 17: "(17 columns)"}
	for n, want := range cases {
		t.Run(want, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			var b strings.Builder
			b.WriteString("name: fresh\nschema: silver\n")
			b.WriteString("columns:\n")
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, "  - name: c%d\n    type: INT\n", i)
			}
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "tables", "silver"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tables", "silver", "fresh.yml"), []byte(b.String()), 0644))
			useBackend(t, &backendtest.MockUCBackend{})

			stdout, stderr, err := executeCmdSplit("plan", "-t", "dev")
			require.NoError(t, err, stderr)
			assert.Contains(t, stdout, want)
			if n == 1 {
				assert.NotContains(t, stdout, "1 columns")
			}

			stdout, _, err = executeCmdSplit("plan", "-t", "dev", "--json")
			require.NoError(t, err)
			assert.Contains(t, stdout, want)
			if n == 1 {
				assert.NotContains(t, stdout, "1 columns")
			}

			withSQL, _, err := executeCmdSplit("plan", "-t", "dev", "--show-sql")
			require.NoError(t, err)
			assert.Contains(t, withSQL, "CREATE TABLE")
			assert.Contains(t, withSQL, want)
		})
	}
}
