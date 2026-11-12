package cmd

import (
	"errors"
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrop_Success verifies that a drop command with --auto-approve calls ApplyChange
// exactly once with DropTable change type and prints a success line.
func TestDrop_Success(t *testing.T) {
	setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	useBackend(t, mock)

	out, err := executeCmd("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.NoError(t, err)

	calls := mock.RecordedCalls()
	require.Len(t, calls, 1, "exactly one ApplyChange call expected")
	assert.Equal(t, change.DropTable, calls[0].Kind)
	assert.Contains(t, calls[0].Describe, "silver")
	assert.Contains(t, out, "dropped")
	assert.Contains(t, out, "silver")
}

// TestDrop_NoAutoApprove verifies that drop without --auto-approve returns an error
// and makes no ApplyChange calls.
func TestDrop_NoAutoApprove(t *testing.T) {
	setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	useBackend(t, mock)

	out, err := executeCmd("drop", "-t", "dev", "--schema", "silver", "--table", "orders")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "auto-approve")
	assert.Empty(t, mock.RecordedCalls(), "no ApplyChange calls expected without --auto-approve")
}

// TestDrop_NoTarget verifies that drop without -t returns an error about the missing target.
func TestDrop_NoTarget(t *testing.T) {
	out, err := executeCmd("drop", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "drop requires -t")
}

// TestDrop_UnknownSchema verifies that an unresolvable schema name is rejected.
func TestDrop_UnknownSchema(t *testing.T) {
	setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	useBackend(t, mock)

	out, err := executeCmd("drop", "-t", "dev", "--schema", "badschema", "--table", "orders", "--auto-approve")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "unknown schema")
}

// TestDrop_ApplyError verifies that an error from ApplyChange is propagated.
func TestDrop_ApplyError(t *testing.T) {
	setupTestProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetApplyError(errors.New("table not found"))
	useBackend(t, mock)

	_, err := executeCmd("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.Error(t, err, "ApplyChange error must propagate to the caller")
}

// --auto-approve is bound to apply and drop only.
func TestAutoApproveFlagVisibleOnApplyAndDrop(t *testing.T) {
	root := newRootCmd(newApp())
	for _, name := range []string{cmdApply, cmdDrop} {
		c, _, err := root.Find([]string{name})
		require.NoError(t, err)
		assert.NotNil(t, c.Flags().Lookup("auto-approve"), "%s must have --auto-approve", name)
	}
}

// A table that does not exist: the DROP TABLE statement (no IF EXISTS) is rejected by the warehouse,
// so drop exits with code 1 and an error that names the table and carries the reason; no "dropped" line.
func TestDrop_MissingTable_ExitCode1WithClearError(t *testing.T) {
	setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetApplyError(errors.New("[TABLE_OR_VIEW_NOT_FOUND] The table or view `dev`.`silver`.`ghost` cannot be found"))
	useBackend(t, mock)

	stdout, stderr, err := executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "ghost", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Equal(t, "Error: drop `dev`.`silver`.`ghost`: [TABLE_OR_VIEW_NOT_FOUND] The table or view `dev`.`silver`.`ghost` cannot be found\n", stderr)
	assert.Empty(t, stdout, "no success line for a table that was not dropped")
}

// The statement drop sends does not tolerate a missing table (no IF EXISTS); this is what makes the
// case above an error on a real warehouse.
func TestDrop_StatementHasNoIfExists(t *testing.T) {
	setupTestProject(t)
	spy := newSpy(nil)
	useSpy(t, spy)

	_, _, err := executeCmdSplit("drop", "-t", "dev", "--schema", "silver", "--table", "orders", "--auto-approve")
	require.NoError(t, err)
	require.Equal(t, 1, spy.applyCalls)
	assert.Equal(t, []string{"DROP TABLE `dev`.`silver`.`orders`"}, spy.sqls)
}
