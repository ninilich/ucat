package cmd

// Types as users spell them, through the CLI:
//   - spaced and mixed-case YAML types against compact live types (plan/apply see no change, a real change
//     is one ALTER with the original spelling);
//   - a type with whitespace inside a word, or a STRUCT field with NOT NULL/COMMENT, is rejected by
//     validate, plan, apply, drop and pull before any backend call.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
)

// through plan/apply: spaced and mixed-case YAML types against compact live types.

const spacedOrdersYAML = "name: orders\nschema: silver\ncolumns:\n" +
	"  - name: price\n    type: DECIMAL(10, 2)\n" +
	"  - name: s\n    type: \"STRUCT<a: INT, b: STRING>\"\n" +
	"  - name: arr\n    type: \"ARRAY< STRING >\"\n" +
	"  - name: m\n    type: \"MAP<STRING, INT>\"\n"

func spacedLiveCols(price string) []config.Column {
	return []config.Column{
		{Name: litPrice, Type: price},
		{Name: "s", Type: "struct<a:int,b:string>"},
		{Name: "arr", Type: "array<string>"},
		{Name: "m", Type: "map<string,int>"},
	}
}

func spacedMock(price string) *backendtest.MockUCBackend {
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: spacedLiveCols(price),
	})})
	return mock
}

func TestPlanApply_SpacedTypes_NoChanges(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders", spacedOrdersYAML)
	mock := spacedMock("decimal(10,2)")
	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev", "--detailed-exitcode")
	require.NoError(t, err)
	assert.Contains(t, out, "No changes")

	out, err = executeCmd("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)
	assert.Contains(t, out, "No changes")
	assert.Empty(t, mock.RecordedCalls())
}

func TestPlanApply_SpacedTypes_RealChangeIsOneAlterWithOriginalSpelling(t *testing.T) {
	tmpDir := setupTestProject(t)
	// DECIMAL(10, 2) -> String (mixed case) in YAML, live still varchar(10): the one supported type change.
	addTableFile(t, tmpDir, "silver", "orders", strings.Replace(spacedOrdersYAML, "DECIMAL(10, 2)", "String", 1))
	mock := spacedMock(typVarchar10)
	useBackend(t, mock)

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err, stderr)
	var p planJSON
	decodeSingleJSON(t, stdout, &p)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, "alter_column", p.Changes[0].Op)
	assert.Equal(t, 1, p.Summary.Change)

	out, err := executeCmd("plan", "-t", "dev", "--show-sql")
	require.NoError(t, err)
	assert.Contains(t, out, "ALTER TABLE `dev`.`silver`.`orders` ALTER COLUMN `price` TYPE String")
	assert.NotContains(t, out, "TYPE string")
	assert.NotContains(t, out, "TYPE STRING")

	out, err = executeCmd("apply", "-t", "dev", "--auto-approve", "--show-sql")
	require.NoError(t, err)
	assert.Contains(t, out, "TYPE String")
	calls := mock.RecordedCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, change.AlterColumn, calls[0].Kind)
	assert.Contains(t, calls[0].Describe, "String")
	assert.Contains(t, calls[0].Describe, "varchar(10)")
}

// Column rename and a type change with spaces in one plan: rename + one alter, original type.
func TestPlan_ColumnRenameWithSpacedTypeChange(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: amount\n    rename_from: price\n    type: String\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: litPrice, Type: typVarchar10}},
	})})
	mock.TableProperties = map[string]map[string]string{
		litDevSilverOrders: {propColumnMapping: litName},
	}
	useBackend(t, mock)

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err, stderr)
	var p planJSON
	decodeSingleJSON(t, stdout, &p)
	require.Len(t, p.Changes, 2)
	ops := []string{p.Changes[0].Op, p.Changes[1].Op}
	assert.ElementsMatch(t, []string{"rename_column", "alter_column"}, ops)

	out, err := executeCmd("apply", "-t", "dev", "--auto-approve", "--show-sql")
	require.NoError(t, err)
	assert.Contains(t, out, "TYPE String")
	assert.Len(t, mock.RecordedCalls(), 2)

	// Same type as live (other spelling) after rename: only the rename remains.
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: litPrice, Type: litString}},
	})})
	stdout, stderr, err = executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err, stderr)
	decodeSingleJSON(t, stdout, &p)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, "rename_column", p.Changes[0].Op)
}

