package differ_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }

func makeTable(name, schema string) config.Table {
	return config.Table{
		Name:            name,
		Schema:          schema,
		ResolvedCatalog: litProd,
		ResolvedSchema:  schema,
	}
}

// --- 7.1 Column tests ---

func TestDiff_addColumn(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: litDiscount, Type: typDOUBLE}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())
	require.Len(t, changes, 1)
	assert.Equal(t, change.AddColumn, changes[0].Type())
	assert.Equal(t, "ALTER TABLE `prod`.`silver`.`orders` ADD COLUMN `discount` DOUBLE", changes[0].SQL())
}

func TestDiff_addColumn_withComment(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "discount", Type: "DOUBLE", Comment: "test"}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].SQL(), "COMMENT 'test'")
}

// a new NOT NULL column of an existing table is a planning error (no ADD COLUMN ... NOT NULL).
func TestDiff_addColumn_notNull_isError(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "discount", Type: "DOUBLE", Nullable: boolPtr(false)}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	require.True(t, diags.HasError())
	assert.Equal(t, `table "silver.orders", column "discount": a new NOT NULL column cannot be added to an existing table (not supported in v1): add it as nullable, fill it with data, then set nullable: false`, diags[0].Summary)
}

// Nullable (explicit or by default) new columns are added without NOT NULL; all errors are reported, in column order.
func TestDiff_addColumn_nullableVariantsAndAllErrors(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "a", Type: typINT, Nullable: boolPtr(false)},
		{Name: "b", Type: typINT, Nullable: boolPtr(true)},
		{Name: "c", Type: typINT},
		{Name: "d", Type: typINT, Nullable: boolPtr(false), Comment: "x"},
	}
	live := makeTable("orders", "silver")
	live.Columns = []config.Column{}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	require.Len(t, diags, 2)
	assert.Contains(t, diags[0].Summary, `column "a"`)
	assert.Contains(t, diags[1].Summary, `column "d"`)

	desired.Columns = desired.Columns[1:3]
	changes, diags = differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())
	require.Len(t, changes, 2)
	for _, ch := range changes {
		assert.NotContains(t, ch.SQL(), "NOT NULL")
	}
}

// Renamed column plus nullable change is not a new column: AlterNullable as before.
func TestDiff_renameColumn_withNotNull_notAnError(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: litFullName, Type: typSTRING, RenameFrom: litUserName, Nullable: boolPtr(false)}}
	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: litUserName, Type: typSTRING}}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())
	require.Len(t, changes, 2)
	assert.Equal(t, change.RenameColumn, changes[0].Type())
	assert.Contains(t, changes[1].SQL(), "ALTER COLUMN `full_name` SET NOT NULL")
}

// A table being created (CREATE with NOT NULL) or dropped is not checked.
func TestDiff_notNullColumn_createAndDrop_noError(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT, Nullable: boolPtr(false)}}
	_, diags := differ.Diff(desired, nil)
	assert.False(t, diags.HasError())

	desired.Drop = true
	live := makeTable("orders", "silver")
	_, diags = differ.Diff(desired, backendtest.LiveOf(live))
	assert.False(t, diags.HasError())
}

func TestDiff_dropColumn(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "old_col", Type: typSTRING}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.DropColumn, changes[0].Type())
	assert.True(t, changes[0].IsDestructive())
	assert.Equal(t, "ALTER TABLE `prod`.`silver`.`orders` DROP COLUMN `old_col`", changes[0].SQL())
}

func TestDiff_renameColumn_hintMatch(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "full_name", Type: typSTRING, RenameFrom: "user_name"}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "user_name", Type: typSTRING}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.RenameColumn, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "RENAME COLUMN `user_name` TO `full_name`")
}

func TestDiff_renameColumn_hintNoMatch(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "full_name", Type: typSTRING, RenameFrom: "ghost"}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	require.True(t, diags.HasError())
	assert.Contains(t, fmt.Sprint(diags), `rename_from "ghost" not found`)
}

func TestDiff_alterType(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "status", Type: typSTRING}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "status", Type: "VARCHAR(100)"}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterColumn, changes[0].Type())
	assert.Equal(t, "ALTER TABLE `prod`.`silver`.`orders` ALTER COLUMN `status` TYPE STRING", changes[0].SQL())
}

func TestDiff_alterComment(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT, Comment: "new comment"}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "id", Type: typBIGINT, Comment: "old comment"}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterColumn, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "COMMENT 'new comment'")
}

func TestDiff_alterNullable_toNotNull(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT, Nullable: boolPtr(false)}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "id", Type: typBIGINT, Nullable: nil}} // nil = nullable

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterColumn, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "SET NOT NULL")
}

func TestDiff_alterNullable_toNullable(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT, Nullable: nil}} // nil = nullable

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "id", Type: typBIGINT, Nullable: boolPtr(false)}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterColumn, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "DROP NOT NULL")
}

func TestDiff_columnOrderChange(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "b", Type: typSTRING},
		{Name: "a", Type: typBIGINT},
	}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{
		{Name: "a", Type: typBIGINT},
		{Name: "b", Type: typSTRING},
	}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	assert.False(t, diags.HasError())
}

func TestDiff_noChange_identical(t *testing.T) {
	nullable := boolPtr(true)
	col := config.Column{Name: "id", Type: typBIGINT, Nullable: nullable}

	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{col}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{col}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	assert.False(t, diags.HasError())
}

