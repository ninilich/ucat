package planner_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/planner"
)

// --- orderGroup guards ---

func TestOrderGroup_allKinds(t *testing.T) {
	for ct := change.Kind(0); ct < change.KindCount; ct++ {
		assert.NotPanics(t, func() {
			g := planner.OrderGroup(ct)
			assert.GreaterOrEqual(t, g, 0, "type %d", ct)
			assert.LessOrEqual(t, g, 4, "type %d", ct)
		}, "type %d", ct)
	}
}

func TestOrderGroup_unknownPanics(t *testing.T) {
	assert.Panics(t, func() { planner.OrderGroup(change.KindCount) })
	assert.Panics(t, func() { planner.OrderGroup(change.KindCount + 1) })
	assert.Panics(t, func() { planner.OrderGroup(change.Kind(-1)) })
}

func TestOrderGroup_exactGroups(t *testing.T) {
	want := map[change.Kind]int{
		change.CreateTable:        0,
		change.RenameTable:        0,
		change.SetTBLProperties:   1,
		change.UnsetTBLProperties: 1,
		change.AddColumn:          2,
		change.AlterColumn:        2,
		change.RenameColumn:       2,
		change.AlterClustering:    2,
		change.AlterTableComment:  2,
		change.DropTable:          3,
		change.DropColumn:         4,
	}
	require.Len(t, want, int(change.KindCount), "every Kind must be pinned")
	for ct, g := range want {
		assert.Equal(t, g, planner.OrderGroup(ct), "type %d", ct)
	}
}

// inGroup returns the changes of planner group g in their order in changes.
func inGroup(changes []change.Change, g int) []change.Change {
	var out []change.Change
	for _, c := range changes {
		if planner.OrderGroup(c.Type()) == g {
			out = append(out, c)
		}
	}
	return out
}

// indexOfChange is the position of the very change target (by identity) in changes, or -1.
func indexOfChange(changes []change.Change, target change.Change) int {
	for k, c := range changes {
		if c == target {
			return k
		}
	}
	return -1
}

func TestPlan_allTypesShuffled_stableGroupOrder(t *testing.T) {
	p := testPath
	rename := &change.RenameTableChange{OldPath: p, NewName: "n"}
	base := []change.Change{
		&change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: "x", Type: typINT}},
		&change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: "x"},
		&change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: "x", Kind: change.AlterType, OldValue: typINT, NewValue: "BIGINT"},
		&change.RenameColumnChange{Base: change.Base{Table: p}, OldName: "a", NewName: "b"},
		rename,
		&change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"k": "v"}},
		&change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}, Keys: []string{"k"}},
		&change.AlterClusteringChange{Base: change.Base{Table: p}, Clustering: &config.Clustering{Auto: true}},
		&change.AlterTableCommentChange{Base: change.Base{Table: p}, Comment: "c"},
		&change.DropTableChange{Base: change.Base{Table: p}},
		&change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{Name: litTbl}},
	}
	require.Len(t, base, int(change.KindCount), "fixture must cover every Kind")

	rng := rand.New(rand.NewSource(6))
	for i := 0; i < 30; i++ {
		in := append([]change.Change{}, base...)
		rng.Shuffle(len(in), func(a, b int) { in[a], in[b] = in[b], in[a] })

		result := planner.Plan(in)
		require.Len(t, result, len(in))

		prev := 0
		for _, c := range result {
			g := planner.OrderGroup(c.Type())
			assert.GreaterOrEqual(t, g, prev, "groups must be non-decreasing: %v", typesOf(result))
			prev = g
		}
		// SliceStable: changes of the same group keep their input relative order.
		for g := 0; g <= 4; g++ {
			wantSeq, gotSeq := inGroup(in, g), inGroup(result, g)
			require.Len(t, gotSeq, len(wantSeq))
			for k := range wantSeq {
				assert.Same(t, wantSeq[k], gotSeq[k], "group %d must keep input order", g)
			}
		}
		// RenameTable precedes every non-group-0 change.
		renameAt := indexOfChange(result, rename)
		for k, c := range result {
			if planner.OrderGroup(c.Type()) > 0 {
				assert.Less(t, renameAt, k)
			}
		}
	}
}
