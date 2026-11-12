package cmd

// selector_test.go covers the --schema / --table selectors of plan and apply.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
)

const (
	selGoldOrders = "gold.orders"
	selPlatinum   = "platinum"

	selSilverCustomers = "silver.customers"
	selSilverOrders    = "silver.orders"
)

func tbl(schema, name string) config.Table { return config.Table{Schema: schema, Name: name} }

func names(tables []config.Table) []string {
	out := make([]string, len(tables))
	for i, t := range tables {
		out[i] = t.Schema + "." + t.Name
	}
	return out
}

var selectorTables = []config.Table{
	tbl(litSilver, litOrders), tbl(litSilver, "customers"), tbl("gold", litOrders), tbl("gold", "Events"), tbl("Bronze", "raw"),
}

func TestSelector_SelectTables(t *testing.T) {
	for _, tc := range []struct {
		name string
		sel  selector
		want []string
	}{
		{"no selector picks everything", selector{}, names(selectorTables)},
		{"schema", selector{schemas: []string{litSilver}}, []string{selSilverOrders, selSilverCustomers}},
		{"schema ignores case, both sides", selector{schemas: []string{"SILVER", litBronze}}, []string{selSilverOrders, selSilverCustomers, "Bronze.raw"}},
		{"qualified table", selector{tables: []string{selGoldOrders}}, []string{selGoldOrders}},
		{"qualified table ignores case", selector{tables: []string{"GOLD.events"}}, []string{"gold.Events"}},
		{"bare name picks it in every schema", selector{tables: []string{litOrders}}, []string{selSilverOrders, selGoldOrders}},
		{"bare name ignores case", selector{tables: []string{"EVENTS"}}, []string{"gold.Events"}},
		{"repeated tables are a union, in project order", selector{tables: []string{selGoldOrders, selSilverCustomers}}, []string{selSilverCustomers, selGoldOrders}},
		{"schema and table are a union", selector{schemas: []string{litBronze}, tables: []string{selGoldOrders}}, []string{selGoldOrders, "Bronze.raw"}},
		{"overlap is not doubled", selector{schemas: []string{litSilver}, tables: []string{selSilverOrders, litOrders}}, []string{selSilverOrders, selSilverCustomers, selGoldOrders}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.sel.selectTables(selectorTables)
			require.NoError(t, err)
			assert.Equal(t, tc.want, names(got))
		})
	}
}

