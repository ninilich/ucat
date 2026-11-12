package preflight_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/ninilich/ucat/internal/planner"
	"github.com/ninilich/ucat/internal/preflight"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBackend is a minimal UCBackend that supports GetTableProperties for preflight tests.
type mockBackend struct {
	backendtest.MockUCBackend
}

var testTablePath = change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}

// newMockWithProps returns a *mockBackend with preset tblproperties for testTablePath.
// Uses unquoted "catalog.schema.name" key to match MockUCBackend.GetTableProperties lookup.
func newMockWithProps(props map[string]string) *mockBackend {
	m := &mockBackend{}
	if props != nil {
		key := testTablePath.Catalog + "." + testTablePath.Schema + "." + testTablePath.Name
		m.TableProperties = map[string]map[string]string{
			key: props,
		}
	}
	return m
}

// --- ColumnMappingChecker ---

// TestColumnMappingChecker_AppliesOnlyToRenameAndDropColumn verifies that Applies returns true
// only for RenameColumn and DropColumn changes, false for all other change types.
func TestColumnMappingChecker_AppliesOnlyToRenameAndDropColumn(t *testing.T) {
	checker := &preflight.ColumnMappingChecker{}

	renameCol := &change.RenameColumnChange{
		Base:    change.Base{Table: testTablePath},
		OldName: litOldCol,
		NewName: litNewCol,
	}
	assert.True(t, checker.Applies(renameCol),
		"Applies must return true for RenameColumnChange")
	assert.True(t, checker.Applies(&change.DropColumnChange{Base: change.Base{Table: testTablePath}, ColumnName: "x"}),
		"Applies must return true for DropColumnChange")

	otherChanges := []change.Change{
		&change.AddColumnChange{Base: change.Base{Table: testTablePath}, Column: testColumn()},
		&change.AlterColumnChange{Base: change.Base{Table: testTablePath}, ColumnName: "x", Kind: change.AlterType},
		&change.RenameTableChange{OldPath: testTablePath, NewName: litOther},
		&change.SetTBLPropertiesChange{Base: change.Base{Table: testTablePath}, Properties: map[string]string{"k": "v"}},
		&change.DropTableChange{Base: change.Base{Table: testTablePath}},
		&change.CreateTableChange{Base: change.Base{Table: testTablePath}},
	}
	for _, ch := range otherChanges {
		assert.False(t, checker.Applies(ch),
			"Applies must return false for %T", ch)
	}
}

// TestColumnMappingChecker_Check_ColumnMappingSet verifies that when the live table
// has delta.columnMapping.mode = "name", no diagnostics are returned.
func TestColumnMappingChecker_Check_ColumnMappingSet(t *testing.T) {
	checker := &preflight.ColumnMappingChecker{}

	mock := newMockWithProps(map[string]string{
		propColumnMapping: litName,
	})

	ch := &change.RenameColumnChange{
		Base:    change.Base{Table: testTablePath},
		OldName: litOldCol,
		NewName: litNewCol,
	}

	diags := checker.Check(context.Background(), ch, mock)
	assert.False(t, diags.HasError(),
		"no errors expected when delta.columnMapping.mode = 'name', diags: %v", diags)
	assert.Empty(t, diags, "zero diagnostics expected when column mapping is configured")
}

// TestColumnMappingChecker_Check_ColumnMappingMissing verifies that when the live table
// does NOT have delta.columnMapping.mode, an error diagnostic is returned
// mentioning "delta.columnMapping.mode".
func TestColumnMappingChecker_Check_ColumnMappingMissing(t *testing.T) {
	checker := &preflight.ColumnMappingChecker{}

	mock := newMockWithProps(map[string]string{
		"someOtherProp": "value",
	})

	ch := &change.RenameColumnChange{
		Base:    change.Base{Table: testTablePath},
		OldName: litOldCol,
		NewName: litNewCol,
	}

	diags := checker.Check(context.Background(), ch, mock)
	require.True(t, diags.HasError(),
		"error expected when delta.columnMapping.mode is absent")

	found := false
	for _, d := range diags {
		if strings.Contains(d.Summary, "delta.columnMapping.mode") {
			found = true
		}
	}
	assert.True(t, found,
		"diagnostic must mention 'delta.columnMapping.mode'; got: %v", diags)
}

