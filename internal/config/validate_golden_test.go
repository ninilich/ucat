package config_test

// Characterization (golden) tests for ValidateLocal: they pin the exact order, severity, text and
// location of every diagnostic, so a refactoring of the function cannot silently change them.
//
// Regenerate the golden files after an intended change:
//
//	go test ./internal/config -run 'TestValidateLocal_Golden|TestValidateLocal_Order' -update
//
// Coverage map (check number in validator.go / docs/contributing/architecture.md -> fixture id in checkFixtures):
//
//	1 unknown schema ref            c01          2 duplicate resolved table       c02
//	3 clustering XOR partitioning   c03          4 auto XOR columns               c04
//	5 table name required           c05          6 column name required           c06
//	7 column type (2 reasons)       c07a, c07b   8 table rename_from == name      c08
//	9 rename_from, empty name       c09          10 column rename_from == name    c10
//	11 dot/space in table name      c11          12 duplicate rename_from         c12
//	13 dot/space in column name     c13          14 rename_from -> defined table  c14
//	15 duplicate column             c15          16 table name not lowercase      c16
//	17 resolved UC name not lower   c17          18 empty clustering block        c18
//	19 column rename conflicts      c19          20 empty catalog name            c20
//	21 schema in several catalogs   c21a, c21b   22 empty uc_name (+NoTarget skip) c22a, c22b
//	23 physical schema collision    c23a, c23b   24 drop + rename_from            c24
//	25 clustering/partition columns c25          26 case-only collisions          c26
//	(unnumbered) columns required   c27
//
// All checks are reachable in isolation; some fixtures also trigger a neighbouring check
// (noted in the golden file, which fixes the full output of each fixture).

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

// assertGolden compares got with testdata/<name>; with -update it rewrites the file.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run with -update")
	assert.Equal(t, string(want), got, "golden mismatch for %s (run with -update if the change is intended)", name)
}

func sevName(s diag.Severity) string {
	if s == diag.Warning {
		return "warning"
	}
	return "error"
}

// formatDiags renders diagnostics one per line: severity|summary|location.
func formatDiags(ds diag.Diagnostics) string {
	var b strings.Builder
	for _, d := range ds {
		fmt.Fprintf(&b, "%s|%s|%s\n", sevName(d.Severity), d.Summary, d.Location)
	}
	return b.String()
}

type checkFixture struct {
	id  string
	cfg config.ResolvedConfig
	// wantSummary is a fragment that must appear in the output: it ties the fixture to its check.
	wantSummary string
}

func withSource(t config.Table, src string) config.Table {
	t.SourceFile = src
	return t
}

