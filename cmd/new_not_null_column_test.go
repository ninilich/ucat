package cmd

// a new NOT NULL column of an existing table is a planning error; nothing is applied.

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const notNullErrFmt = `table "silver.%s", column "%s": a new NOT NULL column cannot be added to an existing table (not supported in v1): add it as nullable, fill it with data, then set nullable: false`

func notNullFixture(t *testing.T) *backendtest.MockUCBackend {
	t.Helper()
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: a\n    type: INT\n    nullable: false\n  - name: ok\n    type: INT\n")
	addTableFile(t, dir, "silver", "items",
		"name: items\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: b\n    type: INT\n    nullable: false\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
		backendtest.LiveOf(config.Table{Name: "items", Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver, Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	useSpy(t, newSpy(mock))
	return mock
}

func TestNewNotNullColumn_PlanApplyValidate_Fail(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}, {cmdValidate, "-t", litDev}} {
		t.Run(args[0], func(t *testing.T) {
			mock := notNullFixture(t)
			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err, "stdout: %s", stdout)
			assert.Equal(t, 1, codeOf(err))
			// both tables are reported, in table order of the schema
			assert.Contains(t, stderr, "error: "+sprintfNotNull("items", "b"))
			assert.Contains(t, stderr, "error: "+sprintfNotNull("orders", "a"))
			assert.NotContains(t, stderr, `"ok"`)
			assert.Empty(t, mock.RecordedCalls(), "nothing may be applied")
		})
	}
}

func TestNewNotNullColumn_PlanJSON_NoPlan(t *testing.T) {
	notNullFixture(t)
	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Empty(t, stdout, "no JSON on a planning error")
	assert.False(t, json.Valid([]byte(stdout)))
	assert.Contains(t, stderr, "Error: diff failed for dev.silver")
}

func TestNewNullableColumn_AddColumnWithoutNotNull(t *testing.T) {
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: a\n    type: INT\n  - name: b\n    type: INT\n    nullable: true\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{backendtest.LiveOf(config.Table{Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver, Columns: []config.Column{{Name: "id", Type: typBIGINT}}})})
	useSpy(t, newSpy(mock))

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.NotContains(t, stdout, "NOT NULL")
	calls := mock.RecordedCalls()
	require.Len(t, calls, 2)
	for _, c := range calls {
		assert.Equal(t, change.AddColumn, c.Kind)
	}
}

func sprintfNotNull(table, column string) string {
	return fmt.Sprintf(notNullErrFmt, table, column)
}
