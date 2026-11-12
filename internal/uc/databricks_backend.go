// Package uc implements backend.UCBackend on top of the Databricks SDK
// (Unity Catalog API and SQL statement execution).
package uc

import (
	"context"
	"errors"
	"fmt"

	databricks "github.com/databricks/databricks-sdk-go"
	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
)

// DatabricksBackend implements backend.UCBackend against a real Databricks workspace.
type DatabricksBackend struct {
	client      *databricks.WorkspaceClient
	warehouseID string
}

// NewDatabricksBackend creates a DatabricksBackend connected to the given workspace.
// If host is empty, the SDK credential chain is used.
func NewDatabricksBackend(host, warehouseID string) (*DatabricksBackend, error) {
	client, err := newWorkspaceClient(host)
	if err != nil {
		return nil, fmt.Errorf("databricks backend: %w", err)
	}
	return &DatabricksBackend{client: client, warehouseID: warehouseID}, nil
}

// errNoWarehouse is the guard error of a backend created without a warehouse. The commands check
// warehouse_id first (cmd requireWarehouse, warehouseWarning), so the user does not normally see it.
var errNoWarehouse = errors.New("SQL warehouse is not configured")

// ListTables returns all MANAGED tables in the given schema from live UC.
func (b *DatabricksBackend) ListTables(ctx context.Context, schema backend.SchemaRef) ([]*backend.LiveTable, error) {
	return listTables(ctx, b.client, schema)
}

// ApplyChange executes a Change against UC via SQL Statement Execution API.
// v1: all changes use SQL; warehouseID must be set.
func (b *DatabricksBackend) ApplyChange(ctx context.Context, ch change.Change) error {
	stmt := ch.SQL()
	if stmt == "" {
		return fmt.Errorf("change %q produced empty SQL", ch.Describe())
	}
	if b.warehouseID == "" {
		return errNoWarehouse
	}
	return executeSQL(ctx, b.client, b.warehouseID, stmt)
}

// CheckWarehouse verifies that the configured warehouse is reachable by running SELECT 1.
func (b *DatabricksBackend) CheckWarehouse(ctx context.Context) error {
	if b.warehouseID == "" {
		return errNoWarehouse
	}
	return executeSQL(ctx, b.client, b.warehouseID, "SELECT 1")
}

// GetTableProperties returns the live tblproperties for the given table.
func (b *DatabricksBackend) GetTableProperties(ctx context.Context, cat, schema, table string) (map[string]string, error) {
	fullName := cat + "." + schema + "." + table
	ti, err := b.client.Tables.GetByFullName(ctx, fullName)
	if err != nil {
		return nil, fmt.Errorf("get table %s: %w", fullName, err)
	}
	result := make(map[string]string, len(ti.Properties))
	for k, v := range ti.Properties {
		result[k] = v
	}
	return result, nil
}

// Compile-time check that DatabricksBackend implements UCBackend.
var _ backend.UCBackend = (*DatabricksBackend)(nil)

// newWorkspaceClient creates a Databricks SDK workspace client.
// If host is non-empty, it sets the workspace host explicitly.
// If host is empty, the SDK uses its credential chain:
//
//	env vars (DATABRICKS_HOST + DATABRICKS_TOKEN),
//	~/.databrickscfg profile,
//	Azure MSI, oauth-m2m, etc.
func newWorkspaceClient(host string) (*databricks.WorkspaceClient, error) {
	if host != "" {
		return databricks.NewWorkspaceClient(&databricks.Config{Host: host})
	}
	return databricks.NewWorkspaceClient(&databricks.Config{})
}