func checkFixtures() []checkFixture {
	one := func(t config.Table) config.ResolvedConfig { return resolvedConfig([]config.Table{t}) }
	tbl := func(name string, mod func(*config.Table)) config.Table {
		t := withSource(resolvedTable(name), "tables/silver/"+name+".yml")
		if mod != nil {
			mod(&t)
		}
		return t
	}
	cols := func(c ...config.Column) []config.Column { return c }
	two := func(a, b config.Table) config.ResolvedConfig { return resolvedConfig([]config.Table{a, b}) }

	return []checkFixture{
		{"c01", one(tbl("t", func(t *config.Table) { t.Schema = litNope; t.ResolvedCatalog = ""; t.ResolvedSchema = "" })), "unknown schema ref"},
		{"c02", two(tbl("t", nil), withSource(tbl("t", nil), "tables/silver/t2.yml")), "duplicate table"},
		{"c03", one(tbl("t", func(t *config.Table) {
			t.Clustering = &config.Clustering{Columns: []string{"id"}}
			t.Partitioning = &config.Partitioning{Columns: []string{"id"}}
		})), "mutually exclusive"},
		{"c04", one(tbl("t", func(t *config.Table) { t.Clustering = &config.Clustering{Auto: true, Columns: []string{"id"}} })), "clustering.auto and clustering.columns"},
		{"c05", one(tbl("", nil)), "table name is required"},
		{"c06", one(tbl("t", func(t *config.Table) { t.Columns = cols(config.Column{Name: "", Type: typINT}) })), "column name is required"},
		{"c07a", one(tbl("t", func(t *config.Table) { t.Columns = cols(config.Column{Name: "a", Type: "VARCHAR(10)"}) })), "is not supported; supported types"},
		{"c07b", one(tbl("t", func(t *config.Table) { t.Columns = cols(config.Column{Name: "a", Type: typBIGINT2}) })), "whitespace inside a word"},
		{"c08", one(tbl("t", func(t *config.Table) { t.RenameFrom = "t" })), "is same as name; no rename will occur"},
		{"c09", one(tbl("", func(t *config.Table) { t.RenameFrom = litOld })), "rename_from set but table name is empty"},
		{"c10", one(tbl("t", func(t *config.Table) { t.Columns = cols(config.Column{Name: "Id", Type: typINT, RenameFrom: "ID"}) })), "rename_from is same as name"},
		{"c11", one(tbl("a.b c", nil)), "name contains a dot or whitespace"},
		{"c12", two(tbl("n1", func(t *config.Table) { t.RenameFrom = litOld }), tbl("n2", func(t *config.Table) { t.RenameFrom = litOld })), "is used by both table"},
		{"c13", one(tbl("t", func(t *config.Table) {
			t.Columns = cols(config.Column{Name: litAB, Type: typINT}, config.Column{Name: "c.d", Type: typINT})
		})), "column \"a b\": name contains"},
		{"c14", two(tbl("a", func(t *config.Table) { t.RenameFrom = "b" }), tbl("b", func(t *config.Table) { t.RenameFrom = "a" })), "refers to a table that is also defined"},
		{"c15", one(tbl("t", func(t *config.Table) {
			t.Columns = cols(config.Column{Name: "a", Type: typINT}, config.Column{Name: "A", Type: typINT})
		})), "duplicate column"},
		{"c16", one(tbl("Orders", func(t *config.Table) { t.RenameFrom = "OldOrders" })), "table name \"Orders\" must be lowercase"},
		{"c17", config.ResolvedConfig{WarehouseID: "wh",
			Schemas: []config.ResolvedSchema{{LogicalCatalog: litMain, LogicalSchema: litSilver2, UCCatalog: litProd2, UCSchema: litSilver, SourceFile: "catalogs/main.yml"}},
			Tables:  []config.Table{tbl("t", func(t *config.Table) { t.ResolvedCatalog = "Prod"; t.ResolvedSchema = litSilver })}}, "must be lowercase: Unity Catalog stores catalog names"},
		{"c18", one(tbl("t", func(t *config.Table) { t.Clustering = &config.Clustering{} })), "clustering must set columns or auto"},
		{"c19", one(tbl("t", func(t *config.Table) {
			t.Columns = cols(
				config.Column{Name: "a", Type: typINT, RenameFrom: "x"},
				config.Column{Name: "b", Type: typINT, RenameFrom: "X"},
				config.Column{Name: "c", Type: typINT, RenameFrom: "d"},
				config.Column{Name: "d", Type: typINT, RenameFrom: "c"})
		})), "chain and swap renames are not supported"},
		{"c20", schemaCfg(rs("", "silver", "prod", "silver", "catalogs/empty.yml")), "catalog name is empty"},
		{"c21a", schemaCfg(rs("common", "silver", "common", "silver", "catalogs/common.yml"), rs("main", "silver", "prod", "silver", "catalogs/main.yml")), "is defined in both"},
		{"c21b", schemaCfg(rs("a", "silver", "a", "silver", "catalogs/a.yml"), rs("b", "silver", "b", "silver", ""), rs("c", "silver", "c", "silver", "catalogs/c.yml")), "is defined in all of"},
		{"c22a", schemaCfg(rs("main", "silver", "", "silver", "catalogs/main.yml")), "uc_name is not set"},
		{"c22b", config.ResolvedConfig{NoTarget: true, Schemas: []config.ResolvedSchema{rs("main", "silver", "", "silver", "catalogs/main.yml")}}, ""},
		{"c23a", schemaCfg(rs("main", "a", "prod", "s", "catalogs/main.yml"), rs("main", "b", "prod", "s", "catalogs/main.yml")), "resolve to physical schema prod.s"},
		{"c23b", schemaCfg(rs("main", "a", "prod", "s", "catalogs/main.yml"), rs("main", "b", "prod", "s", "catalogs/main.yml"), rs("other", "c", "prod", "s", "")), "all resolve to physical schema"},
		{"c24", one(tbl("t", func(t *config.Table) { t.Drop = true; t.RenameFrom = "old" })), "drop: true cannot be combined with rename_from"},
		{"c25", one(tbl("t", func(t *config.Table) {
			t.Clustering = &config.Clustering{Columns: []string{"id", "zzz"}}
		})), "clustering column \"zzz\" is not defined"},
		{"c26", schemaCfg(rs("main", "silver", "prod", "silver", "catalogs/main.yml"), rs("MAIN", "Silver", "prod2", "silver2", "catalogs/MAIN.yml")), "differ only in case"},
		{"c27", one(tbl("t", func(t *config.Table) { t.Columns = nil })), "columns is required"},
		{"c28-ok", one(tbl("t", nil)), ""},
	}
}

