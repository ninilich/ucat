package differ_test

// Characterization (golden) tests for Diff and DiffSchema: they pin the whole result (the list of
// changes with the operation name (Type().Op()), TableName, Describe, SQL and the diagnostics with their order, severity, text and
// location), so a refactoring of Diff cannot silently change anything observable. SQL per change type
// is in sql_golden_test.go; here the subject is the Diff result as a whole.
//
// Regenerate after an intended change:
//
//	go test ./internal/differ -run 'TestDiff_Golden|TestDiffSchema_Golden' -update

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

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

// formatResult renders changes (one line each) and diagnostics (severity|summary|location).
func formatResult(changes []change.Change, ds diag.Diagnostics) string {
	var b strings.Builder
	if len(changes) == 0 {
		b.WriteString("changes: none\n")
	}
	for i, c := range changes {
		fmt.Fprintf(&b, "change %d: %s destructive=%v table=%s\n", i, c.Type().Op(), c.IsDestructive(), c.TableName())
		fmt.Fprintf(&b, "  describe: %s\n", c.Describe())
		fmt.Fprintf(&b, "  sql: %q\n", c.SQL())
	}
	if len(ds) == 0 {
		b.WriteString("diags: none\n")
	}
	for _, d := range ds {
		sev := "error"
		if d.Severity == diag.Warning {
			sev = "warning"
		}
		fmt.Fprintf(&b, "diag: %s|%s|%s\n", sev, d.Summary, d.Location)
	}
	return b.String()
}

func gdTable(name string, mod func(*config.Table)) config.Table {
	t := config.Table{Name: name, Schema: litSilver, ResolvedCatalog: litProd, ResolvedSchema: litSilver,
		SourceFile: "tables/silver/" + name + ".yml"}
	if mod != nil {
		mod(&t)
	}
	return t
}

func gdPtr(s string) *string { return &s }
func gdBool(b bool) *bool    { return &b }

type diffScenario struct {
	id      string
	desired config.Table
	live    *backend.LiveTable
}

func gdLive(name string, mod func(*config.Table)) *backend.LiveTable {
	return backendtest.LiveOf(gdTable(name, mod))
}

func gdCols(c ...config.Column) []config.Column { return c }

