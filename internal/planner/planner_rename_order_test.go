package planner_test

import (
	"context"
	"io"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/ninilich/ucat/internal/planner"
	"github.com/ninilich/ucat/internal/preflight"
)

// --- RenameTable must precede every other change of the same table ---

func typesOf(changes []change.Change) []change.Kind {
	out := make([]change.Kind, len(changes))
	for i, c := range changes {
		out[i] = c.Type()
	}
	return out
}

// newPathOf returns the path under which a change addresses its table (the NEW name for renames).
func newPathOf(c change.Change) change.TablePath {
	switch v := c.(type) {
	case *change.RenameTableChange:
		return change.TablePath{Catalog: v.OldPath.Catalog, Schema: v.OldPath.Schema, Name: v.NewName}
	case *change.CreateTableChange:
		return v.Table
	case *change.SetTBLPropertiesChange:
		return v.Table
	case *change.UnsetTBLPropertiesChange:
		return v.Table
	case *change.AddColumnChange:
		return v.Table
	case *change.AlterColumnChange:
		return v.Table
	case *change.RenameColumnChange:
		return v.Table
	case *change.AlterClusteringChange:
		return v.Table
	case *change.AlterTableCommentChange:
		return v.Table
	case *change.DropTableChange:
		return v.Table
	case *change.DropColumnChange:
		return v.Table
	}
	panic("unexpected change type")
}

func assertRenameFirst(t *testing.T, plan []change.Change, label string) {
	t.Helper()
	renamedAt := map[change.TablePath]int{}
	for i, c := range plan {
		if _, ok := c.(*change.RenameTableChange); ok {
			renamedAt[newPathOf(c)] = i
		}
	}
	for i, c := range plan {
		if _, ok := c.(*change.RenameTableChange); ok {
			continue
		}
		if at, ok := renamedAt[newPathOf(c)]; ok {
			assert.Less(t, at, i, "%s: RenameTable of %s must precede %q (types: %v)",
				label, newPathOf(c).Name, c.Describe(), typesOf(plan))
		}
	}
}

func renamedTableChanges(oldName, newName string) []change.Change {
	p := change.TablePath{Catalog: litCat, Schema: litSch, Name: newName}
	return []change.Change{
		&change.RenameTableChange{OldPath: change.TablePath{Catalog: litCat, Schema: litSch, Name: oldName}, NewName: newName},
		&change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"k": "v"}},
		&change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}, Keys: []string{litOld}},
		&change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: "c", Type: typINT}},
		&change.RenameColumnChange{Base: change.Base{Table: p}, OldName: "a", NewName: "b"},
		&change.AlterClusteringChange{Base: change.Base{Table: p}, Clustering: &config.Clustering{Auto: true}},
		&change.AlterTableCommentChange{Base: change.Base{Table: p}, Comment: "x"},
		&change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: "z"},
	}
}

func TestPlan_RenameTableFirst_ExactOrder(t *testing.T) {
	p := change.TablePath{Catalog: litCat, Schema: litSch, Name: litNew}
	rename := &change.RenameTableChange{OldPath: change.TablePath{Catalog: litCat, Schema: litSch, Name: litOld}, NewName: litNew}
	set := &change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"k": "v"}}
	unset := &change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}, Keys: []string{litOld}}
	add := &change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: "c", Type: typINT}}
	renCol := &change.RenameColumnChange{Base: change.Base{Table: p}, OldName: "a", NewName: "b"}
	clus := &change.AlterClusteringChange{Base: change.Base{Table: p}, Clustering: &config.Clustering{Auto: true}}
	cmt := &change.AlterTableCommentChange{Base: change.Base{Table: p}, Comment: "x"}
	dropCol := &change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: "z"}

	// Shuffled input.
	in := []change.Change{dropCol, cmt, unset, renCol, set, clus, add, rename}
	result := planner.Plan(in)
	require.Len(t, result, 8)

	assert.Same(t, rename, result[0], "RenameTable first")
	assert.ElementsMatch(t, []change.Change{set, unset}, result[1:3], "tblproperties group second")
	assert.ElementsMatch(t, []change.Change{add, renCol, clus, cmt}, result[3:7], "column/clustering/comment group third")
	assert.Same(t, dropCol, result[7], "DropColumn last")
}

