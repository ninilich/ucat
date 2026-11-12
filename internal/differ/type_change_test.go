package differ_test

// v1 supports changing the type of an existing column only from varchar(n)/char(n) to STRING
// (types compared after normalization); every other change is a planning error.

import (
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

func TestDiff_typeChange_supportedVarcharCharToString(t *testing.T) {
	pairs := [][2]string{ // {yaml, live}
		{typSTRING, typVarchar10},
		{typSTRING, typChar5},
		{typStringLower, "VARCHAR(10)"},
		{" String ", "Varchar( 10 )"},
		{typSTRING, "CHAR( 255 )"},
		{typSTRING, "varchar(0)"},
	}
	for _, p := range pairs {
		t.Run(p[1]+" to "+p[0], func(t *testing.T) {
			changes, diags := typeDiffAll(p[0], p[1])
			assert.Empty(t, diags)
			require.Len(t, changes, 1)
			ch, ok := changes[0].(*change.AlterColumnChange)
			require.True(t, ok)
			assert.Equal(t, change.AlterType, ch.Kind)
			assert.Equal(t, p[0], ch.NewValue)
			assert.Equal(t, p[1], ch.OldValue)
		})
	}
}

func TestDiff_typeChange_equalAfterNormalizationIsNoChange(t *testing.T) {
	pairs := [][2]string{
		{typVarchar10, "VARCHAR( 10 )"},
		{"CHAR(5)", typChar5},
		{typSTRING, typStringLower},
		{typINT, "int"},
	}
	for _, p := range pairs {
		t.Run(p[0]+" vs "+p[1], func(t *testing.T) {
			changes, diags := typeDiffAll(p[0], p[1])
			assert.Empty(t, changes)
			assert.Empty(t, diags)
		})
	}
}

func TestDiff_typeChange_unsupportedIsPlanningError(t *testing.T) {
	pairs := [][2]string{ // {yaml, live}
		{typBIGINT, typINT},
		{typINT, typSTRING},
		{typVarchar10, typSTRING},
		{"varchar(20)", typVarchar10},
		{typINT, typVarchar10},
		{"CHAR(5)", typSTRING},
		{"varchar(5)", typChar5},
		{"DECIMAL(12,2)", typDecimal102},
		{typArrayString, "array<varchar(5)>"},
		{"struct<a:bigint>", "struct<a:int>"},
		{"struct<a:string>", "struct<a:varchar(5)>"},
		{typSTRING, "varchar(abc)"},
		{typSTRING, "varchar()"},
		{typSTRING, "varchar(10) COLLATE UTF8_LCASE"},
		{typSTRING, "varchar(10),"},
		{typSTRING, "xvarchar(10)"},
		{typSTRING, "varchar(10)x"},
		{typSTRING, "varchar(-1)"},
		{typSTRING, "varchar(1,2)"},
		{typSTRING, "array<varchar(5)>"},
		{"string ", typINT},
		{"STRING COLLATE UTF8_LCASE", typVarchar10},
		{"", typVarchar10},
	}
	for _, p := range pairs {
		t.Run(p[1]+" to "+p[0], func(t *testing.T) {
			assertTypeChangeRejected(t, p[0], p[1])
		})
	}
}

func TestDiff_typeChange_errorText(t *testing.T) {
	t.Run("INT to BIGINT", func(t *testing.T) {
		changes, diags := typeDiffAll("BIGINT", "int")
		assert.Empty(t, changes)
		require.Len(t, diags, 1)
		assert.Equal(t, diag.Error, diags[0].Severity)
		assert.Equal(t, `table "silver.orders", column "c": changing the type from int to BIGINT is not supported in v1 (only varchar(n)/char(n) to STRING); type widening is planned for v2`, diags[0].Summary)
	})
	t.Run("STRING to VARCHAR", func(t *testing.T) {
		_, diags := typeDiffAll("VARCHAR(10)", "string")
		require.Len(t, diags, 1)
		assert.Equal(t, `table "silver.orders", column "c": changing the type from string to VARCHAR(10) is not supported in v1 (only varchar(n)/char(n) to STRING); type widening is planned for v2`, diags[0].Summary)
	})
}

// With rename_from the type check applies to the renamed column too; the label carries the new table name.
func TestDiff_typeChange_afterColumnRename(t *testing.T) {
	desired := makeTable(litOrders, litSilver)
	desired.SourceFile = "tables/silver/orders.yml"
	desired.Columns = []config.Column{{Name: litAmount, Type: typBIGINT, RenameFrom: litSum}}
	live := makeTable(litOrders, litSilver)
	live.Columns = []config.Column{{Name: litSum, Type: typINT}}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	require.Len(t, diags, 1)
	assert.Equal(t, "tables/silver/orders.yml", diags[0].Location)
	assert.Equal(t, []string{`table "silver.orders", column "amount": changing the type from INT to BIGINT is not supported in v1 (only varchar(n)/char(n) to STRING); type widening is planned for v2`}, summaries(diags, diag.Error))
}

// One column changes the type in a supported way, another in an unsupported way: the table gets the
// error (naming the column and the table) and no changes at all, like a new NOT NULL column.
func TestDiffSchema_typeChange_supportedPlusUnsupported_noChanges(t *testing.T) {
	desired := makeTable(litOrders, litSilver)
	desired.SourceFile = "tables/silver/orders.yml"
	desired.Columns = []config.Column{
		{Name: "ok", Type: typSTRING},
		{Name: litAmount, Type: typBIGINT},
		{Name: litAdded, Type: typINT},
		{Name: litNote, Type: typSTRING, Comment: litNew},
	}
	live := makeTable(litOrders, litSilver)
	live.Columns = []config.Column{{Name: "ok", Type: typVarchar10}, {Name: litAmount, Type: typINT}, {Name: litNote, Type: typSTRING}}

	changes, diags := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	assert.Empty(t, changes, "nothing may be published for a table with a planning error")
	require.True(t, diags.HasError())
	assert.Equal(t, []string{`table "silver.orders", column "amount": changing the type from INT to BIGINT is not supported in v1 (only varchar(n)/char(n) to STRING); type widening is planned for v2`}, summaries(diags, diag.Error))
}

// Two unsupported changes in one table: both reported, in column order.
func TestDiff_typeChange_twoColumnsBothReported(t *testing.T) {
	desired := makeTable(litOrders, litSilver)
	desired.Columns = []config.Column{{Name: "zz", Type: typBIGINT}, {Name: "aa", Type: typINT}}
	live := makeTable(litOrders, litSilver)
	live.Columns = []config.Column{{Name: "zz", Type: typINT}, {Name: "aa", Type: typSTRING}}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 2)
	assert.Contains(t, errs[0], `column "zz"`)
	assert.Contains(t, errs[1], `column "aa"`)
}

