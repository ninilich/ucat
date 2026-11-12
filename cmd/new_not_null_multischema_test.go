package cmd

// end to end over several schemas: all errors in one output, deterministic order,
// a healthy schema is not applied, validate -t vs local validate, table rename label.

import (
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// two schemas: gold.gb and silver.sb each get a new NOT NULL column; healthyGold makes gold.gh a valid
// nullable ADD COLUMN (gold) instead of the failing gb.
func twoSchemaNotNull(t *testing.T, goldFails, silverFails bool) *backendtest.MockUCBackend {
	t.Helper()
	dir := setupTestProject(t)
	addGoldSchema(t, dir)
	goldCol, silverCol := "    nullable: true\n", "    nullable: true\n"
	if goldFails {
		goldCol = "    nullable: false\n"
	}
	if silverFails {
		silverCol = "    nullable: false\n"
	}
	addTableFile(t, dir, "gold", "gb", tableYAML("gold", "gb", "")+"  - name: n\n    type: INT\n"+goldCol)
	addTableFile(t, dir, "silver", "sb", tableYAML("silver", "sb", "")+"  - name: m\n    type: INT\n"+silverCol)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(goldRef, []*backend.LiveTable{liveTable("gold", "gb")})
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "sb")})
	useSpy(t, newSpy(mock))
	return mock
}

func TestNewNotNull_TwoSchemas_AllErrorsDeterministic(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			mock := twoSchemaNotNull(t, true, true)
			var first string
			for i := 0; i < 5; i++ {
				stdout, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				assert.Equal(t, 1, codeOf(err))
				got := stdout + "|" + stderr
				if i == 0 {
					first = got
				}
				assert.Equal(t, first, got, "output must be byte-identical between runs")
			}
			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Empty(t, stdout)
			gi := strings.Index(stderr, `table "gold.gb", column "n"`)
			si := strings.Index(stderr, `table "silver.sb", column "m"`)
			require.GreaterOrEqual(t, gi, 0, stderr)
			require.GreaterOrEqual(t, si, 0, stderr)
			assert.Less(t, gi, si, "schemas in (catalog, schema) order")
			assert.Equal(t, "diff failed for dev.gold", err.Error(), "names the first failed schema")
			assert.Equal(t, 2, strings.Count(stderr, "a new NOT NULL column"))
			assert.Empty(t, mock.RecordedCalls())
		})
	}
}

// One schema fails, the other is healthy (would add a column): nothing is applied, no JSON, no extra diagnostics.
func TestNewNotNull_OneSchemaFails_HealthyNotApplied(t *testing.T) {
	for _, tc := range []struct {
		name               string
		goldFails          bool
		wantFailed, wantOK string
	}{
		{"first fails", true, "dev.gold", litSilver},
		{"second fails", false, "dev.silver", litGold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := twoSchemaNotNull(t, tc.goldFails, !tc.goldFails)
			for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdPlan, "-t", litDev, flagJSON}, {cmdApply, "-t", litDev, flagAutoApprove}} {
				stdout, stderr, err := executeCmdSplit(args...)
				require.Error(t, err, args)
				assert.Equal(t, 1, codeOf(err))
				assert.Equal(t, "diff failed for "+tc.wantFailed, err.Error())
				assert.Empty(t, stdout, "no plan, no JSON: %v", args)
				assert.Equal(t, 1, strings.Count(stderr, "a new NOT NULL column"), stderr)
				assert.NotContains(t, stderr, tc.wantOK+".", "the healthy schema prints no diagnostics")
			}
			assert.Empty(t, mock.RecordedCalls(), "the healthy schema must not be applied")
		})
	}
}

// Table renamed in the same plan: the label has the new name.
func TestNewNotNull_RenamedTable_LabelNewName(t *testing.T) {
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "b", tableYAML("silver", "b", "rename_from: a\n")+"  - name: n\n    type: INT\n    nullable: false\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "a")})
	useSpy(t, newSpy(mock))

	_, stderr, err := executeCmdSplit("plan", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, stderr, `table "silver.b", column "n": a new NOT NULL column`)
	assert.NotContains(t, stderr, `"silver.a"`)
	assert.Empty(t, mock.RecordedCalls())
}

// Two new NOT NULL columns in one table: both, in YAML column order.
func TestNewNotNull_TwoColumnsOneTable_YAMLOrder(t *testing.T) {
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "sb", tableYAML("silver", "sb", "")+"  - name: zz\n    type: INT\n    nullable: false\n  - name: aa\n    type: INT\n    nullable: false\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "sb")})
	useSpy(t, newSpy(mock))

	_, stderr, err := executeCmdSplit("plan", "-t", "dev")
	require.Error(t, err)
	zi := strings.Index(stderr, `column "zz"`)
	ai := strings.Index(stderr, `column "aa"`)
	require.GreaterOrEqual(t, zi, 0, stderr)
	require.GreaterOrEqual(t, ai, 0, stderr)
	assert.Less(t, zi, ai)
}

// Fact: with a column rename_from error in the same table only that error is shown.
func TestNewNotNull_WithColumnRenameError_OnlyRenameShown(t *testing.T) {
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "sb", tableYAML("silver", "sb", "")+"  - name: y\n    type: INT\n    rename_from: x\n  - name: n\n    type: INT\n    nullable: false\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "sb")})
	useSpy(t, newSpy(mock))

	_, stderr, err := executeCmdSplit("plan", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, stderr, `rename_from "x"`)
	assert.NotContains(t, stderr, "NOT NULL")
}

// validate -t sees the error (live), local validate does not read live and passes.
func TestNewNotNull_ValidateLiveVsLocal(t *testing.T) {
	mock := twoSchemaNotNull(t, true, true)

	out, err := executeCmd("validate", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Contains(t, err.Error(), "live validation failed")
	assert.Contains(t, out, `table "gold.gb", column "n": a new NOT NULL column`)
	assert.Contains(t, out, `table "silver.sb", column "m": a new NOT NULL column`)
	assert.Empty(t, mock.RecordedCalls())

	useBackend(t, newSpy(nil).forbid(t))
	out, err = executeCmd("validate")
	require.NoError(t, err, out)
	assert.NotContains(t, out, "NOT NULL")
}
