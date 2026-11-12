package output_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/output"
	"github.com/ninilich/ucat/internal/planner"
)

func tp(schema, name string) change.TablePath {
	return change.TablePath{Catalog: litCat, Schema: schema, Name: name}
}

func addCol(p change.TablePath, col string) change.Change {
	return &change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: col, Type: typSTRING}}
}

func dropCol(p change.TablePath, col string) change.Change {
	return &change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: col}
}

func setProps(p change.TablePath) change.Change {
	return &change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"k": "v"}}
}

func renameTbl(old change.TablePath, newName string) change.Change {
	return &change.RenameTableChange{OldPath: old, NewName: newName}
}

func planOf(t *testing.T, in []change.Change) []change.Change {
	t.Helper()
	plan := planner.Plan(in)
	require.Len(t, plan, len(in))
	return plan
}

func printPlan(plan []change.Change) string {
	var buf bytes.Buffer
	output.PrintPlan(&buf, plan, false)
	return buf.String()
}

// planKeyOf is the expected group key: the NEW full path for renames, TableName() otherwise.
func planKeyOf(ch change.Change) string {
	if r, ok := ch.(*change.RenameTableChange); ok {
		return change.TablePath{Catalog: r.OldPath.Catalog, Schema: r.OldPath.Schema, Name: r.NewName}.FullName()
	}
	return ch.TableName()
}

// entries flattens PrintPlan output (without the summary) into "H:<table>" and "R:<description>" items.
func entries(out string) []string {
	var res []string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.TrimSpace(line) == "", strings.HasPrefix(line, "Plan:"):
		case strings.HasPrefix(line, "    "):
			rest := strings.TrimSpace(line)
			_, size := utf8.DecodeRuneInString(rest) // skip the symbol
			res = append(res, "R:"+strings.TrimSpace(rest[size:]))
		default:
			res = append(res, "H:"+strings.TrimSpace(line))
		}
	}
	return res
}

// expectedEntries is the order PrintPlan must produce for a plan: plan order, header on key change.
func expectedEntries(plan []change.Change) []string {
	var res []string
	prev := ""
	for i, ch := range plan {
		if k := planKeyOf(ch); i == 0 || k != prev {
			res = append(res, "H:"+k)
			prev = k
		}
		res = append(res, "R:"+ch.Describe())
	}
	return res
}

func TestPrintPlanOrder_RenameSetPropsAddColumnOneHeader(t *testing.T) {
	old := tp("sch", "old_name")
	newPath := tp("sch", "new_name")
	// Input deliberately not in execution order.
	plan := planOf(t, []change.Change{
		addCol(newPath, "c"),
		setProps(newPath),
		renameTbl(old, "new_name"),
	})
	out := printPlan(plan)

	assert.Equal(t, []string{
		"H:" + newPath.FullName(),
		"R:" + plan[0].Describe(),
		"R:set 1 tblproperties",
		"R:" + plan[2].Describe(),
	}, entries(out))
	assert.Equal(t, 1, strings.Count(out, "  "+newPath.FullName()+"\n"), "exactly one header with the new name")
	assert.NotContains(t, out, "  "+old.FullName()+"\n", "old name must not be a header")
	assert.Contains(t, out, "rename table old_name")
	assert.Contains(t, out, `add column "c"`)
	assert.Less(t, strings.Index(out, "rename table"), strings.Index(out, "set 1 tblproperties"))
	assert.Less(t, strings.Index(out, "set 1 tblproperties"), strings.Index(out, "add column"))
}

func TestPrintPlanOrder_RenameOnly(t *testing.T) {
	old := tp("sch", "old_name")
	plan := planOf(t, []change.Change{renameTbl(old, "new_name")})
	out := printPlan(plan)

	assert.Equal(t, []string{
		"H:" + tp("sch", "new_name").FullName(),
		"R:" + plan[0].Describe(),
	}, entries(out))
	assert.Contains(t, out, "rename table old_name → new_name")
	assert.Contains(t, out, "Plan: 0 to add, 1 to change, 0 to destroy.")
}

func TestPrintPlanOrder_InterleavedTablesFollowPlanOrder(t *testing.T) {
	a, b := tp("sch", "a"), tp("sch", "b")
	plan := planOf(t, []change.Change{
		dropCol(a, "old"),
		addCol(a, "x"),
		addCol(b, "y"),
	})
	// Planner: add a.x, add b.y (group 2), then drop a.old (group 4).
	require.Equal(t, a.FullName(), plan[0].TableName())
	require.Equal(t, b.FullName(), plan[1].TableName())
	require.Equal(t, a.FullName(), plan[2].TableName())

	out := printPlan(plan)
	assert.Equal(t, []string{
		"H:" + a.FullName(), "R:" + plan[0].Describe(),
		"H:" + b.FullName(), "R:" + plan[1].Describe(),
		"H:" + a.FullName(), "R:" + plan[2].Describe(),
	}, entries(out))
	assert.Equal(t, 2, strings.Count(out, "  "+a.FullName()+"\n"), "table header repeats on alternation")
	assert.Contains(t, out, "Plan: 0 to add, 2 to change, 1 to destroy.")
}

