package uc

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	databricks "github.com/databricks/databricks-sdk-go"
	"github.com/databricks/databricks-sdk-go/service/catalog"
	"github.com/ninilich/ucat/internal/backend"
)

// listTables fetches all MANAGED tables in the given UC schema via the SDK.
// Returns only MANAGED tables — external tables, views, streaming tables are skipped.
func listTables(
	ctx context.Context,
	client *databricks.WorkspaceClient,
	schema backend.SchemaRef,
) ([]*backend.LiveTable, error) {
	tables, err := client.Tables.ListAll(ctx, catalog.ListTablesRequest{
		CatalogName: schema.UCCatalog,
		SchemaName:  schema.UCSchema,
	})
	if err != nil {
		return nil, err
	}

	var result []*backend.LiveTable
	for _, ti := range tables {
		if ti.TableType != catalog.TableTypeManaged {
			continue
		}
		result = append(result, mapTableInfo(ti))
	}
	return result, nil
}

// mapTableInfo converts the SDK description of a table into a backend.LiveTable: columns in
// position order, partition columns in partition order, clustering from the system properties
// and only the user-defined tblproperties.
func mapTableInfo(ti catalog.TableInfo) *backend.LiveTable {
	t := &backend.LiveTable{
		Catalog: ti.CatalogName,
		Schema:  ti.SchemaName,
		Name:    ti.Name,
		Comment: ti.Comment,
	}

	cols := append([]catalog.ColumnInfo(nil), ti.Columns...)
	sort.SliceStable(cols, func(i, j int) bool { return cols[i].Position < cols[j].Position })
	for _, c := range cols {
		t.Columns = append(t.Columns, backend.LiveColumn{
			Name:     c.Name,
			Type:     c.TypeText,
			Comment:  c.Comment,
			Nullable: c.Nullable,
		})
	}

	t.Partitioning = partitionColumns(ti.Columns)

	props := ti.Properties
	if props["clusterByAuto"] == "true" {
		t.ClusterAuto = true
	} else if raw, ok := props["clusteringColumns"]; ok && raw != "" {
		t.Clustering = parseClusteringColumns(raw)
	}

	// Properties: keep only user-defined properties.
	for k, v := range props {
		if backend.IsSystemTblPropKey(k) {
			continue
		}
		if t.Properties == nil {
			t.Properties = make(map[string]string)
		}
		t.Properties[k] = v
	}
	return t
}

// partitionColumns returns the names of the partition columns ordered by PartitionIndex (equal
// indexes keep the response order), or nil.
func partitionColumns(cols []catalog.ColumnInfo) []string {
	var part []catalog.ColumnInfo
	for _, c := range cols {
		if c.PartitionIndex > 0 {
			part = append(part, c)
		}
	}
	if len(part) == 0 {
		return nil
	}
	sort.SliceStable(part, func(i, j int) bool { return part[i].PartitionIndex < part[j].PartitionIndex })
	names := make([]string, len(part))
	for i, c := range part {
		names[i] = c.Name
	}
	return names
}

// parseClusteringColumns parses the clusteringColumns property value.
// DBR 14.3+ returns nested field paths ([["a"],["b","c"]]); flat JSON and comma-separated are fallbacks.
func parseClusteringColumns(raw string) []string {
	raw = strings.TrimSpace(raw)
	var nested [][]string
	if err := json.Unmarshal([]byte(raw), &nested); err == nil {
		var result []string
		for _, path := range nested {
			if name := strings.Join(path, "."); name != "" {
				result = append(result, name)
			}
		}
		return result
	}
	var flat []string
	if err := json.Unmarshal([]byte(raw), &flat); err == nil {
		var result []string
		for _, name := range flat {
			if name != "" {
				result = append(result, name)
			}
		}
		return result
	}
	if strings.HasPrefix(raw, "[") {
		return nil // malformed JSON: never split it into garbage names
	}
	parts := strings.Split(strings.TrimSpace(raw), ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
