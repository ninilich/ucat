package cmd

import (
	"path/filepath"
	"testing"
)

// projectOpts customises newProject. The zero value is the minimal valid project:
//   - ucat.yml (project testproj, tables_dir tables)
//   - catalogs/main.yml (logical "main" -> UC catalog "dev", schema "silver" -> "silver")
//   - targets/dev.yml (workspace host https://dev.example.com, warehouse wh123)
type projectOpts struct {
	Project string            // body of ucat.yml; empty = the default
	Catalog string            // body of catalogs/main.yml; empty = the default
	Target  string            // body of targets/dev.yml; empty = the default
	Files   map[string]string // more files by slash path relative to the project; they win over the three above
}

const (
	defaultProjectYAML = "project: testproj\ntables_dir: tables\n"
	defaultCatalogYAML = "name: main\nuc_name: dev\nschemas:\n  silver:\n    uc_name: silver\n"
	defaultTargetYAML  = "workspace:\n  host: https://dev.example.com\n  warehouse_id: wh123\n"
)

// newProject writes a project into a fresh temp directory, makes it the working directory for the
// rest of the test (t.Chdir, restored by the testing package; so such tests cannot be parallel) and
// returns the directory.
func newProject(t *testing.T, opts projectOpts) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		pathProjectFile: orDefault(opts.Project, defaultProjectYAML),
		pathCatalogMain: orDefault(opts.Catalog, defaultCatalogYAML),
		pathTargetDev:   orDefault(opts.Target, defaultTargetYAML),
	}
	for rel, content := range opts.Files {
		files[rel] = content
	}
	for rel, content := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
	t.Chdir(dir)
	return dir
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// emptyDir makes a fresh empty temp directory (no ucat project) the working directory for the rest
// of the test and returns it.
func emptyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// Fixture projects: named presets of newProject.

// tableYAMLOf is a one-column table file body: name, schema, id BIGINT.
func tableYAMLOf(name, schema string) string {
	return "name: " + name + "\nschema: " + schema + "\ncolumns:\n  - name: id\n    type: BIGINT\n"
}

// setupTestProject is the minimal valid project without tables.
func setupTestProject(t *testing.T) string {
	t.Helper()
	return newProject(t, projectOpts{})
}

// setupProjectWithCatalog is setupTestProject with a custom catalogs/main.yml body.
func setupProjectWithCatalog(t *testing.T, catalogYAML string) string {
	t.Helper()
	return newProject(t, projectOpts{Catalog: catalogYAML})
}

// setupPlanProject is the minimal project with one table: orders in silver, one column "id BIGINT".
func setupPlanProject(t *testing.T) string {
	t.Helper()
	return newProject(t, projectOpts{Files: map[string]string{
		pathSilverOrders: tableYAMLOf("orders", "silver"),
	}})
}

// setupTwoTableProject is the minimal project with two tables in silver: orders and customers.
func setupTwoTableProject(t *testing.T) string {
	t.Helper()
	return newProject(t, projectOpts{Files: map[string]string{
		pathSilverOrders:              tableYAMLOf("orders", "silver"),
		"tables/silver/customers.yml": tableYAMLOf("customers", "silver"),
	}})
}

// setupMultiSchemaProject is a project with 3 schemas (silver, gold, bronze) and one table
// ("events") in each, so the pipeline actually fetches all three.
func setupMultiSchemaProject(t *testing.T) string {
	t.Helper()
	files := map[string]string{}
	for _, schema := range []string{litSilver, litGold, litBronze} {
		files["tables/"+schema+"/events.yml"] = tableYAMLOf("events", schema)
	}
	return newProject(t, projectOpts{
		Catalog: "name: main\nuc_name: dev\nschemas:\n" +
			"  silver:\n    uc_name: silver\n" +
			"  gold:\n    uc_name: gold\n" +
			"  bronze:\n    uc_name: bronze\n",
		Files: files,
	})
}
