package differ_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- case-insensitive column matching ---

func caseTables(desired, live []config.Column) (config.Table, *config.Table) {
	d := makeTable("orders", "silver")
	d.Columns = desired
	l := makeTable("orders", "silver")
	l.Columns = live
	return d, &l
}

func TestDiff_columnCaseOnlyDifference_noChanges(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: litUserIDCamel, Type: typBIGINT}},
		[]config.Column{{Name: litUserid, Type: typBIGINT}},
	)
	changes, diags := differ.Diff(d, backendtest.LiveOf(*l))
	assert.False(t, diags.HasError())
	assert.Empty(t, changes)
}

func TestDiff_columnCaseOnlyDifference_reverseDirection(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: litUserid, Type: typBIGINT}},
		[]config.Column{{Name: litUserID, Type: typBIGINT}},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	assert.Empty(t, changes)
}

func TestDiff_columnCaseNonASCII_noChanges(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "ÜBER", Type: typINT}},
		[]config.Column{{Name: "über", Type: typINT}},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	assert.Empty(t, changes)
}

func TestDiff_caseInsensitiveMatch_realDifferencesStillDetected(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d, l := caseTables(
			[]config.Column{{Name: litUserID, Type: typSTRING}},
			[]config.Column{{Name: litUserid, Type: typVarchar10}},
		)
		changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
		require.Len(t, changes, 1)
		assert.Equal(t, change.AlterColumn, changes[0].Type())
	})
	t.Run("comment", func(t *testing.T) {
		d, l := caseTables(
			[]config.Column{{Name: litUserID, Type: typBIGINT, Comment: litNew}},
			[]config.Column{{Name: litUserid, Type: typBIGINT, Comment: litOld}},
		)
		changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
		require.Len(t, changes, 1)
		assert.Equal(t, change.AlterColumn, changes[0].Type())
	})
	t.Run("nullable", func(t *testing.T) {
		d, l := caseTables(
			[]config.Column{{Name: litUserID, Type: typBIGINT, Nullable: boolPtr(false)}},
			[]config.Column{{Name: litUserid, Type: typBIGINT}},
		)
		changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
		require.Len(t, changes, 1)
		assert.Equal(t, change.AlterColumn, changes[0].Type())
	})
}

func TestDiff_dropColumnUsesLiveSpelling(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "id", Type: typBIGINT}},
		[]config.Column{{Name: "id", Type: typBIGINT}, {Name: "OldCol", Type: typSTRING}},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	require.Len(t, changes, 1)
	drop, ok := changes[0].(*change.DropColumnChange)
	require.True(t, ok, "got %T", changes[0])
	assert.Equal(t, "OldCol", drop.ColumnName)
}

func TestDiff_addColumnUsesDesiredSpelling(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "id", Type: typBIGINT}, {Name: litNewCol, Type: typSTRING}},
		[]config.Column{{Name: "id", Type: typBIGINT}},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	require.Len(t, changes, 1)
	add, ok := changes[0].(*change.AddColumnChange)
	require.True(t, ok, "got %T", changes[0])
	assert.Equal(t, "NewCol", add.Column.Name)
}

func TestDiff_columnRenameFromCaseInsensitive(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: litUserID2, Type: typBIGINT, RenameFrom: "USERID"}},
		[]config.Column{{Name: litUserid, Type: typBIGINT}},
	)
	changes, diags := differ.Diff(d, backendtest.LiveOf(*l))
	assert.False(t, diags.HasError())
	require.Len(t, changes, 1)
	ren, ok := changes[0].(*change.RenameColumnChange)
	require.True(t, ok, "got %T", changes[0])
	assert.Equal(t, "userid", ren.OldName, "OldName must be the live spelling")
	assert.Equal(t, "user_id", ren.NewName)
	for _, c := range changes {
		assert.NotEqual(t, change.AddColumn, c.Type())
		assert.NotEqual(t, change.DropColumn, c.Type())
	}
}

func TestDiff_columnRenameFromLiveMixedCase(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "user_id", Type: typBIGINT, RenameFrom: litUserid}},
		[]config.Column{{Name: "UserId", Type: typBIGINT}},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	require.Len(t, changes, 1)
	ren, ok := changes[0].(*change.RenameColumnChange)
	require.True(t, ok, "got %T", changes[0])
	assert.Equal(t, "UserId", ren.OldName)
	assert.Equal(t, "user_id", ren.NewName)
}