// TestColumnMappingChecker_Check_ColumnMappingWrongValue verifies that when
// delta.columnMapping.mode is set to a value other than "name", an error is returned.
func TestColumnMappingChecker_Check_ColumnMappingWrongValue(t *testing.T) {
	checker := &preflight.ColumnMappingChecker{}

	mock := newMockWithProps(map[string]string{
		propColumnMapping: "id", // not "name"
	})

	ch := &change.RenameColumnChange{
		Base:    change.Base{Table: testTablePath},
		OldName: litOldCol,
		NewName: "new_col",
	}

	diags := checker.Check(context.Background(), ch, mock)
	assert.True(t, diags.HasError(),
		"error expected when delta.columnMapping.mode is not 'name'")
}

// --- Run integration ---

// TestRun_NoCheckerApplies verifies that Run returns 0 diagnostics when no checker applies.
func TestRun_NoCheckerApplies(t *testing.T) {
	plan := []change.Change{
		&change.AddColumnChange{Base: change.Base{Table: testTablePath}, Column: testColumn()},
	}
	// ColumnMappingChecker does not apply to AddColumn.
	diags := preflight.Run(context.Background(), plan, nil, preflight.DefaultCheckers(plan))
	assert.Empty(t, diags)
}

// TestRun_ColumnMappingCheckRunsForRenameColumn verifies that Run invokes
// ColumnMappingChecker when plan contains a RenameColumn change.
func TestRun_ColumnMappingCheckRunsForRenameColumn(t *testing.T) {
	mock := newMockWithProps(nil) // no column mapping mode set

	plan := []change.Change{
		&change.RenameColumnChange{
			Base:    change.Base{Table: testTablePath},
			OldName: "old",
			NewName: "new",
		},
	}

	diags := preflight.Run(context.Background(), plan, mock, preflight.DefaultCheckers(plan))
	assert.True(t, diags.HasError(),
		"Run must surface ColumnMappingChecker error when property is missing")
}

// --- ColumnMappingChecker lookahead ---

// TestColumnMappingChecker_Lookahead_SetTBLInPlan verifies that when the plan contains a
// SetTBLPropertiesChange setting delta.columnMapping.mode=name for the same table, followed
// by a RenameColumnChange, the checker produces 0 errors (lookahead sees the mapping will be set).
func TestColumnMappingChecker_Lookahead_SetTBLInPlan(t *testing.T) {
	path := testTablePath // "prod.silver.orders"

	plan := []change.Change{
		// Prerequisite: set column mapping before rename
		&change.SetTBLPropertiesChange{
			Base:       change.Base{Table: path},
			Properties: map[string]string{propColumnMapping: litName},
		},
		// Column rename (only valid after mapping is set)
		&change.RenameColumnChange{
			Base:    change.Base{Table: path},
			OldName: litUserName,
			NewName: litFullName,
		},
	}

	// Live table does NOT have column mapping set yet (it will be set by SetTBLProperties)
	mock := newMockWithProps(nil)

	diags := preflight.Run(context.Background(), plan, mock, preflight.DefaultCheckers(plan))

	// Contract: lookahead sees SetTBLProperties will enable column mapping
	// → RenameColumn is safe → 0 errors
	assert.False(t, diags.HasError(),
		"ColumnMappingChecker with lookahead must see SetTBLProperties in same plan "+
			"and allow RenameColumn; diags: %v", diags)
}

// TestColumnMappingChecker_Lookahead_NoSetTBL_StillErrors verifies the negative case:
// without SetTBLProperties in the plan, RenameColumn without live column mapping must still error.
func TestColumnMappingChecker_Lookahead_NoSetTBL_StillErrors(t *testing.T) {
	plan := []change.Change{
		&change.RenameColumnChange{
			Base:    change.Base{Table: testTablePath},
			OldName: "user_name",
			NewName: "full_name",
		},
	}

	// Live: no column mapping, no SetTBLProperties in plan
	mock := newMockWithProps(nil)

	diags := preflight.Run(context.Background(), plan, mock, preflight.DefaultCheckers(plan))
	assert.True(t, diags.HasError(),
		"ColumnMappingChecker must error when neither live props nor plan sets column mapping")
}

