package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/output"
)

func init() {
	color.NoColor = true // disable ANSI codes for all output tests
}

var testPath = change.TablePath{Catalog: litCat, Schema: litSch, Name: litTbl}

func TestPrintPlan_symbols(t *testing.T) {
	createTbl := &change.CreateTableChange{Base: change.Base{Table: testPath}, Def: config.Table{Name: litTbl}}
	dropTbl := &change.DropTableChange{Base: change.Base{Table: testPath}}
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}

	var buf bytes.Buffer
	output.PrintPlan(&buf, []change.Change{createTbl, dropTbl, addCol}, false)
	out := buf.String()

	assert.Contains(t, out, "+", "CreateTable should produce + symbol")
	assert.Contains(t, out, "-", "DropTable should produce - symbol")
	assert.Contains(t, out, "~", "AddColumn should produce ~ symbol")
}

func TestPrintPlan_summaryLine(t *testing.T) {
	createTbl := &change.CreateTableChange{Base: change.Base{Table: testPath}, Def: config.Table{Name: "tbl"}}
	dropTbl := &change.DropTableChange{Base: change.Base{Table: testPath}}
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}

	var buf bytes.Buffer
	output.PrintPlan(&buf, []change.Change{createTbl, dropTbl, addCol}, false)
	out := buf.String()

	// 1 create (add), 1 addColumn (change), 1 dropTable (destroy)
	assert.Contains(t, out, "Plan: 1 to add, 1 to change, 1 to destroy.", "summary line should have correct counts")
}

func TestNoChanges(t *testing.T) {
	var buf bytes.Buffer
	output.NoChanges(&buf)

	assert.Equal(t, "No changes. Infrastructure is up-to-date.\n", buf.String())
}

// showSQL appends the SQL of every change after the summary line; without it nothing is added.
func TestPrintPlan_showSQL(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}
	plan := []change.Change{addCol}

	var plain, withSQL bytes.Buffer
	output.PrintPlan(&plain, plan, false)
	output.PrintPlan(&withSQL, plan, true)

	var sqlOnly bytes.Buffer
	output.PrintShowSQL(&sqlOnly, plan)
	assert.NotContains(t, plain.String(), "ALTER TABLE")
	assert.Equal(t, plain.String()+sqlOnly.String(), withSQL.String())
	assert.Contains(t, withSQL.String(), "ALTER TABLE")
}

func TestPrintVerbose_containsSQL(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}

	var buf bytes.Buffer
	output.PrintShowSQL(&buf, []change.Change{addCol})
	out := buf.String()

	assert.Contains(t, out, "ALTER TABLE", "SQL should appear in verbose output")
	// SQL should be indented with 4 spaces
	lines := strings.Split(out, "\n")
	foundIndented := false
	for _, line := range lines {
		if strings.HasPrefix(line, "    ALTER TABLE") {
			foundIndented = true
			break
		}
	}
	assert.True(t, foundIndented, "SQL should be indented with 4 spaces")
}

func TestPrintVerbose_blankSeparator(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}
	dropTbl := &change.DropTableChange{Base: change.Base{Table: testPath}}

	var buf bytes.Buffer
	output.PrintShowSQL(&buf, []change.Change{addCol, dropTbl})
	out := buf.String()

	// There should be a blank line between the two changes
	assert.Contains(t, out, "\n\n", "blank separator line between changes")
}

func TestPrintJSON_validJSON(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}

	var buf bytes.Buffer
	err := output.PrintJSON(&buf, []change.Change{addCol})
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON")
}

func TestPrintJSON_schema(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}

	var buf bytes.Buffer
	err := output.PrintJSON(&buf, []change.Change{addCol})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	summary, ok := result["summary"].(map[string]interface{})
	require.True(t, ok, "summary field must be present and be an object")
	assert.Contains(t, summary, "add")
	assert.Contains(t, summary, "change")
	assert.Contains(t, summary, "destroy")
	assert.Contains(t, result, "changes")
	changes, ok := result["changes"].([]interface{})
	require.True(t, ok)
	assert.NotNil(t, changes)
}

func TestPrintJSON_opNames(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}}

	var buf bytes.Buffer
	err := output.PrintJSON(&buf, []change.Change{addCol})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	changes := result["changes"].([]interface{})
	require.Len(t, changes, 1)
	first := changes[0].(map[string]interface{})
	assert.Equal(t, "add_column", first["op"], "op name for AddColumn must be 'add_column'")
}

