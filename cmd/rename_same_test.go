package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// a table with rename_from equal to its name gets the validator warning and no rename.
func TestPlanApply_TableRenameFromEqualsName_NoChanges(t *testing.T) {
	tmp := setupPlanProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "tables", "silver", "orders.yml"),
		[]byte("name: orders\nschema: silver\nrename_from: orders\ncolumns:\n  - name: id\n    type: BIGINT\n"), 0o644))

	m := &backendtest.MockUCBackend{}
	m.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: "id", Type: typBIGINT}},
	})})
	useBackend(t, m)

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No changes")
	assert.NotContains(t, stdout, "rename")
	assert.Contains(t, stderr, `table rename_from "orders" is same as name; no rename will occur`)

	stdout, _, err = executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)
	assert.NotContains(t, stdout, "rename")
	assert.Empty(t, m.RecordedCalls())
}