func TestSelector_Unknown(t *testing.T) {
	for _, tc := range []struct {
		name string
		sel  selector
		bad  string
	}{
		{"schema", selector{schemas: []string{selPlatinum}}, `--schema "platinum"`},
		{"table", selector{tables: []string{"silver.nope"}}, `--table "silver.nope"`},
		{"bare table", selector{tables: []string{litNope}}, `--table "nope"`},
		{"table in the wrong schema", selector{tables: []string{"bronze.orders"}}, `--table "bronze.orders"`},
		{"comma is not a separator", selector{schemas: []string{"silver,gold"}}, `--schema "silver,gold"`},
		{"empty schema", selector{schemas: []string{""}}, `--schema ""`},
		{"blank schema", selector{schemas: []string{"  "}}, `--schema "  "`},
		{"empty table", selector{tables: []string{""}}, `--table ""`},
		{"lone dot", selector{tables: []string{"."}}, `--table "."`},
		{"three parts", selector{tables: []string{"silver.orders.x"}}, `--table "silver.orders.x"`},
		{"one good one bad is still an error", selector{schemas: []string{litSilver, selPlatinum}}, `--schema "platinum"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.sel.selectTables(selectorTables)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), tc.bad)
			assert.Contains(t, err.Error(), "available schemas: Bronze, gold, silver")
			assert.Contains(t, err.Error(), "available tables: Bronze.raw, gold.Events, gold.orders, silver.customers, silver.orders")
		})
	}
	// every bad selector is named, not only the first
	_, err := selector{schemas: []string{"a"}, tables: []string{"b"}}.selectTables(selectorTables)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `--schema "a"`)
	assert.Contains(t, err.Error(), `--table "b"`)

	_, err = selector{schemas: []string{litSilver}}.selectTables(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the project has no tables")
}

// selectorProject: silver has orders and customers, gold has orders and events; every table is missing in
// UC (a create table each) unless a test sets live tables.
func selectorProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		pathSilverOrders:              tableYAMLOf(litOrders, litSilver),
		"tables/silver/customers.yml": tableYAMLOf("customers", litSilver),
		"tables/gold/orders.yml":      tableYAMLOf(litOrders, litGold),
		"tables/gold/events.yml":      tableYAMLOf("events", litGold),
	}
	for k, v := range extra {
		files[k] = v
	}
	return newProject(t, projectOpts{
		Catalog: "name: main\nuc_name: dev\nschemas:\n  silver:\n    uc_name: silver\n  gold:\n    uc_name: gold\n",
		Files:   files,
	})
}

func TestSelector_Plan_OnlySelectedTablesAreRead(t *testing.T) {
	selectorProject(t, nil)
	b := useSpy(t, newSpy(nil))

	stdout, stderr, err := executeCmdSplit(cmdPlan, "-t", litDev, flagSchema, "SILVER")
	require.NoError(t, err, stderr)

	assert.Equal(t, []backend.SchemaRef{jsonSilverRef}, b.listed, "gold must not be read at all")
	assert.Contains(t, stdout, "`dev`.`silver`.`orders`")
	assert.Contains(t, stdout, "`dev`.`silver`.`customers`")
	assert.NotContains(t, stdout, "`gold`")
	assert.Contains(t, stdout, "Plan: 2 to add, 0 to change, 0 to destroy.")
}

func TestSelector_Plan_JSONSummaryIsOfTheSelection(t *testing.T) {
	selectorProject(t, nil)
	useSpy(t, newSpy(nil))

	stdout, _, err := executeCmdSplit(cmdPlan, "-t", litDev, flagTable, selGoldOrders, flagTable, selSilverCustomers, flagJSON)
	require.NoError(t, err)

	var plan planJSON
	decodeSingleJSON(t, stdout, &plan)
	assert.Equal(t, 2, plan.Summary.Add)
	assert.Zero(t, plan.Summary.Change)
	assert.Zero(t, plan.Summary.Destroy)
	assert.Len(t, plan.Changes, 2)
}

func TestSelector_Plan_DetailedExitcodeIgnoresOtherTables(t *testing.T) {
	selectorProject(t, nil)
	b := newSpy(nil)
	// silver.orders is up to date; every other table is missing in UC and would be created
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id")})
	useSpy(t, b)

	stdout, _, err := executeCmdSplit(cmdPlan, "-t", litDev, flagTable, selSilverOrders, flagDetailedExitcode)
	require.NoError(t, err)
	assert.Equal(t, 0, codeOf(err))
	assert.Contains(t, stdout, "No changes")

	_, _, err = executeCmdSplit(cmdPlan, "-t", litDev, flagTable, selGoldOrders, flagDetailedExitcode)
	assert.Equal(t, 2, codeOf(err))
}

func TestSelector_Apply_OnlySelectedTablesAreApplied(t *testing.T) {
	selectorProject(t, nil)
	b := useSpy(t, newSpy(nil))

	stdout, stderr, err := executeCmdSplit(cmdApply, "-t", litDev, flagAutoApprove, flagTable, litOrders)
	require.NoError(t, err, stderr)

	require.Len(t, b.sqls, 2, "orders in silver and in gold, nothing else")
	assert.Contains(t, b.sqls[0], "`dev`.`gold`.`orders`") // plan order: schemas are sorted, gold first
	assert.Contains(t, b.sqls[1], "`dev`.`silver`.`orders`")
	assert.Contains(t, stdout, "[2/2] applied in ")
	assert.ElementsMatch(t, []backend.SchemaRef{jsonSilverRef, goldRef}, b.listed)
}

func TestSelector_Apply_JSONTotalsAreOfTheSelection(t *testing.T) {
	selectorProject(t, nil)
	useSpy(t, newSpy(nil))

	stdout, _, err := executeCmdSplit(cmdApply, "-t", litDev, flagAutoApprove, flagJSON, flagTable, "gold.events")
	require.NoError(t, err)

	var res applyJSON
	decodeSingleJSON(t, stdout, &res)
	assert.Equal(t, 1, res.Total)
	assert.Equal(t, 1, res.Applied)
	assert.Zero(t, res.Skipped)
	assert.Empty(t, res.Aborted)
}

// A table with drop: true is a table of the YAML like any other: it is selected by name, and
// its drop is planned only when it is selected. A destructive change outside of the selection neither
// asks for confirmation nor runs.
func TestSelector_DropTrueTables(t *testing.T) {
	setup := func(t *testing.T) *spyBackend {
		selectorProject(t, map[string]string{
			"tables/silver/legacy.yml": "name: legacy\nschema: silver\ndrop: true\n",
			"tables/gold/old.yml":      "name: old\nschema: gold\ndrop: true\n",
		})
		b := newSpy(nil)
		b.SetFixture(jsonSilverRef, []*backend.LiveTable{
			liveOrders("id"), backendtest.Live("dev", litSilver, "customers", backendtest.Col("id", "BIGINT")),
			backendtest.Live("dev", litSilver, "legacy", backendtest.Col("id", "BIGINT")),
		})
		b.SetFixture(goldRef, []*backend.LiveTable{
			backendtest.Live("dev", "gold", litOrders, backendtest.Col("id", "BIGINT")),
			backendtest.Live("dev", "gold", "events", backendtest.Col("id", "BIGINT")),
			backendtest.Live("dev", "gold", "old", backendtest.Col("id", "BIGINT")),
		})
		return useSpy(t, b)
	}

	t.Run("the schema of the drop is selected", func(t *testing.T) {
		b := setup(t)
		_, stderr, err := executeCmdSplit(cmdApply, "-t", litDev, flagAutoApprove, flagSchema, litSilver)
		require.NoError(t, err, stderr)
		assert.Equal(t, []string{"DROP TABLE `dev`.`silver`.`legacy`"}, b.sqls)
	})
	t.Run("the table itself is selected", func(t *testing.T) {
		b := setup(t)
		_, _, err := executeCmdSplit(cmdApply, "-t", litDev, flagAutoApprove, flagTable, "gold.old")
		require.NoError(t, err)
		assert.Equal(t, []string{"DROP TABLE `dev`.`gold`.`old`"}, b.sqls)
	})
	t.Run("another table of the schema is selected: no drop, no prompt", func(t *testing.T) {
		b := setup(t)
		useStdin(t, "") // a prompt would fail with "no input"
		stdout, stderr, err := executeCmdSplit(cmdApply, "-t", litDev, flagTable, selSilverOrders)
		require.NoError(t, err, stderr)
		assert.Empty(t, b.sqls)
		assert.Contains(t, stdout, "No changes")
		assert.NotContains(t, stdout+stderr, "Type 'yes'")
	})
	t.Run("plan with the other schema shows no destroy", func(t *testing.T) {
		setup(t)
		stdout, _, err := executeCmdSplit(cmdPlan, "-t", litDev, flagSchema, litGold, flagJSON)
		require.NoError(t, err)
		var plan planJSON
		decodeSingleJSON(t, stdout, &plan)
		assert.Equal(t, 1, plan.Summary.Destroy, "gold.old only")
	})
}

// rename_from: the table is named by its name in the YAML; the old name selects nothing.
func TestSelector_RenameFromUsesNewName(t *testing.T) {
	selectorProject(t, map[string]string{
		pathSilverOrders: "name: orders_v2\nschema: silver\nrename_from: orders\ncolumns:\n  - name: id\n    type: BIGINT\n",
	})
	b := newSpy(nil)
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id")})
	useSpy(t, b)

	stdout, stderr, err := executeCmdSplit(cmdPlan, "-t", litDev, flagTable, "silver.orders_v2")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "rename table")

	_, _, err = executeCmdSplit(cmdPlan, "-t", litDev, flagTable, selSilverOrders)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `--table "silver.orders"`)
}

// An unknown selector fails before UC is touched (not even a connection), code 1, with the list of
// what is available.
func TestSelector_Unknown_FailsBeforeBackend(t *testing.T) {
	for _, cmdName := range []string{cmdPlan, cmdApply} {
		for _, args := range [][]string{{flagSchema, selPlatinum}, {flagTable, "silver.nope"}, {flagTable, ""}, {flagSchema, "silver,gold"}} {
			t.Run(cmdName+" "+args[0]+" "+args[1], func(t *testing.T) {
				selectorProject(t, nil)
				b := useSpy(t, newSpy(nil).forbid(t))

				cmdLine := []string{cmdName, "-t", litDev}
				if cmdName == cmdApply {
					cmdLine = append(cmdLine, flagAutoApprove)
				}
				stdout, stderr, err := executeCmdSplit(append(cmdLine, args...)...)
				require.Error(t, err)
				assert.Equal(t, 1, codeOf(err))
				assert.Empty(t, stdout)
				assert.Contains(t, err.Error(), "unknown selector: --"+args[0][2:]+" "+quoted(args[1]))
				assert.Contains(t, err.Error(), "available schemas: gold, silver")
				assert.Contains(t, err.Error(), "available tables: gold.events, gold.orders, silver.customers, silver.orders")
				assert.NotContains(t, stderr, "Plan:")
				b.assertUntouched(t)
			})
		}
	}
}

// The selectors narrow the diff, not the validation: a broken table outside the selection still fails.
func TestSelector_ValidatesWholeProject(t *testing.T) {
	selectorProject(t, map[string]string{
		"tables/gold/events.yml": "name: events\nschema: gold\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: id\n    type: STRING\n",
	})
	b := useSpy(t, newSpy(nil).forbid(t))

	_, _, err := executeCmdSplit(cmdPlan, "-t", litDev, flagSchema, litSilver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local validation failed")
	b.assertUntouched(t)
}

func TestSelector_FlagsAreRepeatableAndNotShared(t *testing.T) {
	// the values of one call must not leak into the next one (a fresh command tree per call)
	selectorProject(t, nil)
	b := useSpy(t, newSpy(nil))
	_, _, err := executeCmdSplit(cmdPlan, "-t", litDev, flagSchema, litSilver)
	require.NoError(t, err)
	_, _, err = executeCmdSplit(cmdPlan, "-t", litDev)
	require.NoError(t, err)
	assert.Len(t, b.listed, 3, "silver once, then silver and gold: the second call has no selector")
}

func quoted(s string) string { return `"` + s + `"` }