// TestColumnMappingChecker_Lookahead_DifferentTable_StillErrors verifies that lookahead
// only considers SetTBLProperties for the SAME table as the RenameColumn.
func TestColumnMappingChecker_Lookahead_DifferentTable_StillErrors(t *testing.T) {
	otherPath := change.TablePath{Catalog: litProd, Schema: litSilver, Name: "other_table"}

	plan := []change.Change{
		// Sets column mapping on "other_table", not on "orders"
		&change.SetTBLPropertiesChange{
			Base:       change.Base{Table: otherPath},
			Properties: map[string]string{propColumnMapping: litName},
		},
		// Rename column on "orders" - no column mapping for this table
		&change.RenameColumnChange{
			Base:    change.Base{Table: testTablePath}, // "orders"
			OldName: "user_name",
			NewName: "full_name",
		},
	}

	mock := newMockWithProps(nil)

	diags := preflight.Run(context.Background(), plan, mock, preflight.DefaultCheckers(plan))
	assert.True(t, diags.HasError(),
		"SetTBLProperties on a different table must not satisfy ColumnMappingChecker for 'orders'")
}

// --- helpers ---

func testColumn() config.Column {
	return config.Column{Name: "x", Type: typSTRING}
}

// --- ColumnMappingChecker for DropColumn (via DefaultCheckers) ---

// dropErr is the expected preflight error for dropping column "old_col"; table is "<schema>.<table>".
func dropErr(table string) string {
	return `table "` + table + `": column drop "old_col" requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`
}

type errPropsBackend struct {
	backendtest.MockUCBackend
}

func (e *errPropsBackend) GetTableProperties(_ context.Context, _, _, _ string) (map[string]string, error) {
	return nil, errors.New("boom")
}

func dropChange() *change.DropColumnChange {
	return &change.DropColumnChange{Base: change.Base{Table: testTablePath}, ColumnName: litOldCol}
}

// runDefault runs the default checkers and returns error summaries.
func runDefault(plan []change.Change, b backend.UCBackend) []string {
	var out []string
	for _, d := range preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan)) {
		if d.Severity == diag.Error {
			out = append(out, d.Summary)
		}
	}
	return out
}

func TestDropColumn_LookaheadNameMode_OK(t *testing.T) {
	plan := []change.Change{
		&change.SetTBLPropertiesChange{Base: change.Base{Table: testTablePath}, Properties: map[string]string{propColumnMapping: litName}},
		dropChange(),
	}
	// live is not queried: nil backend must not panic.
	assert.Empty(t, runDefault(plan, nil))
}

func TestDropColumn_LookaheadOtherValue_FallsBackToLive(t *testing.T) {
	plan := []change.Change{
		&change.SetTBLPropertiesChange{Base: change.Base{Table: testTablePath}, Properties: map[string]string{propColumnMapping: "id"}},
		dropChange(),
	}
	assert.Equal(t, []string{dropErr("silver.orders")}, runDefault(plan, newMockWithProps(nil)))
	assert.Empty(t, runDefault(plan, newMockWithProps(map[string]string{propColumnMapping: litName})))
}

func TestDropColumn_LookaheadOtherTable_FallsBackToLive(t *testing.T) {
	other := change.TablePath{Catalog: litProd, Schema: litSilver, Name: "other"}
	plan := []change.Change{
		&change.SetTBLPropertiesChange{Base: change.Base{Table: other}, Properties: map[string]string{propColumnMapping: litName}},
		dropChange(),
	}
	assert.Equal(t, []string{dropErr("silver.orders")}, runDefault(plan, newMockWithProps(nil)))
}

func TestDropColumn_LiveMode(t *testing.T) {
	plan := []change.Change{dropChange()}
	assert.Empty(t, runDefault(plan, newMockWithProps(map[string]string{propColumnMapping: litName})))
	assert.Equal(t, []string{dropErr("silver.orders")}, runDefault(plan, newMockWithProps(nil)))
	assert.Equal(t, []string{dropErr("silver.orders")},
		runDefault(plan, newMockWithProps(map[string]string{propColumnMapping: "id"})))
}

func TestDropColumn_BackendError(t *testing.T) {
	plan := []change.Change{dropChange()}
	errs := runDefault(plan, &errPropsBackend{})
	require.Len(t, errs, 1)
	assert.True(t, strings.HasPrefix(errs[0], `table "silver.orders": failed to get table properties: `), errs[0])
}

// Same short name in different schemas: two distinguishable errors, in plan order.
func TestColumnMapping_SameNameDifferentSchemas_Distinguishable(t *testing.T) {
	gold := change.TablePath{Catalog: litProd, Schema: "gold", Name: litOrders}
	plan := []change.Change{
		&change.DropColumnChange{Base: change.Base{Table: testTablePath}, ColumnName: litOldCol},
		&change.DropColumnChange{Base: change.Base{Table: gold}, ColumnName: litOldCol},
	}
	assert.Equal(t, []string{dropErr("silver.orders"), dropErr("gold.orders")}, runDefault(plan, newMockWithProps(nil)))
}

