package cmd

// json_all_ops_test.go: plan/apply --json for a project that produces every Kind through the
// real pipeline (loader -> differ.DiffSchema -> planner.Plan -> preflight -> output).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allOpsProject builds one project/live state where plan has all 11 kinds of change.
func allOpsProject(t *testing.T) *backendtest.MockUCBackend {
	t.Helper()
	dir := setupTestProject(t)
	addTableFile(t, dir, "silver", "t_create", "name: t_create\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n    nullable: false\n")
	addTableFile(t, dir, "silver", "t_drop", "name: t_drop\nschema: silver\ndrop: true\n")
	addTableFile(t, dir, "silver", "t_new", "name: t_new\nschema: silver\nrename_from: t_old\ncolumns:\n  - name: id\n    type: BIGINT\n")
	addTableFile(t, dir, "silver", "t_cols", `name: t_cols
schema: silver
comment: hello
clustering:
  columns: [id]
tblproperties:
  team: data
  obsolete: null
columns:
  - name: id
    type: BIGINT
  - name: new_n
    type: STRING
    rename_from: old_n
  - name: c_type
    type: STRING
  - name: c_cmt
    type: INT
    comment: after
  - name: c_nul
    type: INT
    nullable: false
  - name: added
    type: INT
`)
	m := allOpsLive()
	useSpy(t, newSpy(m))
	return m
}

// allOpsLive is the live state allOpsProject plans against.
func allOpsLive() *backendtest.MockUCBackend {
	live := func(name string, cols ...backend.LiveColumn) *backend.LiveTable {
		return backendtest.Live("dev", "silver", name, cols...)
	}
	id := backendtest.Col("id", "BIGINT")
	cols := live("t_cols", id,
		backendtest.Col("old_n", "STRING"),
		backendtest.Col("gone", "STRING"),
		backendtest.Col("c_type", "varchar(10)"),
		backend.LiveColumn{Name: "c_cmt", Type: "INT", Comment: "before", Nullable: true},
		backendtest.Col("c_nul", "INT"))
	cols.Properties = map[string]string{"obsolete": "x"}
	m := &backendtest.MockUCBackend{}
	m.TableProperties = map[string]map[string]string{"dev.silver.t_cols": {propColumnMapping: litName}}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{live("t_drop", id), live("t_old", id), cols})
	return m
}

var allOps = []string{
	"add_column", "alter_clustering", "alter_column", "alter_table_comment", "create_table",
	"drop_column", "drop_table", "rename_column", "rename_table", "set_tbl_properties", "unset_tbl_properties",
}

func TestPlanJSON_EveryKindOp(t *testing.T) {
	allOpsProject(t)
	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err, "stderr: %s", stderr)

	var p struct {
		SchemaVersion int `json:"schema_version"`
		Summary       struct{ Add, Change, Destroy int }
		Changes       []struct {
			Op          string `json:"op"`
			Step        int    `json:"step"`
			Path        string `json:"path"`
			Destructive bool   `json:"destructive"`
		} `json:"changes"`
	}
	decodeSingleJSON(t, stdout, &p)
	assert.Equal(t, 1, p.SchemaVersion)
	seen := map[string]int{}
	for i, c := range p.Changes {
		seen[c.Op]++
		assert.Equal(t, i+1, c.Step, "step is the 1-based position in the plan")
		assert.NotContains(t, c.Path, "`", "path has no SQL quoting")
		assert.Equal(t, c.Op == "drop_table" || c.Op == "drop_column", c.Destructive, "destructive of %s", c.Op)
	}
	var got []string
	for op := range seen {
		got = append(got, op)
	}
	sort.Strings(got)
	assert.Equal(t, allOps, got, "plan --json must produce exactly the 11 op names")
	assert.Equal(t, 2, p.Summary.Destroy)

	// Removed v2 stubs and old field names never reach the output.
	for _, bad := range []string{"drop_create", "requires_warehouse", "↺", "DropCreate"} {
		assert.NotContains(t, stdout, bad)
	}
	assert.True(t, json.Valid([]byte(stdout)))
}

// indentJSON re-indents the one-line JSON of a command for a golden file that can be reviewed line by line.
func indentJSON(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.Indent(&buf, []byte(strings.TrimSpace(s)), "", "  "))
	return buf.String() + "\n"
}

// The --json contract v1 of plan, pinned for every op: field names, order, step numbers, catalog,
// schema, table, column, previous_name, alter and path of each change. Regenerate with -update only
// for a deliberate change of the contract (fields are only added within one version).
func TestPlanJSON_Golden_EveryKindOp(t *testing.T) {
	allOpsProject(t)
	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err, "stderr: %s", stderr)
	assertGolden(t, "plan_json_all_ops.golden", indentJSON(t, stdout))
}

func TestApplyJSON_EveryKindOp(t *testing.T) {
	m := allOpsProject(t)
	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve", "--json")
	require.NoError(t, err, "stderr: %s", stderr)
	var a applyJSON
	decodeSingleJSON(t, stdout, &a)
	assert.Nil(t, a.Failed)
	assert.Equal(t, 1, a.SchemaVersion)
	assert.Zero(t, a.Skipped)
	assert.Equal(t, a.Total, a.Applied)
	assert.Len(t, m.RecordedCalls(), a.Total)
	assert.NotContains(t, stdout, "drop_create")
}

// A failure on each step of the all-ops plan reports exactly that change in `failed` (its op, step and
// fields as in plan --json) and counts the rest as skipped.
func TestApplyJSON_Golden_FailedStepOfEveryOp(t *testing.T) {
	allOpsProject(t) // only to learn the plan; each run below starts from the same fixtures
	planOut, _, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)
	var plan struct {
		Changes []map[string]any `json:"changes"`
	}
	require.NoError(t, json.Unmarshal([]byte(planOut), &plan))

	var golden strings.Builder
	for i, planned := range plan.Changes {
		step := i + 1
		b := newSpy(allOpsLive())
		b.failApplyOn = step
		useSpy(t, b)
		stdout, _, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve", "--json")
		require.Error(t, err)
		assert.Equal(t, 1, codeOf(err))

		var raw struct {
			Total, Applied, Skipped int
			Failed                  map[string]any `json:"failed"`
		}
		decodeSingleJSON(t, stdout, &raw)
		require.NotNil(t, raw.Failed, "step %d", step)
		assert.Equal(t, len(plan.Changes), raw.Total)
		assert.Equal(t, step-1, raw.Applied)
		assert.Equal(t, len(plan.Changes)-step, raw.Skipped)
		require.Contains(t, raw.Failed, "error")
		assert.Contains(t, raw.Failed["error"], "boom")
		failed := maps.Clone(raw.Failed)
		delete(failed, "error")
		assert.Equal(t, planned, failed, "failed change = the planned change of step %d", step)
		fmt.Fprintf(&golden, "step %d fails:\n%s", step, indentJSON(t, stdout))
	}
	assertGolden(t, "apply_json_failed_each_op.golden", golden.String())
}