// --- 7.2 tblproperties tests ---

func TestDiff_tblprops_newKey(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{propChangeDataFeed: strPtr("true")}

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

func TestDiff_tblprops_changedValue(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{"myProp": strPtr("new")}

	live := makeTable("orders", "silver")
	live.TblProperties = map[string]*string{"myProp": strPtr("old")}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

func TestDiff_tblprops_removedFromYAML(t *testing.T) {
	desired := makeTable("orders", "silver")
	// key not present in desired

	live := makeTable("orders", "silver")
	live.TblProperties = map[string]*string{"someKey": strPtr("val")}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
}

func TestDiff_tblprops_systemKeyClusterByAuto(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{litClusterByAuto: strPtr("true")}

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
}

func TestDiff_tblprops_deltaKey_inYAML(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{"delta.enableChangeDataFeed": strPtr("true")}

	live := makeTable("orders", "silver")
	// delta key not in live

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

// --- 7.3 Clustering tests ---

func TestDiff_clustering_noneToExplicit(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Clustering = &config.Clustering{Columns: []string{"a", "b"}}

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterClustering, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "CLUSTER BY (`a`, `b`)")
}

func TestDiff_clustering_explicitToNone(t *testing.T) {
	desired := makeTable("orders", "silver")
	// no clustering

	live := makeTable("orders", "silver")
	live.Clustering = &config.Clustering{Columns: []string{"a", "b"}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterClustering, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "CLUSTER BY NONE")
}

func TestDiff_clustering_autoToAuto(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Clustering = &config.Clustering{Auto: true}

	live := makeTable("orders", "silver")
	live.Clustering = &config.Clustering{Auto: true}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
}

func TestDiff_clustering_noAutoToAuto(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Clustering = &config.Clustering{Auto: true}

	live := makeTable("orders", "silver")
	live.Clustering = &config.Clustering{Columns: []string{"x"}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].SQL(), "CLUSTER BY AUTO")
}

func TestDiff_clustering_autoToExplicit(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Clustering = &config.Clustering{Columns: []string{"a", "b"}}

	live := makeTable("orders", "silver")
	live.Clustering = &config.Clustering{Auto: true}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterClustering, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "CLUSTER BY (`a`, `b`)")
}

func TestDiff_clustering_sameColumns(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Clustering = &config.Clustering{Columns: []string{"b", "a"}} // different order

	live := makeTable("orders", "silver")
	live.Clustering = &config.Clustering{Columns: []string{"a", "b"}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes) // order-insensitive
}

func TestDiff_clustering_differentColumns(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Clustering = &config.Clustering{Columns: []string{"a", "b"}}

	live := makeTable("orders", "silver")
	live.Clustering = &config.Clustering{Columns: []string{"c", "d"}}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.AlterClustering, changes[0].Type())
}

// --- 7.4 Special cases ---

func TestDiff_dropTrue(t *testing.T) {
	desired := makeTable("legacy", "silver")
	desired.Drop = true

	live := makeTable("legacy", "silver")

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.DropTable, changes[0].Type())
	assert.True(t, changes[0].IsDestructive())
	assert.False(t, diags.HasError())
}

func TestDiff_liveNil_createTable(t *testing.T) {
	desired := makeTable("new_table", "silver")
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT}}

	changes, _ := differ.Diff(desired, nil)
	require.Len(t, changes, 1)
	assert.Equal(t, change.CreateTable, changes[0].Type())
	assert.Contains(t, changes[0].SQL(), "CREATE TABLE")
}

func TestDiff_createTable_SQL_columns(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "id", Type: typBIGINT},
		{Name: litName, Type: typSTRING},
	}

	changes, _ := differ.Diff(desired, nil)
	require.Len(t, changes, 1)
	sql := changes[0].SQL()
	assert.Contains(t, sql, "`id` BIGINT")
	assert.Contains(t, sql, "`name` STRING")
}

func TestDiff_createTable_SQL_notNull(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "id", Type: typBIGINT, Nullable: boolPtr(false)},
	}

	changes, _ := differ.Diff(desired, nil)
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].SQL(), "NOT NULL")
}

func TestDiff_createTable_SQL_tblprops(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{"delta.enableChangeDataFeed": strPtr("true")}

	changes, _ := differ.Diff(desired, nil)
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].SQL(), "TBLPROPERTIES")
}

