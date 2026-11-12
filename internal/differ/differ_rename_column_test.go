package differ_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
)

// --- column rename conflicts in Step A ---

func col(name, rf string) config.Column {
	return config.Column{Name: name, Type: typSTRING, RenameFrom: rf}
}

func diffCols(desired, live []config.Column) ([]change.Change, diag.Diagnostics) {
	d := makeTable("orders", "silver")
	d.SourceFile = "orders.yaml"
	d.Columns = desired
	l := makeTable("orders", "silver")
	l.Columns = live
	return differ.Diff(d, backendtest.LiveOf(l))
}

func summaries(diags diag.Diagnostics, sev diag.Severity) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == sev {
			out = append(out, d.Summary)
		}
	}
	return out
}

func TestDiff_renameColumn_control(t *testing.T) {
	changes, diags := diffCols([]config.Column{col("b", "a")}, []config.Column{col("a", "")})
	require.False(t, diags.HasError())
	require.Len(t, changes, 1)
	rc, ok := changes[0].(*change.RenameColumnChange)
	require.True(t, ok)
	assert.Equal(t, "a", rc.OldName)
	assert.Equal(t, "b", rc.NewName)
}

func TestDiff_renameColumn_targetAlreadyInLive_error(t *testing.T) {
	changes, diags := diffCols(
		[]config.Column{col("b", "a")},
		[]config.Column{col("a", ""), col("b", "")},
	)
	require.True(t, diags.HasError())
	assert.Empty(t, changes, "no changes for a table with a rename conflict")
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], `"b"`)
	assert.Contains(t, errs[0], `"a"`)
	assert.Equal(t, "orders.yaml", diags[0].Location)
}

func TestDiff_renameColumn_targetInLiveDifferentCase_error(t *testing.T) {
	changes, diags := diffCols(
		[]config.Column{col("B", "a")},
		[]config.Column{col("a", ""), col("b", "")},
	)
	require.True(t, diags.HasError())
	assert.Empty(t, changes)
}

func TestDiff_renameColumn_oldNameReusedByOtherColumn_error(t *testing.T) {
	changes, diags := diffCols(
		[]config.Column{col("b", "a"), col("a", "")},
		[]config.Column{col("a", "")},
	)
	require.True(t, diags.HasError())
	for _, c := range changes {
		assert.NotEqual(t, change.RenameColumn, c.Type())
	}
	assert.Empty(t, changes)
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], `"a"`)
}

func TestDiff_renameColumn_oldNameReusedBeforeRename_error(t *testing.T) {
	changes, diags := diffCols(
		[]config.Column{col("a", ""), col("b", "a")},
		[]config.Column{col("a", "")},
	)
	require.True(t, diags.HasError())
	assert.Empty(t, changes)
}

func TestDiff_renameColumn_staleSource_warningOnly(t *testing.T) {
	// Rename already applied: live {b}, desired keeps rename_from a.
	changes, diags := diffCols([]config.Column{col("b", "a")}, []config.Column{col("b", "")})
	require.False(t, diags.HasError())
	assert.Empty(t, changes)
	warns := summaries(diags, diag.Warning)
	require.Len(t, warns, 1)
	assert.Contains(t, warns[0], "rename_from")
	assert.Contains(t, warns[0], "can be removed")
	assert.True(t, strings.HasPrefix(warns[0], `table "silver.orders": column "b"`), "like every warning, it names the table first: %q", warns[0])
}

func TestDiff_renameColumn_staleSourceAndNewColumnWithOldName(t *testing.T) {
	// Rename done earlier; "a" is now a brand-new column.
	changes, diags := diffCols(
		[]config.Column{col("b", "a"), col("a", "")},
		[]config.Column{col("b", "")},
	)
	require.False(t, diags.HasError())
	require.Len(t, changes, 1)
	add, ok := changes[0].(*change.AddColumnChange)
	require.True(t, ok)
	assert.Equal(t, "a", add.Column.Name)
	require.Len(t, summaries(diags, diag.Warning), 1)
}

func TestDiff_renameColumn_plusAddOtherColumn_bothEmitted(t *testing.T) {
	changes, diags := diffCols(
		[]config.Column{col("b", "a"), col("c", "")},
		[]config.Column{col("a", "")},
	)
	require.False(t, diags.HasError())
	require.Len(t, changes, 2)
	assert.Equal(t, change.RenameColumn, changes[0].Type())
	assert.Equal(t, change.AddColumn, changes[1].Type())
}

func TestDiff_twoIndependentRenames_bothEmitted(t *testing.T) {
	changes, diags := diffCols(
		[]config.Column{col("b", "a"), col("d", "c")},
		[]config.Column{col("a", ""), col("c", "")},
	)
	require.False(t, diags.HasError())
	require.Len(t, changes, 2)
	assert.Equal(t, change.RenameColumn, changes[0].Type())
	assert.Equal(t, change.RenameColumn, changes[1].Type())
}

func TestDiff_renameColumn_caseOnly_noChangeNoError(t *testing.T) {
	changes, diags := diffCols([]config.Column{col("A", "a")}, []config.Column{col("a", "")})
	assert.False(t, diags.HasError())
	assert.Empty(t, changes)
}

