package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testdataPath returns the absolute path to a testdata subdirectory.
func testdataPath(sub string) string {
	abs, err := filepath.Abs(filepath.Join("testdata", sub))
	if err != nil {
		panic(err)
	}
	return abs
}

func TestLoadProject_ok(t *testing.T) {
	root := testdataPath("project-ok")
	proj, diags := config.LoadProject(root)
	require.False(t, diags.HasError(), diags)
	assert.Equal(t, "my-platform", proj.Project)
	assert.Equal(t, "abc123default", proj.Workspace.WarehouseID)
	// tables_dir is explicitly set; if it were absent it would default to "tables"
	assert.Equal(t, "tables", proj.TablesDir)
}

func TestLoadProject_tablesDir_defaultsToTables(t *testing.T) {
	// project-duplicate has no tables_dir field
	root := testdataPath("project-duplicate")
	proj, diags := config.LoadProject(root)
	require.False(t, diags.HasError(), diags)
	assert.Equal(t, "tables", proj.TablesDir)
}

func TestLoadProject_missing(t *testing.T) {
	_, diags := config.LoadProject("/nonexistent/path/that/does/not/exist")
	assert.True(t, diags.HasError())
}

func TestLoadProject_malformed(t *testing.T) {
	tmp := t.TempDir()
	err := os.WriteFile(filepath.Join(tmp, "ucat.yml"), []byte(":\tbad yaml: [unclosed"), 0644)
	require.NoError(t, err)
	_, diags := config.LoadProject(tmp)
	assert.True(t, diags.HasError())
}

func TestLoadCatalogs_ok(t *testing.T) {
	root := testdataPath("project-ok")
	cats, diags := config.LoadCatalogs(root)
	require.False(t, diags.HasError(), diags)
	require.Len(t, cats, 1)
	assert.Equal(t, "main", cats[0].Name)
	assert.Equal(t, "prod", cats[0].UCName)
	require.Contains(t, cats[0].Schemas, "silver")
	assert.Equal(t, "silver", cats[0].Schemas["silver"].UCName)
}

func TestLoadCatalogs_noCatalogsDir(t *testing.T) {
	// project-drop has no catalogs/ directory
	root := testdataPath("project-drop")
	cats, diags := config.LoadCatalogs(root)
	assert.False(t, diags.HasError())
	assert.Empty(t, cats)
}

// writeCatalogFiles creates catalogs/<file> under a temp root with the given contents.
func writeCatalogFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "catalogs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	return root
}

func catalogYAML(name string) string {
	return "name: " + name + "\nuc_name: prod\nschemas:\n  silver: {}\n"
}

func errorSummaries(diags diag.Diagnostics) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == diag.Error {
			out = append(out, d.Summary)
		}
	}
	return out
}

func TestLoadCatalogs_sourceFile(t *testing.T) {
	cats, diags := config.LoadCatalogs(testdataPath("project-ok"))
	require.False(t, diags.HasError(), diags)
	require.Len(t, cats, 1)
	assert.Equal(t, "catalogs/main.yml", cats[0].SourceFile)
}

func TestLoadCatalogs_duplicateName(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		pathAYml: catalogYAML("main"),
		pathBYml: catalogYAML("main"),
	})
	_, diags := config.LoadCatalogs(root)
	require.True(t, diags.HasError())
	errs := errorSummaries(diags)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], `"main"`)
	assert.Contains(t, errs[0], "a.yml")
	assert.Contains(t, errs[0], "b.yml")
}

func TestLoadCatalogs_duplicateName_threeFiles(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		pathCYml: catalogYAML("main"),
		pathAYml: catalogYAML("main"),
		pathBYml: catalogYAML("main"),
	})
	_, diags := config.LoadCatalogs(root)
	errs := errorSummaries(diags)
	require.Len(t, errs, 2, "each extra file must be reported")
	all := strings.Join(errs, "\n")
	for _, f := range []string{pathAYml, pathBYml, pathCYml} {
		assert.Contains(t, all, f)
	}
}

func TestLoadCatalogs_duplicateName_twoPairs(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		pathAYml: catalogYAML("main"),
		pathBYml: catalogYAML("main"),
		pathCYml: catalogYAML("raw"),
		"d.yml":  catalogYAML("raw"),
	})
	_, diags := config.LoadCatalogs(root)
	errs := errorSummaries(diags)
	require.Len(t, errs, 2)
	all := strings.Join(errs, "\n")
	assert.Contains(t, all, `"main"`)
	assert.Contains(t, all, `"raw"`)
}