// Through the CLI: a type with whitespace inside a word is rejected by validate,
// plan, apply, drop and pull before any backend call.

func wsTypeProject(t *testing.T, typ string) (*backendtest.MockUCBackend, *spyBackend) {
	t.Helper()
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: tags\n    type: \""+typ+"\"\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, []*backend.LiveTable{})
	return mock, useSpy(t, newSpy(mock))
}

func TestCLI_WhitespaceInWord_RejectedEverywhere(t *testing.T) {
	const typ = "ARRAY<IN T>"
	const want = `table "orders", column "tags": type "ARRAY<IN T>" is not supported: whitespace inside a word`
	for _, args := range [][]string{
		{cmdValidate},
		{cmdValidate, "-t", litDev},
		{cmdPlan, "-t", litDev},
		{cmdApply, "-t", litDev},
		{cmdApply, "-t", litDev, flagAutoApprove},
		{cmdPlan, "-t", litDev, flagJSON},
	} {
		t.Run(args[0]+"/"+strings.Join(args, " "), func(t *testing.T) {
			mock, spy := wsTypeProject(t, typ)
			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, stderr, want)
			assert.Contains(t, stderr, "orders")
			assert.Contains(t, stderr, "tags")
			assert.NotContains(t, stdout, "all local checks passed")
			assert.Zero(t, spy.factoryCalls, "backend must not be created")
			assert.Zero(t, countApplied(mock))
		})
	}
}

func TestCLI_WhitespaceInWord_PullRejectsBeforeBackend(t *testing.T) {
	mock, spy := wsTypeProject(t, "BIG INT")
	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Contains(t, stderr, `type "BIG INT" is not supported: whitespace inside a word`)
	assert.Zero(t, spy.factoryCalls)
	assert.Zero(t, countApplied(mock))
}

// By design drop does not load tables (skipTables), so a bad type in some table file does not
// block it. Fixed by fact.
func TestCLI_Drop_IgnoresBadTypeInTableFiles(t *testing.T) {
	mock, _ := wsTypeProject(t, "BIG INT")
	_, stderr, err := executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.NoError(t, err, stderr)
	assert.Equal(t, 1, countApplied(mock))
}

// STRUCT<a:INT NOT NULL>: rejected with exit code 1 with the STRUCT-specific reason.
func TestCLI_StructNotNull_Rejected(t *testing.T) {
	mock, spy := wsTypeProject(t, "STRUCT<a:INT NOT NULL>")
	_, stderr, err := executeCmdSplit("validate")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Contains(t, stderr, `type "STRUCT<a:INT NOT NULL>" is not supported: NOT NULL and COMMENT are not supported inside STRUCT in v1`)
	assert.NotContains(t, stderr, "whitespace inside a word")
	assert.Zero(t, spy.factoryCalls)
	assert.Zero(t, countApplied(mock))
}

func TestCLI_StructComment_GeneralMessage(t *testing.T) {
	// YAML double-quoted string with an escaped quote is awkward: use single quotes inside.
	_, _ = wsTypeProject(t, "STRUCT<a:INT COMMENT 'x'>")
	_, stderr, err := executeCmdSplit("validate")
	require.Error(t, err)
	assert.Contains(t, stderr, "is not supported; supported types:")
}

// pull -> validate round trip on the types UC returns.
func TestCLI_PullThenValidate_UCTypes(t *testing.T) {
	dir := setupTestProject(t)
	cols := []config.Column{
		{Name: litCStruct, Type: "struct<a:int,b:string>"},
		{Name: litCDec, Type: typDecimal102},
		{Name: litCMap, Type: "map<string,array<int>>"},
		{Name: "c_interval", Type: "interval day to second"},
		{Name: "c_arr_varchar", Type: typArrayVarchar5},
		{Name: "c_nested_char", Type: "struct<a:array<char(3)>,b:int>"},
		{Name: "c_ym", Type: "interval year to month"},
	}
	installPullFixture(t, pullLive("t", cols, nil))

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err, stderr)
	body := pullReadTable(t, dir, "t")
	assert.Contains(t, body, "struct<a:int,b:string>")
	assert.Contains(t, body, "interval day to second")

	stdout, stderr, err := executeCmdSplit("validate")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "all local checks passed")

	// And plan against the same live state: no changes.
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, []*backend.LiveTable{pullLive("t", cols, nil)})
	useBackend(t, mock)
	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, "No changes")
}
