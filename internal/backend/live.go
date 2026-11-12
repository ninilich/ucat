package backend

import "strings"

// LiveColumn is a column of a live UC table. Unlike config.Column every field is the plain value
// UC reported: Nullable is a bool (UC always says), not a pointer where nil means "default".
type LiveColumn struct {
	Name     string
	Type     string
	Comment  string // "" means no comment
	Nullable bool
}

// LiveTable is a managed table as UC reports it. It is the live counterpart of config.Table (the
// desired state from YAML) and carries only what UC knows: the real catalog and schema names,
// plain values with no "unset" markers, and no desired-only fields (logical schema, drop,
// rename_from, source file).
type LiveTable struct {
	Catalog string // real UC catalog name
	Schema  string // real UC schema name
	Name    string
	Comment string // "" means no comment

	Columns []LiveColumn // in UC column order

	// ClusterAuto is set for automatic liquid clustering; Clustering holds the clustering columns
	// otherwise. A column that is a nested field is reported as a path joined with dots ("a.b").
	ClusterAuto bool
	Clustering  []string

	Partitioning []string // partition columns in partition order

	// Properties are the user-defined tblproperties: the system keys of clustering
	// (IsSystemTblPropKey) are already removed. A property is either present with a value or absent.
	Properties map[string]string
}

// HasClustering reports whether the table has liquid clustering (automatic or by columns).
func (t *LiveTable) HasClustering() bool {
	return t.ClusterAuto || len(t.Clustering) > 0
}

// NestedClusteringField returns the first clustering column that is a nested field path ("a.b"), or "".
// v1 does not support clustering on nested fields, so differ keeps such clustering and pull does not write it.
func (t *LiveTable) NestedClusteringField() string {
	for _, col := range t.Clustering {
		if strings.Contains(col, ".") {
			return col
		}
	}
	return ""
}
