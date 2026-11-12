package cmd

// quickstart_example_test.go keeps examples/quickstart valid and the scenario it documents true:
// base tables, step 1 (add a column, clustering, a tblproperty) and step 2 (rename a table).

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
)

const (
	qsCatalog = "your_catalog" // uc_name in targets/dev.yml.example
	qsSchema  = "shop"

	qsOrderItems = "order_items"

	qsOpAddColumn       = "add_column"
	qsOpAlterClustering = "alter_clustering"
)

var qsDevRef = backend.SchemaRef{UCCatalog: qsCatalog, UCSchema: qsSchema}

// qsChange is the part of a plan --json change the scenario asserts on.
type qsChange struct {
	Op           string `json:"op"`
	Schema       string `json:"schema"`
	Table        string `json:"table"`
	Column       string `json:"column"`
	PreviousName string `json:"previous_name"`
	SQL          string `json:"sql"`
}

type qsPlan struct {
	Changes []qsChange `json:"changes"`
}

// qsRoot is examples/quickstart, found from the location of this file (not from the working directory).
func qsRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "examples", "quickstart")
}

func qsCopyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	writeFile(t, dst, string(data))
}

// qsProject copies the example into a temp directory, turns targets/*.yml.example into targets/*.yml,
// applies the first steps steps of the README (1: add a column, 2: rename a table) and returns the
// directory. It does not change the working directory.
func qsProject(t *testing.T, steps int) string {
	t.Helper()
	src := qsRoot(t)
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.ToSlash(rel), "steps/") {
			return nil
		}
		qsCopyFile(t, path, filepath.Join(dst, strings.TrimSuffix(rel, ".example")))
		return nil
	})
	require.NoError(t, err)
	if steps >= 1 {
		qsCopyFile(t, filepath.Join(src, "steps", "1-add-column", "orders.yml"), filepath.Join(dst, "tables", "shop", "orders.yml"))
	}
	if steps >= 2 {
		require.NoError(t, os.Remove(filepath.Join(dst, "tables", "shop", "order_items.yml")))
		qsCopyFile(t, filepath.Join(src, "steps", "2-rename-table", "order_lines.yml"), filepath.Join(dst, "tables", "shop", "order_lines.yml"))
	}
	return dst
}

// qsLive is the live state that matches the tables of the project exactly (dev target).
func qsLive(t *testing.T, dir string) []*backend.LiveTable {
	t.Helper()
	proj, diags := config.LoadProject(dir)
	require.False(t, diags.HasError(), diags)
	tables, diags := config.LoadTables(dir, proj)
	require.Empty(t, diags, diags)
	live := make([]*backend.LiveTable, 0, len(tables))
	for _, tb := range tables {
		tb.ResolvedCatalog, tb.ResolvedSchema = qsCatalog, qsSchema
		live = append(live, backendtest.LiveOf(tb))
	}
	return live
}

// qsPlanJSON runs plan -t dev --json in the project and returns the changes.
func qsPlanJSON(t *testing.T, dir string, mock *backendtest.MockUCBackend) []qsChange {
	t.Helper()
	t.Chdir(dir)
	useBackend(t, mock)
	stdout, stderr, err := executeCmdSplit("plan", "-t", litDev, "--json")
	require.NoError(t, err, stderr)
	var p qsPlan
	decodeSingleJSON(t, stdout, &p)
	return p.Changes
}

func qsAssertNoChanges(t *testing.T, dir string, mock *backendtest.MockUCBackend) {
	t.Helper()
	t.Chdir(dir)
	useBackend(t, mock)
	stdout, stderr, err := executeCmdSplit("plan", "-t", litDev, "--detailed-exitcode")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "No changes")
}

func qsMock(tables []*backend.LiveTable) *backendtest.MockUCBackend {
	m := &backendtest.MockUCBackend{}
	m.SetFixture(qsDevRef, tables)
	return m
}

// Every stage of the example validates locally without warnings, and plan loads for both targets.
func TestQuickstart_ValidateAndLoad(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps int
	}{{"base", 0}, {"step 1", 1}, {"step 2", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(qsProject(t, tc.steps))
			stdout, stderr, err := executeCmdSplit("validate")
			require.NoError(t, err, stderr)
			assert.Contains(t, stdout, "all local checks passed")
			assert.NotContains(t, stderr, "share the UC catalog", "dev and prod use different catalogs")
			assert.Empty(t, stderr)

			for _, target := range []string{litDev, litProd} {
				useBackend(t, &backendtest.MockUCBackend{})
				_, stderr, err := executeCmdSplit("plan", "-t", target)
				require.NoError(t, err, "%s: %s", target, stderr)
				assert.NotContains(t, stderr, "share the UC catalog")
			}
		})
	}
}

// Against an empty schema the base stage creates the three tables; once they exist, nothing is left to do.
func TestQuickstart_Base(t *testing.T) {
	dir := qsProject(t, 0)

	changes := qsPlanJSON(t, dir, qsMock(nil))
	var created []string
	clustered := map[string]string{"customers": "CLUSTER BY (`country`)", litOrders: "CLUSTER BY (`created_at`)", qsOrderItems: "CLUSTER BY (`order_id`)"}
	for _, c := range changes {
		assert.Equal(t, "create_table", c.Op)
		assert.Equal(t, qsSchema, c.Schema)
		assert.Contains(t, c.SQL, clustered[c.Table], "%s is created with explicit clustering", c.Table)
		created = append(created, c.Table)
	}
	assert.ElementsMatch(t, []string{"customers", litOrders, qsOrderItems}, created)

	qsAssertNoChanges(t, dir, qsMock(qsLive(t, dir)))
}

