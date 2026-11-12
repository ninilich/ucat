package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/ninilich/ucat/internal/output"
)

func testApplyPlan() []change.Change {
	return []change.Change{
		&change.AddColumnChange{Base: change.Base{Table: testPath}, Column: config.Column{Name: "x", Type: typSTRING}},
		&change.DropColumnChange{Base: change.Base{Table: testPath}, ColumnName: "y"},
		&change.DropTableChange{Base: change.Base{Table: testPath}},
	}
}

func printApply(t *testing.T, res executor.Result) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, output.PrintApplyJSON(&buf, res))
	return buf.String()
}

func TestPrintApplyJSON_Success(t *testing.T) {
	assert.Equal(t, `{"schema_version":1,"total":3,"applied":3,"failed":null,"skipped":0}`+"\n",
		printApply(t, executor.Result{Total: 3, Applied: 3}))
}

func TestPrintApplyJSON_FailureOnSecond(t *testing.T) {
	plan := testApplyPlan()
	res := executor.Result{Total: 3, Applied: 1, Skipped: 1, Failed: &executor.Failure{Step: 2, Change: plan[1], Err: errors.New("boom")}}
	out := printApply(t, res)
	assert.Equal(t, `{"schema_version":1,"total":3,"applied":1,`+
		`"failed":{"op":"drop_column","step":2,"catalog":"cat","schema":"sch","table":"tbl","column":"y","path":"cat.sch.tbl",`+
		`"description":"drop column \"y\"","sql":"ALTER TABLE `+"`cat`.`sch`.`tbl`"+` DROP COLUMN `+"`y`"+`","destructive":true,"error":"boom"},`+
		`"skipped":1}`+"\n", out)
}

// A declined prompt: nothing ran, nothing failed, every step is skipped, and aborted says why.
func TestPrintApplyJSON_Aborted(t *testing.T) {
	for _, why := range []string{executor.AbortDeclined, executor.AbortInputError} {
		assert.Equal(t, `{"schema_version":1,"total":3,"applied":0,"failed":null,"skipped":3,"aborted":"`+why+`"}`+"\n",
			printApply(t, executor.Result{Total: 3, Skipped: 3, Aborted: why}))
	}
	assert.Equal(t, "declined", executor.AbortDeclined)
	assert.Equal(t, "input_error", executor.AbortInputError)
}

// An interrupt (Ctrl+C) in the middle of the plan: some steps applied, the rest skipped, nothing failed,
// aborted says "interrupted". The Elapsed time of the result is not part of the JSON.
func TestPrintApplyJSON_Interrupted(t *testing.T) {
	assert.Equal(t, `{"schema_version":1,"total":3,"applied":1,"failed":null,"skipped":2,"aborted":"interrupted"}`+"\n",
		printApply(t, executor.Result{Total: 3, Applied: 1, Skipped: 2, Aborted: executor.AbortInterrupted, Elapsed: 5e9}))
	assert.Equal(t, "interrupted", executor.AbortInterrupted)
}

func TestPrintApplyJSON_EmptyPlan(t *testing.T) {
	assert.Equal(t, `{"schema_version":1,"total":0,"applied":0,"failed":null,"skipped":0}`+"\n", printApply(t, executor.Result{}))
}

// The failed change has the fields of a change in plan --json (same op, step, names, path, sql).
func TestPrintApplyJSON_FailedMatchesPlanJSON(t *testing.T) {
	plan := testApplyPlan()
	var planBuf bytes.Buffer
	require.NoError(t, output.PrintJSON(&planBuf, plan))
	var p struct {
		Changes []map[string]any `json:"changes"`
	}
	require.NoError(t, json.Unmarshal(planBuf.Bytes(), &p))

	for i, ch := range plan {
		res := executor.Result{Total: len(plan), Applied: i, Failed: &executor.Failure{Step: i + 1, Change: ch, Err: errors.New("e")}}
		var a struct {
			Failed map[string]any `json:"failed"`
		}
		require.NoError(t, json.Unmarshal([]byte(printApply(t, res)), &a))
		assert.Equal(t, "e", a.Failed["error"])
		delete(a.Failed, "error")
		assert.Equal(t, p.Changes[i], a.Failed)
	}
}