func TestDiff_renameTable(t *testing.T) {
	desired := makeTable("new_name", "silver")
	desired.RenameFrom = "old_name"

	live := makeTable("old_name", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.NotEmpty(t, changes)
	assert.Equal(t, change.RenameTable, changes[0].Type())
	assert.Equal(t, "ALTER TABLE `prod`.`silver`.`old_name` RENAME TO `prod`.`silver`.`new_name`", changes[0].SQL())
}

func TestDiff_partitioningChange(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Partitioning = &config.Partitioning{Columns: []string{"date"}}

	live := makeTable("orders", "silver")
	live.Partitioning = &config.Partitioning{Columns: []string{"other"}}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	require.True(t, len(diags) > 0, "expected at least one diagnostic")
	assert.False(t, diags.HasError(), "partitioning change should produce Warning, not Error")
}

// --- 7.5 DiffSchema tests ---

func TestDiffSchema_unmanagedTable(t *testing.T) {
	desired := []config.Table{makeTable("managed", "silver")}

	unmanaged := makeTable("unmanaged", "silver")
	live := []*backend.LiveTable{backendtest.LiveOf(unmanaged)}

	changes, _ := differ.DiffSchema(desired, live)
	// unmanaged is in live but not desired → CreateTable for "managed" only
	for _, c := range changes {
		assert.NotEqual(t, "unmanaged", c.Describe()) // no change targets unmanaged
	}
}

func TestDiffSchema_multipleTablesCollected(t *testing.T) {
	t1 := makeTable("t1", "silver")
	t1.Columns = []config.Column{{Name: "a", Type: typBIGINT}}
	t2 := makeTable("t2", "silver")
	t2.Columns = []config.Column{{Name: "b", Type: typSTRING}}

	desired := []config.Table{t1, t2}
	live := []*backend.LiveTable{} // both tables missing → CreateTable for each

	changes, _ := differ.DiffSchema(desired, live)
	assert.Len(t, changes, 2)
	for _, c := range changes {
		assert.Equal(t, change.CreateTable, c.Type())
	}
}

// --- 7.6 SQL purity tests ---

func TestSQL_addColumn_pure(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "x", Type: typINT}}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	sql1 := changes[0].SQL()
	sql2 := changes[0].SQL()
	assert.Equal(t, sql1, sql2)
}

// --- drop idempotency ---

// TestDiff_DropTrue_TableNotInUC verifies that when drop:true is set but the table
// is already gone from UC (live=nil), no changes are produced but a warning is emitted.
func TestDiff_DropTrue_TableNotInUC(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Drop = true

	// live = nil means table is already gone
	changes, diags := differ.Diff(desired, nil)

	assert.Empty(t, changes, "no changes expected when drop:true but table already gone")
	assert.False(t, diags.HasError(), "must not be an error")
	require.True(t, len(diags) > 0, "expected at least one warning")

	hasDropWarning := false
	for _, d := range diags {
		if strings.Contains(d.Summary, "drop: true") && strings.Contains(d.Summary, "not in UC") {
			hasDropWarning = true
		}
	}
	assert.True(t, hasDropWarning, "warning must mention 'drop: true' and 'not in UC', diags: %v", diags)
}

// TestDiff_DropTrue_TableExists verifies that when drop:true and the table exists in UC,
// a DropTableChange is produced (existing behavior preserved).
func TestDiff_DropTrue_TableExists(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Drop = true

	live := makeTable("orders", "silver")

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))

	require.Len(t, changes, 1, "one DropTableChange expected")
	assert.Equal(t, change.DropTable, changes[0].Type())
	assert.False(t, diags.HasError())
}

// TestDiffSchema_DropTrue_Idempotent verifies drop idempotency via DiffSchema:
// when table has drop:true but is already gone from live, result is 0 changes + warning.
func TestDiffSchema_DropTrue_Idempotent(t *testing.T) {
	desired := []config.Table{
		{Name: litOrders, Drop: true, Schema: litSilver, ResolvedCatalog: litProd, ResolvedSchema: litSilver},
	}
	live := []*backend.LiveTable{} // table already gone

	changes, diags := differ.DiffSchema(desired, live)

	assert.Empty(t, changes, "no changes expected when drop:true and table already absent")
	require.True(t, len(diags) > 0, "expected at least one warning diagnostic")

	hasDropWarning := false
	for _, d := range diags {
		if strings.Contains(d.Summary, "drop: true") || strings.Contains(d.Summary, "not in UC") {
			hasDropWarning = true
		}
	}
	assert.True(t, hasDropWarning, "warning must mention drop:true idempotency, diags: %v", diags)
}

// --- table rename via DiffSchema ---

// TestDiffSchema_RenameTable verifies that a table with rename_from produces a RenameTableChange
// (not a CreateTableChange) when the old table exists in live.
func TestDiffSchema_RenameTable(t *testing.T) {
	desired := []config.Table{
		{
			Name:            litOrdersV2,
			RenameFrom:      litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litProd,
			ResolvedSchema:  litSilver,
		},
	}
	oldTable := makeTable("orders", "silver")
	live := []*backend.LiveTable{backendtest.LiveOf(oldTable)}

	changes, _ := differ.DiffSchema(desired, live)

	require.NotEmpty(t, changes, "expected at least one change")
	found := false
	for _, c := range changes {
		if c.Type() == change.RenameTable {
			found = true
			sql := c.SQL()
			assert.True(t,
				strings.Contains(sql, "RENAME") || strings.Contains(sql, "ALTER TABLE"),
				"SQL must contain RENAME or ALTER TABLE, got: %s", sql,
			)
		}
	}
	assert.True(t, found, "expected a RenameTableChange, got: %v", changes)

	// Must NOT produce a CreateTableChange for orders_v2
	for _, c := range changes {
		assert.NotEqual(t, change.CreateTable, c.Type(),
			"must not emit CreateTableChange when rename_from matches live table")
	}
}