// TestValidateLocal_GoldenPerCheck pins the output of one minimal fixture per check.
// Update: go test ./internal/config -run TestValidateLocal_GoldenPerCheck -update
func TestValidateLocal_GoldenPerCheck(t *testing.T) {
	var b strings.Builder
	for _, f := range checkFixtures() {
		ds := config.ValidateLocal(f.cfg)
		out := formatDiags(ds)
		if f.wantSummary != "" {
			assert.Contains(t, out, f.wantSummary, "fixture %s no longer triggers its check", f.id)
		}
		fmt.Fprintf(&b, "== %s\n%s", f.id, out)
	}
	assertGolden(t, "validate_checks.golden", b.String())
}

// bigProject is the "everything broken at once" project: many schemas, catalogs and tables, so that
// most checks fire together and their relative order is pinned.
func bigProject() config.ResolvedConfig {
	schemas := []config.ResolvedSchema{
		rs("", "empty_s", "", "x", "catalogs/empty.yml"),
		rs("main", "silver", "prod", "silver", "catalogs/main.yml"),
		rs("main", "gold", "prod", "Gold", "catalogs/main.yml"),
		rs("common", "silver", "common", "silver", "catalogs/common.yml"),
		rs("Main", "bronze", "Prod", "bronze", "catalogs/Main.yml"),
		rs("legacy", "Silver", "", "silver", "catalogs/legacy.yml"),
		rs("a", "x", "phys", "s", "catalogs/a.yml"),
		rs("b", "y", "phys", "s", ""),
		rs("z", "zz", "zc", "zs", "catalogs/z.yml"),
		rs("n1", "gold", "aaa", "t", "catalogs/n1.yml"),
		rs("n1", "gold2", "aaa", "t", "catalogs/n1.yml"),
		rs("n1", "gold3", "aaa", "t", "catalogs/n1.yml"),
	}
	mk := func(name, schema, cat, sch, file string, mod func(*config.Table)) config.Table {
		t := config.Table{Name: name, Schema: schema, ResolvedCatalog: cat, ResolvedSchema: sch, SourceFile: file,
			Columns: []config.Column{{Name: "id", Type: typBIGINT}}}
		if mod != nil {
			mod(&t)
		}
		return t
	}
	tables := []config.Table{
		mk("ok", "silver", "prod", "silver", "tables/silver/ok.yml", nil),
		mk("", "silver", "prod", "silver", "tables/silver/noname.yml", func(t *config.Table) { t.RenameFrom = "ghost" }),
		mk("Dup", "silver", "prod", "silver", "tables/silver/dup1.yml", func(t *config.Table) { t.RenameFrom = "Dup" }),
		mk("dup", "silver", "prod", "silver", "tables/silver/dup2.yml", func(t *config.Table) { t.RenameFrom = "dupold" }),
		mk("dup", "silver", "prod", "silver", "tables/silver/dup3.yml", func(t *config.Table) { t.RenameFrom = "dupold" }),
		mk("ghost_tbl", "nowhere", "", "", "tables/nowhere/ghost_tbl.yml", nil),
		mk("r1", "silver", "prod", "silver", "tables/silver/r1.yml", func(t *config.Table) { t.RenameFrom = "shared_old" }),
		mk("r2", "silver", "prod", "silver", "tables/silver/r2.yml", func(t *config.Table) { t.RenameFrom = "shared_old" }),
		mk("swap_a", "silver", "prod", "silver", "tables/silver/swap_a.yml", func(t *config.Table) { t.RenameFrom = "swap_b" }),
		mk("swap_b", "silver", "prod", "silver", "tables/silver/swap_b.yml", func(t *config.Table) { t.RenameFrom = "swap_a" }),
		mk("with.dot", "gold", "prod", "Gold", "tables/gold/dot.yml", nil),
		mk("nocols", "gold", "prod", "Gold", "tables/gold/nocols.yml", func(t *config.Table) { t.Columns = nil }),
		mk("dropped", "gold", "prod", "Gold", "tables/gold/dropped.yml", func(t *config.Table) {
			t.Drop = true
			t.RenameFrom = "was"
			t.Columns = nil
			t.Clustering = &config.Clustering{Columns: []string{litNope}}
		}),
		mk("both", "bronze", "Prod", "bronze", "tables/bronze/both.yml", func(t *config.Table) {
			t.Clustering = &config.Clustering{Auto: true, Columns: []string{"id", "missing"}}
			t.Partitioning = &config.Partitioning{Columns: []string{"gone"}}
		}),
		mk("emptycl", "bronze", "Prod", "bronze", "tables/bronze/emptycl.yml", func(t *config.Table) {
			t.Clustering = &config.Clustering{}
		}),
		mk("cols", "silver", "prod", "silver", "tables/silver/cols.yml", func(t *config.Table) {
			t.Columns = []config.Column{
				{Name: "", Type: typINT},
				{Name: "a b", Type: "VARCHAR(5)"},
				{Name: "x.y", Type: "BIG INT"},
				{Name: "dup", Type: typINT},
				{Name: "DUP", Type: typSTRING},
				{Name: "same", Type: typINT, RenameFrom: "SAME"},
				{Name: "n1", Type: typINT, RenameFrom: "old"},
				{Name: "n2", Type: typINT, RenameFrom: "OLD"},
				{Name: "c1", Type: typINT, RenameFrom: "c2"},
				{Name: "c2", Type: typINT, RenameFrom: "c1"},
				{Name: "nottype", Type: ""},
			}
		}),
		mk("Upper", "silver", "prod", "silver", "tables/silver/upper.yml", func(t *config.Table) { t.RenameFrom = "OldUpper" }),
		mk("nosrc", "silver", "prod", "silver", "", func(t *config.Table) { t.RenameFrom = "nosrc" }),
		mk("zt", "zz", "zc", "zs", "tables/z/zt.yml", nil),
		mk("", "zz", "zc", "zs", "", nil),
	}
	// Order-pinning tables: each one fires SEVERAL checks at once, so the relative order of the
	// check groups inside checkTable / checkColumn is visible in the output (swapping two adjacent
	// calls changes the golden file). Neighbouring pairs that can never fire together (4 and 18, 6 and
	// 13, 10 and 19, 24 and 25 on a drop table, ...) are equivalent under a swap.
	tables = append(tables,
		// definition target for check 14 (also check 16 on its own name)
		mk("ZBase", "silver", "prod", "silver", "tables/silver/zbase.yml", nil),
		// 16(name) 16(rename) 11 14, schema unknown (1) but resolved so that 2/12/14 can fire
		mk("Zk.One", "nowhere", "prod", "silver", "tables/silver/zk0.yml", func(t *config.Table) { t.RenameFrom = "ZBase" }),
		// the same plus 2, 12, columns required, 3, 4, 25 (clustering and partitioning, no columns)
		mk("Zk.One", "nowhere", "prod", "silver", "tables/silver/zk1.yml", func(t *config.Table) {
			t.RenameFrom = "ZBase"
			t.Columns = nil
			t.Clustering = &config.Clustering{Auto: true, Columns: []string{litNope}}
			t.Partitioning = &config.Partitioning{Columns: []string{"nope2"}}
		}),
		// 11, columns required, 3, 18, 25 (partitioning)
		mk("zk.two", "silver", "prod", "silver", "tables/silver/zk2.yml", func(t *config.Table) {
			t.Columns = nil
			t.Clustering = &config.Clustering{}
			t.Partitioning = &config.Partitioning{Columns: []string{"pp"}}
		}),
		// drop: 3, 18, 24 and then the column checks 6, 7 (25 is skipped for a drop table)
		mk("zk3", "silver", "prod", "silver", "tables/silver/zk3.yml", func(t *config.Table) {
			t.Drop = true
			t.RenameFrom = "old3"
			t.Clustering = &config.Clustering{}
			t.Partitioning = &config.Partitioning{Columns: []string{"pp"}}
			t.Columns = []config.Column{{Name: "", Type: "BAD"}}
		}),
		// drop: 4, 24, then check 7
		mk("zk4", "silver", "prod", "silver", "tables/silver/zk4.yml", func(t *config.Table) {
			t.Drop = true
			t.RenameFrom = "old4"
			t.Clustering = &config.Clustering{Auto: true, Columns: []string{"c"}}
			t.Columns = []config.Column{{Name: "a", Type: "BAD"}}
		}),
		// 5, 9, 16(rename), 1
		mk("", "nowhere", "", "", "tables/nowhere/zk5.yml", func(t *config.Table) { t.RenameFrom = "OldX" }),
		// 8, 16(name), 16(rename), 1
		mk("Same", "nowhere", "", "", "tables/nowhere/zk6.yml", func(t *config.Table) { t.RenameFrom = "Same" }),
		// 25 (clustering) followed by every column check on the same columns
		mk("zcm", "silver", "prod", "silver", "tables/silver/zcm.yml", func(t *config.Table) {
			t.Clustering = &config.Clustering{Columns: []string{"nope"}}
			t.Columns = []config.Column{
				{Name: "x", Type: typINT, RenameFrom: "r1"},
				{Name: "", Type: "BAD1", RenameFrom: "r1"},       // 6, 7, 19 (duplicate source)
				{Name: "p q", Type: "BIG INT", RenameFrom: "r1"}, // 13, 7, 19 (duplicate source)
				{Name: "P Q", Type: "BAD3"},                      // 13, 15, 7
				{Name: "dd", Type: "NOPE", RenameFrom: "DD"},     // 7, 10
				{Name: "a b", Type: typINT, RenameFrom: "A B"},   // 13, 10
				{Name: "s", Type: "BAD4", RenameFrom: "t"},       // 7, 19 (chain)
				{Name: "t", Type: typINT, RenameFrom: "s"},       // 19 (chain)
				{Name: "T", Type: typINT, RenameFrom: "s"},       // 15, 19 (duplicate source), 19 (chain)
				{Name: "e.f", Type: "INT", RenameFrom: "r1"},     // 13, 19 (duplicate source)
				{Name: "Dd", Type: "INT", RenameFrom: "DD"},      // 15, 10
			}
		}),
	)
	return config.ResolvedConfig{WarehouseID: "wh", Schemas: schemas, Tables: tables}
}

