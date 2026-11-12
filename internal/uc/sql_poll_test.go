package uc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	databricks "github.com/databricks/databricks-sdk-go"
	"github.com/databricks/databricks-sdk-go/apierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pollFailServer answers ExecuteStatement with PENDING and every status poll with HTTP 500.
func pollFailServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/2.0/sql/statements") {
			_, _ = w.Write([]byte(`{"statement_id":"st-1","status":{"state":"PENDING"}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error_code":"BAD_REQUEST","message":"poll exploded"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExecuteSQL_pollError_hintAndWrapping(t *testing.T) {
	setPollInterval(t, time.Millisecond)

	srv := pollFailServer(t)
	client, err := databricks.NewWorkspaceClient(&databricks.Config{Host: srv.URL, Token: "x"})
	require.NoError(t, err)

	err = executeSQL(context.Background(), client, "wh", "SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll statement st-1: ")
	assert.Contains(t, err.Error(), "poll exploded")
	assert.Contains(t, err.Error(), "the change may have been applied on the server; run `ucat plan` to see the actual state")
	// %w chain intact: the SDK API error is still reachable with errors.As.
	var apiErr *apierr.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "BAD_REQUEST", apiErr.ErrorCode)
}