// TestDiffSchema_RenameTable_SourceGone_Warning verifies that when the source table
// (rename_from) is already gone and the destination (new name) exists, no changes are
// produced but a warning is emitted suggesting to remove rename_from.
func TestDiffSchema_RenameTable_SourceGone_Warning(t *testing.T) {
	desired := []config.Table{
		{
			Name:            "orders_v2",
			RenameFrom:      litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litProd,
			ResolvedSchema:  litSilver,
		},
	}
	// Already renamed: orders_v2 exists, orders is gone
	existing := makeTable("orders_v2", "silver")
	live := []*backend.LiveTable{backendtest.LiveOf(existing)}

	changes, diags := differ.DiffSchema(desired, live)

	assert.Empty(t, changes, "no changes expected when rename already completed")
	require.True(t, len(diags) > 0, "expected at least one warning")

	hasRenameWarning := false
	for _, d := range diags {
		if strings.Contains(d.Summary, "orders_v2") && strings.Contains(d.Summary, "rename_from") {
			hasRenameWarning = true
		}
	}
	assert.True(t, hasRenameWarning,
		`warning must contain "orders_v2" and "rename_from", diags: %v`, diags)
}

// --- warning message content ---

// TestDiff_ColumnRenameFrom_Warning_NewName verifies that when a column's rename_from
// source is already gone (old name absent, new name present), the warning mentions the
// NEW column name (not just the old name) and mentions "rename_from".
func TestDiff_ColumnRenameFrom_Warning_NewName(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "name1", Type: typSTRING, RenameFrom: litName},
	}

	live := makeTable("orders", "silver")
	live.Columns = []config.Column{
		{Name: "name1", Type: typSTRING}, // already renamed; old name "name" is gone
	}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))

	assert.Empty(t, changes, "no changes expected when rename already completed")
	require.True(t, len(diags) > 0, "expected at least one warning")

	hasCorrectWarning := false
	for _, d := range diags {
		if strings.Contains(d.Summary, "name1") && strings.Contains(d.Summary, "rename_from") {
			hasCorrectWarning = true
		}
	}
	assert.True(t, hasCorrectWarning,
		`warning must contain new column name "name1" and "rename_from"; diags: %v`, diags)

	// Warning must NOT only mention the old name without the new name
	for _, d := range diags {
		if strings.Contains(d.Summary, "rename_from") {
			assert.Contains(t, d.Summary, "name1",
				`warning about rename_from must mention the new column name "name1"`)
		}
	}
}

// TestDiff_TableRenameFrom_Warning_NewName verifies the same at table level via DiffSchema:
// when rename already completed, warning mentions the new table name.
func TestDiff_TableRenameFrom_Warning_NewName(t *testing.T) {
	desired := []config.Table{
		{
			Name:            "orders_v2",
			RenameFrom:      litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litProd,
			ResolvedSchema:  litSilver,
		},
	}
	existing := makeTable("orders_v2", "silver")
	live := []*backend.LiveTable{backendtest.LiveOf(existing)}

	_, diags := differ.DiffSchema(desired, live)

	require.True(t, len(diags) > 0, "expected at least one warning")

	hasNewNameInWarning := false
	for _, d := range diags {
		if strings.Contains(d.Summary, "orders_v2") && strings.Contains(d.Summary, "rename_from") {
			hasNewNameInWarning = true
		}
	}
	assert.True(t, hasNewNameInWarning,
		`warning must contain new table name "orders_v2" and "rename_from"; diags: %v`, diags)
}

// --- tblproperties idempotency and UNSET ---

// TestDiff_TblProps_DeltaKey_Idempotency verifies that when a delta.* tblproperty
// has the same value in both desired and live, no change is produced.
func TestDiff_TblProps_DeltaKey_Idempotency(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{propColumnMapping: strPtr("name")}

	live := makeTable("orders", "silver")
	live.TblProperties = map[string]*string{propColumnMapping: strPtr("name")}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes, "0 changes expected: delta key with same value must be idempotent")
	assert.False(t, diags.HasError(), "no errors expected for idempotent diff")
}

// TestDiff_TblProps_DeltaKey_ValueChanged verifies that when a delta.* key has a
// different value in desired vs live, a SetTBLPropertiesChange is produced.
func TestDiff_TblProps_DeltaKey_ValueChanged(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{propColumnMapping: strPtr("name")}

	live := makeTable("orders", "silver")
	live.TblProperties = map[string]*string{propColumnMapping: strPtr("id")}

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

// TestDiff_TblProps_DeltaKey_NotInLive verifies that a delta.* key present in desired
// but absent from live produces a SetTBLPropertiesChange.
func TestDiff_TblProps_DeltaKey_NotInLive(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{propColumnMapping: strPtr("name")}

	live := makeTable("orders", "silver")
	// live.TblProperties is empty

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1, "SET change expected when delta key is absent from live")
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

// TestDiff_TblProps_SparkKey_NotSystemKey verifies that spark.* keys in desired
// are included in the diff (systemTblPropKey no longer filters spark.*).
func TestDiff_TblProps_SparkKey_NotSystemKey(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{"spark.sql.sources.provider": strPtr("delta")}

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1, "spark.* keys must now be included in diff after the fix")
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

// TestDiff_TblProps_ClusterByAuto_StillFiltered verifies that clusterByAuto remains
// in the systemTblPropKey list and produces no changes when present in desired.
func TestDiff_TblProps_ClusterByAuto_StillFiltered(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{"clusterByAuto": strPtr("true")}

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes, "clusterByAuto must still be filtered by systemTblPropKey")
}

