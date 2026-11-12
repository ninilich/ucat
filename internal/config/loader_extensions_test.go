package config_test

// loader_extensions_test.go: x- keys, a second YAML document, ignored *.yaml files and the
// shared-catalog warning of the loaders.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
)

const unknownHint = "if the file was written for a newer ucat, upgrade ucat"

func warnSummaries(d diag.Diagnostics) []string {
	var out []string
	for _, x := range d {
		if x.Severity == diag.Warning {
			out = append(out, x.Summary+" @"+x.Location)
		}
	}
	return out
}

// An x- key is ignored where it is allowed: the top level of every file, a table, a column and a catalog
// (in a catalog file and in a target): no error, no warning, and the rest of the file is read.
func TestExtensionKeys_AcceptedWhereAllowed(t *testing.T) {
	cases := map[string]string{
		litProject: "project: p\nx-owner: team-a\n",
		litCatalog: "name: main\nuc_name: dev\nx-note: shared\nschemas:\n  silver: {}\n",
		litTarget:  "x-note: n\nworkspace:\n  host: https://x\ncatalogs:\n  main:\n    x-note: n\n    uc_name: dev\n",
		litTable:   "x-owner: team-a\nname: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n    x-pii: false\n",
	}
	byName := map[string]syntaxLoader{}
	for _, l := range syntaxLoaders() {
		byName[l.name] = l
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, byName[name].load(t, content))
		})
	}
}

// x- keys carry anchors: a mapping under an x- key can be merged into a table.
func TestExtensionKeys_AnchorsStillWork(t *testing.T) {
	tables, d := loadTablesFrom(t, "x-defaults: &d\n  comment: shared\nname: orders\nschema: silver\n<<: *d\n")
	assert.Empty(t, d)
	require.Len(t, tables, 1)
	assert.Equal(t, "shared", tables[0].Comment)
}

// Everywhere else an x- key is as unknown as any other key; so are x without the dash and other cases.
func TestExtensionKeys_RejectedElsewhere(t *testing.T) {
	cases := []struct {
		name, content, field string
		line                 int
	}{
		{"workspace of ucat.yml", "project: p\nworkspace:\n  x-a: 1\n", litXA, 3},
		{"clustering", "name: t\nschema: silver\nclustering:\n  x-a: 1\n", litXA, 4},
		{"partitioning", "name: t\nschema: silver\npartitioning:\n  x-a: 1\n", litXA, 4},
		{"schema of a catalog", "name: main\nuc_name: dev\nschemas:\n  silver:\n    x-a: 1\n", litXA, 5},
		{"no dash", "name: t\nschema: silver\nxfoo: 1\n", "xfoo", 3},
		{"upper case", "name: t\nschema: silver\nX-foo: 1\n", "X-foo", 3},
		{"x- in the middle", "name: t\nschema: silver\nfoo-x-bar: 1\n", "foo-x-bar", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d diag.Diagnostics
			switch {
			case strings.HasPrefix(tc.content, "project"):
				root := t.TempDir()
				writeFile(t, root, "ucat.yml", tc.content)
				_, d = config.LoadProject(root)
			case strings.HasPrefix(tc.content, "name: main"):
				root := t.TempDir()
				writeFile(t, root, "catalogs/main.yml", tc.content)
				_, d = config.LoadCatalogs(root)
			default:
				_, d = loadTablesFrom(t, tc.content)
			}
			e := oneError(t, d)
			assert.Equal(t, `field "`+tc.field+`" is not supported; `+unknownHint, e.Summary)
			assert.True(t, strings.HasSuffix(e.Location, ":"+strconv.Itoa(tc.line)), e.Location)
		})
	}
}

// A real unknown field next to an x- key is the only error, at its own line.
func TestExtensionKeys_MixedWithUnknownField(t *testing.T) {
	_, d := loadTablesFrom(t, "x-a: 1\nname: orders\ntypo: 2\nschema: silver\nx-b: 3\n")
	requireOneUnsupported(t, d, "typo", "orders.yml:3")
}