func TestLoadCatalogs_duplicateName_deterministic(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		"c.yml":  catalogYAML("main"),
		pathAYml: catalogYAML("main"),
		pathBYml: catalogYAML("main"),
		"d.yml":  catalogYAML("raw"),
		"e.yml":  catalogYAML("raw"),
	})
	first, firstDiags := config.LoadCatalogs(root)
	for i := 0; i < 20; i++ {
		cats, diags := config.LoadCatalogs(root)
		assert.Equal(t, firstDiags, diags)
		assert.Equal(t, first, cats)
	}
}

func TestLoadCatalogs_nameDiffersFromFilename_notDuplicate(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		"a.yml": catalogYAML("alpha"),
		"b.yml": catalogYAML("beta"),
	})
	cats, diags := config.LoadCatalogs(root)
	assert.False(t, diags.HasError(), diags)
	assert.Len(t, cats, 2)
	warnings := 0
	for _, d := range diags {
		if d.Severity == diag.Warning && strings.Contains(d.Summary, "does not match filename") {
			warnings++
		}
	}
	assert.Equal(t, 2, warnings)
}

func TestLoadCatalogs_multipleDistinct(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		pathMainYml: catalogYAML("main"),
		"raw.yml":   catalogYAML("raw"),
	})
	cats, diags := config.LoadCatalogs(root)
	assert.Empty(t, diags)
	require.Len(t, cats, 2)
	assert.Equal(t, "catalogs/main.yml", cats[0].SourceFile)
	assert.Equal(t, "catalogs/raw.yml", cats[1].SourceFile)
}

func TestLoadCatalogs_emptyCatalogNoSchemas(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		"main.yml":  catalogYAML("main"),
		"empty.yml": "name: empty\nuc_name: e\n",
	})
	cats, diags := config.LoadCatalogs(root)
	assert.False(t, diags.HasError(), diags)
	require.Len(t, cats, 2)
	assert.Empty(t, cats[0].Schemas)
}

func TestLoadTarget_ok(t *testing.T) {
	root := testdataPath("project-ok")
	target, diags := config.LoadTarget(root, "dev")
	require.False(t, diags.HasError(), diags)
	assert.Equal(t, "https://dev.azuredatabricks.net", target.Workspace.Host)
	assert.Equal(t, "abc123dev", target.Workspace.WarehouseID)
	require.Contains(t, target.Catalogs, "main")
	assert.Equal(t, "dev", target.Catalogs["main"].UCName)
}

func TestLoadTarget_missing(t *testing.T) {
	root := testdataPath("project-ok")
	_, diags := config.LoadTarget(root, "nonexistent")
	assert.True(t, diags.HasError())
}

func TestLoadTables_recursive(t *testing.T) {
	root := testdataPath("project-ok")
	proj, diags := config.LoadProject(root)
	require.False(t, diags.HasError())

	tables, diags := config.LoadTables(root, proj)
	require.False(t, diags.HasError(), diags)
	assert.Len(t, tables, 3) // orders, customers, sessions
}

func TestLoadTables_duplicate(t *testing.T) {
	root := testdataPath("project-duplicate")
	proj, diags := config.LoadProject(root)
	require.False(t, diags.HasError())

	_, diags = config.LoadTables(root, proj)
	assert.True(t, diags.HasError(), "expected error for duplicate table name")
}

func TestLoadTables_dropTrue(t *testing.T) {
	root := testdataPath("project-drop")
	proj, diags := config.LoadProject(root)
	require.False(t, diags.HasError())

	tables, diags := config.LoadTables(root, proj)
	require.False(t, diags.HasError(), diags)
	require.Len(t, tables, 1)
	assert.True(t, tables[0].Drop)
	assert.Equal(t, "legacy_table", tables[0].Name)
}

func TestLoadTables_sourceFile(t *testing.T) {
	root := testdataPath("project-ok")
	proj, diags := config.LoadProject(root)
	require.False(t, diags.HasError())

	tables, diags := config.LoadTables(root, proj)
	require.False(t, diags.HasError(), diags)
	for _, tbl := range tables {
		assert.NotEmpty(t, tbl.SourceFile, "Table %q should have SourceFile set", tbl.Name)
	}
}

