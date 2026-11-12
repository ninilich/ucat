package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/uc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sqlRESTBackend reads the live state from the mock but applies changes through the real
// DatabricksBackend.ApplyChange against a fake statement API whose status poll always fails.
type sqlRESTBackend struct {
	*spyBackend
	real *uc.DatabricksBackend
}

func (b *sqlRESTBackend) ApplyChange(ctx context.Context, ch change.Change) error {
	return b.real.ApplyChange(ctx, ch)
}

// A failed statement poll reaches the apply output with the "may have been applied" hint.
func TestApply_PollError_HintInOutput(t *testing.T) {
	setupPlanProject(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/.well-known/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"statement_id":"st-9","status":{"state":"PENDING"}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error_code":"BAD_REQUEST","message":"poll exploded"}`))
		}
	}))
	defer srv.Close()
	// The SDK credential chain: a token from the environment, no profile file from the user's home.
	t.Setenv("DATABRICKS_TOKEN", "x")
	t.Setenv("DATABRICKS_CONFIG_FILE", filepath.Join(t.TempDir(), "none"))
	realBackend, err := uc.NewDatabricksBackend(srv.URL, "wh")
	require.NoError(t, err)

	m := addColumnMock()
	b := &sqlRESTBackend{spyBackend: newSpy(m), real: realBackend}
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) { return b, nil })

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.Error(t, err)
	out := stdout + stderr
	assert.Contains(t, out, "poll statement st-9: ")
	assert.Contains(t, out, "poll exploded")
	assert.Contains(t, out, "the change may have been applied on the server; run `ucat plan` to see the actual state")
}
