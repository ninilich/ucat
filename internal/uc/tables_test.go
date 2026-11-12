package uc

import (
	"fmt"
	"testing"

	"github.com/databricks/databricks-sdk-go/service/catalog"
	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClusteringColumns(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"nested two", litIDCreatedAt, []string{"id", litCreatedAt}},
		{"nested one camelCase", `[["asOfDate"]]`, []string{"asOfDate"}},
		{"nested field path", `[["a","b"]]`, []string{"a.b"}},
		{"nested mixed path", `[["a","b"],["c"]]`, []string{"a.b", "c"}},
		{"flat json", `["a","b"]`, []string{"a", "b"}},
		{"plain csv", `a,b`, []string{"a", "b"}},
		{"plain csv spaces", `a, b`, []string{"a", "b"}},
		{"nested with whitespace", ` [ ["id"] , ["x"] ] `, []string{"id", "x"}},
		{"unicode", `[["имя"],["列"]]`, []string{"имя", "列"}},
		{"empty array", `[]`, nil},
		{"empty nested", litEmptyNested, nil},
		{"empty string", ``, nil},
		{"blank", `  `, nil},
		{"nested null", `[null]`, nil},
		{"nested inner null", `[[null]]`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseClusteringColumns(tt.raw)
			if len(tt.want) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseClusteringColumns_NeverGarbage(t *testing.T) {
	for _, raw := range []string{`[]`, `[[]]`, ``, `  `, `[["id"`, `[[null]]`, `[null]`, `[[""]]`} {
		for _, c := range parseClusteringColumns(raw) {
			assert.NotContains(t, c, "[", "raw %q produced garbage %q", raw, c)
			assert.NotContains(t, c, `"`, "raw %q produced garbage %q", raw, c)
			assert.NotEmpty(t, c, "raw %q produced empty column", raw)
		}
	}
}

func tableInfoWithProps(props map[string]string) catalog.TableInfo {
	return catalog.TableInfo{
		Name:        litOrders,
		CatalogName: litProd,
		SchemaName:  litSilver,
		Columns: []catalog.ColumnInfo{
			{Name: "id", TypeText: typBIGINT, Position: 0},
			{Name: litCreatedAt, TypeText: "TIMESTAMP", Position: 1},
		},
		Properties: props,
	}
}

func TestMapTableInfo_NestedClusteringColumns(t *testing.T) {
	tbl := mapTableInfo(tableInfoWithProps(map[string]string{litClusteringColumns: `[["id"],["created_at"]]`}))
	assert.False(t, tbl.ClusterAuto)
	assert.Equal(t, []string{"id", "created_at"}, tbl.Clustering)
}

func TestMapTableInfo_ClusterByAutoWins(t *testing.T) {
	tbl := mapTableInfo(tableInfoWithProps(map[string]string{
		"clusterByAuto":      "true",
		litClusteringColumns: `[["id"]]`,
	}))
	assert.True(t, tbl.ClusterAuto)
	assert.Empty(t, tbl.Clustering)
}

func TestMapTableInfo_EmptyClusteringColumnsYieldsNoClustering(t *testing.T) {
	for _, raw := range []string{`[]`, `[[]]`} {
		tbl := mapTableInfo(tableInfoWithProps(map[string]string{litClusteringColumns: raw}))
		assert.False(t, tbl.HasClustering(), "raw %q", raw)
	}
}

// Pipeline: live table built from the UC property, diffed by the real differ.
func TestDiffSchema_ClusteringFromUCProperty(t *testing.T) {
	live := mapTableInfo(tableInfoWithProps(map[string]string{"clusteringColumns": `[["id"],["created_at"]]`}))

	desired := func(cols ...string) config.Table {
		return config.Table{
			Name:            litOrders,
			Schema:          litSilver,
			ResolvedCatalog: litProd,
			ResolvedSchema:  litSilver,
			Columns: []config.Column{
				{Name: "id", Type: typBIGINT},
				{Name: "created_at", Type: "TIMESTAMP"},
			},
			Clustering: &config.Clustering{Columns: cols},
		}
	}
	countClustering := func(changes []change.Change) int {
		n := 0
		for _, c := range changes {
			if _, ok := c.(*change.AlterClusteringChange); ok {
				n++
			}
		}
		return n
	}

	t.Run("same columns is idempotent", func(t *testing.T) {
		changes, _ := differ.DiffSchema([]config.Table{desired("id", "created_at")}, []*backend.LiveTable{live})
		assert.Equal(t, 0, countClustering(changes))
	})
	t.Run("different columns produces one change", func(t *testing.T) {
		changes, _ := differ.DiffSchema([]config.Table{desired("id")}, []*backend.LiveTable{live})
		assert.Equal(t, 1, countClustering(changes))
	})
}

// Equal or zero positions keep the order of the API response (stable sort); partition columns too.
// 40 columns: sort.Slice would not be stable here (small slices use insertion sort, which is).
func TestMapTableInfo_StableOrderForEqualPositions(t *testing.T) {
	var want, wantPart []string
	cols := make([]catalog.ColumnInfo, 0, 40)
	for i := 0; i < 40; i++ {
		n := fmt.Sprintf("c%02d", (i*7)%40)
		ci := catalog.ColumnInfo{Name: n, TypeText: typINT, Position: i % 4}
		if i%3 == 0 {
			ci.PartitionIndex = 1 + i%2
		}
		cols = append(cols, ci)
	}
	// expected: by position, response order inside one position
	for pos := 0; pos < 4; pos++ {
		for _, ci := range cols {
			if int(ci.Position) == pos {
				want = append(want, ci.Name)
			}
		}
	}
	tbl := mapTableInfo(catalog.TableInfo{Name: "t", Columns: cols})
	names := make([]string, len(tbl.Columns))
	for i, c := range tbl.Columns {
		names[i] = c.Name
	}
	assert.Equal(t, want, names)
	for idx := 1; idx <= 2; idx++ {
		for _, ci := range cols {
			if ci.PartitionIndex == idx {
				wantPart = append(wantPart, ci.Name)
			}
		}
	}
	assert.Equal(t, wantPart, tbl.Partitioning)

	// distinct positions still win over response order
	tbl = mapTableInfo(catalog.TableInfo{Name: "t", Columns: []catalog.ColumnInfo{
		{Name: "b", TypeText: typINT, Position: 1}, {Name: "a", TypeText: "INT", Position: 0},
	}})
	require.Len(t, tbl.Columns, 2)
	assert.Equal(t, "a", tbl.Columns[0].Name)
}

// mapTableInfo fills every field of a LiveTable from the SDK description: real names, columns
// with a plain nullable bool, partitions, clustering and only the user-defined properties.
func TestMapTableInfo_FillsLiveTable(t *testing.T) {
	got := mapTableInfo(catalog.TableInfo{
		Name: "orders", CatalogName: "prod", SchemaName: "silver", Comment: "the orders",
		Columns: []catalog.ColumnInfo{
			{Name: "b", TypeText: "STRING", Position: 1, Nullable: true, Comment: "bee"},
			{Name: "a", TypeText: "BIGINT", Position: 0, Nullable: false, PartitionIndex: 1},
		},
		Properties: map[string]string{"clusteringColumns": `[["a"],["b"]]`, "clusteringColumnsIndexed": "x", litTeam: litData},
	})
	assert.Equal(t, &backend.LiveTable{
		Catalog: "prod", Schema: "silver", Name: "orders", Comment: "the orders",
		Columns: []backend.LiveColumn{
			{Name: "a", Type: "BIGINT", Nullable: false},
			{Name: "b", Type: "STRING", Comment: "bee", Nullable: true},
		},
		Clustering:   []string{"a", "b"},
		Partitioning: []string{"a"},
		Properties:   map[string]string{"team": "data"},
	}, got)
}

// A table without properties, partitions or clustering maps to the zero values, not empty containers.
func TestMapTableInfo_BareTable(t *testing.T) {
	got := mapTableInfo(catalog.TableInfo{Name: "t", CatalogName: "c", SchemaName: "s"})
	assert.Equal(t, &backend.LiveTable{Catalog: "c", Schema: "s", Name: "t"}, got)
	assert.False(t, got.HasClustering())
	assert.Empty(t, got.NestedClusteringField())
}
