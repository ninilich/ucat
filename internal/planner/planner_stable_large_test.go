package planner_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/planner"
)

// Plan is stable for big plans too: sort.Slice is only unstable above ~12 elements, so small
// fixtures cannot tell it from sort.SliceStable. Within one group the input order is kept.
func TestPlan_largePlanKeepsInputOrderWithinGroup(t *testing.T) {
	var in []change.Change
	for i := 0; i < 60; i++ {
		p := change.TablePath{Catalog: "c", Schema: "s", Name: fmt.Sprintf("t%02d", i)}
		in = append(in,
			&change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: "x"},
			&change.AlterTableCommentChange{Base: change.Base{Table: p}, Comment: "c"},
			&change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"k": "v"}},
			&change.RenameTableChange{OldPath: p, NewName: p.Name + "_n"},
		)
	}
	out := planner.Plan(in)
	require.Len(t, out, len(in))
	// Per type: tables appear in the input order t00, t01, ...
	next := map[change.Kind]int{}
	for _, ch := range out {
		want := fmt.Sprintf("`c`.`s`.`t%02d`", next[ch.Type()])
		assert.Equal(t, want, ch.TableName(), "type %d out of input order", ch.Type())
		next[ch.Type()]++
	}
}