// TestDiff_TblProps_UNSET_SQL verifies the SQL for UnsetTblPropertiesChange.
func TestDiff_TblProps_UNSET_SQL(t *testing.T) {
	path := change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}
	unset := &change.UnsetTBLPropertiesChange{
		Base: change.Base{Table: path},
		Keys: []string{"myKey"},
	}

	sql := unset.SQL()
	assert.Contains(t, sql, "UNSET TBLPROPERTIES",
		"SQL must contain UNSET TBLPROPERTIES")
	assert.Contains(t, sql, "'myKey'",
		"SQL must contain quoted key name")
	assert.Contains(t, sql, "`prod`.`silver`.`orders`",
		"SQL must contain fully-qualified table name")
}

// TestDiff_TblProps_UNSET_SQL_sorted verifies that UNSET SQL sorts keys.
func TestDiff_TblProps_UNSET_SQL_sorted(t *testing.T) {
	path := change.TablePath{Catalog: "prod", Schema: "silver", Name: litOrders}
	unset := &change.UnsetTBLPropertiesChange{
		Base: change.Base{Table: path},
		Keys: []string{"zzz", "aaa", "mmm"},
	}

	sql := unset.SQL()
	aaa := strings.Index(sql, "'aaa'")
	mmm := strings.Index(sql, "'mmm'")
	zzz := strings.Index(sql, "'zzz'")
	assert.True(t, aaa < mmm && mmm < zzz,
		"UNSET SQL keys must be sorted alphabetically; got: %s", sql)
}

// TestDiff_TblProps_UNSET_NotDestructive verifies that UNSET change is not destructive.
func TestDiff_TblProps_UNSET_NotDestructive(t *testing.T) {
	path := change.TablePath{Catalog: "prod", Schema: "silver", Name: "orders"}
	unset := &change.UnsetTBLPropertiesChange{
		Base: change.Base{Table: path},
		Keys: []string{"k"},
	}
	assert.False(t, unset.IsDestructive())
	assert.Equal(t, "unset 1 tblproperties", unset.Describe())
}

// TestDiffSchema_TblProps_DeltaKey_Idempotency verifies the same idempotency
// invariant through DiffSchema (the contract must hold end-to-end).
func TestDiffSchema_TblProps_DeltaKey_Idempotency(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{"delta.columnMapping.mode": strPtr("name")}
	desiredList := []config.Table{desired}

	live := makeTable("orders", "silver")
	live.TblProperties = map[string]*string{"delta.columnMapping.mode": strPtr("name")}
	liveList := []*backend.LiveTable{backendtest.LiveOf(live)}

	changes, diags := differ.DiffSchema(desiredList, liveList)
	assert.Empty(t, changes,
		"DiffSchema: 0 changes expected when delta key matches in both desired and live")
	assert.False(t, diags.HasError())
}

// TestDiff_TblProps_PostUnset_ZeroChanges verifies the post-apply state:
// after UNSET is applied (key removed from live), a subsequent diff produces 0 changes.
func TestDiff_TblProps_PostUnset_ZeroChanges(t *testing.T) {
	// Desired no longer has the key (UNSET was applied, manifest updated)
	desired := makeTable("orders", "silver")

	// Live no longer has the key
	live := makeTable("orders", "silver")

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes,
		"0 changes expected after key was removed from both desired and live")
	assert.False(t, diags.HasError())
}

// compile-time check that UnsetTBLPropertiesChange implements change.Change.
var _ change.Change = (*change.UnsetTBLPropertiesChange)(nil)

// --- Table comment tracking ---

// TestDiff_TableComment_AddComment verifies that when desired has a table comment
// but live does not, a change is produced that sets the comment.
func TestDiff_TableComment_AddComment(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Comment = litHello

	live := makeTable("orders", "silver")
	// live.Comment is empty (no comment)

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())

	// Expect a change that targets the table comment.
	found := false
	for _, ch := range changes {
		sql := ch.SQL()
		if strings.Contains(sql, "COMMENT") && strings.Contains(sql, "hello") {
			found = true
		}
	}
	assert.True(t, found,
		"Diff must produce a change to set table comment 'hello', got changes: %v", changes)
}

// TestDiff_TableComment_RemoveComment verifies that when desired has no comment but live
// has one, a change is produced that clears the comment (NULL).
func TestDiff_TableComment_RemoveComment(t *testing.T) {
	desired := makeTable("orders", "silver")
	// desired.Comment is empty

	live := makeTable("orders", "silver")
	live.Comment = "old comment"

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())

	// Expect a change that resets the table comment to NULL.
	found := false
	for _, ch := range changes {
		sql := ch.SQL()
		if strings.Contains(sql, "COMMENT") && strings.Contains(strings.ToUpper(sql), "NULL") {
			found = true
		}
	}
	assert.True(t, found,
		"Diff must produce a change to clear table comment (set to NULL), got changes: %v", changes)
}

// TestDiff_TableComment_SameComment_NoChange verifies idempotency:
// when desired and live have the same comment, 0 changes are produced.
func TestDiff_TableComment_SameComment_NoChange(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Comment = litHello

	live := makeTable("orders", "silver")
	live.Comment = "hello"

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())

	// No comment-related change expected.
	for _, ch := range changes {
		sql := ch.SQL()
		assert.NotContains(t, sql, "COMMENT ON TABLE",
			"no COMMENT ON TABLE change expected when comments match; changes: %v", changes)
	}
	// Whole diff should be empty (no other differences either)
	assert.Empty(t, changes,
		"Diff must produce 0 changes when desired and live comments are identical")
}

