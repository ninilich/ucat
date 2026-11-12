// Package planner orders changes so that every statement is valid when it runs
// (for example renames before column changes, drops last).
package planner

import (
	"fmt"
	"sort"

	"github.com/ninilich/ucat/internal/change"
)

// Plan returns the changes in execution order.
// Result order: CREATE/RenameTable first, then TBLPROPERTIES, then other non-destructive ALTERs,
// then DropTable, then DropColumn. The sort is stable: changes of one group keep their input order.
func Plan(changes []change.Change) []change.Change {
	result := make([]change.Change, len(changes))
	copy(result, changes)
	sort.SliceStable(result, func(i, j int) bool {
		return orderGroup(result[i].Type()) < orderGroup(result[j].Type())
	})
	return result
}

// orderGroup returns the sort priority for a Kind (lower = earlier in plan).
func orderGroup(ct change.Kind) int {
	switch ct {
	case change.CreateTable:
		return 0
	case change.RenameTable:
		// All other changes of a renamed table target its new name, so the rename must come first.
		return 0
	case change.SetTBLProperties, change.UnsetTBLProperties:
		return 1
	case change.AddColumn, change.AlterColumn, change.RenameColumn,
		change.AlterClustering, change.AlterTableComment:
		return 2
	case change.DropTable:
		return 3
	case change.DropColumn:
		return 4
	}
	panic(fmt.Sprintf("planner: unknown change type %d: add it to orderGroup", ct))
}
