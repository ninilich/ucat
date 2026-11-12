package cmd

// validate_parity_test.go covers: validate -t loads and validates the config
// exactly like plan -t (shared loadConfig + ValidateLocal on the target config).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const parityCols = "columns:\n  - name: id\n    type: BIGINT\n"

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

type parityCase struct {
	name        string
	mutate      func(t *testing.T, dir string)
	targetOnly  bool // violation lives in the target file: validate without -t cannot see it
	wantInError string
}

func parityCases() []parityCase {
	return []parityCase{
		{name: "unknown field in table yaml", wantInError: "bogus",
			mutate: func(t *testing.T, dir string) {
				addTableFile(t, dir, "silver", "orders", "name: orders\nschema: silver\nbogus: 1\n"+parityCols)
			}},
		{name: "uppercase catalog in target", targetOnly: true, wantInError: "Prod",
			mutate: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "targets", "dev.yml"), "catalogs:\n  main:\n    uc_name: Prod\n")
			}},
		{name: litUnknownSchema, wantInError: "unknown schema",
			mutate: func(t *testing.T, dir string) {
				addTableFile(t, dir, "nonexistent", "ghost", "name: ghost\nschema: nonexistent\n"+parityCols)
			}},
		{name: litDuplicateTable, wantInError: litOrders,
			mutate: func(t *testing.T, dir string) {
				addTableFile(t, dir, "silver", "orders_a", "name: orders\nschema: silver\n"+parityCols)
				addTableFile(t, dir, "silver", "orders_b", "name: orders\nschema: silver\n"+parityCols)
			}},
		{name: "invalid column type", wantInError: "type",
			mutate: func(t *testing.T, dir string) {
				addTableFile(t, dir, "silver", "orders", "name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: NOTATYPE\n")
			}},
		{name: "clustering and partitioning together", wantInError: litOrders,
			mutate: func(t *testing.T, dir string) {
				addTableFile(t, dir, "silver", "orders",
					"name: orders\nschema: silver\nclustering:\n  columns: [id]\npartitioning:\n  columns: [id]\n"+parityCols)
			}},
		{name: "empty clustering", wantInError: "clustering",
			mutate: func(t *testing.T, dir string) {
				addTableFile(t, dir, "silver", "orders", "name: orders\nschema: silver\nclustering: {}\n"+parityCols)
			}},
		{name: "no columns", wantInError: "column",
			mutate: func(t *testing.T, dir string) {
				addTableFile(t, dir, "silver", "orders", "name: orders\nschema: silver\n")
			}},
		{name: "logical schema in two catalogs", wantInError: litSilver,
			mutate: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "catalogs", "other.yml"),
					"name: other\nuc_name: other\nschemas:\n  silver:\n    uc_name: silver\n")
				addTableFile(t, dir, "silver", "orders", "name: orders\nschema: silver\n"+parityCols)
			}},
	}
}

func TestValidateTarget_PlanParity(t *testing.T) {
	for _, tc := range parityCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupTestProject(t)
			tc.mutate(t, dir)
			spy := useSpy(t, newSpy(nil).forbid(t))

			planOut, planErr, err := executeCmdSplit("plan", "-t", "dev")
			require.Error(t, err)
			valOut, valErr, err := executeCmdSplit("validate", "-t", "dev")
			require.Error(t, err, "validate -t must fail like plan -t; stdout: %s", valOut)

			planLines, valLines := errorLines(planErr), errorLines(valErr)
			require.NotEmpty(t, planLines, "plan stderr: %s", planErr)
			assert.Contains(t, strings.Join(planLines, "\n"), tc.wantInError)
			assert.Equal(t, planLines, valLines, "plan and validate must report identical errors")
			assert.Empty(t, planOut)
			assert.NotContains(t, valOut, "all local checks passed")
			assert.Zero(t, spy.factoryCalls, "backend must not be reached")
		})
	}
}

// validate without -t stays local: no target file, no backend, local errors are still caught.
func TestValidateNoTarget_StaysLocal(t *testing.T) {
	for _, tc := range parityCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupTestProject(t)
			tc.mutate(t, dir)
			require.NoError(t, os.RemoveAll(filepath.Join(dir, "targets", "dev.yml")))
			spy := useSpy(t, newSpy(nil).forbid(t))

			stdout, stderr, err := executeCmdSplit("validate")
			if tc.targetOnly {
				require.NoError(t, err, "target-only violation is invisible without -t; stderr: %s", stderr)
				assert.Contains(t, stdout, "all local checks passed")
			} else {
				require.Error(t, err, "stdout: %s", stdout)
				assert.Contains(t, strings.Join(errorLines(stderr), "\n"), tc.wantInError)
				assert.NotContains(t, stdout, "all local checks passed")
			}
			assert.Zero(t, spy.factoryCalls)
		})
	}
}

