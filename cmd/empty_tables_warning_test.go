package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- warning when no tables are defined ---

// countEmptyTablesWarnings counts the "no tables" warnings (any of the two spellings) in s.
func countEmptyTablesWarnings(s string) int {
	return strings.Count(s, "tables_dir ") + strings.Count(s, "no table files")
}

// emptyTablesProject makes a project whose ucat.yml has the given tables_dir (written as a YAML
// string, so backslashes survive), with an empty live schema. It returns the project dir.
func emptyTablesProject(t *testing.T, tablesDir string) string {
	t.Helper()
	dir := setupTestProject(t)
	writeFile(t, filepath.Join(dir, "ucat.yml"), "project: p\ntables_dir: '"+tablesDir+"'\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{})
	useBackend(t, mock)
	return dir
}

// tablesWarningArgs are the commands that must warn: plan, apply, validate and validate -t.
var tablesWarningArgs = [][]string{
	{cmdPlan, "-t", litDev},
	{cmdApply, "-t", litDev},
	{cmdValidate},
	{cmdValidate, "-t", litDev},
}

func TestEmptyTablesWarning_PerCommand(t *testing.T) {
	const notFound = `warning: tables_dir "tables" not found (no tables are defined)`
	const noFiles = `warning: no table files (*.yml) found in "tables"`
	setup := func(t *testing.T, tablesDir string, yamlFiles int) {
		dir := setupTestProject(t)
		if tablesDir != "" {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, tablesDir, "silver"), 0755))
		}
		for i := 0; i < yamlFiles; i++ {
			writeFile(t, filepath.Join(dir, "tables", "silver", "t"+string(rune('a'+i))+".yaml"), "name: x\n")
		}
		mock := &backendtest.MockUCBackend{}
		mock.SetFixture(silverRef, []*backend.LiveTable{})
		useBackend(t, mock)
	}

	t.Run("dir missing", func(t *testing.T) {
		for _, args := range tablesWarningArgs {
			setup(t, "", 0)
			_, stderr, err := executeCmdSplit(args...)
			require.NoError(t, err, args)
			assert.Equal(t, 1, strings.Count(stderr, notFound), "%v: %s", args, stderr)
			assert.Equal(t, 1, countEmptyTablesWarnings(stderr), "%v: exactly one warning: %s", args, stderr)
		}
	})
	t.Run("no yml, yaml hint", func(t *testing.T) {
		setup(t, "tables", 2)
		_, stderr, err := executeCmdSplit("plan", "-t", "dev")
		require.NoError(t, err)
		assert.Contains(t, stderr, noFiles+"; found 2 file(s) named *.yaml or with an upper-case extension (*.YML), which are ignored: rename them to .yml\n")
		assert.Equal(t, 1, strings.Count(stderr, "file(s)"), "the files are reported once, by the empty-tables warning: %s", stderr)
	})
	t.Run("empty dir", func(t *testing.T) {
		setup(t, "tables", 0)
		stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(stderr, noFiles))
		assert.NotContains(t, stderr, "*.yaml")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(stdout), "{"), stdout)
	})
	t.Run("not for pull and drop", func(t *testing.T) {
		setup(t, "", 0)
		_, stderr, err := executeCmdSplit("pull", "-t", "dev")
		require.NoError(t, err)
		assert.NotContains(t, stderr, "tables_dir")
		_, stderr, err = executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "x", "--auto-approve")
		require.NoError(t, err)
		assert.NotContains(t, stderr, "tables_dir")
	})
	t.Run("drop-only tables are defined", func(t *testing.T) {
		setup(t, "tables", 0)
		writeFile(t, filepath.Join("tables", "silver", "old.yml"), "name: old\nschema: silver\ndrop: true\n")
		_, stderr, err := executeCmdSplit("plan", "-t", "dev")
		require.NoError(t, err)
		assert.NotContains(t, stderr, "tables_dir")
		assert.NotContains(t, stderr, "no table files")
	})
}

