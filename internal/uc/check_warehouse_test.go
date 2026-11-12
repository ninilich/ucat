package uc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	databricks "github.com/databricks/databricks-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
)

// warehouseServer answers statement execution with the given state (and error message) and records the request bodies.
func warehouseServer(t *testing.T, state, errMsg string) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/2.0/sql/statements") {
			raw, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			mu.Lock()
			bodies = append(bodies, m)
			mu.Unlock()
			status := `{"state":"` + state + `"`
			if errMsg != "" {
				status += `,"error":{"message":"` + errMsg + `"}`
			}
			status += "}"
			_, _ = w.Write([]byte(`{"statement_id":"st-1","status":` + status + `}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

func newTestBackend(t *testing.T, srv *httptest.Server, wh string) *DatabricksBackend {
	t.Helper()
	client, err := databricks.NewWorkspaceClient(&databricks.Config{Host: srv.URL, Token: "x"})
	require.NoError(t, err)
	return &DatabricksBackend{client: client, warehouseID: wh}
}

func TestCheckWarehouse_runsSelect1OnConfiguredWarehouse(t *testing.T) {
	srv, bodies := warehouseServer(t, "SUCCEEDED", "")
	b := newTestBackend(t, srv, "wh1")

	require.NoError(t, b.CheckWarehouse(context.Background()))
	got := bodies()
	require.Len(t, got, 1)
	assert.Equal(t, "SELECT 1", got[0]["statement"])
	assert.Equal(t, "wh1", got[0]["warehouse_id"])
}

func TestCheckWarehouse_failedStatementIsReported(t *testing.T) {
	srv, _ := warehouseServer(t, "FAILED", "warehouse is stopped")
	b := newTestBackend(t, srv, "wh1")

	err := b.CheckWarehouse(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "warehouse is stopped")
}

func TestCheckWarehouse_emptyWarehouseID(t *testing.T) {
	srv, bodies := warehouseServer(t, "SUCCEEDED", "")
	b := newTestBackend(t, srv, "")

	err := b.CheckWarehouse(context.Background())
	require.EqualError(t, err, "SQL warehouse is not configured")
	assert.Empty(t, bodies(), "no request without a warehouse id")
}

// ApplyChange without a warehouse id fails with the same guard error and sends nothing.
func TestApplyChange_emptyWarehouseID(t *testing.T) {
	srv, bodies := warehouseServer(t, "SUCCEEDED", "")
	b := newTestBackend(t, srv, "")

	err := b.ApplyChange(context.Background(), &change.DropTableChange{Base: change.Base{Table: change.TablePath{Catalog: "c", Schema: "s", Name: "t"}}})
	require.EqualError(t, err, "SQL warehouse is not configured")
	assert.Empty(t, bodies(), "no request without a warehouse id")
}
