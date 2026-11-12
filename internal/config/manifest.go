// Package config loads and validates a ucat project: the manifest (ucat.yml),
// schema files and table definitions, and resolves target-specific names.
package config

// Column represents a single table column in the manifest.
type Column struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`
	Comment    string `yaml:"comment,omitempty"`
	Nullable   *bool  `yaml:"nullable,omitempty"` // pointer: nil means true (default)
	RenameFrom string `yaml:"rename_from,omitempty"`
}

// IsNullable reports whether the column accepts NULL: an unset nullable means true.
func (c Column) IsNullable() bool {
	return c.Nullable == nil || *c.Nullable
}

// Clustering configures liquid clustering. Auto and Columns are mutually exclusive.
type Clustering struct {
	Auto    bool     `yaml:"auto,omitempty"`
	Columns []string `yaml:"columns,omitempty"`
}

// Partitioning configures Hive-style partitioning.
type Partitioning struct {
	Columns []string `yaml:"columns"`
}

// Table is the top-level struct for a table YAML file.
type Table struct {
	Name          string             `yaml:"name"`
	Schema        string             `yaml:"schema"` // logical schema name, defined in catalogs/
	Comment       string             `yaml:"comment,omitempty"`
	Columns       []Column           `yaml:"columns,omitempty"`
	Clustering    *Clustering        `yaml:"clustering,omitempty"`   // pointer: nil means no clustering
	Partitioning  *Partitioning      `yaml:"partitioning,omitempty"` // pointer: nil means no partitioning
	TblProperties map[string]*string `yaml:"tblproperties,omitempty"`
	Drop          bool               `yaml:"drop,omitempty"`
	RenameFrom    string             `yaml:"rename_from,omitempty"`

	// Resolved fields - populated by resolver, not from YAML.
	ResolvedCatalog string `yaml:"-"`
	ResolvedSchema  string `yaml:"-"`

	// SourceFile is the relative path from project root, set by loader for diagnostics.
	SourceFile string `yaml:"-"`
}

// SchemaConfig is the per-schema block inside a catalog config file.
type SchemaConfig struct {
	UCName string `yaml:"uc_name,omitempty"` // real UC schema name; defaults to the map key
}

// CatalogConfig is loaded from catalogs/<name>.yml.
type CatalogConfig struct {
	Name    string                  `yaml:"name"`              // logical catalog name - authoritative
	UCName  string                  `yaml:"uc_name"`           // real UC catalog name
	Schemas map[string]SchemaConfig `yaml:"schemas,omitempty"` // key = logical schema name

	SourceFile string `yaml:"-"` // path relative to project root; set by the loader
}

// WorkspaceConfig holds Databricks workspace connectivity settings.
type WorkspaceConfig struct {
	Host        string `yaml:"host,omitempty"`
	WarehouseID string `yaml:"warehouse_id,omitempty"`
}

// ProjectConfig is loaded from ucat.yml.
type ProjectConfig struct {
	Project   string          `yaml:"project,omitempty"`
	TablesDir string          `yaml:"tables_dir,omitempty"` // default: "tables"
	Workspace WorkspaceConfig `yaml:"workspace,omitempty"`
}

// TargetConfig is loaded from targets/<name>.yml.
// Fields here override the corresponding fields in ProjectConfig and CatalogConfig.
type TargetConfig struct {
	Workspace WorkspaceConfig          `yaml:"workspace,omitempty"`
	Catalogs  map[string]CatalogConfig `yaml:"catalogs,omitempty"` // key = logical catalog name
}

// ResolvedSchema is a fully resolved catalog+schema pair with both logical and real UC names.
type ResolvedSchema struct {
	LogicalCatalog string // e.g. "main"
	LogicalSchema  string // e.g. "silver"
	UCCatalog      string // e.g. "prod" (real UC catalog name)
	UCSchema       string // e.g. "silver" (real UC schema name)
	SourceFile     string // catalog file the schema comes from; empty if the catalog exists only in a target
}

// Source returns the catalog file for diagnostics, or "catalog:<name>" when the catalog exists only in a target.
func (rs ResolvedSchema) Source() string {
	if rs.SourceFile != "" {
		return rs.SourceFile
	}
	return "catalog:" + rs.LogicalCatalog
}

// ResolvedConfig is the output of the Resolve step.
// It contains everything the pipeline needs to talk to UC.
type ResolvedConfig struct {
	WorkspaceHost string           // Databricks workspace URL (may be empty for SDK credential chain)
	WarehouseID   string           // SQL Warehouse ID; may be empty if plan has no SQL changes
	Schemas       []ResolvedSchema // all schemas in scope
	Tables        []Table          // all tables with ResolvedCatalog and ResolvedSchema filled
	NoTarget      bool             // no -t: UC names set only in a target are unknown, so name checks that need them are skipped
}

// SchemaByLogical returns the first schema with the given logical name (a table's schema: field).
// A logical name declared in several catalogs is rejected by ValidateLocal, so after validation
// the match is unique.
func (c ResolvedConfig) SchemaByLogical(name string) (ResolvedSchema, bool) {
	for _, rs := range c.Schemas {
		if rs.LogicalSchema == name {
			return rs, true
		}
	}
	return ResolvedSchema{}, false
}
