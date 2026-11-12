package live_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/live"
)

// failingBackend fails ListTables for the schemas in failFor and counts the calls.
type failingBackend struct {
	*backendtest.MockUCBackend
	failFor map[backend.SchemaRef]bool
	calls   atomic.Int32
}

func (f *failingBackend) ListTables(ctx context.Context, ref backend.SchemaRef) ([]*backend.LiveTable, error) {
	f.calls.Add(1)
	if f.failFor[ref] {
		return nil, errors.New("boom " + ref.UCSchema)
	}
	return f.MockUCBackend.ListTables(ctx, ref)
}

func tbl(cat, schema, name string) config.Table {
	return config.Table{Name: name, ResolvedCatalog: cat, ResolvedSchema: schema}
}

func TestGroupBySchemaSortsRefs(t *testing.T) {
	groups, refs := live.GroupBySchema([]config.Table{
		tbl("prod", "silver", "b"), tbl("dev", "gold", "x"), tbl("prod", "bronze", "a"), tbl("prod", "silver", "c"),
	})
	assert.Equal(t, []backend.SchemaRef{
		{UCCatalog: "dev", UCSchema: "gold"},
		{UCCatalog: litProd, UCSchema: "bronze"},
		{UCCatalog: "prod", UCSchema: "silver"},
	}, refs)
	assert.Len(t, groups[backend.SchemaRef{UCCatalog: "prod", UCSchema: "silver"}], 2)
	assert.Equal(t, "b", groups[refs[2]][0].Name, "tables keep their order inside a schema")

	groups, refs = live.GroupBySchema(nil)
	assert.Empty(t, groups)
	assert.Empty(t, refs)
}

func TestFetchTablesKeepsRefOrder(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		m := &backendtest.MockUCBackend{}
		a := backend.SchemaRef{UCCatalog: "c", UCSchema: "a"}
		b := backend.SchemaRef{UCCatalog: "c", UCSchema: "b"}
		m.SetFixture(a, []*backend.LiveTable{backendtest.LiveOf(config.Table{Name: "ta"})})
		m.SetFixture(b, []*backend.LiveTable{backendtest.LiveOf(config.Table{Name: "tb"})})

		got, err := live.FetchTables(context.Background(), m, []backend.SchemaRef{b, a}, parallel)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "tb", got[0][0].Name)
		assert.Equal(t, "ta", got[1][0].Name)
	}
}

func TestFetchTablesErrorNamesSchema(t *testing.T) {
	bad := backend.SchemaRef{UCCatalog: "c", UCSchema: "bad"}
	for _, parallel := range []bool{false, true} {
		fb := &failingBackend{MockUCBackend: &backendtest.MockUCBackend{}, failFor: map[backend.SchemaRef]bool{bad: true}}
		_, err := live.FetchTables(context.Background(), fb, []backend.SchemaRef{bad}, parallel)
		require.Error(t, err)
		assert.Equal(t, "list tables in c.bad: boom bad", err.Error())
	}
}

func TestFetchTablesSequentialStopsAtFirstError(t *testing.T) {
	bad := backend.SchemaRef{UCCatalog: "c", UCSchema: "a"}
	fb := &failingBackend{MockUCBackend: &backendtest.MockUCBackend{}, failFor: map[backend.SchemaRef]bool{bad: true}}
	_, err := live.FetchTables(context.Background(), fb, []backend.SchemaRef{bad, {UCCatalog: "c", UCSchema: "b"}}, false)
	require.Error(t, err)
	assert.EqualValues(t, 1, fb.calls.Load())
}

func TestFetchTablesEmpty(t *testing.T) {
	got, err := live.FetchTables(context.Background(), &backendtest.MockUCBackend{}, nil, true)
	require.NoError(t, err)
	assert.Empty(t, got)
}
