package cmd

// changing the type of an existing column is supported in v1 only from varchar(n)/char(n) to STRING;
// any other change (INT -> BIGINT) is a planning error: nothing is applied, no JSON is printed.

import (
	"encoding/json"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const typeChangeErr = `table "silver.orders", column "amount": changing the type from INT to BIGINT is not supported in v1 (only varchar(n)/char(n) to STRING); type widening is planned for v2`

func typeChangeFixture(t *testing.T, liveType, yamlType string) *backendtest.MockUCBackend {
	t.Helper()
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: amount\n    type: "+yamlType+"\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: "id", Type: typBIGINT}, {Name: "amount", Type: liveType}},
	})})
	useSpy(t, newSpy(mock))
	return mock
}

func TestTypeChangeUnsupported_PlanApplyValidate_Fail(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}, {cmdValidate, "-t", litDev}} {
		t.Run(args[0], func(t *testing.T) {
			mock := typeChangeFixture(t, "INT", typBIGINT)
			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err, "stdout: %s", stdout)
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, stderr, "error: "+typeChangeErr)
			if args[0] == cmdValidate {
				assert.Contains(t, stderr, "Error: live validation failed")
			} else {
				assert.Contains(t, stderr, "Error: diff failed for dev.silver")
			}
			assert.Empty(t, mock.RecordedCalls(), "nothing may be applied")
		})
	}
}

func TestTypeChangeUnsupported_PlanJSON_NoPlan(t *testing.T) {
	typeChangeFixture(t, "INT", typBIGINT)
	stdout, stderr, err := executeCmdSplit(cmdPlan, "-t", litDev, "--json")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Empty(t, stdout, "no JSON on a planning error")
	assert.False(t, json.Valid([]byte(stdout)))
	assert.Contains(t, stderr, typeChangeErr)
	assert.Contains(t, stderr, "Error: diff failed for dev.silver")
}

func TestTypeChangeUnsupported_ApplyJSON_NoSQL(t *testing.T) {
	mock := typeChangeFixture(t, "INT", typBIGINT)
	stdout, _, err := executeCmdSplit(cmdApply, "-t", litDev, flagAutoApprove, "--json")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.False(t, json.Valid([]byte(stdout)))
	assert.Empty(t, mock.RecordedCalls())
}

// The one supported change: varchar(n) -> STRING is a single ALTER ... TYPE STRING.
func TestTypeChangeSupported_VarcharToString_Applies(t *testing.T) {
	mock := typeChangeFixture(t, typVarchar10, typSTRING)
	out, stderr, err := executeCmdSplit(cmdApply, "-t", litDev, flagAutoApprove, "--show-sql")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, out, "ALTER COLUMN `amount` TYPE STRING")
	calls := mock.RecordedCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, change.AlterColumn, calls[0].Kind)
}
