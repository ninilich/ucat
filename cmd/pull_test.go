package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

// --- Layer 1: flag validation (no backend needed) ---

// TestPull_SchemaWithoutCatalog verifies that --schema without --catalog returns an error.
func TestPull_SchemaWithoutCatalog(t *testing.T) {
	out, err := executeCmd("pull", "--schema", "silver")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "--schema requires --catalog")
}

// TestPull_RequiresTarget verifies that pull without -t returns an error.
func TestPull_RequiresTarget(t *testing.T) {
	out, err := executeCmd("pull")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "pull requires -t")
}

// --- Layer 2: with mock backend ---

// TestPull_CreatesNewFile verifies that a table returned by the backend creates a new YAML file.
func TestPull_CreatesNewFile(t *testing.T) {
	tmpDir := setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Schema: litSilver, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useBackend(t, mock)

	out, err := executeCmd("pull", "-t", "dev")
	require.NoError(t, err)

	filePath := filepath.Join(tmpDir, "tables", "silver", "orders.yml")
	assert.FileExists(t, filePath)

	content, readErr := os.ReadFile(filePath)
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "schema: silver")
	assert.Contains(t, out, "created")
	assert.Contains(t, out, "tables/silver/orders.yml")
}

// TestPull_SkipsExistingFile verifies that an existing file is not overwritten without --force.
func TestPull_SkipsExistingFile(t *testing.T) {
	tmpDir := setupTestProject(t)

	dir := filepath.Join(tmpDir, "tables", "silver")
	require.NoError(t, os.MkdirAll(dir, 0755))
	existingContent := "name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: INT\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yml"), []byte(existingContent), 0644))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Schema: litSilver, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useBackend(t, mock)

	out, err := executeCmd("pull", "-t", "dev")
	require.NoError(t, err)

	assert.Contains(t, out, "skipped")
	assert.Contains(t, out, "tables/silver/orders.yml")
	assert.Contains(t, out, "defined in tables/silver/orders.yml")

	content, readErr := os.ReadFile(filepath.Join(dir, "orders.yml"))
	require.NoError(t, readErr)
	assert.Equal(t, existingContent, string(content), "file must not be overwritten without --force")
}

// TestPull_ForceOverwrites verifies that --force overwrites an existing file.
func TestPull_ForceOverwrites(t *testing.T) {
	tmpDir := setupTestProject(t)

	dir := filepath.Join(tmpDir, "tables", "silver")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yml"), []byte("name: orders\nschema: silver\ncomment: old content\ncolumns:\n  - name: id\n    type: INT\n"), 0644))

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Schema: litSilver, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useBackend(t, mock)

	_, err := executeCmd("pull", "-t", "dev", "--force")
	require.NoError(t, err)

	content, readErr := os.ReadFile(filepath.Join(dir, "orders.yml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "schema: silver")
	assert.NotContains(t, string(content), "old content", "file must be overwritten with --force")
}

// TestPull_EmptySchema verifies that no files are created when the backend returns no tables.
func TestPull_EmptySchema(t *testing.T) {
	tmpDir := setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{})
	useBackend(t, mock)

	_, err := executeCmd("pull", "-t", "dev")
	require.NoError(t, err)

	tablesDir := filepath.Join(tmpDir, "tables")
	entries, readErr := os.ReadDir(tablesDir)
	if os.IsNotExist(readErr) {
		return // tables dir not created - acceptable for empty schema
	}
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no table files should be created for an empty schema")
}

// TestPull_GeneratedYAMLFields verifies that comment and tblproperties are written to the YAML.
func TestPull_GeneratedYAMLFields(t *testing.T) {
	tmpDir := setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name:          litOrders,
			Schema:        litSilver,
			Comment:       "Order data",
			TblProperties: map[string]*string{"delta.autoOptimize.optimizeWrite": strPtr("true")},
			Columns:       []config.Column{{Name: "id", Type: "BIGINT"}},
		}),
	})
	useBackend(t, mock)

	_, err := executeCmd("pull", "-t", "dev", "--tblproperties", "delta.autoOptimize.*")
	require.NoError(t, err)

	content, readErr := os.ReadFile(filepath.Join(tmpDir, "tables", "silver", "orders.yml"))
	require.NoError(t, readErr)
	body := string(content)
	assert.Contains(t, body, "comment: Order data")
	assert.Contains(t, body, "delta.autoOptimize.optimizeWrite")
}

// --- strict YAML: round-trip and lifecycle ---

func falsePtr() *bool { b := false; return &b }

// strictFixtureTables returns two fully populated tables (clustering and partitioning
// are mutually exclusive, so they live in separate tables). Resolved fields are set
// so the same fixture can serve as live state for plan.
func strictFixtureTables() []*backend.LiveTable {
	cols := []config.Column{
		{Name: "id", Type: "BIGINT", Comment: "primary key", Nullable: falsePtr()},
		{Name: "dt", Type: "DATE"},
	}
	return []*backend.LiveTable{
		backendtest.LiveOf(config.Table{
			Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
			Comment:       "Order data",
			Columns:       cols,
			Clustering:    &config.Clustering{Columns: []string{"id"}},
			TblProperties: map[string]*string{"delta.autoOptimize.optimizeWrite": strPtr("true")},
		}),
		backendtest.LiveOf(config.Table{
			Name: litEvents, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
			Comment:      "Event data",
			Columns:      cols,
			Partitioning: &config.Partitioning{Columns: []string{"dt"}},
		}),
	}
}

