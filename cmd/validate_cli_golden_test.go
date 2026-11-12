package cmd

// Characterization (golden) tests for the CLI output of `validate`, `validate -t dev` and `plan -t dev`
// on projects with groups of violations: stdout, stderr and the exit code of each run are pinned in
// testdata/validate_<project>.golden (the mock backend serves the live state). They guard the
// order and the text of the diagnostics across a refactoring of ValidateLocal / Diff / DiffSchema.
//
// Regenerate after an intended change:
//
//	go test ./cmd -run TestValidateCLI_Golden -update

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
)

const gcCatalog = "name: main\nuc_name: main\nschemas:\n  silver:\n    uc_name: silver\n  gold:\n    uc_name: gold\n"
const gcTarget = "workspace:\n  host: https://dev.example.com\n  warehouse_id: wh1\ncatalogs:\n  main:\n    uc_name: dev\n"

type cliProject struct {
	name  string
	files map[string]string // path relative to the project root; the base files are added when missing
	live  func() []*backend.LiveTable
}

func cliProjects() []cliProject {
	liveSilver := func(names ...string) []*backend.LiveTable {
		var out []*backend.LiveTable
		for _, n := range names {
			out = append(out, backendtest.Live("dev", "silver", n, backendtest.Col("id", "BIGINT"), backendtest.Col("name", "STRING")))
		}
		return out
	}
	return []cliProject{
		{name: "clean", files: map[string]string{
			pathSilverOrders: "name: orders\nschema: silver\n" + idCols,
		}, live: func() []*backend.LiveTable { return []*backend.LiveTable{liveOrders("id")} }},
		{name: "table_level", files: map[string]string{
			"tables/silver/a_dup1.yml":     "name: dup\nschema: silver\n" + idCols,
			"tables/silver/b_upper.yml":    "name: Upper\nschema: silver\nrename_from: OldUpper\n" + idCols,
			"tables/silver/c_noschema.yml": "name: lost\nschema: nowhere\n" + idCols,
			"tables/silver/d_cl.yml":       "name: both\nschema: silver\nclustering:\n  columns: [id, nope]\npartitioning:\n  columns: [gone]\n" + idCols,
			"tables/silver/e_nocols.yml":   "name: nocols\nschema: silver\n",
			"tables/silver/f_drop.yml":     "name: dropped\nschema: silver\ndrop: true\nrename_from: was\n",
			"tables/gold/a_dot.yml":        "name: with.dot\nschema: gold\n" + idCols,
			"tables/gold/b_swap_a.yml":     "name: swap_a\nschema: gold\nrename_from: swap_b\n" + idCols,
			"tables/gold/c_swap_b.yml":     "name: swap_b\nschema: gold\nrename_from: swap_a\n" + idCols,
			"tables/gold/d_rn1.yml":        "name: r1\nschema: gold\nrename_from: shared\n" + idCols,
			"tables/gold/e_rn2.yml":        "name: r2\nschema: gold\nrename_from: shared\n" + idCols,
		}, live: func() []*backend.LiveTable { return nil }},
		{name: "column_level", files: map[string]string{
			"tables/silver/cols.yml": "name: cols\nschema: silver\ncolumns:\n" +
				"  - name: a b\n    type: VARCHAR(5)\n" +
				"  - name: x.y\n    type: BIG INT\n" +
				"  - name: dup\n    type: INT\n  - name: DUP\n    type: STRING\n" +
				"  - name: same\n    type: INT\n    rename_from: SAME\n" +
				"  - name: n1\n    type: INT\n    rename_from: old\n  - name: n2\n    type: INT\n    rename_from: OLD\n" +
				"  - name: c1\n    type: INT\n    rename_from: c2\n  - name: c2\n    type: INT\n    rename_from: c1\n",
			"tables/gold/other.yml": "name: other\nschema: gold\ncolumns:\n  - name: ok\n    type: INT\n  - name: bad\n    type: MONEY\n",
		}, live: func() []*backend.LiveTable { return nil }},
		{name: "catalog_level", files: map[string]string{
			"catalogs/common.yml": "name: common\nuc_name: common\nschemas:\n  silver:\n    uc_name: silver\n",
			"catalogs/noname.yml": "name: \"\"\nuc_name: noname\nschemas:\n  zeta:\n    uc_name: zeta\n",
			"catalogs/other.yml":  "name: Main\nuc_name: Prod\nschemas:\n  Gold:\n    uc_name: Gold\n  shared:\n    uc_name: shared\n",
			"catalogs/phys.yml":   "name: phys\nuc_name: shared_cat\nschemas:\n  p1:\n    uc_name: same\n  p2:\n    uc_name: same\n",
			pathSilverOrders:      "name: orders\nschema: silver\n" + idCols,
		}, live: func() []*backend.LiveTable { return nil }},
		{name: "loader_errors", files: map[string]string{
			"tables/silver/unknown_field.yml": "name: a\nschema: silver\nbogus: 1\n" + idCols,
			"tables/silver/syntax.yml":        "name: b\nschema: [silver\n",
			"tables/silver/ok.yml":            "name: ok\nschema: silver\n" + idCols,
		}, live: func() []*backend.LiveTable { return nil }},
		{name: "target_only", files: map[string]string{
			pathTargetDev:    "workspace:\n  host: https://dev.example.com\ncatalogs:\n  main:\n    uc_name: Dev\n    schemas:\n      gold:\n        uc_name: silver\n  extra:\n    uc_name: extra\n    schemas:\n      silver:\n        uc_name: silver\n",
			pathSilverOrders: "name: orders\nschema: silver\n" + idCols,
		}, live: func() []*backend.LiveTable { return nil }},
		{name: "live_diff", files: map[string]string{
			"tables/silver/a_newnotnull.yml": "name: a\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: req\n    type: INT\n    nullable: false\n",
			"tables/silver/b_typo.yml":       "name: b\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: title\n    type: STRING\n    rename_from: nmae\n",
			"tables/silver/c_rename.yml":     "name: c_new\nschema: silver\nrename_from: c\n" + idCols + "  - name: name\n    type: STRING\n",
			"tables/silver/d_stale.yml":      "name: d\nschema: silver\nrename_from: d_old\n" + idCols + "  - name: name\n    type: STRING\n",
			"tables/silver/e_drop_gone.yml":  "name: e\nschema: silver\ndrop: true\n",
			"tables/silver/f_part.yml":       "name: f\nschema: silver\npartitioning:\n  columns: [id]\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: name\n    type: STRING\n",
			"tables/silver/g_conflict.yml":   "name: g\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n    rename_from: name\n",
			"tables/gold/new.yml":            "name: brand_new\nschema: gold\nclustering:\n  columns: [id]\ncomment: it's new\n" + idCols,
		}, live: func() []*backend.LiveTable { return liveSilver("a", "b", "c", "d", "f", "g") }},
		{name: "warnings_only", files: map[string]string{
			"tables/silver/a_same.yml": "name: a\nschema: silver\nrename_from: a\n" + idCols + "  - name: name\n    type: STRING\n    rename_from: NAME\n",
			"tables/silver/b_dot.yml":  "name: with.dot\nschema: silver\n" + idCols,
			"tables/silver/c_cols.yml": "name: c\nschema: silver\ncolumns:\n  - name: a b\n    type: INT\n",
		}, live: func() []*backend.LiveTable { return liveSilver("a") }},
	}
}

// TestValidateCLI_Golden pins validate, validate -t dev and plan -t dev for each project.
// Update: go test ./cmd -run TestValidateCLI_Golden -update
func TestValidateCLI_Golden(t *testing.T) {
	cmds := [][]string{{cmdValidate}, {cmdValidate, "-t", litDev}, {cmdPlan, "-t", litDev}}
	for _, p := range cliProjects() {
		t.Run(p.name, func(t *testing.T) {
			tmp := newProject(t, projectOpts{Project: "project: golden\ntables_dir: tables\n", Catalog: gcCatalog, Target: gcTarget, Files: p.files})

			var b strings.Builder
			for _, args := range cmds {
				m := &backendtest.MockUCBackend{}
				m.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, p.live())
				useBackend(t, m)
				stdout, stderr, runErr := executeCmdSplit(args...)
				fmt.Fprintf(&b, "== ucat %s\n-- exit code\n%d\n-- stdout\n%s-- stderr\n%s",
					strings.Join(args, " "), codeOf(runErr), goldenBlock(normTmp(stdout, tmp)), goldenBlock(normTmp(stderr, tmp)))
			}
			assertGolden(t, "validate_"+p.name+".golden", b.String())
		})
	}
}