func TestFindProjectRoot_found(t *testing.T) {
	root := testdataPath("project-ok")
	// Call from a subdir
	subdir := filepath.Join(root, "tables", "silver")
	found, err := config.FindProjectRoot(subdir)
	require.NoError(t, err)
	assert.Equal(t, root, found)
}

func TestFindProjectRoot_notFound(t *testing.T) {
	tmp := t.TempDir()
	_, err := config.FindProjectRoot(tmp)
	assert.Error(t, err)
}

// --- strict YAML decoding ---

// writeFile writes content to root/rel, creating parent dirs.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0644))
}

// errDiags returns only Error-severity diagnostics.
func errDiags(d diag.Diagnostics) diag.Diagnostics {
	var out diag.Diagnostics
	for _, x := range d {
		if x.Severity == diag.Error {
			out = append(out, x)
		}
	}
	return out
}

// loadTablesFrom writes one table file and loads it via LoadTables.
func loadTablesFrom(t *testing.T, content string) ([]config.Table, diag.Diagnostics) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "tables/silver/orders.yml", content)
	return config.LoadTables(root, config.ProjectConfig{TablesDir: litTables})
}

// requireOneUnsupported asserts exactly one error "field <name> is not supported" located at a path ending with locSuffix.
func requireOneUnsupported(t *testing.T, d diag.Diagnostics, field, locSuffix string) {
	t.Helper()
	errs := errDiags(d)
	require.Len(t, errs, 1, errs)
	assert.Equal(t, fmt.Sprintf("field %q is not supported; if the file was written for a newer ucat, upgrade ucat", field), errs[0].Summary)
	assert.True(t, strings.HasSuffix(errs[0].Location, locSuffix), "location %q should end with %q", errs[0].Location, locSuffix)
}

func TestStrict_unknownField_allLoaders(t *testing.T) {
	cases := map[string]struct {
		field string
		loc   string
		load  func(t *testing.T) diag.Diagnostics
	}{
		litProject: {"bar", "ucat.yml:2", func(t *testing.T) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "ucat.yml", "project: p\nbar: 1\n")
			_, d := config.LoadProject(root)
			return d
		}},
		litCatalog: {"foo", "main.yml:3", func(t *testing.T) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "catalogs/main.yml", "name: main\nuc_name: dev\nfoo: 1\n")
			_, d := config.LoadCatalogs(root)
			return d
		}},
		litTarget: {"baz", "dev.yml:3", func(t *testing.T) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "targets/dev.yml", "workspace:\n  host: x\nbaz: 1\n")
			_, d := config.LoadTarget(root, "dev")
			return d
		}},
		litTable: {"tblpropertis", "orders.yml:3", func(t *testing.T) diag.Diagnostics {
			_, d := loadTablesFrom(t, "name: orders\nschema: silver\ntblpropertis:\n  a: b\n")
			return d
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requireOneUnsupported(t, tc.load(t), tc.field, tc.loc)
		})
	}
}

func TestStrict_unknownNestedField(t *testing.T) {
	t.Run("project workspace", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "ucat.yml", "project: p\nworkspace:\n  hots: x\n")
		_, d := config.LoadProject(root)
		requireOneUnsupported(t, d, "hots", "ucat.yml:3")
	})
	t.Run("catalog schema", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "catalogs/main.yml", "name: main\nuc_name: dev\nschemas:\n  silver:\n    ucname: x\n")
		_, d := config.LoadCatalogs(root)
		requireOneUnsupported(t, d, "ucname", "main.yml:5")
	})
	t.Run("target catalog", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "targets/dev.yml", "catalogs:\n  main:\n    uc_name: dev\n    extra: 1\n")
		_, d := config.LoadTarget(root, "dev")
		requireOneUnsupported(t, d, "extra", "dev.yml:4")
	})
	t.Run("table column", func(t *testing.T) {
		_, d := loadTablesFrom(t, "name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: a\n    type: INT\n    foo: 1\n")
		requireOneUnsupported(t, d, "foo", "orders.yml:8")
	})
	t.Run("table clustering", func(t *testing.T) {
		_, d := loadTablesFrom(t, "name: orders\nschema: silver\nclustering:\n  columns: [id]\n  foo: 1\n")
		requireOneUnsupported(t, d, "foo", "orders.yml:5")
	})
}