func TestDiff_renameColumn_caseOnlyWithLiveSameName_noError(t *testing.T) {
	// new name folds to the rename source itself: not a conflict.
	changes, diags := diffCols([]config.Column{col("A", "a"), col("x", "")}, []config.Column{col("a", ""), col("x", "")})
	assert.False(t, diags.HasError())
	assert.Empty(t, changes)
}

func TestDiffSchema_columnRenameConflict_propagatesError(t *testing.T) {
	d := makeTable("orders", "silver")
	d.Columns = []config.Column{col("b", "a")}
	l := makeTable("orders", "silver")
	l.Columns = []config.Column{col("a", ""), col("b", "")}

	changes, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(l)})
	assert.True(t, diags.HasError())
	assert.Empty(t, changes)
}

// --- L: create -> rename column -> plan again is empty ---

func TestLifecycle_createRenameColumnReplanEmpty(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{col("a", "")}

	// 1. Table does not exist: create.
	changes, diags := differ.Diff(desired, nil)
	require.False(t, diags.HasError())
	require.Len(t, changes, 1)
	assert.Equal(t, change.CreateTable, changes[0].Type())

	// 2. Table exists; rename a -> b.
	live := makeTable("orders", "silver")
	live.Columns = []config.Column{col("a", "")}
	desired.Columns = []config.Column{col("b", "a")}
	changes, diags = differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())
	require.Len(t, changes, 1)
	assert.Equal(t, change.RenameColumn, changes[0].Type())

	// 3. After apply live has b: replan has no changes (only the "can be removed" warning).
	live.Columns = []config.Column{col("b", "")}
	changes, diags = differ.Diff(desired, backendtest.LiveOf(live))
	assert.False(t, diags.HasError())
	assert.Empty(t, changes)

	// 4. Drop the stale rename_from: clean plan, no diagnostics.
	desired.Columns = []config.Column{col("b", "")}
	changes, diags = differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	assert.Empty(t, diags)
}

// --- S: SQL of existing Change types is unchanged ---

func TestChangeSQL_golden(t *testing.T) {
	p := change.TablePath{Catalog: "cat", Schema: "sch", Name: "tbl"}
	cases := []struct {
		name string
		ch   change.Change
		want string
	}{
		{"add", &change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: "c", Type: typINT, Comment: "it's"}},
			"ALTER TABLE `cat`.`sch`.`tbl` ADD COLUMN `c` INT COMMENT 'it\\'s'"},
		{"drop", &change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: "c"},
			"ALTER TABLE `cat`.`sch`.`tbl` DROP COLUMN `c`"},
		{"alter type", &change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: "c", Kind: change.AlterType, NewValue: typBIGINT},
			"ALTER TABLE `cat`.`sch`.`tbl` ALTER COLUMN `c` TYPE BIGINT"},
		{"rename column", &change.RenameColumnChange{Base: change.Base{Table: p}, OldName: "a", NewName: "b"},
			"ALTER TABLE `cat`.`sch`.`tbl` RENAME COLUMN `a` TO `b`"},
		{"rename table", &change.RenameTableChange{OldPath: p, NewName: "n"},
			"ALTER TABLE `cat`.`sch`.`tbl` RENAME TO `cat`.`sch`.`n`"},
		{"set props", &change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"b": "2", "a": "1"}},
			"ALTER TABLE `cat`.`sch`.`tbl` SET TBLPROPERTIES ('a'='1', 'b'='2')"},
		{"unset props", &change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}, Keys: []string{"b", "a"}},
			"ALTER TABLE `cat`.`sch`.`tbl` UNSET TBLPROPERTIES ('a', 'b')"},
		{"clustering", &change.AlterClusteringChange{Base: change.Base{Table: p}, Clustering: &config.Clustering{Columns: []string{"x", "y"}}},
			"ALTER TABLE `cat`.`sch`.`tbl` CLUSTER BY (`x`, `y`)"},
		{"comment", &change.AlterTableCommentChange{Base: change.Base{Table: p}, Comment: "hi"},
			"COMMENT ON TABLE `cat`.`sch`.`tbl` IS 'hi'"},
		{"drop table", &change.DropTableChange{Base: change.Base{Table: p}},
			"DROP TABLE `cat`.`sch`.`tbl`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.ch.SQL())
		})
	}
}

// --- live clustering on a nested field is left untouched when YAML has no clustering block ---

func TestDiff_NestedLiveClustering_NotTouchedWithoutBlock(t *testing.T) {
	d := makeTable("orders", "silver")
	l := makeTable("orders", "silver")
	l.Clustering = &config.Clustering{Columns: []string{"a", "b.c"}}
	changes, diags := differ.Diff(d, backendtest.LiveOf(l))
	require.False(t, diags.HasError())
	for _, ch := range changes {
		assert.NotEqual(t, change.AlterClustering, ch.Type(), "no CLUSTER BY change expected")
	}

	// A flat live clustering without a YAML block is still removed.
	l.Clustering = &config.Clustering{Columns: []string{"a"}}
	changes, _ = differ.Diff(d, backendtest.LiveOf(l))
	found := false
	for _, ch := range changes {
		found = found || ch.Type() == change.AlterClustering
	}
	assert.True(t, found)
}