// The hint is part of every unknown-field error, in all four loaders.
func TestUnknownFieldHint_AllLoaders(t *testing.T) {
	for _, l := range syntaxLoaders() {
		t.Run(l.name, func(t *testing.T) {
			content := map[string]string{
				litProject: "foo: 1\n", litCatalog: "name: main\nfoo: 1\n", litTarget: "foo: 1\n", litTable: "name: t\nschema: s\nfoo: 1\n",
			}[l.name]
			e := oneError(t, l.load(t, content))
			assert.True(t, strings.HasSuffix(e.Summary, "; "+unknownHint), e.Summary)
		})
	}
}

// A second document after --- is ignored with a warning at the line of the ---; the first document is read.
func TestSecondDocument_WarnedAndIgnored(t *testing.T) {
	byName := map[string]syntaxLoader{}
	for _, l := range syntaxLoaders() {
		byName[l.name] = l
	}
	const warn = "the file has a second YAML document (after ---): it is ignored, only the first document is read"
	cases := map[string]struct{ content, loc string }{
		litProject: {"project: p\n---\nproject: q\n", "ucat.yml:2"},
		litCatalog: {"name: main\nuc_name: dev\n---\nname: other\n", pathCatalogMain + ":3"},
		litTarget:  {"workspace:\n  host: https://x\n---\nworkspace: {}\n", "targets/dev.yml:3"},
		litTable:   {"name: orders\nschema: silver\n---\nname: other\nschema: silver\n", "tables/silver/orders.yml:3"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := byName[name].load(t, tc.content)
			assert.Equal(t, []string{warn + " @" + tc.loc}, warnSummaries(d))
			assert.False(t, d.HasError())
		})
	}
	t.Run("the first document is the one that is read", func(t *testing.T) {
		tables, d := loadTablesFrom(t, "name: orders\nschema: silver\n---\nname: other\nschema: gold\n")
		require.Len(t, tables, 1)
		assert.Equal(t, "orders", tables[0].Name)
		assert.Len(t, warnSummaries(d), 1)
	})
}

func TestSecondDocument_NoWarning(t *testing.T) {
	for name, content := range map[string]string{
		"no separator":                  "name: orders\nschema: silver\n",
		"trailing separator only":       "name: orders\nschema: silver\n---\n",
		"trailing separator and spaces": "name: orders\nschema: silver\n---\n\n# end\n",
		"leading separator":             "---\nname: orders\nschema: silver\n",
		"document end marker":           "name: orders\nschema: silver\n...\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, d := loadTablesFrom(t, content)
			assert.Empty(t, d)
		})
	}
}

// An error in the first document is reported alone; no second-document warning on top of it.
func TestSecondDocument_NotWarnedWhenFirstHasErrors(t *testing.T) {
	_, d := loadTablesFrom(t, "name: orders\nschema: silver\nfoo: 1\n---\nname: x\n")
	assert.Empty(t, warnSummaries(d))
	assert.True(t, d.HasError())
}

// With an x- key in the first document the second one is still reported.
func TestSecondDocument_WithExtensionKey(t *testing.T) {
	_, d := loadTablesFrom(t, "x-a: 1\nname: orders\nschema: silver\n---\nname: x\n")
	assert.Len(t, warnSummaries(d), 1)
	assert.False(t, d.HasError())
}

// --- *.yaml files ---

func TestIgnoredYAMLWarning(t *testing.T) {
	root := t.TempDir()
	proj := config.ProjectConfig{TablesDir: litTables}
	assert.Empty(t, config.IgnoredYAMLWarning(root, proj, true), "no files, no warning")

	writeFile(t, root, "ucat.yml", "project: p\n")
	writeFile(t, root, "catalogs/main.yml", "name: main\n")
	writeFile(t, root, "tables/silver/a.yml", "name: a\n")
	assert.Empty(t, config.IgnoredYAMLWarning(root, proj, true), ".yml files only")

	writeFile(t, root, "tables/silver/b.yaml", "name: b\n")
	writeFile(t, root, "tables/deep/er/c.yaml", "name: c\n")
	writeFile(t, root, "catalogs/other.yaml", "name: other\n")
	writeFile(t, root, "catalogs/sub/nested.yaml", "x") // catalogs/ is flat: not looked at
	writeFile(t, root, "targets/prod.yaml", "x")
	writeFile(t, root, "ucat.yaml", "project: q\n")
	writeFile(t, root, "other.yaml", "x") // not ucat's file
	writeFile(t, root, "tables/readme.txt", "x")

	d := config.IgnoredYAMLWarning(root, proj, true)
	require.Len(t, d, 1)
	assert.Equal(t, diag.Warning, d[0].Severity)
	assert.Equal(t, "5 file(s) are ignored (ucat reads only files named *.yml with a lower-case extension, not *.yaml or *.YML): ucat.yaml, catalogs/other.yaml, targets/prod.yaml, tables/deep/er/c.yaml, tables/silver/b.yaml; rename them to .yml", d[0].Summary)

	d = config.IgnoredYAMLWarning(root, proj, false)
	require.Len(t, d, 1)
	assert.Contains(t, d[0].Summary, "3 file(s) are ignored")
	assert.NotContains(t, d[0].Summary, "tables/")
}

