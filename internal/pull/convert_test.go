package pull

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
)

func filterOf(t *testing.T, spec string) *Filter {
	t.Helper()
	f, err := ParseFilter(spec)
	require.NoError(t, err)
	return f
}

func TestLiveToDesired_nullable(t *testing.T) {
	live := &backend.LiveTable{Name: "t", Columns: []backend.LiveColumn{
		{Name: "n", Type: "INT", Nullable: true},
		{Name: "r", Type: "INT", Nullable: false},
	}}
	d, diags := liveToDesired(live, "silver", "x.yml", filterOf(t, ""))
	assert.Empty(t, diags)
	require.Len(t, d.Columns, 2)
	assert.Nil(t, d.Columns[0].Nullable, "nullable: true is the default and is not written")
	require.NotNil(t, d.Columns[1].Nullable)
	assert.False(t, *d.Columns[1].Nullable)
	assert.True(t, d.Columns[0].IsNullable())
	assert.False(t, d.Columns[1].IsNullable())
}

func TestLiveToDesired_charTypesAndComments(t *testing.T) {
	live := &backend.LiveTable{Name: "t", Comment: "", Columns: []backend.LiveColumn{
		{Name: "c", Type: "CHAR(3)", Comment: "keep", Nullable: true},
		{Name: "v", Type: " varchar ( 10 ) ", Nullable: true},
		{Name: "n", Type: "ARRAY<VARCHAR(5)>", Nullable: true},
	}}
	d, diags := liveToDesired(live, "s", "x.yml", filterOf(t, ""))
	assert.Equal(t, "", d.Comment, "empty comment stays empty (omitted in YAML)")
	assert.Equal(t, []config.Column{
		{Name: "c", Type: "STRING", Comment: "keep"},
		{Name: "v", Type: "STRING"},
		{Name: "n", Type: "ARRAY<VARCHAR(5)>"},
	}, d.Columns)
	require.Len(t, diags, 2, "one warning per converted column, none for a nested type")
	for _, dg := range diags {
		assert.Equal(t, diag.Warning, dg.Severity)
		assert.Equal(t, "x.yml", dg.Location)
	}
	assert.Equal(t, `table "t", column "c": CHAR(3) pulled as STRING (length limit is dropped)`, diags[0].Summary)
}

func TestLiveToDesired_filterSelectsProperties(t *testing.T) {
	live := &backend.LiveTable{Name: "t", Properties: map[string]string{"owner": "a", "delta.x": "1"}}
	d, _ := liveToDesired(live, "s", "x.yml", filterOf(t, ""))
	assert.Nil(t, d.TblProperties, "no filter writes nothing")
	d, _ = liveToDesired(live, "s", "x.yml", filterOf(t, "owner"))
	require.Len(t, d.TblProperties, 1)
	assert.Equal(t, "a", *d.TblProperties["owner"])
	assert.Equal(t, map[string]string{"owner": "a", "delta.x": "1"}, live.Properties, "the live table is not modified")
}

func TestLiveToDesired_clustering(t *testing.T) {
	t.Run("columns", func(t *testing.T) {
		live := &backend.LiveTable{Name: "t", Clustering: []string{"a", "b"}}
		d, diags := liveToDesired(live, "s", "x.yml", filterOf(t, ""))
		assert.Empty(t, diags)
		assert.Equal(t, &config.Clustering{Columns: []string{"a", "b"}}, d.Clustering)
	})
	t.Run("auto", func(t *testing.T) {
		d, _ := liveToDesired(&backend.LiveTable{Name: "t", ClusterAuto: true}, "s", "x.yml", filterOf(t, ""))
		assert.Equal(t, &config.Clustering{Auto: true}, d.Clustering)
	})
	t.Run("nested field is not written, one warning", func(t *testing.T) {
		live := &backend.LiveTable{Name: "t", Clustering: []string{"a", "b.c", "d.e"}}
		d, diags := liveToDesired(live, "s", "x.yml", filterOf(t, ""))
		assert.Nil(t, d.Clustering)
		require.Len(t, diags, 1)
		assert.Equal(t, `table "t": clustering on nested field "b.c" is not supported; clustering is not written`, diags[0].Summary)
	})
	t.Run("none", func(t *testing.T) {
		d, diags := liveToDesired(&backend.LiveTable{Name: "t"}, "s", "x.yml", filterOf(t, ""))
		assert.Nil(t, d.Clustering)
		assert.Empty(t, diags)
	})
}

func TestLiveToDesired_namesPartitioningAndNoUCNames(t *testing.T) {
	live := &backend.LiveTable{Catalog: "prod", Schema: "silver_uc", Name: "t", Comment: "c", Partitioning: []string{"p"}}
	d, _ := liveToDesired(live, "silver", "x.yml", filterOf(t, ""))
	assert.Equal(t, config.Table{Name: "t", Schema: "silver", Comment: "c", Partitioning: &config.Partitioning{Columns: []string{"p"}}}, d,
		"the logical schema is written; UC names (catalog, schema) are not part of a file")
}

// A type that ucat validate does not support is written as UC returned it, with a warning
// (config.ColumnTypeError is the check validate uses); a supported type gets none.
func TestLiveToDesired_unsupportedTypeWarns(t *testing.T) {
	live := &backend.LiveTable{Name: "t", Columns: []backend.LiveColumn{
		{Name: "ok", Type: "map<string,array<varchar(5)>>", Nullable: true},
		{Name: "coll", Type: "string collate UTF8_LCASE", Nullable: true},
		{Name: "field", Type: "struct<`my field`:int>", Nullable: true},
		{Name: "nested", Type: "array<integer>", Nullable: true},
	}}
	d, diags := liveToDesired(live, "s", "tables/s/t.yml", filterOf(t, ""))
	require.Len(t, d.Columns, 4, "the columns are written as they are")
	assert.Equal(t, "string collate UTF8_LCASE", d.Columns[1].Type)
	require.Len(t, diags, 3)
	for i, want := range []struct{ col, typ string }{{"coll", "string collate UTF8_LCASE"}, {"field", "struct<`my field`:int>"}, {"nested", "array<integer>"}} {
		assert.Equal(t, diag.Warning, diags[i].Severity)
		assert.Equal(t, "tables/s/t.yml", diags[i].Location)
		assert.Equal(t, `table "t", column "`+want.col+`": type "`+want.typ+`" is not supported by ucat validate; fix it in the file`, diags[i].Summary)
	}
}