// Table renamed in the plan: the read error names the new table and says which name was read.
func TestColumnMapping_ReadError_RenamedTable(t *testing.T) {
	newPath := change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrdersV2}
	plan := []change.Change{
		&change.RenameTableChange{OldPath: testTablePath, NewName: litOrdersV2},
		&change.DropColumnChange{Base: change.Base{Table: newPath}, ColumnName: litOldCol},
	}
	errs := runDefault(plan, &errPropsBackend{})
	require.Len(t, errs, 1)
	assert.True(t, strings.HasPrefix(errs[0], `table "silver.orders_v2" (read as "silver.orders"): failed to get table properties: `), errs[0])
}

func TestDropColumn_OtherChangesDoNotTrigger(t *testing.T) {
	plan := []change.Change{&change.AddColumnChange{Base: change.Base{Table: testTablePath}, Column: testColumn()}}
	assert.Empty(t, runDefault(plan, newMockWithProps(nil)))

	// RenameColumn without mapping: exactly the existing rename error, no drop error.
	plan = []change.Change{&change.RenameColumnChange{Base: change.Base{Table: testTablePath}, OldName: "a", NewName: "b"}}
	errs := runDefault(plan, newMockWithProps(nil))
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "delta.columnMapping.mode")
	assert.NotEqual(t, dropErr("silver.orders"), errs[0])
}

// pipelinePlan runs the real DiffSchema -> planner.Plan chain.
func pipelinePlan(t *testing.T, desired config.Table, live config.Table, _ backend.UCBackend) []change.Change {
	t.Helper()
	changes, diags := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	require.False(t, diags.HasError(), "diags: %v", diags)
	plan := planner.Plan(changes)
	return plan
}

func dropScenarioTables() (config.Table, config.Table) {
	base := config.Table{Name: litOrders, Schema: litSilver, ResolvedCatalog: litProd, ResolvedSchema: litSilver}
	desired := base
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT}}
	live := base
	live.Columns = []config.Column{{Name: "id", Type: typBIGINT}, {Name: litOldCol, Type: typSTRING}}
	return desired, live
}

func TestPipeline_DropColumn_NoMapping_Error(t *testing.T) {
	desired, live := dropScenarioTables()
	mock := newMockWithProps(nil)
	plan := pipelinePlan(t, desired, live, mock)
	require.Len(t, plan, 1)
	assert.Equal(t, []string{dropErr("silver.orders")}, runDefault(plan, mock))
}

func TestPipeline_DropColumn_MappingInLive_OK(t *testing.T) {
	desired, live := dropScenarioTables()
	mp := litName
	desired.TblProperties = map[string]*string{propColumnMapping: &mp}
	live.TblProperties = map[string]*string{propColumnMapping: &mp}
	mock := newMockWithProps(map[string]string{propColumnMapping: litName})
	plan := pipelinePlan(t, desired, live, mock)
	assert.Empty(t, runDefault(plan, mock))
}

func TestPipeline_DropColumn_MappingInDesired_Lookahead(t *testing.T) {
	desired, live := dropScenarioTables()
	mp := litName
	desired.TblProperties = map[string]*string{propColumnMapping: &mp}
	mock := newMockWithProps(nil) // live has no mapping yet
	plan := pipelinePlan(t, desired, live, mock)

	setIdx, dropIdx := -1, -1
	for i, c := range plan {
		switch c.Type() {
		case change.SetTBLProperties:
			setIdx = i
		case change.DropColumn:
			dropIdx = i
		}
	}
	require.NotEqual(t, -1, setIdx, "plan: %v", plan)
	require.NotEqual(t, -1, dropIdx, "plan: %v", plan)
	assert.Less(t, setIdx, dropIdx, "SetTBLProperties must precede DropColumn")
	assert.Empty(t, runDefault(plan, mock))
}

func renamedTableScenario() (config.Table, config.Table) {
	desired, live := dropScenarioTables()
	desired.Name = litOrdersV2
	desired.RenameFrom = litOrders
	return desired, live
}

