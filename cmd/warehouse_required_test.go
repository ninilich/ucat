package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- apply and drop need a SQL warehouse ---

const noWarehouseTarget = "workspace:\n  host: https://dev.example.com\n"

// noWarehouseProject is the plan project with a target without warehouse_id and without the env var.
func noWarehouseProject(t *testing.T) (string, *backendtest.MockUCBackend) {
	t.Helper()
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), noWarehouseTarget)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{}) // orders is missing live: CreateTable
	useBackend(t, mock)
	return dir, mock
}

func TestDrop_NoWarehouse_Fails(t *testing.T) {
	dir := setupTestProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), noWarehouseTarget)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	mock := &backendtest.MockUCBackend{}
	useBackend(t, mock)

	_, err := executeCmd("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, "drop requires a SQL warehouse: set workspace.warehouse_id in targets/dev.yml (or ucat.yml), or DATABRICKS_WAREHOUSE_ID", err.Error())
	assert.Equal(t, 1, codeOf(err))
	assert.Zero(t, countApplied(mock))
}

// drop checks flags, --auto-approve and the schema first: those errors win over the warehouse one.
func TestDrop_NoWarehouse_OtherChecksComeFirst(t *testing.T) {
	dir := setupTestProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), noWarehouseTarget)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	useBackend(t, &backendtest.MockUCBackend{})

	_, err := executeCmd("drop", "-t", "dev", "--schema", "nope", "--table", "orders", "--auto-approve")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown schema "nope"`)
	assert.NotContains(t, err.Error(), "SQL warehouse")

	_, err = executeCmd("drop", "-t", "dev", "--schema", "silver", "--table", "orders")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SQL warehouse")
}

// drop with a warehouse from the environment works.
func TestDrop_WarehouseFromEnv_Works(t *testing.T) {
	dir := setupTestProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), noWarehouseTarget)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", litEnvWH)
	mock := &backendtest.MockUCBackend{}
	useBackend(t, mock)

	_, err := executeCmd("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.NoError(t, err)
	assert.Equal(t, 1, countApplied(mock))
}

const wantApplyNoWarehouse = "apply requires a SQL warehouse: set workspace.warehouse_id in targets/dev.yml (or ucat.yml), or DATABRICKS_WAREHOUSE_ID"

func TestApply_NoWarehouse_AutoApproveSameError(t *testing.T) {
	_, mock := noWarehouseProject(t)
	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, wantApplyNoWarehouse, err.Error())
	assert.Equal(t, 1, codeOf(err))
	assert.Zero(t, countApplied(mock))
	assert.NotContains(t, stdout+stderr, "ApplyChange")
	assert.NotContains(t, stdout+stderr, "Applying")
}

// The error is raised before the plan is printed and before the prompt is read: stdin is untouched.
func TestApply_NoWarehouse_NoPromptNoPlanOutput(t *testing.T) {
	_, mock := noWarehouseProject(t)
	r := &countingReader{data: "yes\n"}
	useStdinReader(t, r)
	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, wantApplyNoWarehouse, err.Error())
	assert.Zero(t, r.reads, "stdin must not be read")
	assert.NotContains(t, stdout+stderr, "Type 'yes'")
	assert.NotContains(t, stdout+stderr, "orders")
	assert.Zero(t, countApplied(mock))
}

type countingReader struct {
	data  string
	reads int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads++
	if r.data == "" {
		return 0, os.ErrClosed
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// Non-destructive (create) and destructive (drop: true) plans fail the same way.
func TestApply_NoWarehouse_DestructivePlanSameError(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), noWarehouseTarget)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	mock := destructivePlanMock(t, dir)
	useBackend(t, mock)

	for _, extra := range [][]string{{}, {flagAutoApprove}} {
		_, _, err := executeCmdSplit(append([]string{cmdApply, "-t", litDev}, extra...)...)
		require.Error(t, err)
		assert.Equal(t, wantApplyNoWarehouse, err.Error())
		assert.Zero(t, countApplied(mock))
	}
}

// The target name in the message is the real one.
func TestApply_NoWarehouse_MessageUsesRealTargetName(t *testing.T) {
	dir, _ := noWarehouseProject(t)
	writeFile(t, filepath.Join(dir, "targets", "prod.yml"), noWarehouseTarget)
	_, err := executeCmd("apply", "-t", "prod", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, "apply requires a SQL warehouse: set workspace.warehouse_id in targets/prod.yml (or ucat.yml), or DATABRICKS_WAREHOUSE_ID", err.Error())
}

// --json: fixed by fact. The error is returned before anything is printed to stdout (stdout is
// empty), exit code 1.
func TestApplyJSON_NoWarehouse_StdoutEmpty(t *testing.T) {
	_, mock := noWarehouseProject(t)
	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Equal(t, wantApplyNoWarehouse, err.Error())
	assert.Empty(t, stdout)
	assert.NotContains(t, stderr, "ApplyChange")
	assert.Zero(t, countApplied(mock))
}

// Empty plan: no warehouse is needed, code 0, "No changes" (also with --auto-approve and --json).
func TestApply_NoWarehouse_EmptyPlan(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), noWarehouseTarget)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	useBackend(t, upToDateMock())

	stdout, _, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No changes")

	stdout, _, err = executeCmdSplit("apply", "-t", "dev", "--json")
	require.NoError(t, err)
	var v map[string]any
	decodeSingleJSON(t, stdout, &v)
}

// The warehouse is taken from the environment, from ucat.yml or from the target; the backend
// factory receives the resolved value, and apply does not fail.
func TestApply_WarehouseSources(t *testing.T) {
	cases := []struct {
		name   string
		env    string
		target string
		ucat   string
		want   string
	}{
		{"env only", litEnvWH, noWarehouseTarget, "project: testproj\ntables_dir: tables\n", litEnvWH},
		{"ucat.yml only", "", noWarehouseTarget, "project: testproj\ntables_dir: tables\nworkspace:\n  warehouse_id: ucat-wh\n", "ucat-wh"},
		{"target only", "", noWarehouseTarget + "  warehouse_id: tgt-wh\n", "project: testproj\ntables_dir: tables\n", litTgtWH},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupPlanProject(t)
			writeFile(t, filepath.Join(dir, "targets", "dev.yml"), tc.target)
			writeFile(t, filepath.Join(dir, "ucat.yml"), tc.ucat)
			t.Setenv("DATABRICKS_WAREHOUSE_ID", tc.env)
			mock := &backendtest.MockUCBackend{}
			mock.SetFixture(silverRef, []*backend.LiveTable{})
			var got string
			useBackendFactory(t, func(_, wh string) (backend.UCBackend, error) { got = wh; return mock, nil })

			_, _, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, 1, countApplied(mock))
		})
	}
}

// validate -t without warehouse keeps working: a warning and no connectivity check.
func TestValidateLive_NoWarehouse_WarnsAndSkips(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), noWarehouseTarget)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "")
	useBackend(t, upToDateMock())

	stdout, stderr, err := executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "warehouse_id not set: warehouse connectivity not checked")
	assert.NotContains(t, stdout, "warehouse")
	assert.NotContains(t, stderr, "requires a SQL warehouse")
}