// TestDiff_TableComment_AddComment_SQL verifies that the SQL for adding a table comment
// uses the correct syntax: COMMENT ON TABLE <table> IS '<comment>'.
func TestDiff_TableComment_AddComment_SQL(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Comment = "hi"

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.NotEmpty(t, changes, "expected at least one change for table comment")

	var commentSQL string
	for _, ch := range changes {
		sql := ch.SQL()
		if strings.Contains(sql, "COMMENT") && strings.Contains(sql, "hi") {
			commentSQL = sql
		}
	}
	require.NotEmpty(t, commentSQL, "expected a comment-setting SQL statement")
	assert.Contains(t, commentSQL, "COMMENT ON TABLE `prod`.`silver`.`orders` IS 'hi'",
		"SQL for setting comment must use COMMENT ON TABLE syntax")
}

// TestDiff_TableComment_RemoveComment_SQL verifies that removing a comment uses
// COMMENT ON TABLE <table> IS NULL.
func TestDiff_TableComment_RemoveComment_SQL(t *testing.T) {
	desired := makeTable("orders", "silver")
	// no comment

	live := makeTable("orders", "silver")
	live.Comment = litOld

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.NotEmpty(t, changes, "expected at least one change for comment removal")

	var commentSQL string
	for _, ch := range changes {
		sql := ch.SQL()
		if strings.Contains(sql, "COMMENT") && strings.Contains(strings.ToUpper(sql), "NULL") {
			commentSQL = sql
		}
	}
	require.NotEmpty(t, commentSQL, "expected a comment-clearing SQL statement")
	assert.Contains(t, commentSQL, "COMMENT ON TABLE `prod`.`silver`.`orders` IS NULL",
		"SQL for clearing comment must use COMMENT ON TABLE ... IS NULL")
}

// TestDiffSchema_TableComment_AddComment verifies the same invariant through DiffSchema.
func TestDiffSchema_TableComment_AddComment(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Comment = "hello"

	live := makeTable("orders", "silver")
	// no comment

	changes, diags := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	require.False(t, diags.HasError())

	found := false
	for _, ch := range changes {
		if strings.Contains(ch.SQL(), "COMMENT") && strings.Contains(ch.SQL(), "hello") {
			found = true
		}
	}
	assert.True(t, found,
		"DiffSchema must produce a comment-setting change when desired has a comment, got: %v", changes)
}

// --- Adversarial inputs via Diff ---

// TestDiff_TblProps_ApostropheInKey verifies that when a tblproperties key contains
// an apostrophe, the resulting SQL is not broken.
func TestDiff_TblProps_ApostropheInKey(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{
		"key'with'apostrophe": strPtr("value"),
	}

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	sql := changes[0].SQL()
	// The raw apostrophe would break SQL: 'key'with'apostrophe'='value'
	assert.NotContains(t, sql, "'key'with'",
		"apostrophe in tblproperty key must be escaped in SQL; got: %s", sql)
}

// TestDiff_TblProps_ApostropheInValue verifies that apostrophes in tblproperties values
// are properly escaped in the resulting SQL.
func TestDiff_TblProps_ApostropheInValue(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = map[string]*string{
		"myKey": strPtr("O'Brien's value"),
	}

	live := makeTable("orders", "silver")

	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	require.Len(t, changes, 1)
	sql := changes[0].SQL()
	// 'O'Brien's value' is broken SQL
	assert.NotContains(t, sql, "'O'Brien",
		"apostrophe in tblproperty value must be escaped in SQL; got: %s", sql)
}

// --- table rename_from warnings via DiffSchema ---

func renameDesired(name, from string) config.Table {
	t := makeTable(name, "silver")
	t.RenameFrom = from
	t.SourceFile = "tables/silver.yml"
	return t
}

func warningSummaries(diags []diag.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == diag.Warning {
			out = append(out, d.Summary)
		}
	}
	return out
}

func renameWarnings(diags []diag.Diagnostic) []string {
	var out []string
	for _, s := range warningSummaries(diags) {
		if strings.Contains(s, "rename_from") {
			out = append(out, s)
		}
	}
	return out
}

func countType(changes []change.Change, ct change.Kind) int {
	n := 0
	for _, c := range changes {
		if c.Type() == ct {
			n++
		}
	}
	return n
}

func TestDiffSchema_RenameFrom_OldLiveNewAbsent_NoWarning(t *testing.T) {
	old := makeTable("orders", "silver")
	changes, diags := differ.DiffSchema([]config.Table{renameDesired("orders_v2", "orders")}, []*backend.LiveTable{backendtest.LiveOf(old)})

	assert.Equal(t, 1, countType(changes, change.RenameTable))
	assert.Empty(t, diags, "no warning expected when rename is about to happen")
}

func TestDiffSchema_RenameFrom_OldAbsentNewLive_CanBeRemoved(t *testing.T) {
	nw := makeTable("orders_v2", "silver")
	changes, diags := differ.DiffSchema([]config.Table{renameDesired("orders_v2", "orders")}, []*backend.LiveTable{backendtest.LiveOf(nw)})

	assert.Empty(t, changes)
	assert.Equal(t, []string{`table "silver.orders_v2": rename_from "orders" can be removed after apply`}, warningSummaries(diags))
	require.Len(t, diags, 1)
	assert.Equal(t, "tables/silver.yml", diags[0].Location)
}