// pullStrictFixture runs pull against a mock holding strictFixtureTables and returns the mock.
// The backend factory is restored by t.Cleanup (also when require fails).
func pullStrictFixture(t *testing.T) *backendtest.MockUCBackend {
	t.Helper()
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, strictFixtureTables())
	useBackend(t, mock)
	_, err := executeCmd("pull", "-t", "dev", "--tblproperties", "delta.autoOptimize.*")
	require.NoError(t, err)
	return mock
}

// TestPull_RoundTrip_StrictLoader verifies that the strict loader accepts pull's own output.
func TestPull_RoundTrip_StrictLoader(t *testing.T) {
	tmpDir := setupTestProject(t)
	pullStrictFixture(t)

	proj, diags := config.LoadProject(tmpDir)
	require.False(t, diags.HasError(), diags)
	tables, diags := config.LoadTables(tmpDir, proj)
	require.Empty(t, diags, diags)
	require.Len(t, tables, 2)

	byName := map[string]config.Table{}
	for _, tbl := range tables {
		byName[tbl.Name] = tbl
	}
	orders := byName["orders"]
	assert.Equal(t, "Order data", orders.Comment)
	require.Len(t, orders.Columns, 2)
	assert.Equal(t, "primary key", orders.Columns[0].Comment)
	require.NotNil(t, orders.Columns[0].Nullable)
	assert.False(t, *orders.Columns[0].Nullable)
	require.NotNil(t, orders.Clustering)
	assert.Equal(t, []string{"id"}, orders.Clustering.Columns)
	require.Contains(t, orders.TblProperties, "delta.autoOptimize.optimizeWrite")
	require.NotNil(t, byName["events"].Partitioning)
	assert.Equal(t, []string{"dt"}, byName["events"].Partitioning.Columns)
}

// TestPull_Lifecycle_TagsRejectedThenRemoved: pull -> plan 0 -> add tags -> plan fails
// before any backend access -> remove tags -> plan 0.
func TestPull_Lifecycle_TagsRejectedThenRemoved(t *testing.T) {
	tmpDir := setupTestProject(t)
	mock := pullStrictFixture(t)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err, out)
	assert.Contains(t, out, "No changes")

	orders := filepath.Join(tmpDir, "tables", "silver", "orders.yml")
	orig, err := os.ReadFile(orders)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(orders, append(append([]byte{}, orig...), []byte("tags:\n  team: data\n")...), 0644))

	backendCalls := 0
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		backendCalls++
		return mock, nil
	})
	out, err = executeCmd("plan", "-t", "dev")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, `field "tags" is not supported`)
	assert.Contains(t, combined, "orders.yml:")
	assert.Zero(t, backendCalls, "plan must fail on invalid YAML before touching the backend")

	require.NoError(t, os.WriteFile(orders, orig, 0644))
	out, err = executeCmd("plan", "-t", "dev")
	require.NoError(t, err, out)
	assert.Contains(t, out, "No changes")
}

// TestPull_Lifecycle_ValidateAfterPull verifies validate accepts pull's output.
func TestPull_Lifecycle_ValidateAfterPull(t *testing.T) {
	setupTestProject(t)
	pullStrictFixture(t)

	out, err := executeCmd("validate")
	require.NoError(t, err, out)
}

// eachFlag calls fn for every flag (local and persistent) of c and all its subcommands.
func eachFlag(c *cobra.Command, fn func(c *cobra.Command, f *pflag.Flag)) {
	c.Flags().VisitAll(func(f *pflag.Flag) { fn(c, f) })
	c.PersistentFlags().VisitAll(func(f *pflag.Flag) { fn(c, f) })
	for _, sub := range c.Commands() {
		eachFlag(sub, fn)
	}
}

// The root has no PersistentPreRun(E) (the only one, initLogging, went away with --debug), and no
// subcommand adds its own: a hook must be added on purpose, with the tests of its behavior.
func TestNoPersistentPreRunHooks(t *testing.T) {
	root := newRootCmd(newApp())
	assert.Nil(t, root.PersistentPreRun)
	assert.Nil(t, root.PersistentPreRunE)
	require.Greater(t, len(root.Commands()), 0)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			assert.Nil(t, sub.PersistentPreRun, sub.CommandPath())
			assert.Nil(t, sub.PersistentPreRunE, sub.CommandPath())
			walk(sub)
		}
	}
	walk(root)
}

// pull writes no nullable key for a nullable column and nullable: false for a NOT NULL one, and the
// file it wrote plans to no changes against the same live state (nullable true and false both).
func TestPull_NullableNotWrittenForTrue_PlanIsEmpty(t *testing.T) {
	tmpDir := setupTestProject(t)
	pullStrictFixture(t)

	data, err := os.ReadFile(filepath.Join(tmpDir, "tables", "silver", "events.yml"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), "nullable:"), string(data))
	assert.Contains(t, string(data), "nullable: false")
	assert.NotContains(t, string(data), "nullable: true")

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err, out)
	assert.Contains(t, out, "No changes")
}
