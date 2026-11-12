package uc

import (
	"testing"

	"github.com/databricks/databricks-sdk-go/service/sql"
	"github.com/stretchr/testify/assert"
)

// Fixes the current behaviour. CLOSED counts as terminal here; whether a CLOSED statement can be
// a successful one is checked on a live UC, the behaviour is not changed by this test.
func TestIsTerminal(t *testing.T) {
	cases := map[sql.StatementState]bool{
		sql.StatementStatePending:   false,
		sql.StatementStateRunning:   false,
		sql.StatementStateSucceeded: false, // success is handled before isTerminal is asked
		sql.StatementStateFailed:    true,
		sql.StatementStateCanceled:  true,
		sql.StatementStateClosed:    true,
		"":                          false,
	}
	for state, want := range cases {
		assert.Equal(t, want, isTerminal(state), "state %q", state)
	}
}

func TestStatementError(t *testing.T) {
	err := statementError(&sql.StatementStatus{
		State: sql.StatementStateFailed,
		Error: &sql.ServiceError{Message: "boom"},
	})
	assert.EqualError(t, err, "statement FAILED: boom")

	// no Error object (nil) or an empty message: the state alone
	assert.EqualError(t, statementError(&sql.StatementStatus{State: sql.StatementStateCanceled}), "statement CANCELED")
	assert.EqualError(t, statementError(&sql.StatementStatus{
		State: sql.StatementStateClosed, Error: &sql.ServiceError{},
	}), "statement CLOSED")
}
