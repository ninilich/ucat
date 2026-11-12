package cmd

// column rename_from typo (source and new column both missing in UC) is an Error
// that blocks plan and fails validate -t.

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func columnTypoFixture(t *testing.T) *backendtest.MockUCBackend {
	t.Helper()
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n  - name: uid\n    type: BIGINT\n    rename_from: idd\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: "id", Type: typBIGINT}},
	})})
	useSpy(t, newSpy(mock))
	return mock
}

func TestPlan_ColumnRenameFromTypo_Fails(t *testing.T) {
	mock := columnTypoFixture(t)

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev")
	require.Error(t, err, "stdout: %s", stdout)
	assert.Contains(t, stderr, `rename_from "idd" not found`)
	assert.Contains(t, stderr, `"uid"`)
	assert.Empty(t, mock.RecordedCalls())
}

func TestValidateTarget_ColumnRenameFromTypo_Fails(t *testing.T) {
	mock := columnTypoFixture(t)

	stdout, stderr, err := executeCmdSplit("validate", "-t", "dev")
	require.Error(t, err, "stdout: %s", stdout)
	assert.Contains(t, stderr, "error: ")
	assert.Contains(t, stderr, `rename_from "idd" not found`)
	assert.Empty(t, mock.RecordedCalls())
}
