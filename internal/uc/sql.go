package uc

import (
	"context"
	"fmt"
	"time"

	databricks "github.com/databricks/databricks-sdk-go"
	"github.com/databricks/databricks-sdk-go/service/sql"
)

// maxWait and pollInterval are variables only so tests can poll quickly and exercise the timeout.
var (
	maxWait      = 30 * time.Minute
	pollInterval = 2 * time.Second
)

// pollHint is appended to a failed status poll and to a timeout: the statement was already sent, so it may have run.
const pollHint = "the change may have been applied on the server; run `ucat plan` to see the actual state"

// executeSQL executes a SQL statement against a Databricks SQL Warehouse and waits for completion.
// Returns nil on success, error on failure or timeout.
func executeSQL(ctx context.Context, client *databricks.WorkspaceClient, warehouseID, statement string) error {
	resp, err := client.StatementExecution.ExecuteStatement(ctx, sql.ExecuteStatementRequest{
		Statement:     statement,
		WarehouseId:   warehouseID,
		WaitTimeout:   "50s",
		OnWaitTimeout: sql.ExecuteStatementRequestOnWaitTimeoutContinue,
	})
	if err != nil {
		return fmt.Errorf("execute statement: %w", err)
	}

	if resp.Status == nil {
		return fmt.Errorf("execute statement: nil status in response")
	}

	if resp.Status.State == sql.StatementStateSucceeded {
		return nil
	}
	if isTerminal(resp.Status.State) {
		return statementError(resp.Status)
	}
	return pollStatement(ctx, client, resp.StatementId)
}

// pollStatement polls a running statement until it reaches a terminal state, the context ends or
// maxWait passes. Returns nil when the statement succeeded.
func pollStatement(ctx context.Context, client *databricks.WorkspaceClient, stmtID string) error {
	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("execute statement: timed out after %s (statement %s): %s", maxWait, stmtID, pollHint)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		poll, err := client.StatementExecution.GetStatementByStatementId(ctx, stmtID)
		if err != nil {
			return fmt.Errorf("poll statement %s: %w (%s)", stmtID, err, pollHint)
		}
		if poll.Status == nil {
			continue
		}
		if poll.Status.State == sql.StatementStateSucceeded {
			return nil
		}
		if isTerminal(poll.Status.State) {
			return statementError(poll.Status)
		}
	}
}

func isTerminal(s sql.StatementState) bool {
	return s == sql.StatementStateFailed ||
		s == sql.StatementStateCanceled ||
		s == sql.StatementStateClosed
}

func statementError(status *sql.StatementStatus) error {
	if status.Error != nil && status.Error.Message != "" {
		return fmt.Errorf("statement %s: %s", status.State, status.Error.Message)
	}
	return fmt.Errorf("statement %s", status.State)
}
