package cmd

// The observable behaviour of the commands: error texts and streams (list errors, pull, apply,
// drop), the closing lines, empty plans, exit codes and the production defaults of the app.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
)

// --- listing errors name the schema, in every command that lists ---

func TestListError_PlanApplyNameSchemaAndExitOne(t *testing.T) {
	for _, cmdArgs := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
		t.Run(cmdArgs[0], func(t *testing.T) {
			setupMultiSchemaProject(t)
			spy := newSpy(nil)
			gold := backend.SchemaRef{UCCatalog: litDev, UCSchema: litGold}
			spy.failList = &gold
			useSpy(t, spy)

			stdout, stderr, err := executeCmdSplit(cmdArgs...)
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.EqualError(t, err, "list tables in dev.gold: "+assert.AnError.Error())
			assert.Contains(t, stderr, "Error: list tables in dev.gold: ")
			assert.Empty(t, stdout, "no plan is printed when a schema cannot be listed")
			assert.Zero(t, spy.applyCalls)
		})
	}
}

// Both schemas fail: the error names one of them (parallel fetch: whichever failed first).
func TestListError_PlanTwoSchemasFail_NamesOne(t *testing.T) {
	setupMultiSchemaProject(t)
	useSpy(t, listFailSpy())

	_, err := executeCmd("plan", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Regexp(t, `^list tables in dev\.(silver|gold|bronze): list tables boom$`, err.Error())
}

func TestListError_PullNamesSchema(t *testing.T) {
	tmpDir := setupTestProject(t)
	useSpy(t, listFailSpy())

	stdout, _, err := executeCmdSplit("pull", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.EqualError(t, err, "list tables in dev.silver: list tables boom")
	assert.Empty(t, stdout)
	assert.NoDirExists(t, filepath.Join(tmpDir, "tables", "silver"))
}

// The error keeps the cause (errors.Is) through FetchTables.
func TestListError_WrapsCause(t *testing.T) {
	setupPlanProject(t) // has a table, so plan lists its schema too
	spy := newSpy(nil)
	spy.listErr = assert.AnError
	useSpy(t, spy)

	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdPull, "-t", litDev}} {
		err := execRoot(new(strings.Builder), new(strings.Builder), args)
		require.Error(t, err)
		assert.ErrorIs(t, err, assert.AnError, args)
	}
}

// --- validate -t: all schemas are checked, failures are marked with the cross ---

func TestValidateLive_TwoSchemasOneListFails_AllChecked(t *testing.T) {
	setupMultiSchemaProject(t)
	spy := newSpy(nil)
	gold := backend.SchemaRef{UCCatalog: litDev, UCSchema: litGold}
	spy.failList = &gold
	useSpy(t, spy)

	stdout, _, err := executeCmdSplit("validate", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.EqualError(t, err, "live validation failed for schema dev.gold")
	assert.Equal(t, 3, spy.listCalls, "every schema is listed even after a failure")
	assert.Contains(t, stdout, "✗ schema dev.gold: "+assert.AnError.Error()+"\n")
	assert.Contains(t, stdout, "✓ schema dev.silver exists\n")
	assert.Contains(t, stdout, "✓ schema dev.bronze exists\n")
	assert.Contains(t, stdout, "✓ warehouse wh123 reachable\n", "the warehouse is checked after a failed schema")
}

// --- pull ---

func TestPull_EmptySchema_NoOutputNoFiles(t *testing.T) {
	tmpDir := setupTestProject(t)
	pullTablesIn(t, "silver")

	stdout, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "nomatch")
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.NotContains(t, stderr, "matched no property", "no table was written, so no mask warning")
	assert.NoDirExists(t, filepath.Join(tmpDir, "tables", "silver"))
}

// 200 tables given in reverse order are written and reported in name order, run after run.
func TestPull_200Tables_DeterministicOrder(t *testing.T) {
	var names, want []string
	for i := 199; i >= 0; i-- {
		names = append(names, fmt.Sprintf("t%03d", i))
	}
	for i := 0; i < 200; i++ {
		want = append(want, fmt.Sprintf("✓ created  tables/silver/t%03d.yml", i))
	}
	var first string
	for run := 0; run < 2; run++ {
		tmpDir := setupTestProject(t)
		pullTablesIn(t, "silver", names...)
		stdout, _, err := executeCmdSplit("pull", "-t", "dev")
		require.NoError(t, err)
		assert.Equal(t, strings.Join(want, "\n")+"\n", stdout)
		files, err := filepath.Glob(filepath.Join(tmpDir, "tables", "silver", "*.yml"))
		require.NoError(t, err)
		assert.Len(t, files, 200)
		if run == 0 {
			first = stdout
		} else {
			assert.Equal(t, first, stdout)
		}
	}
}

// Two tables of one schema, the second fails its Windows name check: the first is written and
// reported, the second is not, the command fails with exit code 1 and no unmatched-mask warning.
func TestPull_SecondTableOfSchemaFails_FirstKept(t *testing.T) {
	tmpDir := setupTestProject(t)
	useGOOS(t, "windows")
	pullTablesIn(t, "silver", "con", "alpha") // fixture order: the failing one first, sorted: alpha, con

	stdout, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "nomatch")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.EqualError(t, err, `table "con": cannot create tables/silver/con.yml: `+winReserved)
	assert.Equal(t, "✓ created  tables/silver/alpha.yml\n", stdout)
	assert.FileExists(t, filepath.Join(tmpDir, "tables", "silver", "alpha.yml"))
	assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "con.yml"))
	assert.NotContains(t, stderr, "matched no property", "the command failed, mask warnings come only after success")
}