func TestPrintPlanOrder_CrossSchemaNotMerged(t *testing.T) {
	a1, a2 := tp("s1", "t"), tp("s2", "t")
	plan := planOf(t, []change.Change{addCol(a1, "x"), addCol(a2, "y"), setProps(a1)})
	out := printPlan(plan)

	assert.Equal(t, expectedEntries(plan), entries(out))
	assert.Contains(t, out, "  "+a1.FullName()+"\n")
	assert.Contains(t, out, "  "+a2.FullName()+"\n")

	// Adjacent changes of same-named tables in different schemas get separate headers.
	adj := printPlan([]change.Change{addCol(a1, "x"), addCol(a2, "y")})
	assert.Equal(t, []string{
		"H:" + a1.FullName(), "R:" + addCol(a1, "x").Describe(),
		"H:" + a2.FullName(), "R:" + addCol(a2, "y").Describe(),
	}, entries(adj))
}

func TestPrintPlanOrder_CrossSchemaRenameNewKey(t *testing.T) {
	// Rename in s1 to "t" must not share a header with the table "t" in s2.
	plan := planOf(t, []change.Change{
		renameTbl(tp("s1", "x"), "t"),
		addCol(tp("s2", "t"), "c"),
	})
	assert.Equal(t, expectedEntries(plan), entries(printPlan(plan)))
	assert.Len(t, entries(printPlan(plan)), 4, "two headers and two rows")
}

// mixedChanges builds many tables with change types that planner orders into different groups.
func mixedChanges() []change.Change {
	var in []change.Change
	for i := 0; i < 8; i++ {
		schema := fmt.Sprintf("s%d", i%3)
		p := tp(schema, fmt.Sprintf("t%d", i))
		switch i % 4 {
		case 0:
			in = append(in, renameTbl(tp(schema, fmt.Sprintf("old%d", i)), p.Name), setProps(p), addCol(p, "n"), dropCol(p, "o"))
		case 1:
			in = append(in, addCol(p, "a"), addCol(p, "b"), &change.AlterTableCommentChange{Base: change.Base{Table: p}, Comment: "c"})
		case 2:
			in = append(in, &change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{Name: p.Name}}, &change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}, Keys: []string{"k"}})
		default:
			in = append(in, &change.DropTableChange{Base: change.Base{Table: p}}, dropCol(tp(schema, "other"), "z"))
		}
	}
	return in
}

func TestPrintPlanOrder_ShuffledInputMatchesPlanOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for run := 0; run < 20; run++ {
		in := mixedChanges()
		rng.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
		plan := planOf(t, in)

		out := printPlan(plan)
		assert.Equal(t, expectedEntries(plan), entries(out), "run %d", run)
		assert.Equal(t, out, printPlan(plan), "run %d: output must be deterministic", run)
	}
}

func TestPrintPlanOrder_DeterministicAcrossRuns(t *testing.T) {
	first := printPlan(planOf(t, mixedChanges()))
	for run := 0; run < 20; run++ {
		assert.Equal(t, first, printPlan(planOf(t, mixedChanges())), "run %d", run)
	}
}

func TestPrintPlanOrder_SummaryCountsUnchanged(t *testing.T) {
	a, b := tp("sch", "a"), tp("sch", "b")
	plan := planOf(t, []change.Change{
		&change.CreateTableChange{Base: change.Base{Table: b}, Def: config.Table{Name: "b"}},
		renameTbl(tp("sch", "o"), "a"),
		addCol(a, "x"),
		dropCol(a, "y"),
		&change.DropTableChange{Base: change.Base{Table: tp("sch", "gone")}},
	})
	out := printPlan(plan)
	// add: create; change: rename, add column; destroy: drop column, drop table.
	assert.Contains(t, out, "Plan: 1 to add, 2 to change, 2 to destroy.")
}

func TestPrintShowSQL_FollowsPlanOrder(t *testing.T) {
	a, b := tp("sch", "a"), tp("sch", "b")
	plan := planOf(t, []change.Change{
		dropCol(a, "old"),
		addCol(a, "x"),
		addCol(b, "y"),
		renameTbl(tp("sch", "o"), "c"),
	})
	var buf bytes.Buffer
	output.PrintShowSQL(&buf, plan)
	out := buf.String()

	last := -1
	for _, ch := range plan {
		idx := strings.Index(out, ch.SQL())
		require.GreaterOrEqual(t, idx, 0, ch.SQL())
		assert.Greater(t, idx, last, "SQL of %q out of plan order", ch.Describe())
		last = idx
	}
}

// A table rename is reported under the new name (like the later changes of the table) with the old
// name as previous_name, so catalog, schema, table and path agree for all changes of one table.
func TestPrintJSON_RenameTable(t *testing.T) {
	old := tp("sch", "old_name")
	plan := planOf(t, []change.Change{renameTbl(old, "new_name"), addCol(tp("sch", "new_name"), "c")})

	var buf bytes.Buffer
	require.NoError(t, output.PrintJSON(&buf, plan))
	var res struct {
		Changes []struct {
			Op           string `json:"op"`
			Table        string `json:"table"`
			PreviousName string `json:"previous_name"`
			Path         string `json:"path"`
		} `json:"changes"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &res))
	require.Len(t, res.Changes, 2)
	assert.Equal(t, "rename_table", res.Changes[0].Op)
	assert.Equal(t, "new_name", res.Changes[0].Table)
	assert.Equal(t, "old_name", res.Changes[0].PreviousName)
	assert.Equal(t, "cat.sch.new_name", res.Changes[0].Path)
	assert.Equal(t, "cat.sch.new_name", res.Changes[1].Path)
	assert.Empty(t, res.Changes[1].PreviousName)
}