func TestPrintJSON_destructive(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: "STRING"}}
	dropTbl := &change.DropTableChange{Base: change.Base{Table: testPath}}

	var buf bytes.Buffer
	err := output.PrintJSON(&buf, []change.Change{addCol, dropTbl})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	changes := result["changes"].([]interface{})
	require.Len(t, changes, 2)

	addChange := changes[0].(map[string]interface{})
	assert.Equal(t, false, addChange["destructive"], "AddColumn should not be destructive")

	dropChange := changes[1].(map[string]interface{})
	assert.Equal(t, true, dropChange["destructive"], "DropTable should be destructive")
}

func TestPrintJSON_summaryCount(t *testing.T) {
	createTbl := &change.CreateTableChange{Base: change.Base{Table: testPath}, Def: config.Table{Name: "tbl"}}
	dropTbl := &change.DropTableChange{Base: change.Base{Table: testPath}}

	var buf bytes.Buffer
	err := output.PrintJSON(&buf, []change.Change{createTbl, dropTbl})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	summary := result["summary"].(map[string]interface{})
	assert.Equal(t, float64(1), summary["add"], "summary.add should be 1 for one CreateTable")
	assert.Equal(t, float64(1), summary["destroy"], "summary.destroy should be 1 for one DropTable")
	assert.Equal(t, float64(0), summary["change"])
}

func TestPrintJSON_noRequiresWarehouseKey(t *testing.T) {
	addCol := &change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: "STRING"}}
	for name, plan := range map[string][]change.Change{"empty": nil, "non-empty": {addCol}} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, output.PrintJSON(&buf, plan))
			var result map[string]interface{}
			require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
			assert.NotContains(t, result, "requires_warehouse")
			assert.Contains(t, result, "summary")
			assert.Contains(t, result, "changes")
		})
	}
}

// The contract v1 of the plan document: the top-level keys and, per change, the always-present and
// the optional keys. Adding a key is allowed within v1 (extend the lists); removing or renaming one is not.
func TestPrintJSON_ContractV1_Keys(t *testing.T) {
	alterNullable := &change.AlterColumnChange{Base: change.Base{Table: testPath}, ColumnName: "c", Kind: change.AlterNullable}
	renameCol := &change.RenameColumnChange{Base: change.Base{Table: testPath}, OldName: "a", NewName: "b"}
	dropTbl := &change.DropTableChange{Base: change.Base{Table: testPath}}
	var buf bytes.Buffer
	require.NoError(t, output.PrintJSON(&buf, []change.Change{alterNullable, renameCol, dropTbl}))

	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	assert.Len(t, doc, 3)
	assert.JSONEq(t, "1", string(doc["schema_version"]))
	var changes []map[string]any
	require.NoError(t, json.Unmarshal(doc["changes"], &changes))
	require.Len(t, changes, 3)

	always := []string{"op", "step", "catalog", "schema", "table", "path", "description", "sql", "destructive"}
	keys := func(m map[string]any) []string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		return ks
	}
	assert.ElementsMatch(t, append([]string{"column", "alter"}, always...), keys(changes[0]), "alter_column")
	assert.Equal(t, "nullable", changes[0]["alter"])
	assert.ElementsMatch(t, append([]string{"column", "previous_name"}, always...), keys(changes[1]), "rename_column")
	assert.Equal(t, "b", changes[1]["column"])
	assert.Equal(t, "a", changes[1]["previous_name"])
	assert.ElementsMatch(t, always, keys(changes[2]), "drop_table: no optional keys")
	assert.Equal(t, "cat.sch.tbl", changes[2]["path"])
	assert.Equal(t, float64(3), changes[2]["step"])
}

func TestPrintJSON_EmptyPlan_ExactDocument(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, output.PrintJSON(&buf, nil))
	assert.Equal(t, `{"schema_version":1,"summary":{"add":0,"change":0,"destroy":0},"changes":[]}`+"\n", buf.String())
}

// Names with SQL-special characters stay raw in catalog, schema, table and path (quoting belongs to sql).
func TestPrintJSON_PathIsNotQuoted(t *testing.T) {
	p := change.TablePath{Catalog: "c`x", Schema: "s", Name: "t"}
	var buf bytes.Buffer
	require.NoError(t, output.PrintJSON(&buf, []change.Change{&change.DropTableChange{Base: change.Base{Table: p}}}))
	var doc struct {
		Changes []struct{ Catalog, Path, SQL string }
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	assert.Equal(t, "c`x", doc.Changes[0].Catalog)
	assert.Equal(t, "c`x.s.t", doc.Changes[0].Path)
	assert.Contains(t, doc.Changes[0].SQL, "`c``x`")
}