// The table is renamed in the same plan: preflight must read properties of the OLD name.
func TestPipeline_DropColumn_WithTableRename_ReadsOldName(t *testing.T) {
	desired, live := renamedTableScenario()
	mock := newMockWithProps(map[string]string{propColumnMapping: litName}) // keyed by old name "orders"
	plan := pipelinePlan(t, desired, live, mock)
	assert.Empty(t, runDefault(plan, mock))
}

func TestPipeline_DropColumn_WithTableRename_NoMapping_Error(t *testing.T) {
	desired, live := renamedTableScenario()
	mock := newMockWithProps(nil)
	plan := pipelinePlan(t, desired, live, mock)
	assert.Equal(t, []string{dropErr("silver.orders_v2")}, runDefault(plan, mock))
}

func TestPipeline_RenameColumn_WithTableRename_ReadsOldName(t *testing.T) {
	desired, live := renamedTableScenario()
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT}, {Name: "new_col", Type: typSTRING, RenameFrom: litOldCol}}
	mock := newMockWithProps(map[string]string{propColumnMapping: litName})
	plan := pipelinePlan(t, desired, live, mock)
	assert.Empty(t, runDefault(plan, mock))
}

func TestRenameColumn_ErrorNamesTableAndColumn(t *testing.T) {
	plan := []change.Change{&change.RenameColumnChange{Base: change.Base{Table: testTablePath}, OldName: "a", NewName: "b"}}
	assert.Equal(t,
		[]string{`table "silver.orders": column rename "a" requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`},
		runDefault(plan, newMockWithProps(nil)))
}

// Two tables in one plan: one error per table, each naming its own table and column.
func TestColumnMapping_TwoTables_OneErrorEach(t *testing.T) {
	other := change.TablePath{Catalog: litProd, Schema: litSilver, Name: "customers"}
	plan := []change.Change{
		&change.DropColumnChange{Base: change.Base{Table: testTablePath}, ColumnName: litOldCol},
		&change.RenameColumnChange{Base: change.Base{Table: other}, OldName: "n", NewName: litName},
	}
	assert.Equal(t, []string{
		dropErr("silver.orders"),
		`table "silver.customers": column rename "n" requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`,
	}, runDefault(plan, newMockWithProps(nil)))
}

// pipeline: real DiffSchema -> planner.Plan -> preflight.Run for two tables. The table that
// has the mapping in live passes; the other two report the right table and column.
func TestPipeline_TwoTables_ErrorsNameOwnTableAndColumn(t *testing.T) {
	mk := func(name string, liveCols, desiredCols []config.Column) (config.Table, config.Table) {
		base := config.Table{Name: name, Schema: litSilver, ResolvedCatalog: litProd, ResolvedSchema: litSilver}
		d, l := base, base
		d.Columns, l.Columns = desiredCols, liveCols
		return d, l
	}
	id := config.Column{Name: "id", Type: "BIGINT"}
	dA, lA := mk("orders", []config.Column{id, {Name: litOldCol, Type: typSTRING}}, []config.Column{id})
	dB, lB := mk("customers", []config.Column{id, {Name: "n", Type: typSTRING}}, []config.Column{id, {Name: litName, Type: typSTRING, RenameFrom: "n"}})
	dC, lC := mk("ok_table", []config.Column{id, {Name: "gone", Type: typSTRING}}, []config.Column{id})

	mock := &mockBackend{}
	mock.TableProperties = map[string]map[string]string{"prod.silver.ok_table": {"delta.columnMapping.mode": litName}}

	changes, diags := differ.DiffSchema([]config.Table{dA, dB, dC}, []*backend.LiveTable{backendtest.LiveOf(lA), backendtest.LiveOf(lB), backendtest.LiveOf(lC)})
	require.False(t, diags.HasError(), "diags: %v", diags)
	plan := planner.Plan(changes)

	res := preflight.Run(context.Background(), plan, mock, preflight.DefaultCheckers(plan))
	require.True(t, res.HasError())
	var got []string
	for _, d := range res {
		require.Equal(t, diag.Error, d.Severity)
		got = append(got, d.Summary)
	}
	assert.ElementsMatch(t, []string{
		dropErr("silver.orders"),
		`table "silver.customers": column rename "n" requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`,
	}, got)
}