func TestIgnoredYAMLWarning_ListIsCapped(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		writeFile(t, root, "tables/"+n+".yaml", "x")
	}
	d := config.IgnoredYAMLWarning(root, config.ProjectConfig{TablesDir: litTables}, true)
	require.Len(t, d, 1)
	assert.Equal(t, "7 file(s) are ignored (ucat reads only files named *.yml with a lower-case extension, not *.yaml or *.YML): tables/a.yaml, tables/b.yaml, tables/c.yaml, tables/d.yaml, tables/e.yaml, ... (2 more); rename them to .yml", d[0].Summary)
}

// --- targets that resolve a logical catalog to the same physical catalog ---

func sharedMsg(targets, uc, catalog string) string {
	return "targets " + targets + " share the UC catalog \"" + uc + "\" of the logical catalog \"" + catalog +
		"\"; give each its own catalogs." + catalog + ".uc_name in targets/<name>.yml"
}

func ucOverride(name string) string { return "catalogs:\n  main:\n    uc_name: " + name + "\n" }

func writeTargets(t *testing.T, targets map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range targets {
		writeFile(t, root, "targets/"+name+".yml", content)
	}
	writeFile(t, root, "targets/notes.txt", "x")
	return root
}

func TestSharedCatalogWarning(t *testing.T) {
	cases := []struct {
		name    string
		targets map[string]string
		want    []string
	}{
		{"no targets dir", nil, nil},
		{"one target", map[string]string{litDev: litEmptyWorkspace}, nil},
		{"one target with an explicit value", map[string]string{litDev: ucOverride("same")}, nil},
		{"both inherit the catalog file's value", map[string]string{"a": litEmptyWorkspace, "b": ""},
			[]string{sharedMsg("a, b", "base", litMain)}},
		{"both type the value of the catalog file", map[string]string{"a": ucOverride("base"), "b": ucOverride("base")},
			[]string{sharedMsg("a, b", "base", litMain)}},
		{"one inherits, one types the same value", map[string]string{"a": litEmptyWorkspace, "b": ucOverride("base")},
			[]string{sharedMsg("a, b", "base", litMain)}},
		{"both type one explicit value", map[string]string{litDev: ucOverride("same"), litProd: ucOverride("same")},
			[]string{sharedMsg("dev, prod", "same", litMain)}},
		{"different explicit values", map[string]string{litDev: ucOverride(litDev), litProd: ucOverride(litProd)}, nil},
		{"one explicit, one inherited and different", map[string]string{litDev: ucOverride(litDev), litProd: litEmptyWorkspace}, nil},
		{"three targets, two match: only those are named", map[string]string{"a": ucOverride("same"), "b": ucOverride("other"), "c": ucOverride("same")},
			[]string{sharedMsg("a, c", "same", litMain)}},
		{"three targets, two pairs", map[string]string{"a": ucOverride("x"), "b": ucOverride("y"), "c": ucOverride("x"), "d": ucOverride("y")},
			[]string{sharedMsg("a, c", "x", litMain), sharedMsg("b, d", "y", litMain)}},
		{"values compare through FoldName", map[string]string{"a": ucOverride("Same"), "b": ucOverride("same")},
			[]string{sharedMsg("a, b", "Same", litMain)}},
		{"a key of another case is another catalog: the override is not seen", map[string]string{"a": "catalogs:\n  Main:\n    uc_name: same\n", "b": "catalogs:\n  Main:\n    uc_name: same\n"},
			[]string{sharedMsg("a, b", "base", litMain)}},
		{"a catalog that catalogs/ does not have is not looked at", map[string]string{"a": ucOverride("x") + "  extra:\n    uc_name: z\n", "b": ucOverride("y") + "  extra:\n    uc_name: z\n"}, nil},
		{"an entry without uc_name inherits", map[string]string{"a": "catalogs:\n  main:\n    schemas:\n      s: {}\n", "b": litEmptyWorkspace},
			[]string{sharedMsg("a, b", "base", litMain)}},
		{"an unreadable target is left out", map[string]string{"a": litEmptyWorkspace, "b": "{{{ not yaml"}, nil},
		{"x- key does not matter", map[string]string{"a": "x-n: 1\n" + ucOverride(litDev), "b": litEmptyWorkspace}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := config.SharedCatalogWarning(writeTargets(t, tc.targets), []config.CatalogConfig{{Name: litMain, UCName: "base"}})
			var got []string
			for _, x := range d {
				assert.Equal(t, diag.Warning, x.Severity)
				got = append(got, x.Summary)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// A catalog without any uc_name (file and target) has nothing to share; each logical catalog is judged
// on its own and the warnings come sorted by catalog name.
func TestSharedCatalogWarning_CatalogsSortedByName(t *testing.T) {
	root := writeTargets(t, map[string]string{"a": litEmptyWorkspace, "b": litEmptyWorkspace})
	d := config.SharedCatalogWarning(root, []config.CatalogConfig{{Name: "zeta", UCName: "z"}, {Name: "alpha", UCName: "a"}, {Name: litMain, UCName: "m"}})
	require.Len(t, d, 3)
	assert.Equal(t, sharedMsg("a, b", "a", "alpha"), d[0].Summary)
	assert.Equal(t, sharedMsg("a, b", "m", litMain), d[1].Summary)
	assert.Equal(t, sharedMsg("a, b", "z", "zeta"), d[2].Summary)
}

func TestSharedCatalogWarning_SeveralCatalogs(t *testing.T) {
	root := writeTargets(t, map[string]string{
		"a": "catalogs:\n  ref:\n    uc_name: r\n  main:\n    uc_name: m1\n",
		"b": "catalogs:\n  ref:\n    uc_name: r\n  main:\n    uc_name: m2\n",
	})
	d := config.SharedCatalogWarning(root, []config.CatalogConfig{{Name: "ref", UCName: ""}, {Name: litMain, UCName: ""}, {Name: "none"}})
	require.Len(t, d, 1)
	assert.Equal(t, sharedMsg("a, b", "r", "ref"), d[0].Summary)
}

// Not only .yaml: .YML, .Yaml and the like are not read either (only a lower-case .yml is), so they are reported.
func TestIgnoredYAMLWarning_CaseVariants(t *testing.T) {
	root := t.TempDir()
	proj := config.ProjectConfig{TablesDir: litTables}
	writeFile(t, root, "tables/silver/a.YML", "x")
	writeFile(t, root, "tables/silver/b.Yaml", "x")
	writeFile(t, root, "tables/silver/c.YAML", "x")
	writeFile(t, root, "tables/silver/d.yml", "x") // read: not reported
	writeFile(t, root, "tables/silver/e.yamlx", "x")
	writeFile(t, root, "tables/silver/f.txt", "x")
	writeFile(t, root, "catalogs/main.Yml", "x")
	d := config.IgnoredYAMLWarning(root, proj, true)
	require.Len(t, d, 1)
	assert.Equal(t, "4 file(s) are ignored (ucat reads only files named *.yml with a lower-case extension, not *.yaml or *.YML): "+
		"catalogs/main.Yml, tables/silver/a.YML, tables/silver/b.Yaml, tables/silver/c.YAML; rename them to .yml", d[0].Summary)
}

// The project manifest with a wrong-case extension is reported too; an unrelated file in the root is not.
func TestIgnoredYAMLWarning_RootManifestVariants(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "UCAT.YML", "x")
	writeFile(t, root, "docker-compose.yaml", "x")
	d := config.IgnoredYAMLWarning(root, config.ProjectConfig{TablesDir: litTables}, true)
	require.Len(t, d, 1)
	assert.Contains(t, d[0].Summary, "1 file(s) are ignored")
	assert.Contains(t, d[0].Summary, "): UCAT.YML;")
}