// --force over a file whose name is Windows-problematic and which defines that table: updated in place.
// A second, not yet defined table with a reserved name in the same run still fails.
func TestPull_WindowsForce_DefinedProblemNameUpdated_NewOneFails(t *testing.T) {
	tmpDir := setupTestProject(t)
	useGOOS(t, "windows")
	addTableFile(t, tmpDir, "silver", "aux", "name: aux\nschema: silver\ncolumns:\n  - name: id\n    type: INT\n")
	pullTablesIn(t, "silver", "aux", "nul")

	stdout, _, err := executeCmdSplit("pull", "-t", "dev", "--force")
	require.Error(t, err)
	assert.EqualError(t, err, `table "nul": cannot create tables/silver/nul.yml: `+winReserved)
	assert.Equal(t, "✓ updated  tables/silver/aux.yml\n", stdout)
	assert.NoFileExists(t, filepath.Join(tmpDir, "tables", "silver", "nul.yml"))
}

// Not on Windows the same names are written.
func TestPull_NonWindows_ReservedNamesWritten(t *testing.T) {
	tmpDir := setupTestProject(t)
	useGOOS(t, "linux")
	pullTablesIn(t, "silver", "con", "nul")

	_, err := executeCmd("pull", "-t", "dev")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(tmpDir, "tables", "silver", "con.yml"))
	assert.FileExists(t, filepath.Join(tmpDir, "tables", "silver", "nul.yml"))
}

// --- apply: closing line, empty plan, exit code ---

func TestApply_Success_TextClosingLineOnStdout(t *testing.T) {
	setupPlanProject(t)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useBackend(t, m)

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)
	assert.Equal(t, 0, codeOf(err))
	// Clock readings: begin, start and end of the step, finish = 3 intervals of testStep.
	assert.True(t, strings.HasSuffix(stdout, "  [1/1] applied in 1.5s: `dev`.`silver`.`orders` - add column \"id\" BIGINT\n\nApply complete in 4.5s.\n"), "%q", stdout)
	assert.Empty(t, stderr)
}

func TestApply_Success_JSONHasNoClosingLine(t *testing.T) {
	setupPlanProject(t)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useBackend(t, m)

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.NoError(t, err)
	assert.NotContains(t, stdout, "Apply complete")
	assert.NotContains(t, stderr, "Apply complete")
}

func TestApply_EmptyPlan_TextAndJSON(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, upToDateMock())

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)
	assert.Equal(t, "No changes. Infrastructure is up-to-date.\n", stdout)
	assert.Empty(t, stderr)

	stdout, stderr, err = executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.NoError(t, err)
	var r applyJSON
	decodeSingleJSON(t, stdout, &r)
	assert.Zero(t, r.Total)
	assert.Empty(t, stderr, "the no-changes line is not printed in --json mode")
	assert.NotContains(t, stdout, "No changes")
}

func TestPlan_EmptyPlan_TextExactAndJSON(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, upToDateMock())

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--detailed-exitcode")
	require.NoError(t, err)
	assert.Equal(t, 0, codeOf(err))
	assert.Equal(t, "No changes. Infrastructure is up-to-date.\n", stdout)
	assert.Empty(t, stderr)
}

// --- drop ---

func TestDrop_SuccessLine(t *testing.T) {
	setupTestProject(t)
	useBackend(t, &backendtest.MockUCBackend{})

	stdout, stderr, err := executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.NoError(t, err)
	assert.Equal(t, "✓ dropped `dev`.`silver`.`orders`\n", stdout)
	assert.Empty(t, stderr)
}

func TestDrop_BackendError_TextCodeAndStreams(t *testing.T) {
	setupTestProject(t)
	m := &backendtest.MockUCBackend{}
	m.SetApplyError(errors.New("table not found"))
	useBackend(t, m)

	stdout, stderr, err := executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.EqualError(t, err, "drop `dev`.`silver`.`orders`: table not found")
	assert.Equal(t, "Error: drop `dev`.`silver`.`orders`: table not found\n", stderr)
	assert.Empty(t, stdout, "no success line after a failed drop")
	assert.NotContains(t, err.Error(), "apply change")
	assert.Equal(t, "table not found", errors.Unwrap(err).Error(), "the cause is wrapped")
}

// --- working directory error keeps its cause ---

func TestLoadConfig_WorkingDirectoryError_WrapsCause(t *testing.T) {
	cause := errors.New("cwd gone")
	a := newApp()
	a.getwd = func() (string, error) { return "", cause }
	root := newRootCmd(a)
	root.SetOut(new(strings.Builder))
	root.SetErr(new(strings.Builder))
	root.SetArgs([]string{cmdValidate})
	err := root.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, 1, codeOf(err))
}

// newApp wires the production dependencies: the real working directory and operating system.
func TestNewApp_ProductionDefaults(t *testing.T) {
	a := newApp()
	want, err := os.Getwd()
	require.NoError(t, err)
	got, err := a.getwd()
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, runtime.GOOS, a.goos)
	assert.NotNil(t, a.newBackend)
}

func TestExitCodeError_Text(t *testing.T) {
	assert.Equal(t, "exit status 2", (&exitCodeError{code: 2}).Error())
}
