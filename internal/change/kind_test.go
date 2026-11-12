package change_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
)

// sampleChanges returns one change per Kind (zero fields except the table).
func sampleChanges() map[change.Kind]change.Change {
	p := change.TablePath{Catalog: "c", Schema: "s", Name: "t"}
	return map[change.Kind]change.Change{
		change.AddColumn:          &change.AddColumnChange{Base: change.Base{Table: p}},
		change.DropColumn:         &change.DropColumnChange{Base: change.Base{Table: p}},
		change.AlterColumn:        &change.AlterColumnChange{Base: change.Base{Table: p}},
		change.RenameColumn:       &change.RenameColumnChange{Base: change.Base{Table: p}},
		change.RenameTable:        &change.RenameTableChange{OldPath: p, NewName: "t2"},
		change.SetTBLProperties:   &change.SetTBLPropertiesChange{Base: change.Base{Table: p}},
		change.UnsetTBLProperties: &change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}},
		change.AlterClustering:    &change.AlterClusteringChange{Base: change.Base{Table: p}},
		change.AlterTableComment:  &change.AlterTableCommentChange{Base: change.Base{Table: p}},
		change.DropTable:          &change.DropTableChange{Base: change.Base{Table: p}},
		change.CreateTable:        &change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{Name: "t"}},
	}
}

// The full data of every Kind in one table: op name, table name, group key, destructive flag.
// A new Kind without a row here fails the test, as does a change whose Type() disagrees.
func TestKinds_fullDataPerType(t *testing.T) {
	type row struct {
		op          string
		groupKey    string
		destructive bool
	}
	const tbl, tbl2 = "`c`.`s`.`t`", "`c`.`s`.`t2`"
	want := map[change.Kind]row{
		change.AddColumn:          {"add_column", tbl, false},
		change.DropColumn:         {"drop_column", tbl, true},
		change.AlterColumn:        {"alter_column", tbl, false},
		change.RenameColumn:       {"rename_column", tbl, false},
		change.RenameTable:        {"rename_table", tbl2, false}, // keyed by the NEW name
		change.SetTBLProperties:   {"set_tbl_properties", tbl, false},
		change.UnsetTBLProperties: {"unset_tbl_properties", tbl, false},
		change.AlterClustering:    {"alter_clustering", tbl, false},
		change.AlterTableComment:  {"alter_table_comment", tbl, false},
		change.DropTable:          {"drop_table", tbl, true},
		change.CreateTable:        {"create_table", tbl, false},
	}
	samples := sampleChanges()
	ops := map[string]change.Kind{}
	for ct := change.Kind(0); ct < change.KindCount; ct++ {
		ch, ok := samples[ct]
		require.True(t, ok, "Kind %d has no sample: add it to sampleChanges", ct)
		w, ok := want[ct]
		require.True(t, ok, "Kind %d has no row: decide its op, group key and destructiveness", ct)
		require.Equal(t, ct, ch.Type())

		assert.Equal(t, w.op, ct.Op(), "Op of Kind %d", ct)
		if prev, dup := ops[w.op]; dup {
			t.Errorf("op %q used by both Kind %d and %d", w.op, prev, ct)
		}
		ops[w.op] = ct
		assert.Equal(t, w.groupKey, ch.GroupKey(), "GroupKey of Kind %d", ct)
		assert.Equal(t, w.destructive, ch.IsDestructive(), "IsDestructive of Kind %d", ct)
		if ct != change.RenameTable {
			assert.Equal(t, tbl, ch.TableName(), "TableName of Kind %d", ct)
			assert.Equal(t, ch.TableName(), ch.GroupKey(), "GroupKey equals TableName for Kind %d", ct)
		}
	}
	assert.Len(t, samples, int(change.KindCount), "no stray samples")
	assert.Len(t, want, int(change.KindCount), "no stray rows")
	// A rename is the one change whose TableName is the OLD name.
	assert.Equal(t, tbl, samples[change.RenameTable].TableName())
}

func TestKind_opPanicsForUnknown(t *testing.T) {
	assert.Panics(t, func() { _ = change.KindCount.Op() })
	assert.Panics(t, func() { _ = change.Kind(-1).Op() })
}

// Detail of every Kind: the table (the new name for a table rename), the column, the previous name and
// the alter subtype that plan --json shows. A new Kind without a row fails the test.
func TestKinds_Detail(t *testing.T) {
	tbl := change.TablePath{Catalog: "c", Schema: "s", Name: "t"}
	tbl2 := change.TablePath{Catalog: "c", Schema: "s", Name: "t2"}
	samples := sampleChanges()
	samples[change.AddColumn] = &change.AddColumnChange{Base: change.Base{Table: tbl}, Column: config.Column{Name: "a"}}
	samples[change.DropColumn] = &change.DropColumnChange{Base: change.Base{Table: tbl}, ColumnName: "d"}
	samples[change.RenameColumn] = &change.RenameColumnChange{Base: change.Base{Table: tbl}, OldName: "o", NewName: "n"}
	samples[change.AlterColumn] = &change.AlterColumnChange{Base: change.Base{Table: tbl}, ColumnName: "q", Kind: change.AlterComment}
	want := map[change.Kind]change.Detail{
		change.AddColumn:          {Table: tbl, Column: "a"},
		change.DropColumn:         {Table: tbl, Column: "d"},
		change.AlterColumn:        {Table: tbl, Column: "q", Alter: "comment"},
		change.RenameColumn:       {Table: tbl, Column: "n", PreviousName: "o"},
		change.RenameTable:        {Table: tbl2, PreviousName: "t"},
		change.SetTBLProperties:   {Table: tbl},
		change.UnsetTBLProperties: {Table: tbl},
		change.AlterClustering:    {Table: tbl},
		change.AlterTableComment:  {Table: tbl},
		change.DropTable:          {Table: tbl},
		change.CreateTable:        {Table: tbl},
	}
	for ct := change.Kind(0); ct < change.KindCount; ct++ {
		w, ok := want[ct]
		require.True(t, ok, "Kind %d has no row: decide its Detail", ct)
		assert.Equal(t, w, samples[ct].Detail(), "Detail of Kind %d", ct)
	}
	assert.Len(t, want, int(change.KindCount), "no stray rows")
}

func TestAlterKind_Name(t *testing.T) {
	assert.Equal(t, "type", change.AlterType.Name())
	assert.Equal(t, "comment", change.AlterComment.Name())
	assert.Equal(t, "nullable", change.AlterNullable.Name())
	assert.Panics(t, func() { _ = change.AlterKind(99).Name() })
}

// An alter kind that is not known must not turn into some other ALTER: Describe and SQL panic with a
// message that names the value, like Kind.Op and AlterKind.Name.
func TestAlterColumnChange_UnknownKindPanics(t *testing.T) {
	c := &change.AlterColumnChange{Base: change.Base{Table: change.TablePath{Catalog: "c", Schema: "s", Name: "t"}}, ColumnName: "x", Kind: change.AlterKind(99)}
	assert.PanicsWithValue(t, "change: no description for alter kind 99", func() { _ = c.Describe() })
	assert.PanicsWithValue(t, "change: no SQL for alter kind 99", func() { _ = c.SQL() })
	assert.Panics(t, func() { _ = c.Detail() })

	// the known kinds still work, nullable included (it was the silent default)
	for _, k := range []change.AlterKind{change.AlterType, change.AlterComment, change.AlterNullable} {
		c.Kind = k
		assert.NotPanics(t, func() { _ = c.Describe(); _ = c.SQL(); _ = c.Detail() }, "kind %d", k)
	}
}
