package config

import (
	"fmt"
	"maps"
	"sort"

	"github.com/ninilich/ucat/internal/diag"
)

// ResolveOptions is what Resolve takes from outside the project files, so it reads neither
// the environment nor the command line itself.
type ResolveOptions struct {
	// WarehouseID is the fallback warehouse_id, used only when neither the target nor the project sets
	// one (the caller passes the value of the DATABRICKS_WAREHOUSE_ID environment variable). A value
	// from a file always wins; EnvOverrideWarnings reports a differing one.
	WarehouseID string
	// Host is the value of DATABRICKS_HOST. Resolve does not use it (the SDK reads it only when no
	// file sets a host); EnvOverrideWarnings compares it with the host of the files.
	Host string
	// TargetName is the target selected with -t, for the file name in warnings; empty without one.
	TargetName string
	// HasTarget reports whether a target was selected (-t). Without one, UC names that only a
	// target can set are unknown and ResolvedConfig.NoTarget is set.
	HasTarget bool
}

// Resolve merges project config, catalog configs, and target overrides into a single
// ResolvedConfig. It fills in ResolvedCatalog and ResolvedSchema on each Table.
func Resolve(
	project ProjectConfig,
	catalogs []CatalogConfig,
	target TargetConfig,
	tables []Table,
	opts ResolveOptions,
) ResolvedConfig {
	// warehouse_id: first non-empty wins; the environment is the last resort
	warehouseID := firstNonEmpty(
		target.Workspace.WarehouseID,
		project.Workspace.WarehouseID,
		opts.WarehouseID,
	)

	schemas := resolveSchemas(mergeCatalogs(catalogs, target))

	return ResolvedConfig{
		WorkspaceHost: firstNonEmpty(target.Workspace.Host, project.Workspace.Host),
		WarehouseID:   warehouseID,
		Schemas:       schemas,
		Tables:        resolveTables(tables, schemas),
		NoTarget:      !opts.HasTarget,
	}
}

// EnvOverrideWarnings reports the environment variables that a project file overrides: a value from
// targets/<t>.yml or ucat.yml wins over DATABRICKS_WAREHOUSE_ID and DATABRICKS_HOST, so when the
// variable is set to a different value, the warning says which value is used. An unset variable, a
// variable equal to the file value and a value missing in the files (the variable is then the
// fallback) give no warning.
func EnvOverrideWarnings(project ProjectConfig, target TargetConfig, opts ResolveOptions) diag.Diagnostics {
	var diags diag.Diagnostics
	check := func(field, envName, env, targetValue, projectValue string) {
		file, from := targetValue, TargetFile(opts.TargetName)
		if targetValue == "" {
			file, from = projectValue, ProjectFile
		}
		if env == "" || file == "" || env == file {
			return
		}
		diags.Append(diag.Diagnostic{Severity: diag.Warning, Summary: fmt.Sprintf(
			"workspace.%s %q from %s is used; %s (%q) is ignored: a value in a project file wins over the environment",
			field, file, from, envName, env)})
	}
	check("warehouse_id", "DATABRICKS_WAREHOUSE_ID", opts.WarehouseID, target.Workspace.WarehouseID, project.Workspace.WarehouseID)
	check("host", "DATABRICKS_HOST", opts.Host, target.Workspace.Host, project.Workspace.Host)
	return diags
}

// mergeCatalogs returns the catalogs by logical name with the target overrides applied: a target
// may set uc_name, add or override schemas, or declare a catalog that catalogs/ does not have.
func mergeCatalogs(catalogs []CatalogConfig, target TargetConfig) map[string]CatalogConfig {
	catalogMap := make(map[string]CatalogConfig, len(catalogs))
	for _, cat := range catalogs {
		catalogMap[cat.Name] = cat
	}

	for logicalName, targetCat := range target.Catalogs {
		base, exists := catalogMap[logicalName]
		if !exists {
			// Target references a catalog not in catalogs/ - create it
			base = CatalogConfig{Name: logicalName}
		}
		if targetCat.UCName != "" {
			base.UCName = targetCat.UCName
		}
		if len(targetCat.Schemas) > 0 {
			merged := make(map[string]SchemaConfig, len(base.Schemas)+len(targetCat.Schemas))
			maps.Copy(merged, base.Schemas)
			maps.Copy(merged, targetCat.Schemas)
			base.Schemas = merged
		}
		catalogMap[logicalName] = base
	}
	return catalogMap
}

// resolveSchemas lists the schemas of the catalogs with their UC names, sorted by UC catalog,
// UC schema, then logical names (map iteration is random; the order must be stable for validate
// and any other consumer).
func resolveSchemas(catalogMap map[string]CatalogConfig) []ResolvedSchema {
	var resolved []ResolvedSchema
	for logicalCatalog, cat := range catalogMap {
		for schemaKey, schemaCfg := range cat.Schemas {
			ucSchema := schemaCfg.UCName
			if ucSchema == "" {
				ucSchema = schemaKey
			}
			resolved = append(resolved, ResolvedSchema{
				LogicalCatalog: logicalCatalog,
				LogicalSchema:  schemaKey,
				UCCatalog:      cat.UCName,
				UCSchema:       ucSchema,
				SourceFile:     cat.SourceFile,
			})
		}
	}

	sort.Slice(resolved, func(i, j int) bool {
		a, b := resolved[i], resolved[j]
		if a.UCCatalog != b.UCCatalog {
			return a.UCCatalog < b.UCCatalog
		}
		if a.UCSchema != b.UCSchema {
			return a.UCSchema < b.UCSchema
		}
		if a.LogicalCatalog != b.LogicalCatalog {
			return a.LogicalCatalog < b.LogicalCatalog
		}
		return a.LogicalSchema < b.LogicalSchema
	})
	return resolved
}

// resolveTables returns a copy of the tables with ResolvedCatalog and ResolvedSchema set from the
// schema their logical schema name refers to. A table whose schema is unknown keeps them empty
// (the validator reports it). An ambiguous name is rejected by ValidateLocal; if it slips
// through, the last schema wins.
func resolveTables(tables []Table, schemas []ResolvedSchema) []Table {
	lookup := make(map[string]ResolvedSchema, len(schemas))
	for _, rs := range schemas {
		lookup[rs.LogicalSchema] = rs
	}

	resolved := make([]Table, len(tables))
	copy(resolved, tables)
	for i, table := range resolved {
		if rs, ok := lookup[table.Schema]; ok {
			resolved[i].ResolvedCatalog = rs.UCCatalog
			resolved[i].ResolvedSchema = rs.UCSchema
		}
	}
	return resolved
}

// firstNonEmpty returns the first non-empty string from vals, or empty string if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