// The warning is absent for version (init has no config; pull and drop are in PerCommand).
func TestEmptyTablesWarning_NotForInitAndVersion(t *testing.T) {
	emptyTablesProject(t, "tables")
	_, stderr, err := executeCmdSplit("version")
	require.NoError(t, err)
	assert.Zero(t, countEmptyTablesWarnings(stderr))
}

// Every spelling of tables_dir gets exactly one warning per command, with the value shown with "/".
func TestEmptyTablesWarning_TablesDirVariants(t *testing.T) {
	cases := []struct {
		name  string
		value string
		setup func(t *testing.T, dir string)
		want  string
	}{
		{"missing", litTables, nil, `tables_dir "tables" not found (no tables are defined)`},
		{"trailing slash", "tables/", nil, `tables_dir "tables/" not found (no tables are defined)`},
		{"dot slash prefix", pathTables, nil, `tables_dir "./tables" not found (no tables are defined)`},
		{"backslash separator", `tables\sub`, nil, `tables_dir "tables/sub" not found (no tables are defined)`},
		{"backslash trailing", `tables\`, nil, `tables_dir "tables/" not found (no tables are defined)`},
		{"existing nested dir, no files", `tables\sub`, func(t *testing.T, dir string) {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "tables", "sub"), 0755))
		}, `no table files (*.yml) found in "tables/sub"`},
		{"empty dir", litTables, func(t *testing.T, dir string) {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "tables"), 0755))
		}, `no table files (*.yml) found in "tables"`},
		// Contract does not define a file in place of the directory; fixed by fact: "no table files".
		{"path is a file", "afile", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "afile"), "x")
		}, `no table files (*.yml) found in "afile"`},
		// Directories named *.yml, *.yml.bak, *.txt and a .yml outside tables_dir are not tables.
		{"yml only in ignored places", litTables, func(t *testing.T, dir string) {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "tables", "x.yml"), 0755))
			writeFile(t, filepath.Join(dir, "tables", "a.yml.bak"), "x")
			writeFile(t, filepath.Join(dir, "tables", "a.txt"), "x")
			writeFile(t, filepath.Join(dir, "docs", "extra.yml"), "x")
		}, `no table files (*.yml) found in "tables"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.value
			dir := emptyTablesProject(t, value)
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			want := tc.want
			if want == "" {
				want = `tables_dir "` + filepath.ToSlash(value) + `" not found (no tables are defined)`
			}
			for _, args := range tablesWarningArgs {
				_, stderr, err := executeCmdSplit(args...)
				require.NoError(t, err, args)
				assert.Equal(t, "warning: "+want+"\n", extractWarningLines(stderr), "%v: %s", args, stderr)
				assert.Equal(t, 1, countEmptyTablesWarnings(stderr), "%v: %s", args, stderr)
			}
		})
	}
}

// extractWarningLines returns the lines of s that are warnings, each with its newline.
func extractWarningLines(s string) string {
	var b strings.Builder
	for _, l := range strings.SplitAfter(s, "\n") {
		if strings.HasPrefix(l, "warning:") {
			b.WriteString(l)
		}
	}
	return b.String()
}

// The *.yaml hint counts files at any depth, ignores *.yml-less names and skips non-yaml files.
func TestEmptyTablesWarning_YamlHintCountsRecursively(t *testing.T) {
	dir := emptyTablesProject(t, "tables")
	writeFile(t, filepath.Join(dir, "tables", "top.yaml"), "x")
	writeFile(t, filepath.Join(dir, "tables", "silver", "mid.yaml"), "x")
	writeFile(t, filepath.Join(dir, "tables", "silver", "deep", "er", "deepest.yaml"), "x")
	writeFile(t, filepath.Join(dir, "tables", "silver", "note.txt"), "x")
	writeFile(t, filepath.Join(dir, "docs", "outside.yaml"), "x") // outside tables_dir

	for _, args := range tablesWarningArgs {
		_, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, args)
		assert.Equal(t, `warning: no table files (*.yml) found in "tables"; found 3 file(s) named *.yaml or with an upper-case extension (*.YML), which are ignored: rename them to .yml`+"\n",
			extractWarningLines(stderr), "%v", args)
	}
}

