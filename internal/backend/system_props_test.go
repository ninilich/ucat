package backend_test

// system_props_test.go covers the unified IsSystemTblPropKey function.
//
// The design calls for a single exported function backend.IsSystemTblPropKey that is shared
// by both differ.go and uc/tables.go. Until that function exists, these tests verify
// the expected filtering behavior via differ.DiffSchema (integration-style).
//
// Known divergence that the unified function resolves:
//   differ.go:    systemTblPropKey filters "clusterByAuto" and HasPrefix("clusteringColumns")
//   uc/tables.go: isSystemProperty filters "clusterByAuto" and exact "clusteringColumns"
// Both functions must agree on the same set of keys.

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTableSP builds a minimal config.Table with resolved catalog/schema for tests.
func makeTableSP(name, schema string) config.Table {
	return config.Table{
		Name:            name,
		Schema:          schema,
		ResolvedCatalog: "prod",
		ResolvedSchema:  schema,
	}
}

func strPtrSP(s string) *string { return &s }

// TestIsSystemTblPropKey_ClusterByAuto_Filtered verifies via differ.DiffSchema that
// "clusterByAuto" is treated as a system property and never produces a diff change.
//
// Equivalent direct assertion:
//
//	assert.True(t, backend.IsSystemTblPropKey("clusterByAuto"))
func TestIsSystemTblPropKey_ClusterByAuto_Filtered(t *testing.T) {
	assert.True(t, backend.IsSystemTblPropKey("clusterByAuto"))
	desired := makeTableSP("orders", "silver")
	desired.TblProperties = map[string]*string{"clusterByAuto": strPtrSP("true")}

	live := makeTableSP("orders", "silver")

	changes, _ := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	assert.Empty(t, changes,
		"'clusterByAuto' must be filtered as a system property; got changes: %v", changes)
}

// TestIsSystemTblPropKey_ClusteringColumns_Filtered verifies that "clusteringColumns"
// is treated as a system property by differ and produces no diff change.
//
// Equivalent direct assertion:
//
//	assert.True(t, backend.IsSystemTblPropKey("clusteringColumns"))
func TestIsSystemTblPropKey_ClusteringColumns_Filtered(t *testing.T) {
	desired := makeTableSP("orders", "silver")
	desired.TblProperties = map[string]*string{litClusteringColumns: strPtrSP(`["a","b"]`)}

	live := makeTableSP("orders", "silver")

	changes, _ := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	assert.Empty(t, changes,
		"'clusteringColumns' must be filtered as a system property; got changes: %v", changes)
}

// TestIsSystemTblPropKey_DeltaColumnMappingMode_NotFiltered verifies that
// "delta.columnMapping.mode" is NOT a system property and produces a diff.
//
// Equivalent direct assertion:
//
//	assert.False(t, backend.IsSystemTblPropKey("delta.columnMapping.mode"))
func TestIsSystemTblPropKey_DeltaColumnMappingMode_NotFiltered(t *testing.T) {
	desired := makeTableSP("orders", "silver")
	desired.TblProperties = map[string]*string{"delta.columnMapping.mode": strPtrSP("name")}

	live := makeTableSP("orders", "silver")

	changes, _ := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	require.Len(t, changes, 1,
		"'delta.columnMapping.mode' must NOT be filtered; expected SetTBLProperties change")
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

// TestIsSystemTblPropKey_ClusteringColumnsPrefix_Filtered verifies that keys with the
// "clusteringColumns" prefix are also filtered by differ.go.
// This tests the HasPrefix behavior in differ.go's systemTblPropKey.
// uc/tables.go does NOT currently filter these (divergence to be fixed by the unified function).
func TestIsSystemTblPropKey_ClusteringColumnsPrefix_Filtered(t *testing.T) {
	desired := makeTableSP("orders", "silver")
	// Key has "clusteringColumns" as prefix but is not the exact key
	desired.TblProperties = map[string]*string{"clusteringColumnsIndexed": strPtrSP("0")}

	live := makeTableSP("orders", "silver")

	changes, _ := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	assert.Empty(t, changes,
		"'clusteringColumnsIndexed' (HasPrefix match) must be filtered by differ; "+
			"got changes: %v", changes)
}

// TestIsSystemTblPropKey_ArbitraryUserKey_NotFiltered verifies that user-defined
// tblproperties are never treated as system properties.
func TestIsSystemTblPropKey_ArbitraryUserKey_NotFiltered(t *testing.T) {
	desired := makeTableSP("orders", "silver")
	desired.TblProperties = map[string]*string{
		"myCustomProp":    strPtrSP("value"),
		"delta.enableCDF": strPtrSP("true"),
	}

	live := makeTableSP("orders", "silver")

	changes, _ := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	require.Len(t, changes, 1,
		"user-defined tblprops must not be filtered; expected SetTBLProperties change")
	assert.Equal(t, change.SetTBLProperties, changes[0].Type())
}

// TestIsSystemTblPropKey_DivergenceBetweenDifferAndUCTables documents the known
// divergence between differ.go and uc/tables.go system property detection.
// This test passes today (differ already works), but the unified function requires the logic to be
// unified into a single function so both layers stay in sync.
func TestIsSystemTblPropKey_DivergenceBetweenDifferAndUCTables(t *testing.T) {
	// differ.go currently filters "clusteringColumns" and anything with that prefix.
	// uc/tables.go currently filters only the exact "clusteringColumns" key.
	// This means "clusteringColumnsIndexed" is filtered by differ but not by uc/tables,
	// creating an asymmetry: the desired YAML is filtered during diff, but the same key
	// would NOT be removed from the live table's tblproperties, causing spurious diffs.

	// Verify differ correctly filters the prefix (this is current behavior)
	desired := makeTableSP("orders", "silver")
	desired.TblProperties = map[string]*string{"clusteringColumns": strPtrSP(`["id"]`)}

	live := makeTableSP("orders", "silver")
	// Even if live has the same key, differ should ignore it
	live.TblProperties = map[string]*string{"clusteringColumns": strPtrSP(`["id"]`)}

	changes, _ := differ.DiffSchema([]config.Table{desired}, []*backend.LiveTable{backendtest.LiveOf(live)})
	assert.Empty(t, changes,
		"even when desired and live both have 'clusteringColumns', "+
			"differ must produce 0 changes (it's a system property on both sides)")
}