// Step 1 changes only orders: a new column, the clustering (created_at to customer_id) and a tblproperty. Applying it leaves no changes.
func TestQuickstart_Step1_AddColumn(t *testing.T) {
	live := qsLive(t, qsProject(t, 0))
	dir := qsProject(t, 1)

	changes := qsPlanJSON(t, dir, qsMock(live))
	got := make([]qsChange, len(changes))
	for i, c := range changes {
		if c.Op == qsOpAddColumn {
			assert.Contains(t, c.SQL, "`discount` DECIMAL(5,2)", "the new column keeps its type")
		}
		if c.Op == qsOpAlterClustering {
			// the JSON alter field covers alter_column only, so the target columns are read from the SQL
			assert.Contains(t, c.SQL, "CLUSTER BY (`customer_id`)", "orders is re-clustered to customer_id")
			assert.NotContains(t, c.SQL, "created_at")
		}
		got[i] = qsChange{Op: c.Op, Schema: c.Schema, Table: c.Table, Column: c.Column}
	}
	assert.ElementsMatch(t, []qsChange{
		{Op: qsOpAddColumn, Schema: qsSchema, Table: litOrders, Column: "discount"},
		{Op: qsOpAlterClustering, Schema: qsSchema, Table: litOrders},
		{Op: "set_tbl_properties", Schema: qsSchema, Table: litOrders},
	}, got)

	t.Chdir(dir)
	mock := qsMock(live)
	useBackend(t, mock)
	_, stderr, err := executeCmdSplit("apply", "-t", litDev, "--auto-approve")
	require.NoError(t, err, stderr)
	var kinds []change.Kind
	for _, c := range mock.RecordedCalls() {
		kinds = append(kinds, c.Kind)
	}
	assert.ElementsMatch(t, []change.Kind{change.AddColumn, change.AlterClustering, change.SetTBLProperties}, kinds)

	qsAssertNoChanges(t, dir, qsMock(qsLive(t, dir)))
}

// Step 2 is one rename, not a drop and a create. Applying it leaves no changes.
func TestQuickstart_Step2_RenameTable(t *testing.T) {
	live := qsLive(t, qsProject(t, 1))
	dir := qsProject(t, 2)

	changes := qsPlanJSON(t, dir, qsMock(live))
	require.Len(t, changes, 1)
	changes[0].SQL = ""
	assert.Equal(t, qsChange{Op: "rename_table", Schema: qsSchema, Table: "order_lines", PreviousName: qsOrderItems}, changes[0])

	t.Chdir(dir)
	mock := qsMock(live)
	useBackend(t, mock)
	_, stderr, err := executeCmdSplit("apply", "-t", litDev, "--auto-approve")
	require.NoError(t, err, stderr)
	calls := mock.RecordedCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, change.RenameTable, calls[0].Kind)

	qsAssertNoChanges(t, dir, qsMock(qsLive(t, dir)))
}

// Every YAML file of the example loads with its strict loader, the steps parse as table files,
// and no real target (host, warehouse, catalog) is committed next to the examples.
func TestQuickstart_Guard(t *testing.T) {
	root := qsRoot(t)

	t.Run("steps parse as tables", func(t *testing.T) {
		steps, err := filepath.Glob(filepath.Join(root, "steps", "*", "*.yml"))
		require.NoError(t, err)
		require.Len(t, steps, 2)
		base := qsProject(t, 0)
		proj, diags := config.LoadProject(base)
		require.False(t, diags.HasError(), diags)
		for _, step := range steps {
			dir := t.TempDir()
			qsCopyFile(t, step, filepath.Join(dir, proj.TablesDir, "shop", filepath.Base(step)))
			tables, diags := config.LoadTables(dir, proj)
			require.Empty(t, diags, "%s: %v", step, diags)
			require.Len(t, tables, 1, step)
		}
	})

	t.Run("target examples load", func(t *testing.T) {
		examples, err := filepath.Glob(filepath.Join(root, "targets", "*.yml.example"))
		require.NoError(t, err)
		require.Len(t, examples, 2)
		dir := qsProject(t, 0)
		for _, ex := range examples {
			name := strings.TrimSuffix(filepath.Base(ex), ".yml.example")
			target, diags := config.LoadTarget(dir, name)
			require.Empty(t, diags, "%s: %v", name, diags)
			assert.NotEmpty(t, target.Workspace.Host, name)
			assert.NotEmpty(t, target.Catalogs["main"].UCName, name)
		}
	})

	t.Run("only target examples are committed", func(t *testing.T) {
		tracked, err := qsTrackedTargets(filepath.Join(root, "..", ".."))
		if err != nil {
			t.Skipf("cannot list the files git tracks (no git or not a work tree): %v", err)
		}
		assert.Empty(t, qsNotExamples(tracked), "real targets must stay local (gitignored): only *.yml.example may be committed")
	})

	t.Run("the check finds a committed real target", func(t *testing.T) {
		assert.Equal(t,
			[]string{"examples/quickstart/targets/dev.yml"},
			qsNotExamples([]string{"examples/quickstart/targets/dev.yml", "examples/quickstart/targets/prod.yml.example"}))
	})
}

// qsTrackedTargets lists the files git tracks (committed or staged) in examples/quickstart/targets,
// as slash paths relative to the repository root. A test replaces it to simulate a committed target.
var qsTrackedTargets = func(repo string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--", "examples/quickstart/targets")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// qsNotExamples returns the files that are not *.yml.example.
func qsNotExamples(files []string) []string {
	var bad []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".yml.example") {
			bad = append(bad, f)
		}
	}
	return bad
}
