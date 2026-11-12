package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Part boundaries of a table key matter at loader level: (schema "a", name "bc") and
// (schema "ab", name "c") are different tables, not a duplicate.
func TestLoadTables_keyBoundaryNotADuplicate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tables/x/1.yml", "schema: a\nname: bc\n")
	writeFile(t, root, "tables/x/2.yml", "schema: ab\nname: c\n")
	tables, diags := config.LoadTables(root, config.ProjectConfig{TablesDir: litTables})
	assert.Empty(t, diags, "%v", diags)
	assert.Len(t, tables, 2)

	// the same pair twice is still a duplicate
	writeFile(t, root, "tables/x/3.yml", "schema: a\nname: bc\n")
	_, diags = config.LoadTables(root, config.ProjectConfig{TablesDir: litTables})
	assert.True(t, diags.HasError(), "%v", diags)
}

// Same at validator level (check 2): ("ab","c","x") and ("a","bc","x") are different resolved tables.
func TestValidateLocal_resolvedKeyBoundaryNotADuplicate(t *testing.T) {
	a, b := resolvedTable("x"), resolvedTable("x")
	a.ResolvedCatalog, a.ResolvedSchema = "ab", "c"
	b.ResolvedCatalog, b.ResolvedSchema = "a", "bc"
	diags := config.ValidateLocal(resolvedConfig([]config.Table{a, b}))
	assert.False(t, hasErrorContaining(diags, "duplicate table"), "%v", diags)

	c := resolvedTable("x")
	c.ResolvedCatalog, c.ResolvedSchema = "ab", "c"
	diags = config.ValidateLocal(resolvedConfig([]config.Table{a, c}))
	assert.True(t, hasErrorContaining(diags, "duplicate table"), "%v", diags)
}

func TestLoadProject_emptyFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "ucat.yml", "")
	_, diags := config.LoadProject(root)
	// whatever the verdict is, it is deterministic, has a location and never panics
	for _, d := range diags {
		assert.Equal(t, "ucat.yml", d.Location[:len("ucat.yml")], "%v", d)
		assert.NotContains(t, d.Summary, root)
	}
}

func TestLoadProject_tablesDirBackslash(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "ucat.yml", "project: p\ntables_dir: 'defs\\tables'\n")
	proj, diags := config.LoadProject(root)
	require.False(t, diags.HasError(), "%v", diags)
	assert.Equal(t, "defs/tables", proj.TablesDir)

	writeFile(t, root, "defs/tables/s/t.yml", "schema: s\nname: t\n")
	tables, diags := config.LoadTables(root, proj)
	require.False(t, diags.HasError(), "%v", diags)
	require.Len(t, tables, 1)
	assert.Equal(t, "defs/tables/s/t.yml", tables[0].SourceFile)
	assert.Equal(t, tables[0].SourceFile, config.TableFile(proj.TablesDir, "s", "t"))
}

func TestLoadProject_unreadableUcatYml(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("chmod does not deny access")
	}
	root := t.TempDir()
	writeFile(t, root, "ucat.yml", "project: p\n")
	p := filepath.Join(root, "ucat.yml")
	require.NoError(t, os.Chmod(p, 0o000))
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	_, diags := config.LoadProject(root)
	require.Len(t, diags, 1)
	assert.Equal(t, "failed to read ucat.yml: open: permission denied", diags[0].Summary)
}

// Unicode names: FoldName is strings.ToLower, independent of the locale in the environment.
func TestValidateLocal_unicodeNames(t *testing.T) {
	check := func(a, b string) bool {
		tbl := resolvedTable("orders")
		tbl.Columns = []config.Column{{Name: a, Type: typBIGINT}, {Name: b, Type: typBIGINT}}
		return hasErrorContaining(config.ValidateLocal(resolvedConfig([]config.Table{tbl})), "duplicate column")
	}
	for _, env := range []string{"C", "tr_TR.UTF-8", "en_US.UTF-8", "lt_LT.UTF-8"} {
		t.Setenv("LC_ALL", env)
		t.Setenv("LANG", env)
		assert.True(t, check("İ", "i"), "İ folds to i (%s)", env)
		assert.True(t, check("K", "k"), "Kelvin sign folds to k (%s)", env)
		assert.False(t, check("ſ", "s"), "long s is not s (%s)", env)
		assert.False(t, check("ß", "ss"), "sharp s is not ss (%s)", env)
		assert.False(t, check("ß", "SS"), "sharp s is not SS (%s)", env)
		assert.True(t, check("Ä", "ä"), "(%s)", env)
	}
	// lowercase check: ſ and ß are already lowercase, İ is not
	for name, wantErr := range map[string]bool{"ſ": false, "ß": false, "İ": true} {
		tbl := resolvedTable(name)
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		assert.Equal(t, wantErr, hasErrorContaining(diags, "must be lowercase"), "table %q: %v", name, diags)
	}
}

func TestValidateLocal_schemaNameWithDot(t *testing.T) {
	cfg := resolvedConfig(nil)
	cfg.Schemas = []config.ResolvedSchema{{LogicalCatalog: litMain, LogicalSchema: litAB2, UCCatalog: litProd, UCSchema: litAB2}}
	tbl := resolvedTable("orders")
	tbl.Schema, tbl.ResolvedSchema = "a.b", "a.b"
	cfg.Tables = []config.Table{tbl}
	diags := config.ValidateLocal(cfg) // must not panic; the table's schema ref resolves
	assert.False(t, hasErrorContaining(diags, "unknown schema ref"), "%v", diags)
	assert.True(t, config.HasDotOrSpace("a.b"))
}

// rename_from of a different case is "same as name" (warning), not a rename and not an error.
func TestValidateLocal_renameFromCaseOnly(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: "id", Type: typBIGINT, RenameFrom: "ID"}, {Name: "Ñu", Type: typINT, RenameFrom: "ñU"}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, hasErrorContaining(diags, "rename_from"), "%v", diags)
	assert.True(t, hasWarningContaining(diags, `column "id": rename_from is same as name`), "%v", diags)
	assert.True(t, hasWarningContaining(diags, `column "Ñu": rename_from is same as name`), "%v", diags)
}