// TestLoadTables_tagsRejected is a regression for removing Table.Tags: tags used to be silently ignored.
func TestLoadTables_tagsRejected(t *testing.T) {
	_, d := loadTablesFrom(t, "name: orders\nschema: silver\ntags:\n  team: data\n")
	requireOneUnsupported(t, d, "tags", "orders.yml:3")
}

func TestStrict_twoUnknownFields_orderedByLine(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		_, d := loadTablesFrom(t, "name: orders\nzeta: 1\nschema: silver\nalpha: 2\n")
		errs := errDiags(d)
		require.Len(t, errs, 2, errs)
		assert.Equal(t, `field "zeta" is not supported; if the file was written for a newer ucat, upgrade ucat`, errs[0].Summary)
		assert.True(t, strings.HasSuffix(errs[0].Location, "orders.yml:2"), errs[0].Location)
		assert.Equal(t, `field "alpha" is not supported; if the file was written for a newer ucat, upgrade ucat`, errs[1].Summary)
		assert.True(t, strings.HasSuffix(errs[1].Location, "orders.yml:4"), errs[1].Location)
	})
	t.Run("project", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "ucat.yml", "zeta: 1\nproject: p\nalpha: 2\n")
		_, d := config.LoadProject(root)
		errs := errDiags(d)
		require.Len(t, errs, 2, errs)
		assert.Equal(t, `field "zeta" is not supported; if the file was written for a newer ucat, upgrade ucat`, errs[0].Summary)
		assert.Equal(t, `field "alpha" is not supported; if the file was written for a newer ucat, upgrade ucat`, errs[1].Summary)
		assert.True(t, strings.HasSuffix(errs[0].Location, "ucat.yml:1"), errs[0].Location)
		assert.True(t, strings.HasSuffix(errs[1].Location, "ucat.yml:3"), errs[1].Location)
	})
}

func TestStrict_emptyAndCommentOnlyFiles(t *testing.T) {
	for name, content := range map[string]string{litEmpty: "", "comments": "# nothing here\n# at all\n"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "targets/dev.yml", content)
			_, d := config.LoadTarget(root, "dev")
			assert.Empty(t, d)

			writeFile(t, root, "catalogs/main.yml", content)
			_, d = config.LoadCatalogs(root)
			assert.False(t, d.HasError(), d)

			_, d = loadTablesFrom(t, content)
			assert.False(t, d.HasError(), d)
		})
	}
}

func TestStrict_typeMismatch_hasLine(t *testing.T) {
	_, d := loadTablesFrom(t, "name: orders\nschema: silver\ncolumns: notalist\n")
	errs := errDiags(d)
	require.Len(t, errs, 1, errs)
	assert.True(t, strings.HasSuffix(errs[0].Location, "orders.yml:3"), errs[0].Location)
	assert.NotContains(t, errs[0].Summary, "is not supported")
}

func TestStrict_duplicateKey(t *testing.T) {
	_, d := loadTablesFrom(t, "name: orders\nname: other\nschema: silver\n")
	errs := errDiags(d)
	require.NotEmpty(t, errs)
	assert.True(t, strings.HasSuffix(errs[0].Location, "orders.yml:2"), errs[0].Location)
}

func TestStrict_validFixturesNoDiags(t *testing.T) {
	for _, name := range []string{"project-ok", "project-drop"} {
		t.Run(name, func(t *testing.T) {
			root := testdataPath(name)
			proj, d := config.LoadProject(root)
			assert.Empty(t, d)
			_, d = config.LoadCatalogs(root)
			assert.False(t, d.HasError(), d)
			_, d = config.LoadTables(root, proj)
			assert.Empty(t, d)
		})
	}
	_, d := config.LoadTarget(testdataPath("project-ok"), "dev")
	assert.Empty(t, d)
}

func TestStrict_syntaxErrorStillParseFailure(t *testing.T) {
	_, d := loadTablesFrom(t, "name: [unclosed\n")
	errs := errDiags(d)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Summary, "failed to parse")
}

