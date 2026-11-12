package backendtest

import (
	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
)

// Col returns a nullable live column without a comment.
func Col(name, typ string) backend.LiveColumn {
	return backend.LiveColumn{Name: name, Type: typ, Nullable: true}
}

// NotNull returns a copy of the live column that does not accept NULL.
func NotNull(c backend.LiveColumn) backend.LiveColumn {
	c.Nullable = false
	return c
}

// Live returns a live table of catalog.schema without comment, clustering, partitioning and
// tblproperties; set the fields of the result when a test needs them.
func Live(catalog, schema, name string, cols ...backend.LiveColumn) *backend.LiveTable {
	return &backend.LiveTable{Catalog: catalog, Schema: schema, Name: name, Columns: cols}
}

// LiveOf returns the live table that has exactly the state the definition describes: the
// real names are the resolved ones, an unset nullable is true, and a tblproperty with a nil
// value (unset in a definition) is absent. It lets a test state the live side in the same
// spelling as the desired side.
func LiveOf(t config.Table) *backend.LiveTable {
	live := &backend.LiveTable{
		Catalog: t.ResolvedCatalog,
		Schema:  t.ResolvedSchema,
		Name:    t.Name,
		Comment: t.Comment,
	}
	for _, c := range t.Columns {
		live.Columns = append(live.Columns, backend.LiveColumn{Name: c.Name, Type: c.Type, Comment: c.Comment, Nullable: c.IsNullable()})
	}
	if t.Clustering != nil {
		live.ClusterAuto = t.Clustering.Auto
		live.Clustering = append([]string(nil), t.Clustering.Columns...)
	}
	if t.Partitioning != nil {
		live.Partitioning = append([]string(nil), t.Partitioning.Columns...)
	}
	for k, v := range t.TblProperties {
		if v == nil {
			continue
		}
		if live.Properties == nil {
			live.Properties = make(map[string]string)
		}
		live.Properties[k] = *v
	}
	return live
}