// A loader warning (catalog name != file name) reaches stderr in every command.
func TestLoadWarning_AllCommands(t *testing.T) {
	cases := map[string][]string{
		cmdPlan:     {cmdPlan, "-t", litDev},
		cmdValidate: {cmdValidate, "-t", litDev},
		cmdPull:     {cmdPull, "-t", litDev},
		cmdDrop:     {cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			dir := setupPlanProject(t)
			addCatalogFile(t, dir, "zz_file", "extra")
			useSpy(t, newSpy(nil))

			stdout, stderr, _ := executeCmdSplit(args...)
			assert.Contains(t, stderr, "warning: ")
			assert.Contains(t, stderr, `catalog name "extra" does not match filename "zz_file"`)
			assert.NotContains(t, stdout, "does not match filename")
		})
	}
}

func TestValidateTarget_EmptyWarehouse_WarnsAndPasses(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), "workspace:\n  host: https://dev.example.com\n")
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "orders")})
	wb := useSpy(t, newSpy(mock))

	stdout, stderr, err := executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "warning: ")
	assert.Contains(t, stderr, "warehouse_id")
	assert.Contains(t, stdout, "all local checks passed")
	assert.NotContains(t, stdout, "✗")
	assert.Zero(t, wb.checkCalls, "CheckWarehouse must be skipped without warehouse_id")
}

func TestValidateTarget_WarehouseSet_Checked(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"),
		"workspace:\n  host: https://dev.example.com\n  warehouse_id: wh123\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "orders")})
	wb := useSpy(t, newSpy(mock))

	stdout, stderr, err := executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, 1, wb.checkCalls)
	assert.Contains(t, stdout, "✓ warehouse wh123 reachable")
	assert.NotContains(t, stdout, "✗")
	assert.Contains(t, stdout, "wh123")
	assert.NotContains(t, stderr, "warehouse_id")
}

// An unreachable warehouse fails validate -t with the "✗ warehouse" line and a non-zero result.
func TestValidateTarget_WarehouseUnreachable_Fails(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"),
		"workspace:\n  host: https://dev.example.com\n  warehouse_id: wh123\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "orders")})
	mock.SetWarehouseError(errors.New("warehouse stopped"))
	wb := useSpy(t, newSpy(mock))

	stdout, _, err := executeCmdSplit("validate", "-t", "dev")
	require.EqualError(t, err, "live validation failed for warehouse wh123")
	assert.Equal(t, 1, wb.checkCalls)
	assert.Contains(t, stdout, "✗ warehouse wh123: warehouse stopped")
	assert.NotContains(t, stdout, "reachable")
}

// An Error from DiffSchema (rename onto an existing live column) fails validate -t.
func TestValidateTarget_DiffSchemaError_Fails(t *testing.T) {
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: uid\n    type: BIGINT\n    rename_from: id\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: "id", Type: typBIGINT}, {Name: "uid", Type: typBIGINT}},
	})})
	useSpy(t, newSpy(mock))

	stdout, stderr, err := executeCmdSplit("validate", "-t", "dev")
	require.Error(t, err, "stdout: %s", stdout)
	assert.Contains(t, stderr, "error: ")
	assert.Contains(t, stderr, "cannot rename")
	assert.EqualError(t, err, "live validation failed for schema dev.silver", "the error names the schema whose diff failed")
	assert.Empty(t, mock.RecordedCalls())
}

// target with uc_name: Prod -> plan and validate fail with the same text -> fix -> both pass.
func TestValidateTarget_Lifecycle_FixTarget(t *testing.T) {
	dir := setupPlanProject(t)
	targetPath := filepath.Join(dir, "targets", "dev.yml")
	writeFile(t, targetPath, "workspace:\n  host: https://dev.example.com\ncatalogs:\n  main:\n    uc_name: Prod\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litProd, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: "orders", Schema: litSilver, ResolvedCatalog: "prod", ResolvedSchema: litSilver,
			Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useSpy(t, newSpy(mock))

	_, planErr, err := executeCmdSplit("plan", "-t", "dev")
	require.Error(t, err)
	valOut, valErr, err := executeCmdSplit("validate", "-t", "dev")
	require.Error(t, err)
	require.NotEmpty(t, errorLines(planErr))
	assert.Contains(t, planErr, "Prod")
	assert.Equal(t, errorLines(planErr), errorLines(valErr))
	assert.NotContains(t, valOut, "all local checks passed")

	writeFile(t, targetPath, "workspace:\n  host: https://dev.example.com\ncatalogs:\n  main:\n    uc_name: prod\n")

	_, planErr, err = executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err, "stderr: %s", planErr)
	valOut, valErr, err = executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err, "stderr: %s", valErr)
	assert.Contains(t, valOut, "✓ all local checks passed")
	assert.Empty(t, errorLines(valErr))
}
