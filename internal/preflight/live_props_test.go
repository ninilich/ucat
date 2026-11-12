package preflight_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/preflight"
)

func dropCol(cat, sch, tbl string) change.Change {
	return &change.DropColumnChange{Base: change.Base{Table: change.TablePath{Catalog: cat, Schema: sch, Name: tbl}}, ColumnName: "c"}
}

func liveWithProps(cat, sch, tbl string, props map[string]string) *backend.LiveTable {
	return &backend.LiveTable{Catalog: cat, Schema: sch, Name: tbl, Properties: props}
}

func totalCalls(b *fullCountingBackend) int {
	n := 0
	for _, c := range b.calls {
		n += c
	}
	return n
}

// The properties of tables that are in the live state cost no GetTableProperties call, however many
// tables and changes there are.
func TestRun_livePropertiesCostNoBackendCall(t *testing.T) {
	b := &fullCountingBackend{}
	live := [][]*backend.LiveTable{
		{liveWithProps("prod", "silver", "a", map[string]string{propColumnMapping: litName}), liveWithProps("prod", "silver", "b", map[string]string{propColumnMapping: litName})},
		{liveWithProps("prod", "gold", "c", map[string]string{propColumnMapping: litName})},
	}
	plan := []change.Change{dropCol("prod", "silver", "a"), dropCol("prod", "silver", "a"), dropCol("prod", "silver", "b"), dropCol("prod", "gold", "c")}

	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan), live...)

	assert.Empty(t, diags)
	assert.Zero(t, totalCalls(b), "calls: %v", b.calls)
}

// The live state decides, not the backend: a table without the mapping in the live state fails even
// if the backend would say it has one, and a table with it passes even if the backend says it has not.
func TestRun_livePropertiesWinOverBackend(t *testing.T) {
	b := &fullCountingBackend{}
	b.TableProperties = map[string]map[string]string{"prod.silver.nomap": {propColumnMapping: litName}, "prod.silver.map": {}}
	live := [][]*backend.LiveTable{{
		liveWithProps("prod", "silver", "nomap", map[string]string{"owner": "x"}),
		liveWithProps("prod", "silver", "map", map[string]string{propColumnMapping: litName}),
	}}
	plan := []change.Change{dropCol("prod", "silver", "nomap"), dropCol("prod", "silver", "map")}

	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan), live...)

	require.Len(t, diags, 1, "%v", diags)
	assert.Contains(t, diags[0].Summary, `table "silver.nomap"`)
	assert.Zero(t, totalCalls(b))
}

// A live table without any property is a table without the mapping, not a missing one: no backend call.
func TestRun_liveTableWithoutPropertiesIsNotRead(t *testing.T) {
	b := &fullCountingBackend{}
	b.TableProperties = map[string]map[string]string{litProdSilverOrders: {propColumnMapping: litName}}
	live := [][]*backend.LiveTable{{liveWithProps("prod", "silver", litOrders, nil)}}
	plan := []change.Change{dropCol("prod", "silver", litOrders)}

	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan), live...)

	require.Len(t, diags, 1)
	assert.Zero(t, totalCalls(b))
}

// A table that is not in the live state is still read from the backend, once.
func TestRun_tableNotInLiveStateIsReadOnce(t *testing.T) {
	b := &fullCountingBackend{}
	b.TableProperties = map[string]map[string]string{"prod.silver.other": {propColumnMapping: litName}}
	live := [][]*backend.LiveTable{{liveWithProps("prod", "silver", "known", map[string]string{propColumnMapping: litName})}}
	plan := []change.Change{dropCol("prod", "silver", "known"), dropCol("prod", "silver", "other"), dropCol("prod", "silver", "other")}

	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan), live...)

	assert.Empty(t, diags)
	assert.Equal(t, map[string]int{"prod.silver.other": 1}, b.calls)
}