func TestPlan_RenameTable_PrecedesSetAndRenameColumn(t *testing.T) {
	p := change.TablePath{Catalog: litCat, Schema: litSch, Name: litNew}
	in := []change.Change{
		&change.RenameColumnChange{Base: change.Base{Table: p}, OldName: "a", NewName: "b"},
		&change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{propColumnMapping: litName}},
		&change.RenameTableChange{OldPath: change.TablePath{Catalog: litCat, Schema: litSch, Name: "old"}, NewName: "new"},
	}
	result := planner.Plan(in)
	assert.Equal(t, []change.Kind{change.RenameTable, change.SetTBLProperties, change.RenameColumn}, typesOf(result))
}

func TestPlan_RenameTableFirst_InvariantAcrossPermutations(t *testing.T) {
	var base []change.Change
	base = append(base, renamedTableChanges("a_old", "a_new")...)
	base = append(base, renamedTableChanges("b_old", "b_new")...)
	other := change.TablePath{Catalog: litCat, Schema: litSch, Name: "other"}
	base = append(base,
		&change.SetTBLPropertiesChange{Base: change.Base{Table: other}, Properties: map[string]string{"k": "v"}},
		&change.AddColumnChange{Base: change.Base{Table: other}, Column: config.Column{Name: "c", Type: "INT"}},
		&change.CreateTableChange{Base: change.Base{Table: change.TablePath{Catalog: litCat, Schema: litSch, Name: "fresh"}}},
		&change.DropTableChange{Base: change.Base{Table: change.TablePath{Catalog: litCat, Schema: litSch, Name: "gone"}}},
		&change.DropColumnChange{Base: change.Base{Table: other}, ColumnName: "z"},
	)
	n := len(base)

	var perms [][]change.Change
	for r := 0; r < n; r++ {
		rot := append(append([]change.Change{}, base[r:]...), base[:r]...)
		rev := make([]change.Change, n)
		for i, c := range rot {
			rev[n-1-i] = c
		}
		perms = append(perms, rot, rev)
	}
	rng := rand.New(rand.NewSource(43))
	for i := 0; i < 50; i++ {
		s := append([]change.Change{}, base...)
		rng.Shuffle(n, func(a, b int) { s[a], s[b] = s[b], s[a] })
		perms = append(perms, s)
	}

	for i, in := range perms {
		result := planner.Plan(in)
		require.Len(t, result, n)
		assertRenameFirst(t, result, "permutation #"+string(rune('A'+i%26)))
	}
}

func TestPlan_RenameOnly_Unchanged(t *testing.T) {
	r := &change.RenameTableChange{OldPath: change.TablePath{Catalog: litCat, Schema: litSch, Name: "old"}, NewName: "new"}
	result := planner.Plan([]change.Change{r})
	require.Len(t, result, 1)
	assert.Same(t, r, result[0])
}

func TestPlan_TblPropertiesOnly_Unchanged(t *testing.T) {
	in := []change.Change{
		&change.UnsetTBLPropertiesChange{Base: change.Base{Table: testPath}, Keys: []string{"a"}},
		&change.SetTBLPropertiesChange{Base: change.Base{Table: testPath}, Properties: map[string]string{"k": "v"}},
	}
	result := planner.Plan(in)
	require.Len(t, result, 2)
	assert.Same(t, in[0], result[0])
	assert.Same(t, in[1], result[1])
}

// Integration: real DiffSchema -> Plan -> preflight -> Execute.
func TestPipeline_RenameTable_SetProps_RenameColumn_Order(t *testing.T) {
	mode := litName
	desired := []config.Table{{
		Name:            "orders_v2",
		RenameFrom:      "orders",
		ResolvedCatalog: "cat",
		ResolvedSchema:  "sch",
		Columns:         []config.Column{{Name: "full_name", Type: typSTRING, RenameFrom: litUserName}},
		TblProperties:   map[string]*string{propColumnMapping: &mode},
	}}
	live := []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name:            "orders",
		ResolvedCatalog: "cat",
		ResolvedSchema:  "sch",
		Columns:         []config.Column{{Name: "user_name", Type: "STRING"}},
	})}

	changes, diags := differ.DiffSchema(desired, live)
	require.False(t, diags.HasError())
	b := &backendtest.MockUCBackend{} // live props keyed by OLD name: none, no column mapping
	plan := planner.Plan(changes)

	pf := preflight.Run(context.Background(), plan, b, preflight.DefaultCheckers(plan))
	assert.False(t, pf.HasError(), "preflight must pass: desired sets mapping in same plan: %v", pf)

	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true}, nil, io.Discard)
	require.NoError(t, err)
	var got []change.Kind
	for _, c := range b.RecordedCalls() {
		got = append(got, c.Kind)
	}
	assert.Equal(t, []change.Kind{change.RenameTable, change.SetTBLProperties, change.RenameColumn}, got)
}