// TestValidateLocal_GoldenAll pins the full output for the "everything broken" project.
// Update: go test ./internal/config -run TestValidateLocal_GoldenAll -update
func TestValidateLocal_GoldenAll(t *testing.T) {
	ds := config.ValidateLocal(bigProject())
	require.NotEmpty(t, ds)
	assertGolden(t, "validate_all.golden", formatDiags(ds))
}

// TestValidateLocal_OrderDependsOnInputOrder pins what the output is for reordered inputs.
// ValidateLocal does NOT sort tables: diagnostics follow the input order of Tables (and, for the
// schema-level checks, of Schemas; Resolve sorts Schemas before it gets here). The expected output is
// fixed per input order, by fact of the current code.
// Update: go test ./internal/config -run TestValidateLocal_OrderDependsOnInputOrder -update
func TestValidateLocal_OrderDependsOnInputOrder(t *testing.T) {
	rev := func(cfg config.ResolvedConfig, tables, schemas bool) config.ResolvedConfig {
		out := cfg
		out.Tables = append([]config.Table(nil), cfg.Tables...)
		out.Schemas = append([]config.ResolvedSchema(nil), cfg.Schemas...)
		if tables {
			for i, j := 0, len(out.Tables)-1; i < j; i, j = i+1, j-1 {
				out.Tables[i], out.Tables[j] = out.Tables[j], out.Tables[i]
			}
		}
		if schemas {
			for i, j := 0, len(out.Schemas)-1; i < j; i, j = i+1, j-1 {
				out.Schemas[i], out.Schemas[j] = out.Schemas[j], out.Schemas[i]
			}
		}
		return out
	}
	base := bigProject()
	cases := []struct {
		file string
		cfg  config.ResolvedConfig
	}{
		{"validate_all_tables_reversed.golden", rev(base, true, false)},
		{"validate_all_schemas_reversed.golden", rev(base, false, true)},
		{"validate_all_both_reversed.golden", rev(base, true, true)},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			assertGolden(t, c.file, formatDiags(config.ValidateLocal(c.cfg)))
		})
	}
}

// TestValidateLocal_Deterministic: the output of the big project is identical on every run (map
// iteration order must not leak into it).
func TestValidateLocal_Deterministic(t *testing.T) {
	want := formatDiags(config.ValidateLocal(bigProject()))
	for i := 0; i < 20; i++ {
		got := formatDiags(config.ValidateLocal(bigProject()))
		require.Equal(t, want, got, "run %d differs", i)
	}
	// the same for schema-level checks with many groups
	cfg := schemaCfg(
		rs("c1", "s1", "p", "x", "f1"), rs("c2", "s1", "p", "x", "f2"),
		rs("c3", "s2", "q", "y", "f3"), rs("c4", "s2", "q", "y", "f4"),
		rs("C1", "S1", "p2", "x2", "f5"), rs("Cx", "sx", "", "sx", "f6"))
	wantS := formatDiags(config.ValidateLocal(cfg))
	for i := 0; i < 20; i++ {
		require.Equal(t, wantS, formatDiags(config.ValidateLocal(cfg)), "run %d differs", i)
	}
}