// Names are compared without case (UC does): a path spelled differently from the live name is found.
func TestRun_livePropertiesMatchIgnoringCase(t *testing.T) {
	b := &fullCountingBackend{}
	live := [][]*backend.LiveTable{{liveWithProps("prod", "silver", "orders", map[string]string{propColumnMapping: litName})}}
	plan := []change.Change{dropCol("Prod", "Silver", "ORDERS")}

	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan), live...)

	assert.Empty(t, diags)
	assert.Zero(t, totalCalls(b))
}

// The key is the whole path: the same table name in another schema or catalog is another table.
func TestRun_livePropertiesKeyIsFullPath(t *testing.T) {
	b := &fullCountingBackend{}
	live := [][]*backend.LiveTable{{liveWithProps("prod", "silver", litOrders, map[string]string{propColumnMapping: litName})}}
	plan := []change.Change{dropCol("prod", "gold", litOrders), dropCol("dev", "silver", litOrders)}

	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan), live...)

	assert.Len(t, diags, 2, "neither table is the live one, both are read from the backend and have no mapping")
	assert.Equal(t, map[string]int{"prod.gold.orders": 1, "dev.silver.orders": 1}, b.calls)
}

// A table renamed in the plan is found in the live state under its old name.
func TestRun_liveProperties_renamedTable(t *testing.T) {
	b := &fullCountingBackend{}
	live := [][]*backend.LiveTable{{liveWithProps("prod", "silver", "old", map[string]string{propColumnMapping: litName})}}
	plan := []change.Change{
		&change.RenameTableChange{OldPath: change.TablePath{Catalog: litProd, Schema: litSilver, Name: "old"}, NewName: "new"},
		dropCol("prod", "silver", "new"),
	}

	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan), live...)

	assert.Empty(t, diags)
	assert.Zero(t, totalCalls(b))
}

// The properties of a live table are not shared with the checker: what it does to the map cannot change the live state.
func TestRun_livePropertiesAreNotShared(t *testing.T) {
	props := map[string]string{propColumnMapping: litName}
	live := [][]*backend.LiveTable{{liveWithProps("prod", "silver", litOrders, props)}}
	plan := []change.Change{dropCol("prod", "silver", litOrders)}
	var seen map[string]string
	spy := &mutatingChecker{onProps: func(m map[string]string) { seen = m; m["x"] = "y" }}

	_ = preflight.Run(context.Background(), plan, &fullCountingBackend{}, []preflight.Checker{spy}, live...)

	require.NotNil(t, seen)
	assert.Equal(t, map[string]string{propColumnMapping: litName}, props)
}

type mutatingChecker struct{ onProps func(map[string]string) }

func (m *mutatingChecker) Applies(change.Change) bool { return true }

func (m *mutatingChecker) Check(ctx context.Context, ch change.Change, b backend.UCBackend) diag.Diagnostics {
	p := ch.(*change.DropColumnChange).Table
	props, err := b.GetTableProperties(ctx, p.Catalog, p.Schema, p.Name)
	if err != nil {
		panic(errors.New("unexpected: " + err.Error()))
	}
	m.onProps(props)
	return nil
}

// A table that is not in the live state is read once; every caller gets its own copy of the answer.
func TestRun_fallbackPropertiesAreNotShared(t *testing.T) {
	b := &fullCountingBackend{}
	b.TableProperties = map[string]map[string]string{litProdSilverOrders: {propColumnMapping: litName}}
	plan := []change.Change{dropCol("prod", "silver", litOrders)}
	var seen []map[string]string
	spy := &mutatingChecker{onProps: func(m map[string]string) {
		seen = append(seen, m)
		if len(seen) > 1 {
			assert.NotContains(t, m, "x", "a later read must not see what an earlier caller wrote")
		}
		m["x"] = "y"
	}}

	_ = preflight.Run(context.Background(), append(append(plan, plan...), plan...), b, []preflight.Checker{spy})

	assert.Len(t, seen, 3)
	assert.Equal(t, map[string]int{litProdSilverOrders: 1}, b.calls)
	assert.Equal(t, map[string]string{propColumnMapping: litName}, b.TableProperties[litProdSilverOrders], "the backend's own data is not touched")
}