func TestDiffSchema_RenameFrom_BothAbsent_Warning(t *testing.T) {
	changes, diags := differ.DiffSchema([]config.Table{renameDesired("orders_v2", "orders")}, nil)

	assert.Equal(t, 1, countType(changes, change.CreateTable))
	assert.Equal(t, 0, countType(changes, change.RenameTable))
	assert.Equal(t,
		[]string{`table "silver.orders_v2": rename_from "orders" not found in UC schema - a new table will be created`},
		warningSummaries(diags))
	for _, d := range diags {
		assert.NotEqual(t, diag.Error, d.Severity)
		assert.NotContains(t, d.Summary, "can be removed")
		assert.Equal(t, "tables/silver.yml", d.Location)
	}
}

func TestDiffSchema_RenameFrom_BothLive_IgnoredWarning(t *testing.T) {
	old := makeTable("orders", "silver")
	nw := makeTable("orders_v2", "silver")
	d := renameDesired("orders_v2", "orders")
	d.Columns = []config.Column{{Name: "id", Type: typBIGINT}}

	changes, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(old), backendtest.LiveOf(nw)})

	assert.Equal(t, 0, countType(changes, change.RenameTable))
	assert.Equal(t, 1, countType(changes, change.AddColumn), "diff must be against the new live table")
	assert.Equal(t,
		[]string{`table "silver.orders_v2": rename_from "orders" ignored: both "orders" and "orders_v2" exist in UC`},
		warningSummaries(diags))
	for _, dg := range diags {
		assert.Equal(t, "tables/silver.yml", dg.Location)
	}
}

func TestDiffSchema_RenameFrom_Suppressed(t *testing.T) {
	old := makeTable("orders", "silver")
	nw := makeTable("orders_v2", "silver")

	t.Run("drop true", func(t *testing.T) {
		d := renameDesired("orders_v2", "orders")
		d.Drop = true
		_, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(old), backendtest.LiveOf(nw)})
		assert.Empty(t, renameWarnings(diags))
		_, diags = differ.DiffSchema([]config.Table{d}, nil)
		assert.Empty(t, renameWarnings(diags))
	})
	t.Run("rename_from equals name", func(t *testing.T) {
		d := renameDesired("orders", "orders")
		_, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(old)})
		assert.Empty(t, renameWarnings(diags))
		_, diags = differ.DiffSchema([]config.Table{d}, nil)
		assert.Empty(t, renameWarnings(diags))
	})
}

func TestDiffSchema_RenameFrom_OnlyOneOfSeveralTables(t *testing.T) {
	a := makeTable("a", "silver")
	b := makeTable("b", "silver")
	desired := []config.Table{makeTable("a", "silver"), renameDesired("c", "b"), makeTable("d", "silver")}

	changes, diags := differ.DiffSchema(desired, []*backend.LiveTable{backendtest.LiveOf(a), backendtest.LiveOf(b)})

	assert.Equal(t, 1, countType(changes, change.RenameTable))
	assert.Equal(t, 1, countType(changes, change.CreateTable), "only d is created")
	assert.Empty(t, warningSummaries(diags))
}

func TestDiffSchema_RenameFrom_ApplyThenRerun(t *testing.T) {
	desired := []config.Table{renameDesired("orders_v2", "orders")}

	before := makeTable("orders", "silver")
	changes, diags := differ.DiffSchema(desired, []*backend.LiveTable{backendtest.LiveOf(before)})
	require.Equal(t, 1, countType(changes, change.RenameTable))
	assert.Empty(t, diags)

	after := makeTable("orders_v2", "silver")
	changes, diags = differ.DiffSchema(desired, []*backend.LiveTable{backendtest.LiveOf(after)})
	assert.Empty(t, changes)
	assert.Equal(t, []string{`table "silver.orders_v2": rename_from "orders" can be removed after apply`}, warningSummaries(diags))
}

// --- swap / chain behaviour documentation ---

func warnTexts(diags diag.Diagnostics) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == diag.Warning {
			out = append(out, d.Summary)
		}
	}
	return out
}

func changeTypes(changes []change.Change) []change.Kind {
	var out []change.Kind
	for _, c := range changes {
		out = append(out, c.Type())
	}
	return out
}

// S1: swap with both tables live - no rename, two "ignored" warnings.
func TestDiffSchema_Swap_BothLive(t *testing.T) {
	a, b := makeTable("a", "silver"), makeTable("b", "silver")
	desired := []config.Table{renameDesired("a", "b"), renameDesired("b", "a")}

	changes, diags := differ.DiffSchema(desired, []*backend.LiveTable{backendtest.LiveOf(a), backendtest.LiveOf(b)})

	assert.False(t, diags.HasError())
	for _, c := range changes {
		assert.NotEqual(t, change.RenameTable, c.Type())
	}
	assert.ElementsMatch(t, []string{
		`table "silver.a": rename_from "b" ignored: both "b" and "a" exist in UC`,
		`table "silver.b": rename_from "a" ignored: both "a" and "b" exist in UC`,
	}, warnTexts(diags))
}

