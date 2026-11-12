package config

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// Scaffolding: the text of the files "ucat init" creates. The files are built from the same
// structs (and yaml tags) the strict loaders read, so a field renamed there is renamed here too;
// the scaffold tests load every generated file back to keep it that way.

// marshalYAML renders v with the two-space indent the project files use.
func marshalYAML(v any) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ScaffoldProject returns the text of ucat.yml for a new project.
func ScaffoldProject(project string) (string, error) {
	return marshalYAML(ProjectConfig{Project: project, TablesDir: DefaultTablesDir})
}

// ScaffoldCatalog returns the text of catalogs/<logicalCatalog>.yml with one schema.
func ScaffoldCatalog(logicalCatalog, ucCatalog, logicalSchema, ucSchema string) (string, error) {
	return marshalYAML(CatalogConfig{
		Name:    logicalCatalog,
		UCName:  ucCatalog,
		Schemas: map[string]SchemaConfig{logicalSchema: {UCName: ucSchema}},
	})
}

// targetCatalogs is the catalogs: block of a scaffolded target: the UC catalog of one logical catalog.
type targetCatalogs struct {
	Catalogs map[string]struct {
		UCName string `yaml:"uc_name"`
	} `yaml:"catalogs"`
}

// ScaffoldTarget returns the text of targets/<name>.yml: the workspace: block and the catalogs: block
// that gives the logical catalog its UC catalog in this target (the catalog file keeps the logical name
// as uc_name; the target decides the environment). An empty workspace.warehouse_id is written as a
// commented placeholder (a struct cannot carry a comment), and the workspace: block is always
// present so the placeholder has a place.
func ScaffoldTarget(ws WorkspaceConfig, logicalCatalog, ucCatalog string) (string, error) {
	body, err := marshalYAML(ws)
	if err != nil {
		return "", err
	}
	if body == "{}\n" { // no host, no warehouse
		body = ""
	}
	var b strings.Builder
	b.WriteString("workspace:\n")
	for _, line := range strings.SplitAfter(body, "\n") {
		if line != "" {
			b.WriteString("  " + line)
		}
	}
	if ws.WarehouseID == "" {
		b.WriteString("  # warehouse_id: <your-sql-warehouse-id>\n")
	}
	var tc targetCatalogs
	tc.Catalogs = map[string]struct {
		UCName string `yaml:"uc_name"`
	}{logicalCatalog: {UCName: ucCatalog}}
	catalogs, err := marshalYAML(tc)
	if err != nil {
		return "", err
	}
	b.WriteString(catalogs)
	return b.String(), nil
}
