package backendtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testChange implements change.Change for testing only.
type testChange struct {
	ct          change.Kind
	describe    string
	sql         string
	destructive bool
}

func (c *testChange) Type() change.Kind     { return c.ct }
func (c *testChange) Describe() string      { return c.describe }
func (c *testChange) SQL() string           { return c.sql }
func (c *testChange) IsDestructive() bool   { return c.destructive }
func (c *testChange) TableName() string     { return "" }
func (c *testChange) GroupKey() string      { return "" }
func (c *testChange) Detail() change.Detail { return change.Detail{} }

var (
	schemaA = backend.SchemaRef{UCCatalog: "prod", UCSchema: "silver"}
	schemaB = backend.SchemaRef{UCCatalog: "prod", UCSchema: "gold"}
)

func tablesFor(names ...string) []*backend.LiveTable {
	tables := make([]*backend.LiveTable, len(names))
	for i, name := range names {
		tables[i] = &backend.LiveTable{Name: name}
	}
	return tables
}

func TestMockBackend_SetAndListFixture(t *testing.T) {
	m := &backendtest.MockUCBackend{}
	want := tablesFor("orders", "customers")
	m.SetFixture(schemaA, want)

	got, err := m.ListTables(context.Background(), schemaA)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "orders", got[0].Name)
	assert.Equal(t, "customers", got[1].Name)
}

func TestMockBackend_ListUnknownSchema(t *testing.T) {
	m := &backendtest.MockUCBackend{}

	got, err := m.ListTables(context.Background(), schemaA)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMockBackend_ListMultipleSchemas(t *testing.T) {
	m := &backendtest.MockUCBackend{}
	m.SetFixture(schemaA, tablesFor("orders"))
	m.SetFixture(schemaB, tablesFor("sessions"))

	gotA, err := m.ListTables(context.Background(), schemaA)
	require.NoError(t, err)
	require.Len(t, gotA, 1)
	assert.Equal(t, "orders", gotA[0].Name)

	gotB, err := m.ListTables(context.Background(), schemaB)
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "sessions", gotB[0].Name)
}

func TestMockBackend_RecordsApplyCalls(t *testing.T) {
	m := &backendtest.MockUCBackend{}
	c1 := &testChange{ct: change.AddColumn, describe: "add column foo STRING"}
	c2 := &testChange{ct: change.DropColumn, describe: "drop column bar"}

	err1 := m.ApplyChange(context.Background(), c1)
	err2 := m.ApplyChange(context.Background(), c2)
	require.NoError(t, err1)
	require.NoError(t, err2)

	calls := m.RecordedCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, change.AddColumn, calls[0].Kind)
	assert.Equal(t, "add column foo STRING", calls[0].Describe)
	assert.Equal(t, change.DropColumn, calls[1].Kind)
	assert.Equal(t, "drop column bar", calls[1].Describe)
}

func TestMockBackend_ApplyError(t *testing.T) {
	m := &backendtest.MockUCBackend{}
	sentinelErr := errors.New("apply failed")
	m.SetApplyError(sentinelErr)

	ch := &testChange{ct: change.AddColumn, describe: "add column x INT"}

	err := m.ApplyChange(context.Background(), ch)
	assert.ErrorIs(t, err, sentinelErr)

	// second call: error is consumed, should return nil
	err2 := m.ApplyChange(context.Background(), ch)
	assert.NoError(t, err2)
}

// LiveOf spells the live side like a definition: an unset nullable is true, resolved names are the
// real names, and a tblproperty with a nil value is absent.
func TestLiveOf(t *testing.T) {
	no := false
	v := "1"
	got := backendtest.LiveOf(config.Table{
		Name: "t", ResolvedCatalog: "c", ResolvedSchema: "s", Comment: "x",
		Columns:       []config.Column{{Name: "a", Type: typINT}, {Name: "b", Type: typINT, Nullable: &no, Comment: "k"}},
		Clustering:    &config.Clustering{Auto: true},
		Partitioning:  &config.Partitioning{Columns: []string{"a"}},
		TblProperties: map[string]*string{"p": &v, "gone": nil},
	})
	assert.Equal(t, &backend.LiveTable{
		Catalog: "c", Schema: "s", Name: "t", Comment: "x",
		Columns: []backend.LiveColumn{
			{Name: "a", Type: "INT", Nullable: true},
			{Name: "b", Type: "INT", Comment: "k", Nullable: false},
		},
		ClusterAuto:  true,
		Partitioning: []string{"a"},
		Properties:   map[string]string{"p": "1"},
	}, got)
}
