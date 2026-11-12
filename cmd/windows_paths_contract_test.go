package cmd

// windows_paths_contract_test.go: gaps in coverage. tables_dir with backslashes in every command,
// CRLF diagnostics for ucat.yml / catalogs / targets, windowsNameProblem table, pull path spelling.
// All tests are OS-independent: they compare text with "/" and run the same on Windows.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ordersBody = "name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n"

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// projectWithTablesDir builds a project whose ucat.yml has the given tables_dir literal and one
// table under tables/prod/silver.
func projectWithTablesDir(t *testing.T, tablesDirLiteral string) string {
	t.Helper()
	return newProject(t, projectOpts{
		Project: "project: p\ntables_dir: " + tablesDirLiteral + "\n",
		Files:   map[string]string{"tables/prod/silver/orders.yml": ordersBody},
	})
}

// --- 1. tables_dir with backslashes behaves like the slash spelling, in every command ---

func TestTablesDirBackslash_AllCommandsSameAsSlash(t *testing.T) {
	type result struct {
		out   string
		calls int
	}
	run := func(literal string, args ...string) result {
		projectWithTablesDir(t, literal)
		m := &backendtest.MockUCBackend{}
		m.SetFixture(jsonSilverRef, nil)
		useBackend(t, m)
		out, err := executeCmd(args...)
		require.NoError(t, err, "%v: %s", args, out)
		return result{out, len(m.RecordedCalls())}
	}
	cmds := map[string][]string{
		cmdValidate:   {cmdValidate},
		"validate -t": {cmdValidate, "-t", litDev},
		cmdPlan:       {cmdPlan, "-t", litDev},
		cmdApply:      {cmdApply, "-t", litDev, flagAutoApprove},
		cmdDrop:       {cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove},
	}
	for name, args := range cmds {
		t.Run(name, func(t *testing.T) {
			want := run("tables/prod", args...)
			got := run(`'tables\prod'`, args...)
			assert.Equal(t, want, got)
			assert.NotContains(t, got.out, `\`)
		})
	}
}

func TestTablesDirBackslash_PlanSeesTableAndApplyIsIdempotent(t *testing.T) {
	projectWithTablesDir(t, `'tables\prod'`)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, nil)
	useBackend(t, m)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err, out)
	assert.Contains(t, out, "orders", "table from the backslash tables_dir must be planned")

	// world after apply: live has the table, plan shows nothing to do
	m2 := upToDateMock()
	useBackend(t, m2)
	out, err = executeCmd("plan", "-t", "dev", "--detailed-exitcode")
	require.NoError(t, err, out)
}

func TestTablesDirBackslash_DiagnosticPathUsesSlashes(t *testing.T) {
	for _, literal := range []string{`'tables\prod'`, pathTablesProd} {
		for _, args := range [][]string{{cmdValidate}, {cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
			t.Run(literal+"/"+args[0], func(t *testing.T) {
				tmpDir := projectWithTablesDir(t, literal)
				writeFile(t, filepath.Join(tmpDir, "tables", "prod", "silver", "orders.yml"),
					"name: orders\nschema: silver\nbogus: 1\n")
				useBackend(t, &backendtest.MockUCBackend{})
				out, err := executeCmd(args...)
				require.Error(t, err)
				all := out + err.Error()
				assert.Contains(t, all, "tables/prod/silver/orders.yml:3")
				assert.NotContains(t, all, `\`)
			})
		}
	}
}

func TestTablesDirBackslash_DuplicateTableBothPathsSlashed(t *testing.T) {
	tmpDir := projectWithTablesDir(t, `'tables\prod'`)
	writeFile(t, filepath.Join(tmpDir, "tables", "prod", "silver", "orders2.yml"), ordersBody)
	out, err := executeCmd("validate")
	require.Error(t, err)
	assert.Contains(t, out, "also defined in tables/prod/silver/orders.yml")
	assert.Contains(t, out, "tables/prod/silver/orders2.yml")
	assert.NotContains(t, out, `\`)
}

// --- 2. CRLF diagnostics: right file and line, no \r in text ---

func TestCRLF_ConfigErrors_FileLineNoCR(t *testing.T) {
	cases := map[string]struct {
		file, content, want string
	}{
		"ucat.yml unknown field":  {pathProjectFile, "project: p\ntables_dir: tables\nbogus: 1\n", "ucat.yml:3"},
		"ucat.yml wrong type":     {pathProjectFile, "project: p\ntables_dir: [a]\n", "ucat.yml:2"},
		"catalog unknown field":   {pathCatalogMain, "name: main\nuc_name: dev\nzzz: 1\nschemas:\n  silver:\n    uc_name: silver\n", "catalogs/main.yml:3"},
		"catalog wrong type":      {pathCatalogMain, "name: main\nuc_name: dev\nschemas: 5\n", "catalogs/main.yml:3"},
		"target unknown field":    {pathTargetDev, "workspace:\n  host: https://x\n  bad: 1\n", "targets/dev.yml:3"},
		"table unknown field":     {pathSilverOrders, "name: orders\nschema: silver\nbogus: 1\n", "tables/silver/orders.yml:3"},
		"table wrong type":        {pathSilverOrders, "name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: [1]\n", "tables/silver/orders.yml:5"},
		"table duplicate key":     {pathSilverOrders, "name: orders\nname: orders\nschema: silver\n", "tables/silver/orders.yml:2"},
		"table broken after crlf": {"tables/silver/orders.yml", "# c\nname: orders\nschema: silver\ncolumns: x: y\n", "failed to parse: mapping values are not allowed in this context (tables/silver/orders.yml:4)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var texts [2]string
			for i, useCRLF := range []bool{false, true} {
				tmpDir := setupPlanProject(t)
				content := tc.content
				if useCRLF {
					content = crlf(content)
				}
				writeFile(t, filepath.Join(tmpDir, filepath.FromSlash(tc.file)), content)
				out, err := executeCmd("validate", "-t", "dev")
				require.Error(t, err, name)
				texts[i] = out + err.Error()
			}
			assert.Contains(t, texts[1], tc.want)
			assert.NotContains(t, texts[1], "\r")
			assert.Equal(t, texts[0], texts[1], "CRLF diagnostics must equal LF diagnostics")
		})
	}
}

func TestCRLF_DuplicateNames_BothPathsSlashed(t *testing.T) {
	t.Run("tables", func(t *testing.T) {
		tmpDir := setupPlanProject(t)
		writeFile(t, filepath.Join(tmpDir, "tables", "silver", "orders_copy.yml"), ordersBody)
		toCRLF(t, tmpDir)
		out, err := executeCmd("validate")
		require.Error(t, err)
		assert.Contains(t, out, "also defined in tables/silver/orders.yml")
		assert.Contains(t, out, "tables/silver/orders_copy.yml")
		assert.NotContains(t, out, `\`)
		assert.NotContains(t, out, "\r")
	})
	t.Run("catalogs", func(t *testing.T) {
		tmpDir := setupTestProject(t)
		writeFile(t, filepath.Join(tmpDir, "catalogs", "x.yml"),
			"name: main\nuc_name: dev\nschemas:\n  silver:\n    uc_name: silver\n")
		toCRLF(t, tmpDir)
		out, err := executeCmd("validate")
		require.Error(t, err)
		assert.Contains(t, out, "catalogs/main.yml")
		assert.Contains(t, out, "catalogs/x.yml")
		assert.NotContains(t, out, `\`)
		assert.NotContains(t, out, "\r")
	})
}

func TestCRLF_BackslashTablesDir_PlanSameAsLF(t *testing.T) {
	run := func(useCRLF bool) string {
		tmpDir := projectWithTablesDir(t, `'tables\prod'`)
		if useCRLF {
			toCRLF(t, tmpDir)
		}
		useBackend(t, &backendtest.MockUCBackend{})
		out, err := executeCmd("plan", "-t", "dev")
		require.NoError(t, err, out)
		return out
	}
	assert.Equal(t, run(false), run(true))
}

// --- 5, 6. pull: path spelling and tables_dir variants ---

func pullPathLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "tables") {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestPull_TablesDirVariants_SameKeysAndRecognizedOnSecondPull(t *testing.T) {
	variants := map[string]string{
		"tables/prod/": pathTablesProd, `'tables\prod\'`: pathTablesProd, pathTables: litTables,
		`'.\tables'`: litTables, "tables/prod": "tables/prod",
	}
	for literal, base := range variants {
		t.Run(literal, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			writeFile(t, filepath.Join(tmpDir, "ucat.yml"), "project: p\ntables_dir: "+literal+"\n")
			useBackend(t, upToDateMock())

			out, err := executeCmd("pull", "-t", "dev")
			require.NoError(t, err, out)
			created := base + "/silver/orders.yml"
			assert.Contains(t, out, "created  "+created)
			for _, l := range pullPathLines(out) {
				assert.NotContains(t, l, `\`, l)
				assert.NotContains(t, l, "//", l)
				assert.NotContains(t, l, "./", l)
			}
			assert.FileExists(t, filepath.Join(tmpDir, filepath.FromSlash(created)))

			out, err = executeCmd("pull", "-t", "dev")
			require.NoError(t, err, out)
			assert.Contains(t, out, "(defined in "+created+")")
			assert.NotContains(t, out, "created")
			for _, l := range pullPathLines(out) {
				assert.NotContains(t, l, `\`, l)
			}
		})
	}
}

func TestPull_Force_NoBackslashInPaths(t *testing.T) {
	for _, literal := range []string{"tables", `'tables\prod'`} {
		t.Run(literal, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			writeFile(t, filepath.Join(tmpDir, "ucat.yml"), "project: p\ntables_dir: "+literal+"\n")
			useBackend(t, upToDateMock())
			_, err := executeCmd("pull", "-t", "dev")
			require.NoError(t, err)

			out, err := executeCmd("pull", "-t", "dev", "--force")
			require.NoError(t, err, out)
			lines := pullPathLines(out)
			require.NotEmpty(t, lines, out)
			for _, l := range lines {
				assert.NotContains(t, l, `\`, l)
			}

			// without --force the existing-file skip message also uses slashes
			out, err = executeCmd("pull", "-t", "dev")
			require.NoError(t, err, out)
			for _, l := range pullPathLines(out) {
				assert.NotContains(t, l, `\`, l)
			}
		})
	}
}

func toCRLF(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yml") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(p, []byte(crlf(string(b))), 0644)
	}))
}

// A project saved with CRLF (Windows, git autocrlf) behaves exactly like the LF one.
func TestCRLFProject_SameResultAsLF(t *testing.T) {
	run := func(crlf bool) (validate, plan string) {
		tmpDir := setupPlanProject(t)
		// a second table with a comment and a multi-line block, to exercise line handling
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tables", "silver", "extra.yml"),
			[]byte("name: extra\nschema: silver\n# comment\ncolumns:\n  - name: id\n    type: BIGINT\n    comment: \"a\"\n"), 0644))
		if crlf {
			toCRLF(t, tmpDir)
		}
		m := upToDateMock()
		useBackend(t, m)
		v, err := executeCmd("validate", "-t", "dev")
		require.NoError(t, err, v)
		p, _ := executeCmd("plan", "-t", "dev")
		return v, p
	}
	lfV, lfP := run(false)
	crV, crP := run(true)
	assert.Equal(t, lfV, crV)
	assert.Equal(t, lfP, crP)
	assert.NotContains(t, crV+crP, "\r")
}
