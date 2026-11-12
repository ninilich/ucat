// Package backendtest provides an in-memory UCBackend for tests.
package backendtest

import (
	"context"
	"sync"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
)

// ApplyCall records a single call to MockUCBackend.ApplyChange for assertion in tests.
type ApplyCall struct {
	Kind     change.Kind
	Describe string
}

// MockUCBackend is an in-memory UCBackend for use in L2 tests.
// It is safe for concurrent use.
type MockUCBackend struct {
	mu           sync.Mutex
	fixtures     map[backend.SchemaRef][]*backend.LiveTable
	calls        []ApplyCall
	nextApplyErr error
	warehouseErr error
	// TableProperties are the live tblproperties by "catalog.schema.table". Like UC they are reported
	// both by GetTableProperties and, for the tables of a fixture, by ListTables (LiveTable.Properties,
	// without the system keys), so that the two views of one table agree.
	TableProperties map[string]map[string]string
}

// SetFixture stores tables as the live state for the given schema.
// Replaces any previous fixture for that schema.
func (m *MockUCBackend) SetFixture(schema backend.SchemaRef, tables []*backend.LiveTable) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fixtures == nil {
		m.fixtures = make(map[backend.SchemaRef][]*backend.LiveTable)
	}
	m.fixtures[schema] = tables
}

// ListTables returns the fixture for the given schema, or an empty slice if none is set. A table that
// has an entry in TableProperties is returned as a copy with those properties added.
func (m *MockUCBackend) ListTables(_ context.Context, schema backend.SchemaRef) ([]*backend.LiveTable, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tables, ok := m.fixtures[schema]
	if !ok {
		return []*backend.LiveTable{}, nil
	}
	out := make([]*backend.LiveTable, len(tables))
	for i, t := range tables {
		out[i] = m.withProperties(t)
	}
	return out, nil
}

// withProperties returns t, or a copy of it with the TableProperties of the table merged into
// Properties (system keys left out, as in the real backend). The caller holds m.mu.
func (m *MockUCBackend) withProperties(t *backend.LiveTable) *backend.LiveTable {
	props, ok := m.TableProperties[t.Catalog+"."+t.Schema+"."+t.Name]
	if !ok {
		return t
	}
	c := *t
	c.Properties = make(map[string]string, len(t.Properties)+len(props))
	for k, v := range t.Properties {
		c.Properties[k] = v
	}
	for k, v := range props {
		if !backend.IsSystemTblPropKey(k) {
			c.Properties[k] = v
		}
	}
	return &c
}

// ApplyChange records the call and returns any configured error.
// If SetApplyError was called, the error is returned once and then cleared.
func (m *MockUCBackend) ApplyChange(_ context.Context, ch change.Change) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, ApplyCall{
		Kind:     ch.Type(),
		Describe: ch.Describe(),
	})
	if m.nextApplyErr != nil {
		err := m.nextApplyErr
		m.nextApplyErr = nil
		return err
	}
	return nil
}

// SetApplyError configures the next single ApplyChange call to return err.
// The error is consumed on first use.
func (m *MockUCBackend) SetApplyError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextApplyErr = err
}

// CheckWarehouse returns the error configured by SetWarehouseError (nil by default: the warehouse is reachable).
func (m *MockUCBackend) CheckWarehouse(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.warehouseErr
}

// SetWarehouseError makes every CheckWarehouse call return err (nil restores the default: reachable).
func (m *MockUCBackend) SetWarehouseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warehouseErr = err
}

// RecordedCalls returns a copy of all recorded ApplyChange calls in order.
func (m *MockUCBackend) RecordedCalls() []ApplyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]ApplyCall, len(m.calls))
	copy(result, m.calls)
	return result
}

// GetTableProperties returns mock tblproperties for the given table.
// Returns an empty map if no fixture is set for the table.
func (m *MockUCBackend) GetTableProperties(_ context.Context, cat, schema, table string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := cat + "." + schema + "." + table
	if m.TableProperties != nil {
		if props, ok := m.TableProperties[key]; ok {
			result := make(map[string]string, len(props))
			for k, v := range props {
				result[k] = v
			}
			return result, nil
		}
	}
	return map[string]string{}, nil
}

// Compile-time check that MockUCBackend implements UCBackend.
var _ backend.UCBackend = (*MockUCBackend)(nil)
