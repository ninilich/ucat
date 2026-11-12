package preflight_test

import (
	"context"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/ninilich/ucat/internal/planner"
	"github.com/ninilich/ucat/internal/preflight"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fullCountingBackend counts GetTableProperties calls per catalog.schema.table.
type fullCountingBackend struct {
	backendtest.MockUCBackend
	calls map[string]int
}

func (c *fullCountingBackend) GetTableProperties(ctx context.Context, cat, sch, tbl string) (map[string]string, error) {
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[cat+"."+sch+"."+tbl]++
	return c.MockUCBackend.GetTableProperties(ctx, cat, sch, tbl)
}

// The cache key is catalog+schema+table: tables with one name in different schemas / catalogs
// are read separately, so a mapping on one of them never lets a table without it pass.
func TestRun_cacheKeyIsFullTablePath(t *testing.T) {
	drop := func(cat, sch string) change.Change {
		return &change.DropColumnChange{Base: change.Base{Table: change.TablePath{Catalog: cat, Schema: sch, Name: litOrders}}, ColumnName: "c"}
	}
	b := &fullCountingBackend{}
	// Only prod.silver.orders has the mapping; it is the first table, so it is cached first.
	b.TableProperties = map[string]map[string]string{litProdSilverOrders: {propColumnMapping: litName}}
	plan := []change.Change{
		drop("prod", "silver"), // ok, mapping
		drop("prod", "gold"),   // same table name, other schema: must be read and fail
		drop("dev", "silver"),  // same schema+table, other catalog: must be read and fail
		drop("prod", "silver"), // second change of the first table: cached, still ok
	}
	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))

	assert.Equal(t, map[string]int{litProdSilverOrders: 1, "prod.gold.orders": 1, "dev.silver.orders": 1}, b.calls)
	require.Len(t, diags, 2, "diags: %v", diags)
	assert.Contains(t, diags[0].Summary, `"gold.orders"`)
	assert.Contains(t, diags[1].Summary, `"silver.orders"`)

	// Opposite order: the table without a mapping first. It must not poison the one with it.
	b2 := &fullCountingBackend{}
	b2.TableProperties = b.TableProperties
	plan2 := []change.Change{drop("prod", "gold"), drop("dev", "silver"), drop("prod", "silver")}
	diags2 := preflight.Run(context.Background(), plan2, b2, preflight.DefaultCheckers(plan2))
	assert.Len(t, diags2, 2)
	assert.Equal(t, 3, len(b2.calls))
}

// Path parts are not glued together: ("a","bc") and ("ab","c") are different tables.
func TestRun_cacheKeyPartsDoNotCollide(t *testing.T) {
	drop := func(cat, sch string) change.Change {
		return &change.DropColumnChange{Base: change.Base{Table: change.TablePath{Catalog: cat, Schema: sch, Name: "t"}}, ColumnName: "c"}
	}
	b := &fullCountingBackend{}
	b.TableProperties = map[string]map[string]string{"a.bc.t": {propColumnMapping: litName}}
	plan := []change.Change{drop("a", "bc"), drop("ab", "c")}
	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))
	assert.Equal(t, map[string]int{"a.bc.t": 1, "ab.c.t": 1}, b.calls)
	assert.Len(t, diags, 1, "only the table without mapping is an error")
}

// The cache lives for one Run only: properties changed between two Runs are seen by the second.
func TestRun_cacheDoesNotOutliveRun(t *testing.T) {
	plan := []change.Change{dropChange()}
	b := &fullCountingBackend{}
	first := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))
	require.Len(t, first, 1)

	b.TableProperties = map[string]map[string]string{litProdSilverOrders: {propColumnMapping: litName}}
	assert.Empty(t, preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan)), "mapping appeared: a stale cache would still report the error")

	b.TableProperties = nil
	assert.Len(t, preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan)), 1, "mapping gone: a stale cache would let the drop pass")
}