// A table that sets the mapping in the same plan gives no error even with drop and rename together.
func TestPipeline_MappingSetInSamePlan_DropAndRename_NoError(t *testing.T) {
	desired, live := dropScenarioTables()
	desired.Columns = append(desired.Columns, config.Column{Name: "b", Type: typSTRING, RenameFrom: litOldCol})
	mp := "name"
	desired.TblProperties = map[string]*string{"delta.columnMapping.mode": &mp}
	mock := newMockWithProps(nil)
	plan := pipelinePlan(t, desired, live, mock)
	assert.Empty(t, runDefault(plan, mock))
}

// --- single table label in preflight messages ---

// pipeline: two tables with the same short name in different schemas, real
// DiffSchema -> planner.Plan -> preflight.Run. Errors are distinguishable, own column each, in plan order.
func TestPipeline_SameShortNameDifferentSchemas_Distinguishable(t *testing.T) {
	id := config.Column{Name: "id", Type: "BIGINT"}
	mk := func(schema string, liveCols, desiredCols []config.Column) (config.Table, config.Table) {
		base := config.Table{Name: litOrders, Schema: schema, ResolvedCatalog: litProd, ResolvedSchema: schema}
		d, l := base, base
		d.Columns, l.Columns = desiredCols, liveCols
		return d, l
	}
	dS, lS := mk("silver", []config.Column{id, {Name: "legacy_s", Type: typSTRING}}, []config.Column{id})
	dG, lG := mk("gold", []config.Column{id, {Name: "n", Type: "STRING"}}, []config.Column{id, {Name: "name", Type: "STRING", RenameFrom: "n"}})

	mock := &mockBackend{}
	// DiffSchema works per schema (as cmd/pipeline.go does): one call per schema, plan in schema order.
	chS, diags := differ.DiffSchema([]config.Table{dS}, []*backend.LiveTable{backendtest.LiveOf(lS)})
	require.False(t, diags.HasError(), "diags: %v", diags)
	chG, diags := differ.DiffSchema([]config.Table{dG}, []*backend.LiveTable{backendtest.LiveOf(lG)})
	require.False(t, diags.HasError(), "diags: %v", diags)
	plan := planner.Plan(append(chS, chG...))

	res := preflight.Run(context.Background(), plan, mock, preflight.DefaultCheckers(plan))
	require.True(t, res.HasError())
	var got []string
	for _, d := range res {
		require.Equal(t, diag.Error, d.Severity)
		assert.Empty(t, d.Location)
		got = append(got, d.Summary)
	}
	assert.Equal(t, []string{
		// planner orders RenameColumn before DropColumn, so the gold rename comes first.
		`table "gold.orders": column rename "n" requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`,
		`table "silver.orders": column drop "legacy_s" requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`,
	}, got)
}

// Read error for column rename when the table is renamed in the same plan: "(read as ...)" form.
func TestColumnMapping_RenameColumn_ReadError_RenamedTable(t *testing.T) {
	newPath := change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrdersV2}
	plan := []change.Change{
		&change.RenameTableChange{OldPath: testTablePath, NewName: litOrdersV2},
		&change.RenameColumnChange{Base: change.Base{Table: newPath}, OldName: "a", NewName: "b"},
	}
	errs := runDefault(plan, &errPropsBackend{})
	require.Len(t, errs, 1)
	assert.Equal(t, `table "silver.orders_v2" (read as "silver.orders"): failed to get table properties: boom`, errs[0])
}

// Read error for column drop without table rename: no "(read as ...)" part.
func TestDropColumn_ReadError_NoRename_NoReadAs(t *testing.T) {
	errs := runDefault([]change.Change{dropChange()}, &errPropsBackend{})
	require.Len(t, errs, 1)
	assert.Equal(t, `table "silver.orders": failed to get table properties: boom`, errs[0])
	assert.NotContains(t, errs[0], "read as")
}

// Known limitation (owner decision, not a bug): the catalog is not part of the label, so the same
// schema.table in two catalogs gives identical messages.
func TestColumnMapping_SameSchemaTableDifferentCatalogs_IdenticalMessages(t *testing.T) {
	dev := change.TablePath{Catalog: "dev", Schema: litSilver, Name: litOrders}
	main := change.TablePath{Catalog: "main", Schema: litSilver, Name: "orders"}
	plan := []change.Change{
		&change.DropColumnChange{Base: change.Base{Table: main}, ColumnName: "old_col"},
		&change.DropColumnChange{Base: change.Base{Table: dev}, ColumnName: "old_col"},
	}
	errs := runDefault(plan, newMockWithProps(nil))
	assert.Equal(t, []string{dropErr("silver.orders"), dropErr("silver.orders")}, errs)
}

