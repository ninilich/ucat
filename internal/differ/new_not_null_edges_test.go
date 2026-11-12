package differ_test

// edge cases not covered in differ_test.go: label after a table rename, Location,
// NOT NULL together with a column rename_from error, CREATE TABLE order, SQL of nullable flips.

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The table is renamed in the same plan: the label carries the new name; Location is the table file.
func TestDiff_newNotNullColumn_labelAfterTableRename(t *testing.T) {
	desired := makeTable("orders_new", "silver")
	desired.RenameFrom = litOrders
	desired.SourceFile = "tables/silver/orders_new.yml"
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT}, {Name: "a", Type: typINT, Nullable: boolPtr(false)}}
	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "id", Type: typBIGINT}}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Equal(t, `table "silver.orders_new", column "a": a new NOT NULL column cannot be added to an existing table (not supported in v1): add it as nullable, fill it with data, then set nullable: false`, errs[0])
	assert.Equal(t, "tables/silver/orders_new.yml", diags[0].Location)
}

// Fact (known behaviour): a column rename_from error returns earlier, the NOT NULL error is not visible.
func TestDiff_newNotNullColumn_withColumnRenameError_onlyRenameError(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "y", Type: typINT, RenameFrom: "x"}, // typo: neither x nor y in live
		{Name: "a", Type: typINT, Nullable: boolPtr(false)},
	}
	live := makeTable("orders", "silver")
	live.Columns = []config.Column{{Name: "id", Type: typBIGINT}}

	_, diags := differ.Diff(desired, backendtest.LiveOf(live))
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "rename_from")
	assert.NotContains(t, errs[0], "NOT NULL")
}

// Two new NOT NULL columns: both reported, in column order (YAML order, not alphabetical).
func TestDiff_newNotNullColumns_columnOrder(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "zz", Type: typINT, Nullable: boolPtr(false)},
		{Name: "aa", Type: typINT, Nullable: boolPtr(false)},
	}
	live := makeTable("orders", "silver")
	live.Columns = []config.Column{}
	for i := 0; i < 5; i++ {
		_, diags := differ.Diff(desired, backendtest.LiveOf(live))
		errs := summaries(diags, diag.Error)
		require.Len(t, errs, 2)
		assert.Contains(t, errs[0], `column "zz"`)
		assert.Contains(t, errs[1], `column "aa"`)
	}
}

// Existing column: nullable flips are SET/DROP NOT NULL; AddColumnChange never carries NOT NULL.
func TestDiff_nullableFlip_existingColumn(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{
		{Name: "a", Type: typINT, Nullable: boolPtr(false)},
		{Name: "b", Type: typINT, Nullable: boolPtr(true)},
	}
	live := makeTable("orders", "silver")
	live.Columns = []config.Column{
		{Name: "a", Type: typINT},
		{Name: "b", Type: typINT, Nullable: boolPtr(false)},
	}
	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())
	require.Len(t, changes, 2)
	assert.Contains(t, changes[0].SQL(), "ALTER COLUMN `a` SET NOT NULL")
	assert.Contains(t, changes[1].SQL(), "ALTER COLUMN `b` DROP NOT NULL")
}

func TestAddColumnChange_SQL_neverNotNull(t *testing.T) {
	for _, n := range []*bool{nil, boolPtr(true), boolPtr(false)} {
		ch := &change.AddColumnChange{Base: change.Base{Table: change.TablePath{Catalog: "c", Schema: "s", Name: "t"}}, Column: config.Column{Name: "a", Type: typINT, Comment: "x", Nullable: n}}
		assert.NotContains(t, ch.SQL(), "NOT NULL")
	}
}

// CREATE TABLE with nullable: false is allowed: NOT NULL, then COMMENT.
func TestDiff_createTable_notNullThenComment(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT, Nullable: boolPtr(false), Comment: "pk"}}
	changes, diags := differ.Diff(desired, nil)
	require.False(t, diags.HasError())
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].SQL(), "`id` BIGINT NOT NULL COMMENT 'pk'")
}
