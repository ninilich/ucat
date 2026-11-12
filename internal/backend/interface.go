// Package backend defines the UCBackend interface: reading live UC state and executing changes.
package backend

import (
	"context"
	"strings"

	"github.com/ninilich/ucat/internal/change"
)

// IsSystemTblPropKey returns true for tblproperties keys managed by UC clustering internals.
// These keys are excluded from both live state and desired-side diff.
// Exact match for "clusterByAuto"; prefix match for "clusteringColumns" (covers
// "clusteringColumns", "clusteringColumnsIndexed", etc.).
func IsSystemTblPropKey(k string) bool {
	return k == "clusterByAuto" || strings.HasPrefix(k, "clusteringColumns")
}

// SchemaRef identifies a real UC schema by its UC catalog and schema names (not logical names).
type SchemaRef struct {
	UCCatalog string // real UC catalog name, e.g. "prod"
	UCSchema  string // real UC schema name, e.g. "silver"
}

// UCBackend abstracts UC state reading and change execution.
// DatabricksBackend and backendtest.MockUCBackend both implement this interface.
type UCBackend interface {
	// ListTables returns all MANAGED tables in the given schema from live UC state.
	ListTables(ctx context.Context, schema SchemaRef) ([]*LiveTable, error)

	// ApplyChange executes a single Change against UC.
	// v1: all changes via SQL Statement Execution API.
	// v2: REST for DROP; SQL for CREATE/ALTER.
	ApplyChange(ctx context.Context, ch change.Change) error

	// CheckWarehouse verifies that the configured SQL warehouse is reachable
	// (DatabricksBackend runs SELECT 1). It fails if no warehouse is configured.
	CheckWarehouse(ctx context.Context) error

	// GetTableProperties returns the live tblproperties for the given table.
	GetTableProperties(ctx context.Context, catalog, schema, table string) (map[string]string, error)
}