func diffScenarios() []diffScenario {
	baseCols := gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litName, Type: typSTRING, Comment: "n"})
	withCols := func(t *config.Table) { t.Columns = baseCols }
	return []diffScenario{
		{"create full", gdTable("orders", func(t *config.Table) {
			t.Comment = "it's orders"
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT, Nullable: gdBool(false), Comment: "pk"}, config.Column{Name: "d", Type: typDATE})
			t.Clustering = &config.Clustering{Columns: []string{"id"}}
			t.TblProperties = map[string]*string{"k2": gdPtr("v2"), "k1": gdPtr("v1"), "gone": nil}
		}), nil},
		{"create partitioned", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "d", Type: typDATE})
			t.Partitioning = &config.Partitioning{Columns: []string{"d"}}
		}), nil},
		{"create clustering auto", gdTable("orders", func(t *config.Table) {
			t.Columns = baseCols
			t.Clustering = &config.Clustering{Auto: true}
		}), nil},
		{"drop existing", gdTable("orders", func(t *config.Table) { t.Drop = true }), gdLive("orders", withCols)},
		{"drop missing", gdTable("orders", func(t *config.Table) { t.Drop = true }), nil},
		{"no changes", gdTable("orders", withCols), gdLive("orders", withCols)},
		{"add column", gdTable("orders", func(t *config.Table) {
			t.Columns = append(append([]config.Column{}, baseCols...), config.Column{Name: "extra", Type: typINT, Comment: "c"})
		}), gdLive("orders", withCols)},
		{"alter type comment nullable both ways", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(
				config.Column{Name: "id", Type: typSTRING, Comment: litNew, Nullable: gdBool(false)},
				config.Column{Name: litName, Type: typSTRING, Comment: "n", Nullable: gdBool(true)})
		}), gdLive("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typVarchar10}, config.Column{Name: litName, Type: typSTRING, Comment: "n", Nullable: gdBool(false)})
		})},
		{"type change not supported", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typINT}, config.Column{Name: litName, Type: typSTRING, Comment: "n"})
		}), gdLive("orders", withCols)},
		// a supported change next to an unsupported one: the table yields the error and no changes
		{"type change supported and not supported", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typINT}, config.Column{Name: litName, Type: typSTRING, Comment: "n"})
		}), gdLive("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litName, Type: typVarchar10, Comment: "n"})
		})},
		{"drop column", gdTable("orders", func(t *config.Table) { t.Columns = baseCols[:1] }), gdLive("orders", withCols)},
		{"rename column with alter", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litTitle, Type: typSTRING, RenameFrom: litName, Comment: "t"})
		}), gdLive("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litName, Type: typVarchar10, Comment: "n"})
		})},
		{"rename column with type change not supported", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litTitle, Type: "VARCHAR_X", RenameFrom: litName, Comment: "t"})
		}), gdLive("orders", withCols)},
		{"rename column already applied", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litName, Type: typSTRING, Comment: "n", RenameFrom: litOld})
		}), gdLive("orders", withCols)},
		{"rename column typo", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litTitle, Type: typSTRING, RenameFrom: "nmae"})
		}), gdLive("orders", withCols)},
		{"rename column target exists", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT, RenameFrom: litName}, config.Column{Name: "x", Type: typINT})
		}), gdLive("orders", withCols)},
		{"rename column source also defined", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: litName, Type: typSTRING}, config.Column{Name: "z", Type: typINT, RenameFrom: litName})
		}), gdLive("orders", withCols)},
		{"rename column case only", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "ID", Type: typBIGINT, RenameFrom: "id"}, config.Column{Name: litName, Type: typSTRING, Comment: "n"})
		}), gdLive("orders", withCols)},
		{"rename column errors plus new not null", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "a", Type: typINT, Nullable: gdBool(false)}, config.Column{Name: "b", Type: typINT, RenameFrom: "nope"})
		}), gdLive("orders", withCols)},
		{"rename table", gdTable("orders_v2", func(t *config.Table) { t.RenameFrom = litOrders; t.Columns = baseCols }), gdLive("orders", withCols)},
		{"rename table plus column plus comment", gdTable("orders_v2", func(t *config.Table) {
			t.RenameFrom = litOrders
			t.Comment = "c"
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT}, config.Column{Name: "nm", Type: typSTRING, Comment: "n", RenameFrom: litName}, config.Column{Name: "x", Type: typINT})
		}), gdLive("orders", withCols)},
		{"rename table live in other catalog", gdTable("orders_v2", func(t *config.Table) { t.RenameFrom = litOrders; t.Columns = baseCols }),
			gdLive("orders", func(t *config.Table) { withCols(t); t.ResolvedCatalog = "old_cat" })},
		{"clustering none to columns", gdTable("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Columns: []string{"id"}} }), gdLive("orders", withCols)},
		{"clustering columns to none", gdTable("orders", withCols), gdLive("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Columns: []string{"id"}} })},
		{"clustering columns to auto", gdTable("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Auto: true} }),
			gdLive("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Columns: []string{"id"}} })},
		{"clustering same set other order and case", gdTable("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Columns: []string{"NAME", "id"}} }),
			gdLive("orders", func(t *config.Table) {
				withCols(t)
				t.Clustering = &config.Clustering{Columns: []string{"id", litName}}
			})},
		{"clustering keep nested", gdTable("orders", withCols),
			gdLive("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Columns: []string{litAB}} })},
		{"clustering nested replaced by columns", gdTable("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Columns: []string{"id"}} }),
			gdLive("orders", func(t *config.Table) { withCols(t); t.Clustering = &config.Clustering{Columns: []string{"a.b"}} })},
		{"partitioning change warning", gdTable("orders", func(t *config.Table) { withCols(t); t.Partitioning = &config.Partitioning{Columns: []string{"id"}} }), gdLive("orders", withCols)},
		{"partitioning removed warning", gdTable("orders", withCols),
			gdLive("orders", func(t *config.Table) { withCols(t); t.Partitioning = &config.Partitioning{Columns: []string{"id"}} })},
		{"partitioning same case-insensitive", gdTable("orders", func(t *config.Table) { withCols(t); t.Partitioning = &config.Partitioning{Columns: []string{"ID"}} }),
			gdLive("orders", func(t *config.Table) { withCols(t); t.Partitioning = &config.Partitioning{Columns: []string{"id"}} })},
		{"tblproperties set update unset system ignored", gdTable("orders", func(t *config.Table) {
			withCols(t)
			t.TblProperties = map[string]*string{
				"new": gdPtr("1"), "changed": gdPtr("b"), litSame: gdPtr("s"), "unset_b": nil, "unset_a": nil, "unset_missing": nil,
				litClusterByAuto: gdPtr("true"), "clusteringColumns": gdPtr("[[\"id\"]]"), "clusteringColumnsIndexed": nil,
			}
		}), gdLive("orders", func(t *config.Table) {
			withCols(t)
			t.TblProperties = map[string]*string{"changed": gdPtr("a"), "same": gdPtr("s"), "unset_a": gdPtr("x"), "unset_b": gdPtr("y"), "other": gdPtr("o")}
		})},
		{"tblproperties live nil value", gdTable("orders", func(t *config.Table) { withCols(t); t.TblProperties = map[string]*string{"k": gdPtr("v")} }),
			gdLive("orders", func(t *config.Table) { withCols(t); t.TblProperties = map[string]*string{"k": nil} })},
		{"table comment set", gdTable("orders", func(t *config.Table) { withCols(t); t.Comment = "it's new" }), gdLive("orders", withCols)},
		{"table comment unset", gdTable("orders", withCols), gdLive("orders", func(t *config.Table) { withCols(t); t.Comment = litOld })},
		{"new not null column", gdTable("orders", func(t *config.Table) {
			t.Columns = append(append([]config.Column{}, baseCols...), config.Column{Name: "req", Type: typINT, Nullable: gdBool(false)}, config.Column{Name: "req2", Type: typINT, Nullable: gdBool(false)})
		}), gdLive("orders", withCols)},
		{"type normalization spaces and case", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "a", Type: typDECIMAL102}, config.Column{Name: "b", Type: "map< string , array< int > >"},
				config.Column{Name: "c", Type: "STRUCT<`x y`: INT>"}, config.Column{Name: "d", Type: typDECIMAL1022})
		}), gdLive("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "a", Type: typDecimal102}, config.Column{Name: "b", Type: "map<string,array<int>>"},
				config.Column{Name: "c", Type: "struct<`x  y`:int>"}, config.Column{Name: "d", Type: "decimal(10,3)"})
		})},
		{"column names case-insensitive", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "ID", Type: typBIGINT}, config.Column{Name: "Name", Type: typSTRING, Comment: "n"})
		}), gdLive("orders", withCols)},
		// both rename conflicts on one column: target exists (reported) and source also defined (not reached)
		{"rename column target exists and source also defined", gdTable("orders", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "id", Type: typBIGINT, RenameFrom: litName}, config.Column{Name: litName, Type: typSTRING, Comment: "n"})
		}), gdLive("orders", withCols)},
		{"everything at once", gdTable("orders_v2", func(t *config.Table) {
			t.RenameFrom = litOrders
			t.Comment = "new comment"
			t.Columns = gdCols(
				config.Column{Name: "id", Type: typSTRING, Nullable: gdBool(false)},
				config.Column{Name: "title", Type: typSTRING, RenameFrom: "name", Comment: "t"},
				config.Column{Name: litAdded, Type: typDATE, Comment: "a"},
				config.Column{Name: "kept", Type: typINT})
			t.Clustering = &config.Clustering{Columns: []string{"id", "title"}}
			t.Partitioning = &config.Partitioning{Columns: []string{"added"}}
			t.TblProperties = map[string]*string{"s1": gdPtr("1"), "s2": gdPtr("2"), "u1": nil, "u0": nil}
		}), gdLive("orders", func(t *config.Table) {
			t.Comment = litOld
			t.Columns = gdCols(config.Column{Name: "id", Type: typVarchar10}, config.Column{Name: "name", Type: "STRING", Comment: "n"},
				config.Column{Name: "kept", Type: typINT}, config.Column{Name: "gone2", Type: typINT}, config.Column{Name: "gone1", Type: typINT})
			t.TblProperties = map[string]*string{"u0": gdPtr("x"), "u1": gdPtr("y")}
		})},
	}
}

