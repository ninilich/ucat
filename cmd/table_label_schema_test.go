package cmd

// The table label of a diagnostic carries the schema: the same table name in two schemas must read
// differently in plan and in validate -t.

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sameNameTable = "orders"

func sameNameInTwoSchemas(t *testing.T, extra string) {
	t.Helper()
	dir := setupTestProject(t)
	addGoldSchema(t, dir)
	addTableFile(t, dir, litGold, sameNameTable, tableYAML(litGold, sameNameTable, extra))
	addTableFile(t, dir, litSilver, sameNameTable, tableYAML(litSilver, sameNameTable, extra))
}

func TestTableLabel_SameNameTwoSchemas_PlanSeparatesSchemas(t *testing.T) {
	dir := setupTestProject(t)
	addGoldSchema(t, dir)
	addTableFile(t, dir, litGold, sameNameTable, tableYAML(litGold, sameNameTable, "")+"  - name: n\n    type: INT\n    nullable: false\n")
	addTableFile(t, dir, litSilver, sameNameTable, tableYAML(litSilver, sameNameTable, "")+"  - name: m\n    type: INT\n    nullable: false\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(goldRef, []*backend.LiveTable{liveTable(litGold, sameNameTable)})
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable(litSilver, sameNameTable)})
	useSpy(t, newSpy(mock))

	_, stderr, err := executeCmdSplit(cmdPlan, "-t", litDev)
	require.Error(t, err)
	assert.Contains(t, stderr, `table "gold.orders", column "n": a new NOT NULL column`)
	assert.Contains(t, stderr, `table "silver.orders", column "m": a new NOT NULL column`)
}

func TestTableLabel_SameNameTwoSchemas_ValidateLiveSeparatesSchemas(t *testing.T) {
	sameNameInTwoSchemas(t, "rename_from: old\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(goldRef, []*backend.LiveTable{liveTable(litGold, sameNameTable)})
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable(litSilver, sameNameTable)})
	useBackend(t, mock)

	out, err := executeCmd("validate", "-t", litDev)
	require.NoError(t, err, out)
	assert.Contains(t, out, `table "gold.orders": rename_from "old" can be removed after apply`)
	assert.Contains(t, out, `table "silver.orders": rename_from "old" can be removed after apply`)
}
