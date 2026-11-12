package uc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	databricks "github.com/databricks/databricks-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statementServer answers ExecuteStatement with first and then every status poll with the next
// body of polls (the last one repeats). It counts the polls.
func statementServer(t *testing.T, first string, polls ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/.well-known/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(first))
		default:
			i := int(n.Add(1)) - 1
			if i >= len(polls) {
				i = len(polls) - 1
			}
			_, _ = w.Write([]byte(polls[i]))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// setPollInterval sets the polling interval until the end of the test; every test that changes it goes through here.
func setPollInterval(t *testing.T, d time.Duration) {
	t.Helper()
	old := pollInterval
	pollInterval = d
	t.Cleanup(func() { pollInterval = old })
}

func statementClient(t *testing.T, srv *httptest.Server) *databricks.WorkspaceClient {
	t.Helper()
	setPollInterval(t, time.Millisecond)
	client, err := databricks.NewWorkspaceClient(&databricks.Config{Host: srv.URL, Token: "x"})
	require.NoError(t, err)
	return client
}

func state(s string) string {
	return fmt.Sprintf(`{"statement_id":"st-1","status":{"state":%q}}`, s)
}

func stateErr(s, msg string) string {
	return fmt.Sprintf(`{"statement_id":"st-1","status":{"state":%q,"error":{"message":%q}}}`, s, msg)
}

// executeSQL / pollStatement: every statement state ends the way the user expects.
func TestExecuteSQL_statementStates(t *testing.T) {
	tests := []struct {
		name    string
		first   string
		polls   []string
		wantErr string // "" = success
		minPoll int32
	}{
		{"succeeded at once", state("SUCCEEDED"), nil, "", 0},
		{"failed at once with message", stateErr("FAILED", "TABLE_OR_VIEW_NOT_FOUND"), nil, "statement FAILED: TABLE_OR_VIEW_NOT_FOUND", 0},
		{"canceled at once", state("CANCELED"), nil, "statement CANCELED", 0},
		{"closed at once", state("CLOSED"), nil, "statement CLOSED", 0},
		{"pending then succeeded", state("PENDING"), []string{state("PENDING"), state("RUNNING"), state("SUCCEEDED")}, "", 3},
		{"running then failed", state("RUNNING"), []string{state("RUNNING"), stateErr("FAILED", "boom 'x'")}, "statement FAILED: boom 'x'", 2},
		{"running then failed without message", state("RUNNING"), []string{state("FAILED")}, "statement FAILED", 1},
		{"pending then canceled", state("PENDING"), []string{state("CANCELED")}, "statement CANCELED", 1},
		{"running then closed", state("RUNNING"), []string{state("CLOSED")}, "statement CLOSED", 1},
		{"poll without status is skipped", state("PENDING"), []string{`{"statement_id":"st-1"}`, state("SUCCEEDED")}, "", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, polls := statementServer(t, tt.first, tt.polls...)
			err := executeSQL(context.Background(), statementClient(t, srv), "wh", "SELECT 1")
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.NotContains(t, err.Error(), "may have been applied", "the hint belongs to a failed poll request only")
			}
			assert.GreaterOrEqual(t, polls.Load(), tt.minPoll)
			if tt.minPoll == 0 {
				assert.Zero(t, polls.Load(), "a terminal first response is not polled")
			}
		})
	}
}

// A canceled context stops the polling with the context error (not a hang, not a statement error).
func TestPollStatement_contextCanceled(t *testing.T) {
	srv, _ := statementServer(t, state("PENDING"), state("RUNNING"))
	client := statementClient(t, srv)
	setPollInterval(t, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- pollStatement(ctx, client, "st-1") }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, context.DeadlineExceeded, err, "the bare context error: no failed-poll wrapping and no \"may have been applied\" hint")
	case <-time.After(5 * time.Second):
		t.Fatal("pollStatement did not return after the context ended")
	}
}

// A response without a status is an error, not a success.
func TestExecuteSQL_nilStatus(t *testing.T) {
	srv, _ := statementServer(t, `{"statement_id":"st-1"}`)
	err := executeSQL(context.Background(), statementClient(t, srv), "wh", "SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil status")
}

// maxWait passing while the statement is still running is a timeout error that names the statement
// and carries the "may have been applied" hint (the statement was sent and may still run).
func TestPollStatement_timeout(t *testing.T) {
	srv, _ := statementServer(t, state("PENDING"), state("RUNNING"))
	client := statementClient(t, srv)
	oldWait := maxWait
	maxWait = 5 * time.Millisecond
	t.Cleanup(func() { maxWait = oldWait })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pollStatement(ctx, client, "st-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute statement: timed out after 5ms (statement st-1)")
	assert.Contains(t, err.Error(), "the change may have been applied on the server; run `ucat plan` to see the actual state")
	assert.NoError(t, ctx.Err(), "the timeout came from maxWait, not from the test context")
}

// SUCCEEDED in the second poll ends the polling with nil; the context bounds the test.
func TestPollStatement_succeededOnSecondPoll(t *testing.T) {
	srv, polls := statementServer(t, state("PENDING"), state("RUNNING"), state("SUCCEEDED"))
	client := statementClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, pollStatement(ctx, client, "st-1"))
	assert.Equal(t, int32(2), polls.Load(), "polling stops at the first SUCCEEDED")
}