// A project with both .yml and .yaml files has tables: no "no tables" warning, but the ignored .yaml
// is reported by its own warning (see load_warnings_test.go).
func TestEmptyTablesWarning_MixedYmlAndYaml_NoEmptyWarning(t *testing.T) {
	dir := emptyTablesProject(t, "tables")
	writeFile(t, filepath.Join(dir, "tables", "silver", "a.yml"), "name: a\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")
	writeFile(t, filepath.Join(dir, "tables", "silver", "b.yaml"), "name: b\nschema: silver\n")
	for _, args := range tablesWarningArgs {
		if args[0] == cmdApply {
			args = append(append([]string{}, args...), "--auto-approve") // a is a new table
		}
		_, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, args)
		assert.Zero(t, countEmptyTablesWarnings(stderr), "%v: %s", args, stderr)
		assert.Equal(t, 1, strings.Count(stderr, "1 file(s) are ignored"), "%v: %s", args, stderr)
	}
}

// The warning is never an error: exit codes stay 0 and plan --json keeps a valid JSON on stdout.
func TestEmptyTablesWarning_ExitCodesAndJSON(t *testing.T) {
	emptyTablesProject(t, "tables")
	for _, args := range tablesWarningArgs {
		_, _, err := executeCmdSplit(args...)
		require.NoError(t, err, args)
	}
	_, _, err := executeCmdSplit("plan", "-t", "dev", "--detailed-exitcode")
	require.NoError(t, err, "empty plan with --detailed-exitcode is still 0")

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)
	var v map[string]any
	decodeSingleJSON(t, stdout, &v)
	assert.NotContains(t, stdout, "tables_dir")
	assert.Equal(t, 1, countEmptyTablesWarnings(stderr))

	stdout, stderr, err = executeCmdSplit("apply", "-t", "dev", "--json")
	require.NoError(t, err)
	decodeSingleJSON(t, stdout, &v)
	assert.Equal(t, 1, countEmptyTablesWarnings(stderr))
}

// Order is fixed by fact: the warning is printed while the config is loaded, i.e. before the local
// checks result and before any live-validation line.
func TestEmptyTablesWarning_ValidateOrder(t *testing.T) {
	emptyTablesProject(t, "tables")
	out, err := executeCmd("validate", "-t", "dev")
	require.NoError(t, err)
	w := strings.Index(out, "no tables are defined")
	require.GreaterOrEqual(t, w, 0, out)
	assert.Less(t, w, strings.Index(out, "all local checks passed"), out)
	assert.Less(t, w, strings.Index(out, "schema dev.silver exists"), out)
}

// A project that fails earlier (broken catalog) reports that error and no empty-tables warning.
func TestEmptyTablesWarning_NotPrintedWhenConfigFailsEarlier(t *testing.T) {
	dir := emptyTablesProject(t, "tables")
	writeFile(t, filepath.Join(dir, "catalogs", "main.yml"), "name: [broken\n")
	for _, args := range tablesWarningArgs {
		_, stderr, err := executeCmdSplit(args...)
		require.Error(t, err, args)
		assert.Zero(t, countEmptyTablesWarnings(stderr), "%v: %s", args, stderr)
	}
}

// The *.yaml hint of the empty-tables warning counts upper-case extensions too (.YML is not read either).
func TestEmptyTablesWarning_UpperCaseExtensionsCounted(t *testing.T) {
	dir := emptyTablesProject(t, "tables")
	writeFile(t, filepath.Join(dir, "tables", "silver", "a.YML"), "x")
	writeFile(t, filepath.Join(dir, "tables", "silver", "b.yaml"), "x")
	writeFile(t, filepath.Join(dir, "tables", "silver", "c.txt"), "x")
	_, stderr, err := executeCmdSplit("plan", "-t", litDev)
	require.NoError(t, err)
	assert.Contains(t, stderr, `no table files (*.yml) found in "tables"; found 2 file(s) named *.yaml or with an upper-case extension (*.YML), which are ignored`)
}