// S3: chain b{from a}, c{from b} with only a live - rename a->b and create c.
func TestDiffSchema_Chain_OnlyALive(t *testing.T) {
	a := makeTable("a", "silver")
	desired := []config.Table{renameDesired("b", "a"), renameDesired("c", "b")}

	changes, diags := differ.DiffSchema(desired, []*backend.LiveTable{backendtest.LiveOf(a)})

	assert.False(t, diags.HasError())
	require.Len(t, changes, 2)
	rn, ok := changes[0].(*change.RenameTableChange)
	require.True(t, ok, "first change must be a rename, got %T", changes[0])
	assert.Equal(t, "a", rn.OldPath.Name)
	assert.Equal(t, "b", rn.NewName)
	cr, ok := changes[1].(*change.CreateTableChange)
	require.True(t, ok, "second change must be a create, got %T", changes[1])
	assert.Equal(t, "c", cr.Table.Name)
	assert.Equal(t, []string{
		`table "silver.c": rename_from "b" not found in UC schema - a new table will be created`,
	}, warnTexts(diags))
}

// S4: sequential rename (b already live, YAML now c{from b}) - plain rename, no warnings.
func TestDiffSchema_SequentialRename(t *testing.T) {
	b := makeTable("b", "silver")
	changes, diags := differ.DiffSchema([]config.Table{renameDesired("c", "b")}, []*backend.LiveTable{backendtest.LiveOf(b)})

	assert.Empty(t, diags)
	require.Len(t, changes, 1)
	rn, ok := changes[0].(*change.RenameTableChange)
	require.True(t, ok)
	assert.Equal(t, "b", rn.OldPath.Name)
	assert.Equal(t, "c", rn.NewName)
}

// S2 (documented quirk): chain with both a and b live. The differ emits a rename
// b->c while "b" is also diffed as a live table. ValidateLocal rejects this
// YAML in real runs; this test only pins the current DiffSchema output.
func TestDiffSchema_Chain_BothLive_Quirk(t *testing.T) {
	a, b := makeTable("a", "silver"), makeTable("b", "silver")
	desired := []config.Table{renameDesired("b", "a"), renameDesired("c", "b")}

	changes, diags := differ.DiffSchema(desired, []*backend.LiveTable{backendtest.LiveOf(a), backendtest.LiveOf(b)})

	assert.False(t, diags.HasError())
	assert.Equal(t, []string{
		`table "silver.b": rename_from "a" ignored: both "a" and "b" exist in UC`,
	}, warnTexts(diags))
	require.Len(t, changes, 1, "got types: %v", changeTypes(changes))
	rn, ok := changes[0].(*change.RenameTableChange)
	require.True(t, ok)
	assert.Equal(t, "b", rn.OldPath.Name)
	assert.Equal(t, "c", rn.NewName)
}

// --- deterministic tblproperties ordering ---

func manyProps(nilValued bool) map[string]*string {
	m := map[string]*string{}
	for _, k := range []string{"k.zeta", "k.alpha", "k.mid", "k.beta", "k.omega", "k.gamma", "k.delta", "k.1"} {
		if nilValued {
			m[k] = nil
		} else {
			m[k] = strPtr("v-" + k)
		}
	}
	return m
}

func TestDiff_UnsetTblProps_DeterministicOrder(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = manyProps(true)
	live := makeTable("orders", "silver")
	live.TblProperties = map[string]*string{}
	for k := range desired.TblProperties {
		live.TblProperties[k] = strPtr("x")
	}

	var firstSQL, firstDesc string
	for i := 0; i < 20; i++ {
		changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
		require.False(t, diags.HasError())
		require.Len(t, changes, 1)
		u, ok := changes[0].(*change.UnsetTBLPropertiesChange)
		require.True(t, ok)
		require.Len(t, u.Keys, 8)
		assert.True(t, sort.StringsAreSorted(u.Keys), "run %d: keys not sorted: %v", i, u.Keys)
		if i == 0 {
			firstSQL, firstDesc = u.SQL(), u.Describe()
			continue
		}
		assert.Equal(t, firstSQL, u.SQL())
		assert.Equal(t, firstDesc, u.Describe())
	}
}

func TestDiff_SetTblProps_SQLSortedOrder(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.TblProperties = manyProps(false)
	live := makeTable("orders", "silver")

	want := []string{"k.1", "k.alpha", "k.beta", "k.delta", "k.gamma", "k.mid", "k.omega", "k.zeta"}
	for i := 0; i < 20; i++ {
		changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
		require.Len(t, changes, 1)
		sql := changes[0].SQL()
		prev := -1
		for _, k := range want {
			pos := strings.Index(sql, "'"+k+"'=")
			require.GreaterOrEqual(t, pos, 0, "key %s missing in %s", k, sql)
			assert.Greater(t, pos, prev, "run %d: key %s out of order in %s", i, k, sql)
			prev = pos
		}
	}
}

// rename_from equal to the table name is not a rename (the validator warns "no rename will occur").
func TestDiff_tableRenameFromEqualsName_noRename(t *testing.T) {
	for _, rf := range []string{"orders", "Orders"} {
		t.Run(rf, func(t *testing.T) {
			desired := makeTable("orders", "silver")
			desired.RenameFrom = rf
			desired.Columns = []config.Column{{Name: "id", Type: typBIGINT}}
			live := makeTable("orders", "silver")
			live.Columns = []config.Column{{Name: "id", Type: typBIGINT}}

			changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
			require.False(t, diags.HasError())
			assert.Empty(t, changes)

			desired.Columns = append(desired.Columns, config.Column{Name: "extra", Type: typINT})
			changes, _ = differ.Diff(desired, backendtest.LiveOf(live))
			require.Len(t, changes, 1)
			assert.Equal(t, change.AddColumn, changes[0].Type())
		})
	}
}