func TestDiff_columnRenameCaseOnlyProducesNoRename(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "UserId", Type: typBIGINT, RenameFrom: litUserid}},
		[]config.Column{{Name: litUserid, Type: typBIGINT}},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	for _, c := range changes {
		assert.NotEqual(t, change.RenameColumn, c.Type(), "case-only rename must not be emitted: %s", c.Describe())
		assert.NotEqual(t, change.AddColumn, c.Type())
		assert.NotEqual(t, change.DropColumn, c.Type())
	}
	assert.Empty(t, changes)
}

func TestDiff_columnCaseMixedScenario(t *testing.T) {
	d, l := caseTables(
		[]config.Column{
			{Name: litUserIDCamel, Type: typBIGINT}, // case-only difference
			{Name: "Amount", Type: typSTRING},       // real type change (varchar -> STRING)
			{Name: "Created", Type: typDATE},        // add
		},
		[]config.Column{
			{Name: litUserid, Type: typBIGINT},
			{Name: litAmount, Type: typVarchar10},
		},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	require.Len(t, changes, 2, "changes: %v", changes)
	var types []change.Kind
	for _, c := range changes {
		types = append(types, c.Type())
	}
	assert.ElementsMatch(t, []change.Kind{change.AlterColumn, change.AddColumn}, types)
}

func TestDiff_columnCaseIdempotentAfterApply(t *testing.T) {
	desired := []config.Column{
		{Name: litUserIDCamel, Type: typBIGINT},
		{Name: "NewCol", Type: typSTRING},
	}
	d, l := caseTables(desired, []config.Column{{Name: litUserid, Type: typBIGINT}})

	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	require.Len(t, changes, 1) // only the add

	// Pretend apply: UC keeps the spelling used at creation. The existing column keeps
	// its live spelling, the added column gets the desired spelling.
	applied := &config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litProd, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: litUserid, Type: typBIGINT}, {Name: "NewCol", Type: typSTRING}},
	}
	changes, diags := differ.Diff(d, backendtest.LiveOf(*applied))
	assert.False(t, diags.HasError())
	assert.Empty(t, changes, "second plan must be empty")
}

func TestDiff_genuinelyDifferentColumnsStillAddAndDrop(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "user_id", Type: typBIGINT}},
		[]config.Column{{Name: litUserid, Type: typBIGINT}},
	)
	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	var types []change.Kind
	for _, c := range changes {
		types = append(types, c.Type())
	}
	assert.ElementsMatch(t, []change.Kind{change.AddColumn, change.DropColumn}, types)
}

func TestDiffSchema_columnCaseOnlyDifference_noChanges(t *testing.T) {
	d := makeTable("orders", "silver")
	d.Columns = []config.Column{{Name: "OrderDate", Type: typDATE}, {Name: litUserIDCamel, Type: typBIGINT}}
	l := makeTable("orders", "silver")
	l.Columns = []config.Column{{Name: "orderdate", Type: typDATE}, {Name: litUserid, Type: typBIGINT}}

	changes, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(l)})
	assert.False(t, diags.HasError())
	assert.Empty(t, changes)
}

func TestDiff_clusteringAndPartitioningColumnCaseOnlyNoChange(t *testing.T) {
	base := config.Table{Name: "t", ResolvedCatalog: "c", ResolvedSchema: "s",
		Columns: []config.Column{{Name: "userId", Type: typBIGINT}, {Name: litEventDate, Type: "DATE"}}}
	live := base
	live.Columns = []config.Column{{Name: "userid", Type: typBIGINT}, {Name: litEventdate, Type: "DATE"}}

	desired := base
	desired.Clustering = &config.Clustering{Columns: []string{"userId", "eventDate"}}
	live.Clustering = &config.Clustering{Columns: []string{"userid", "eventdate"}}
	changes, _ := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes, "clustering columns differing only by case must not produce changes")

	desired = base
	desired.Partitioning = &config.Partitioning{Columns: []string{"eventDate"}}
	live.Clustering = nil
	live.Partitioning = &config.Partitioning{Columns: []string{"eventdate"}}
	changes, _ = differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes, "partition columns differing only by case must not produce changes")
}

// Same rule as the validator (config.SameName): "ſ" is not "s", so rename_from: ſ on column s
// is a real rename (config.TestValidateLocal_longSRenameFromIsRealRename).
func TestDiff_columnRenameFromLongSIsRealRename(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "s", Type: "BIGINT", RenameFrom: "ſ"}},
		[]config.Column{{Name: "ſ", Type: "BIGINT"}},
	)
	changes, diags := differ.Diff(d, backendtest.LiveOf(*l))
	assert.False(t, diags.HasError())
	require.Len(t, changes, 1)
	ren, ok := changes[0].(*change.RenameColumnChange)
	require.True(t, ok, "got %T", changes[0])
	assert.Equal(t, "ſ", ren.OldName)
	assert.Equal(t, "s", ren.NewName)
}