// Edge spellings around the single supported type change.
func TestDiff_typeChange_edgeSpellings(t *testing.T) {
	supported := [][2]string{ // {yaml, live}
		{typSTRING, "char(1)"},
		{typSTRING, "varchar(99999999999)"},
		{typStringLower, "CHAR(1)"},
	}
	for _, p := range supported {
		t.Run("supported "+p[1]+" to "+p[0], func(t *testing.T) {
			changes, diags := typeDiffAll(p[0], p[1])
			assert.Empty(t, diags)
			require.Len(t, changes, 1)
		})
	}
	rejected := [][2]string{
		{"STRING NOT NULL", typVarchar10},
		{typSTRING, ""},
		{typSTRING, "varchar"},
		{typSTRING, "char"},
		{typSTRING, "varchar(1.5)"},
	}
	for _, p := range rejected {
		t.Run("rejected "+p[1]+" to "+p[0], func(t *testing.T) {
			assertTypeChangeRejected(t, p[0], p[1])
		})
	}
}

// A renamed column changes the type in a supported way, another column is fine: all alters are published.
func TestDiff_typeChange_afterColumnRename_supported(t *testing.T) {
	desired := makeTable(litOrders, litSilver)
	desired.Columns = []config.Column{{Name: litAmount, Type: typSTRING, RenameFrom: litSum}, {Name: litNote, Type: typSTRING, Comment: litNew}}
	live := makeTable(litOrders, litSilver)
	live.Columns = []config.Column{{Name: litSum, Type: typVarchar10}, {Name: litNote, Type: typSTRING}}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, diags)
	require.Len(t, changes, 3)
	_, isRename := changes[0].(*change.RenameColumnChange)
	assert.True(t, isRename)
	typeAlter, ok := changes[1].(*change.AlterColumnChange)
	require.True(t, ok)
	assert.Equal(t, change.AlterType, typeAlter.Kind)
	assert.Equal(t, typVarchar10, typeAlter.OldValue)
	commentAlter, ok := changes[2].(*change.AlterColumnChange)
	require.True(t, ok)
	assert.Equal(t, change.AlterComment, commentAlter.Kind)
}

// Planning errors in two tables of one schema: both diagnostics arrive (in table order), the third table is still diffed.
func TestDiffSchema_typeChange_twoBadTablesThirdIsDiffed(t *testing.T) {
	mk := func(name, desiredType, liveType string) (config.Table, *backend.LiveTable) {
		d := makeTable(name, litSilver)
		d.Columns = []config.Column{{Name: "c", Type: desiredType, Comment: litNew}}
		l := makeTable(name, litSilver)
		l.Columns = []config.Column{{Name: "c", Type: liveType}}
		return d, backendtest.LiveOf(l)
	}
	d1, l1 := mk("t1", typBIGINT, typINT)
	d2, l2 := mk("t2", typINT, typSTRING)
	d3, l3 := mk("t3", typSTRING, typVarchar10)

	changes, diags := differ.DiffSchema([]config.Table{d1, d2, d3}, []*backend.LiveTable{l1, l2, l3})
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 2)
	assert.Contains(t, errs[0], `table "silver.t1"`)
	assert.Contains(t, errs[1], `table "silver.t2"`)
	require.NotEmpty(t, changes)
	for _, ch := range changes {
		alter, ok := ch.(*change.AlterColumnChange)
		require.True(t, ok)
		assert.Equal(t, "t3", alter.Table.Name)
	}
}
