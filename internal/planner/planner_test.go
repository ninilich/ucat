package planner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/planner"
)

var testPath = change.TablePath{Catalog: litCat, Schema: litSch, Name: "tbl"}

func makeChanges() []change.Change {
	return []change.Change{
		&change.DropColumnChange{Base: change.Base{Table: testPath}, ColumnName: "x"},
		&change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "y", Type: typSTRING}},
		&change.CreateTableChange{Base: change.Base{Table: testPath}, Def: config.Table{Name: "tbl"}},
		&change.DropTableChange{Base: change.Base{Table: testPath}},
		&change.AlterColumnChange{Base: change.Base{Table: testPath}, ColumnName: "z", Kind: change.AlterType, OldValue: typINT, NewValue: "BIGINT"},
	}
}

func TestPlan_orderCreatesBeforeAltersBeforeDrops(t *testing.T) {
	changes := makeChanges()

	result := planner.Plan(changes)

	require.Len(t, result, 5)

	// CreateTable must be first
	assert.Equal(t, change.CreateTable, result[0].Type(), "CreateTable should be first")

	// AddColumn and AlterColumn should come before DropTable and DropColumn
	types := make([]change.Kind, len(result))
	for i, c := range result {
		types[i] = c.Type()
	}

	createIdx := indexOf(types, change.CreateTable)
	addIdx := indexOf(types, change.AddColumn)
	alterIdx := indexOf(types, change.AlterColumn)
	dropTblIdx := indexOf(types, change.DropTable)
	dropColIdx := indexOf(types, change.DropColumn)

	assert.Less(t, createIdx, addIdx, "CreateTable before AddColumn")
	assert.Less(t, createIdx, alterIdx, "CreateTable before AlterColumn")
	assert.Less(t, addIdx, dropTblIdx, "AddColumn before DropTable")
	assert.Less(t, alterIdx, dropTblIdx, "AlterColumn before DropTable")
	assert.Less(t, dropTblIdx, dropColIdx, "DropTable before DropColumn")
}

func TestPlan_noChanges(t *testing.T) {
	result := planner.Plan(nil)

	assert.Empty(t, result)
}

func indexOf(types []change.Kind, ct change.Kind) int {
	for i, t := range types {
		if t == ct {
			return i
		}
	}
	return -1
}

// --- Planner sub-ordering within group 1 ---

// TestPlan_SetTBLProperties_BeforeRenameColumn verifies that when a plan contains both
// SetTBLPropertiesChange and RenameColumnChange for the same table, SetTBLProperties
// appears BEFORE RenameColumn in the result.
//
// This matters for the column-rename lifecycle:
//
//	CREATE → SET delta.columnMapping.mode=name → RENAME COLUMN
//
// Input is provided in the WRONG order (RenameColumn first) to verify the planner
// actively re-orders rather than relying on input order.
func TestPlan_SetTBLProperties_BeforeRenameColumn(t *testing.T) {

	// Adversarial input: RenameColumn BEFORE SetTBLProperties
	changes := []change.Change{
		&change.RenameColumnChange{
			Base:    change.Base{Table: testPath},
			OldName: litUserName,
			NewName: litFullName,
		},
		&change.SetTBLPropertiesChange{
			Base:       change.Base{Table: testPath},
			Properties: map[string]string{propColumnMapping: litName},
		},
	}

	result := planner.Plan(changes)
	require.Len(t, result, 2)

	types := make([]change.Kind, len(result))
	for i, c := range result {
		types[i] = c.Type()
	}

	setIdx := indexOf(types, change.SetTBLProperties)
	renameIdx := indexOf(types, change.RenameColumn)

	assert.NotEqual(t, -1, setIdx, "SetTBLProperties must appear in plan")
	assert.NotEqual(t, -1, renameIdx, "RenameColumn must appear in plan")
	assert.Less(t, setIdx, renameIdx,
		"SetTBLProperties must come BEFORE RenameColumn in plan "+
			"(column mapping must be set before rename is attempted); "+
			"got SetTBLProperties at idx %d, RenameColumn at idx %d", setIdx, renameIdx)
}

// TestPlan_SetTBLProperties_BeforeRenameColumn_AlreadyCorrectOrder verifies that when the
// input is already in the correct order (SetTBLProperties before RenameColumn), the planner
// preserves that order (regression check).
func TestPlan_SetTBLProperties_BeforeRenameColumn_AlreadyCorrectOrder(t *testing.T) {

	// Correct input order
	changes := []change.Change{
		&change.SetTBLPropertiesChange{
			Base:       change.Base{Table: testPath},
			Properties: map[string]string{propColumnMapping: litName},
		},
		&change.RenameColumnChange{
			Base:    change.Base{Table: testPath},
			OldName: litUserName,
			NewName: "full_name",
		},
	}

	result := planner.Plan(changes)
	require.Len(t, result, 2)

	types := make([]change.Kind, len(result))
	for i, c := range result {
		types[i] = c.Type()
	}

	setIdx := indexOf(types, change.SetTBLProperties)
	renameIdx := indexOf(types, change.RenameColumn)

	assert.Less(t, setIdx, renameIdx,
		"planner must preserve correct order (SetTBLProperties before RenameColumn); "+
			"got SetTBLProperties at idx %d, RenameColumn at idx %d", setIdx, renameIdx)
}

// TestPlan_MultiChangeTable_SetTBLAndRenameColumnOrdering verifies the ordering when
// there are multiple changes including other types mixed in.
func TestPlan_MultiChangeTable_SetTBLAndRenameColumnOrdering(t *testing.T) {

	// Mix of changes; adversarial order with RenameColumn first
	changes := []change.Change{
		&change.RenameColumnChange{
			Base:    change.Base{Table: testPath},
			OldName: "a",
			NewName: "b",
		},
		&change.AddColumnChange{
			Base:   change.Base{Table: testPath},
			Column: config.Column{Name: "new_col", Type: "STRING"},
		},
		&change.SetTBLPropertiesChange{
			Base:       change.Base{Table: testPath},
			Properties: map[string]string{"delta.columnMapping.mode": "name"},
		},
	}

	result := planner.Plan(changes)
	require.Len(t, result, 3)

	types := make([]change.Kind, len(result))
	for i, c := range result {
		types[i] = c.Type()
	}

	setIdx := indexOf(types, change.SetTBLProperties)
	renameIdx := indexOf(types, change.RenameColumn)

	assert.Less(t, setIdx, renameIdx,
		"SetTBLProperties must be ordered before RenameColumn even with other changes present; "+
			"got plan types: %v", types)
}