// a YAML syntax error is located as <file>:<line> with the reason alone in the summary;
// without a line from the parser the file name stays in the summary.
func TestStrict_syntaxError_allLoaders(t *testing.T) {
	bad := "# c\nname: x\nschema: s\ncolumns: x: y\n"
	cases := map[string]struct {
		loc  string
		load func(t *testing.T, content string) diag.Diagnostics
	}{
		"project": {"ucat.yml:4", func(t *testing.T, c string) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "ucat.yml", c)
			_, d := config.LoadProject(root)
			return d
		}},
		"catalog": {"catalogs/main.yml:4", func(t *testing.T, c string) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "catalogs/main.yml", c)
			_, d := config.LoadCatalogs(root)
			return d
		}},
		"target": {"targets/dev.yml:4", func(t *testing.T, c string) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "targets/dev.yml", c)
			_, d := config.LoadTarget(root, "dev")
			return d
		}},
		"table": {"tables/silver/orders.yml:4", func(t *testing.T, c string) diag.Diagnostics {
			_, d := loadTablesFrom(t, c)
			return d
		}},
	}
	for name, tc := range cases {
		for _, content := range []string{bad, strings.ReplaceAll(bad, "\n", "\r\n")} {
			t.Run(name, func(t *testing.T) {
				errs := errDiags(tc.load(t, content))
				require.Len(t, errs, 1, errs)
				assert.Equal(t, "failed to parse: mapping values are not allowed in this context", errs[0].Summary)
				assert.Equal(t, tc.loc, errs[0].Location)
			})
		}
	}
}

func TestStrict_syntaxError_noLine(t *testing.T) {
	_, d := loadTablesFrom(t, "a: b: c\n")
	errs := errDiags(d)
	require.Len(t, errs, 1, errs)
	assert.Equal(t, "failed to parse tables/silver/orders.yml: yaml: mapping values are not allowed in this context", errs[0].Summary)
	assert.Equal(t, "tables/silver/orders.yml", errs[0].Location)
}

// a load error shows the path relative to the project root, not an absolute one.
func TestLoadTables_DecodeErrorLocationIsRelative(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tables", "s")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "t.yml"), []byte("name: t\nfoo: 1\n"), 0o644))

	_, diags := config.LoadTables(root, config.ProjectConfig{TablesDir: litTables})
	require.True(t, diags.HasError())
	assert.Equal(t, "tables/s/t.yml:2", diags[0].Location)
	assert.NotContains(t, diags[0].Location, root)
}

func TestLoadCatalogs_unreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod does not deny access on Windows or as root")
	}
	root := writeCatalogFiles(t, map[string]string{"main.yml": "name: main\nuc_name: prod\n"})
	dir := filepath.Join(root, "catalogs")
	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	cats, diags := config.LoadCatalogs(root)
	assert.Empty(t, cats)
	require.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "failed to read catalogs/")
	assert.Contains(t, diags[0].Summary, "permission denied")
	assert.NotContains(t, diags[0].Summary, root)
	assert.Equal(t, "catalogs", diags[0].Location)
}

func TestLoadCatalogs_missingDirNoError(t *testing.T) {
	cats, diags := config.LoadCatalogs(t.TempDir())
	assert.Empty(t, diags)
	assert.Empty(t, cats)
}

func TestLoadCatalogs_sortedAndSkipsDirs(t *testing.T) {
	root := writeCatalogFiles(t, map[string]string{
		"b.yml": "name: b\nuc_name: b\n",
		"a.yml": "name: a\nuc_name: a\n",
		"c.yml": "name: c\nuc_name: c\n",
	})
	require.NoError(t, os.Mkdir(filepath.Join(root, "catalogs", "x.yml"), 0o755))

	cats, diags := config.LoadCatalogs(root)
	require.False(t, diags.HasError(), diags)
	var names []string
	for _, c := range cats {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"a", "b", "c"}, names)
}

func TestLoadProject_AbsoluteTablesDir(t *testing.T) {
	for _, v := range []string{"/a", `\a`, litCX, pathCX, "c:x", `\\srv\share`} {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "ucat.yml"), []byte("project: p\ntables_dir: '"+v+"'\n"), 0644))
		_, diags := config.LoadProject(root)
		require.True(t, diags.HasError(), v)
		assert.Equal(t, `tables_dir must be relative to the project root (got "`+v+`")`, diags[0].Summary)
		assert.Equal(t, "ucat.yml:2", diags[0].Location)
	}
	for _, v := range []string{"tables", "./tables", pathAB, `tables\prod`, `..\tables`, ""} {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "ucat.yml"), []byte("project: p\ntables_dir: '"+v+"'\n"), 0644))
		_, diags := config.LoadProject(root)
		assert.False(t, diags.HasError(), v)
	}
}