// A read error of one table is cached for that table only.
func TestRun_cachedErrorDoesNotLeakToOtherTables(t *testing.T) {
	b := &selectiveErrBackend{}
	other := change.TablePath{Catalog: litProd, Schema: litSilver, Name: litUsers}
	b.TableProperties = map[string]map[string]string{litProdSilverUsers: {propColumnMapping: litName}}
	plan := []change.Change{dropChange(), &change.DropColumnChange{Base: change.Base{Table: other}, ColumnName: "x"}}
	diags := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Summary, "failed to get table properties")
	assert.Contains(t, diags[0].Summary, `"silver.orders"`)
}

type selectiveErrBackend struct{ backendtest.MockUCBackend }

func (s *selectiveErrBackend) GetTableProperties(ctx context.Context, cat, sch, tbl string) (map[string]string, error) {
	if tbl == litOrders {
		return nil, assert.AnError
	}
	return s.MockUCBackend.GetTableProperties(ctx, cat, sch, tbl)
}

// pipeline: real DiffSchema -> planner.Plan -> preflight.Run, rename + drop column in ONE table.
func TestPipeline_RenameAndDropColumnSameTable(t *testing.T) {
	id := config.Column{Name: "id", Type: typBIGINT}
	mk := func(name string) (config.Table, config.Table) {
		base := config.Table{Name: name, Schema: litSilver, ResolvedCatalog: litProd, ResolvedSchema: litSilver}
		d, l := base, base
		l.Columns = []config.Column{id, {Name: "n", Type: typSTRING}, {Name: "legacy", Type: typSTRING}}
		d.Columns = []config.Column{id, {Name: litName, Type: typSTRING, RenameFrom: "n"}}
		return d, l
	}
	run := func(t *testing.T, tables []string, props map[string]map[string]string) ([]string, *fullCountingBackend) {
		var ds []config.Table
		var ls []*backend.LiveTable
		for _, n := range tables {
			d, l := mk(n)
			ds = append(ds, d)
			ls = append(ls, backendtest.LiveOf(l))
		}
		changes, diags := differ.DiffSchema(ds, ls)
		require.False(t, diags.HasError(), "diags: %v", diags)
		plan := planner.Plan(changes)
		b := &fullCountingBackend{}
		b.TableProperties = props
		var got []string
		for _, d := range preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan)) {
			got = append(got, d.Summary)
		}
		return got, b
	}
	const tail = ` requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`

	t.Run("no mode: one error per column, one GET", func(t *testing.T) {
		got, b := run(t, []string{litOrders}, nil)
		assert.Equal(t, []string{
			`table "silver.orders": column rename "n"` + tail,
			`table "silver.orders": column drop "legacy"` + tail,
		}, got)
		assert.Equal(t, map[string]int{litProdSilverOrders: 1}, b.calls)
	})
	t.Run("mode=name in live: no errors", func(t *testing.T) {
		got, b := run(t, []string{litOrders}, map[string]map[string]string{litProdSilverOrders: {propColumnMapping: litName}})
		assert.Empty(t, got)
		assert.Equal(t, 1, b.calls["prod.silver.orders"])
	})
	t.Run("two tables are checked independently", func(t *testing.T) {
		got, b := run(t, []string{litOrders, "users"}, map[string]map[string]string{"prod.silver.users": {propColumnMapping: litName}})
		assert.Equal(t, []string{
			`table "silver.orders": column rename "n"` + tail,
			`table "silver.orders": column drop "legacy"` + tail,
		}, got)
		assert.Equal(t, map[string]int{"prod.silver.orders": 1, "prod.silver.users": 1}, b.calls)

		got, _ = run(t, []string{litOrders, "users"}, map[string]map[string]string{"prod.silver.orders": {propColumnMapping: litName}})
		assert.Equal(t, []string{
			`table "silver.users": column rename "n"` + tail,
			`table "silver.users": column drop "legacy"` + tail,
		}, got)
	})
}