// TestDiff_GoldenAll pins the result of Diff on a representative set of scenarios.
// Update: go test ./internal/differ -run TestDiff_GoldenAll -update
func TestDiff_GoldenAll(t *testing.T) {
	render := func() string {
		var b strings.Builder
		for _, s := range diffScenarios() {
			changes, ds := differ.Diff(s.desired, s.live)
			fmt.Fprintf(&b, "== %s\n%s", s.id, formatResult(changes, ds))
		}
		return b.String()
	}
	got := render()
	assertGolden(t, "diff_all.golden", got)
	// no map-order leaks: 20 repeats give the same bytes
	for i := 0; i < 20; i++ {
		require.Equal(t, got, render(), "run %d differs", i)
	}
}

type schemaScenario struct {
	id      string
	desired []config.Table
	live    []*backend.LiveTable
}

func schemaScenarios() []schemaScenario {
	cols := gdCols(config.Column{Name: "id", Type: typBIGINT})
	wc := func(t *config.Table) { t.Columns = cols }
	return []schemaScenario{
		{"several tables in input order", []config.Table{
			gdTable("zeta", func(t *config.Table) { t.Columns = cols; t.Comment = "z" }),
			gdTable("alpha", wc),
			gdTable("mid", func(t *config.Table) { t.Columns = gdCols(config.Column{Name: "id", Type: typSTRING}) }),
			gdTable("gone", func(t *config.Table) { t.Drop = true }),
			gdTable("already_gone", func(t *config.Table) { t.Drop = true }),
		}, []*backend.LiveTable{gdLive("mid", func(t *config.Table) { t.Columns = gdCols(config.Column{Name: "id", Type: typVarchar10}) }), gdLive("zeta", wc), gdLive("gone", wc), gdLive("unmanaged", wc)}},
		{"rename_from table not renamed yet", []config.Table{gdTable("new", func(t *config.Table) { t.Columns = cols; t.RenameFrom = litOld })},
			[]*backend.LiveTable{gdLive("old", wc)}},
		{"rename_from after apply", []config.Table{gdTable("new", func(t *config.Table) { t.Columns = cols; t.RenameFrom = litOld })},
			[]*backend.LiveTable{gdLive("new", wc)}},
		{"rename_from source missing creates new", []config.Table{gdTable("new", func(t *config.Table) { t.Columns = cols; t.RenameFrom = litOld })},
			nil},
		{"rename_from both exist", []config.Table{gdTable("new", func(t *config.Table) { t.Columns = cols; t.RenameFrom = litOld })},
			[]*backend.LiveTable{gdLive("old", wc), gdLive("new", wc)}},
		{"rename_from equals name", []config.Table{gdTable("same", func(t *config.Table) { t.Columns = cols; t.RenameFrom = "same" })},
			[]*backend.LiveTable{gdLive("same", wc)}},
		{"rename_from with drop", []config.Table{gdTable("new", func(t *config.Table) { t.Drop = true; t.RenameFrom = litOld })},
			[]*backend.LiveTable{gdLive("old", wc), gdLive("new", wc)}},
		{"rename_from with drop and nothing live", []config.Table{gdTable("new", func(t *config.Table) { t.Drop = true; t.RenameFrom = litOld })}, nil},
		{"mixed diagnostics from several tables", []config.Table{
			gdTable("a", func(t *config.Table) {
				t.Columns = gdCols(config.Column{Name: "n", Type: typINT, Nullable: gdBool(false)})
			}),
			gdTable("b", func(t *config.Table) {
				t.Columns = cols
				t.Partitioning = &config.Partitioning{Columns: []string{"id"}}
			}),
			gdTable("c", func(t *config.Table) { t.Columns = gdCols(config.Column{Name: "x", Type: "INT", RenameFrom: "typo"}) }),
			gdTable("d", func(t *config.Table) { t.Columns = cols; t.RenameFrom = "d_old" }),
			gdTable("e", func(t *config.Table) { t.Drop = true }),
		}, []*backend.LiveTable{gdLive("a", wc), gdLive("b", wc), gdLive("c", wc), gdLive("d", wc)}},
		// an unsupported type change in one table: its error is reported, the other tables are diffed as usual
		{"unsupported type change in one table", []config.Table{
			gdTable("a", func(t *config.Table) { t.Columns = gdCols(config.Column{Name: "id", Type: typINT}) }),
			gdTable("b", func(t *config.Table) { t.Columns = cols; t.Comment = "b" }),
		}, []*backend.LiveTable{gdLive("a", wc), gdLive("b", wc)}},
		{"all tables new", []config.Table{gdTable("t2", wc), gdTable("t1", wc)}, nil},
		// the DiffSchema rename warning comes before the diagnostics of Diff for the same table
		{"rename_from after apply plus partitioning warning", []config.Table{gdTable("new", func(t *config.Table) {
			t.Columns = cols
			t.RenameFrom = "old"
			t.Partitioning = &config.Partitioning{Columns: []string{"id"}}
		})}, []*backend.LiveTable{gdLive("new", wc)}},
		{"rename_from both exist plus column error", []config.Table{gdTable("new", func(t *config.Table) {
			t.Columns = gdCols(config.Column{Name: "x", Type: "INT", RenameFrom: "typo"})
			t.RenameFrom = "old"
		})}, []*backend.LiveTable{gdLive("old", wc), gdLive("new", wc)}},
	}
}

// TestDiffSchema_GoldenAll pins DiffSchema: lookup by name and rename_from, warnings, change order
// across tables (the input order of the desired tables).
// Update: go test ./internal/differ -run TestDiffSchema_GoldenAll -update
func TestDiffSchema_GoldenAll(t *testing.T) {
	render := func() string {
		var b strings.Builder
		for _, s := range schemaScenarios() {
			changes, ds := differ.DiffSchema(s.desired, s.live)
			fmt.Fprintf(&b, "== %s\n%s", s.id, formatResult(changes, ds))
		}
		return b.String()
	}
	got := render()
	assertGolden(t, "diffschema_all.golden", got)
	for i := 0; i < 20; i++ {
		require.Equal(t, got, render(), "run %d differs", i)
	}
}