// Regression: the label has no backticks and no "preflight:" prefix (old read error used the full backticked name).
func TestLabel_NoBackticksNoPreflightPrefix(t *testing.T) {
	newPath := change.TablePath{Catalog: litProd, Schema: litSilver, Name: "orders_v2"}
	plans := [][]change.Change{
		{dropChange()},
		{&change.RenameColumnChange{Base: change.Base{Table: testTablePath}, OldName: "a", NewName: "b"}},
		{&change.RenameTableChange{OldPath: testTablePath, NewName: "orders_v2"}, &change.DropColumnChange{Base: change.Base{Table: newPath}, ColumnName: "x"}},
	}
	for _, plan := range plans {
		for _, b := range []backend.UCBackend{newMockWithProps(nil), &errPropsBackend{}} {
			for _, s := range runDefault(plan, b) {
				assert.NotContains(t, s, "`")
				assert.NotContains(t, s, "preflight:")
				assert.True(t, strings.HasPrefix(s, `table "silver.`), s)
			}
		}
	}
}

// foreignChange claims a column change type but is not a change.* implementation. Production never
// builds one (Applies is decided by Type() on change.* values).
type foreignChange struct {
	change.Change
	ct change.Kind
}

func (f foreignChange) Type() change.Kind { return f.ct }

// A change that claims a column change type but has another concrete type must stop the run:
// a safety check never skips a change silently (fail-closed, like planner.orderGroup).
func TestColumnMappingChecker_ForeignChange_Panics(t *testing.T) {
	m := newMockWithProps(nil) // without column mapping a real change would be an error
	for name, ct := range map[string]change.Kind{"rename": change.RenameColumn, "drop": change.DropColumn} {
		t.Run(name, func(t *testing.T) {
			assert.PanicsWithValue(t,
				fmt.Sprintf("preflight: ColumnMappingChecker cannot check preflight_test.foreignChange (type %d): add it to columnMappingOps and Check", int(ct)),
				func() { (&preflight.ColumnMappingChecker{}).Check(context.Background(), foreignChange{ct: ct}, m) })
		})
	}
	// A type outside the table panics too.
	assert.Panics(t, func() {
		(&preflight.ColumnMappingChecker{}).Check(context.Background(), foreignChange{ct: change.AddColumn}, m)
	})
}

// countingBackend counts GetTableProperties calls per table.
type countingBackend struct {
	backendtest.MockUCBackend
	calls map[string]int
}

func (c *countingBackend) GetTableProperties(ctx context.Context, cat, sch, tbl string) (map[string]string, error) {
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[sch+"."+tbl]++
	return c.MockUCBackend.GetTableProperties(ctx, cat, sch, tbl)
}

// Several changes of one table read its properties once per Run; each other table once more.
func TestRun_readsTablePropertiesOncePerTable(t *testing.T) {
	other := change.TablePath{Catalog: "prod", Schema: "silver", Name: litUsers}
	drop := func(p change.TablePath, col string) change.Change {
		return &change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: col}
	}
	plan := []change.Change{
		drop(testTablePath, "a"), drop(testTablePath, "b"), drop(testTablePath, "c"),
		&change.RenameColumnChange{Base: change.Base{Table: testTablePath}, OldName: "x", NewName: "y"},
		drop(other, "a"), drop(other, "b"),
	}
	b := &countingBackend{}
	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))

	assert.Equal(t, map[string]int{"silver.orders": 1, "silver.users": 1}, b.calls)
	assert.Len(t, diags, 6, "every change still gets its own diagnostic: no column mapping anywhere")

	// A second Run starts with an empty cache.
	preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))
	assert.Equal(t, map[string]int{"silver.orders": 2, "silver.users": 2}, b.calls)
}

// A failed read is remembered too: one GET, one error per change.
func TestRun_readErrorIsCachedPerTable(t *testing.T) {
	plan := []change.Change{dropChange(), &change.DropColumnChange{Base: change.Base{Table: testTablePath}, ColumnName: "other"}}
	b := &countingErrBackend{}
	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))
	assert.Equal(t, 1, b.n)
	assert.Len(t, diags, 2)
}

type countingErrBackend struct {
	backendtest.MockUCBackend
	n int
}

func (c *countingErrBackend) GetTableProperties(_ context.Context, _, _, _ string) (map[string]string, error) {
	c.n++
	return nil, errors.New("boom")
}
