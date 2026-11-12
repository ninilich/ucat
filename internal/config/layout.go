package config

import (
	"path"
	"strings"
)

// Project layout: the one place that knows where ucat looks for (and writes) its files.
// Paths returned by the functions below are relative to the project root and use "/"
// on every OS (see SlashPath); callers join them to a root with filepath.FromSlash.
const (
	// ProjectFile is the manifest in the project root.
	ProjectFile = "ucat.yml"
	// CatalogsDir holds one <catalog>.yml per logical catalog.
	CatalogsDir = "catalogs"
	// TargetsDir holds one <target>.yml per deployment target.
	TargetsDir = "targets"
	// DefaultTablesDir is the tables directory when ucat.yml sets no tables_dir.
	DefaultTablesDir = "tables"
	// YAMLExt is the extension of every ucat YAML file; *.yaml files are ignored.
	YAMLExt = ".yml"
)

// CatalogFile returns the project-relative path of the catalog file for a logical catalog name.
func CatalogFile(name string) string { return CatalogsDir + "/" + name + YAMLExt }

// TargetFile returns the project-relative path of the target file for a target name.
func TargetFile(name string) string { return TargetsDir + "/" + name + YAMLExt }

// TableFile returns the project-relative path of the table file for a table in a logical schema,
// under tablesDir (ProjectConfig.TablesDir, either slash spelling): <tablesDir>/<schema>/<name>.yml.
func TableFile(tablesDir, schema, name string) string {
	return path.Join(SlashPath(tablesDir), schema, name+YAMLExt)
}

// hasYAMLExt reports whether a file name ends in YAMLExt.
func hasYAMLExt(name string) bool { return strings.HasSuffix(name, YAMLExt) }
