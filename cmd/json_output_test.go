package cmd

// json_output_test.go covers plan/apply --json and plan exit codes.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var jsonSilverRef = backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}

type planJSON struct {
	Summary struct {
		Add     int `json:"add"`
		Change  int `json:"change"`
		Destroy int `json:"destroy"`
	} `json:"summary"`
	Changes []struct {
		Op          string `json:"op"`
		Destructive bool   `json:"destructive"`
	} `json:"changes"`
}

type applyJSON struct {
	SchemaVersion int `json:"schema_version"`
	Total         int `json:"total"`
	Applied       int `json:"applied"`
	Failed        *struct {
		Op          string `json:"op"`
		Step        int    `json:"step"`
		Catalog     string `json:"catalog"`
		Schema      string `json:"schema"`
		Table       string `json:"table"`
		Column      string `json:"column"`
		Path        string `json:"path"`
		Description string `json:"description"`
		Error       string `json:"error"`
	} `json:"failed"`
	Skipped int    `json:"skipped"`
	Aborted string `json:"aborted"`
}

// --- plan --json on an empty plan ---

func TestPlanJSON_EmptyPlan_ValidJSON(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, upToDateMock())

	stdout, _, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)

	var p planJSON
	decodeSingleJSON(t, stdout, &p)
	assert.Contains(t, stdout, `"changes":[]`, "changes must be an empty array, not null")
	assert.Equal(t, 0, p.Summary.Add+p.Summary.Change+p.Summary.Destroy)
	assert.NotContains(t, stdout, "No changes")
}

// --- plan exit codes through the real execution path (text and --json) ---

func TestPlan_ExitCodes(t *testing.T) {
	changes := func() backend.UCBackend {
		m := &backendtest.MockUCBackend{}
		m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
		return m
	}
	upToDate := func() backend.UCBackend { return upToDateMock() }
	listErr := func() backend.UCBackend { return listFailSpy() }
	cases := []struct {
		name string
		b    func() backend.UCBackend
		args []string
		want int
	}{
		{"text, no changes", upToDate, []string{cmdPlan, "-t", litDev}, 0},
		{"text, no changes, detailed", upToDate, []string{cmdPlan, "-t", litDev, flagDetailedExitcode}, 0},
		{"text, changes, no flag", changes, []string{cmdPlan, "-t", litDev}, 0},
		{"text, changes, detailed", changes, []string{cmdPlan, "-t", litDev, flagDetailedExitcode}, 2},
		{"no changes", upToDate, []string{cmdPlan, "-t", litDev, flagJSON}, 0},
		{"no changes, detailed", upToDate, []string{cmdPlan, "-t", litDev, flagJSON, flagDetailedExitcode}, 0},
		{"changes, no flag", changes, []string{cmdPlan, "-t", litDev, flagJSON}, 0},
		{"changes, detailed", changes, []string{cmdPlan, "-t", litDev, flagJSON, flagDetailedExitcode}, 2},
		{"backend error", listErr, []string{cmdPlan, "-t", litDev, flagJSON}, 1},
		{"backend error, detailed", listErr, []string{cmdPlan, "-t", litDev, flagJSON, flagDetailedExitcode}, 1},
		{"unknown target, detailed", changes, []string{cmdPlan, "-t", litNope, flagJSON, "--detailed-exitcode"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupPlanProject(t)
			useBackend(t, tc.b())

			stdout, _, err := executeCmdSplit(tc.args...)
			assert.Equal(t, tc.want, codeOf(err))
			if tc.want == 1 {
				assert.Empty(t, stdout, "errors before output must leave stdout empty")
			}
		})
	}
}

// --- CI gate: count destructive changes in plan --json ---

func TestPlanJSON_CIGate_DestructiveCount(t *testing.T) {
	tmpDir := setupPlanProject(t)
	useBackend(t, destructivePlanMock(t, tmpDir))

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)

	var p planJSON
	decodeSingleJSON(t, stdout, &p)
	destructive := 0
	ops := map[string]bool{}
	for _, c := range p.Changes {
		ops[c.Op] = true
		if c.Destructive {
			destructive++
		}
	}
	assert.Equal(t, 2, destructive)
	assert.Equal(t, 2, p.Summary.Destroy)
	assert.True(t, ops["drop_table"] && ops["drop_column"], "ops: %v", ops)
	assert.NotContains(t, stderr, "{", "stderr must not contain the JSON document")

	t.Run("jq", func(t *testing.T) {
		jq, lookErr := exec.LookPath("jq")
		if lookErr != nil {
			t.Skip("jq not installed")
		}
		c := exec.Command(jq, `[.changes[] | select(.destructive)] | length`)
		c.Stdin = strings.NewReader(stdout)
		var out bytes.Buffer
		c.Stdout = &out
		require.NoError(t, c.Run())
		assert.Equal(t, "2", strings.TrimSpace(out.String()))
	})
}

// --- apply --json ---

// threeAddsProject rewrites orders.yml to 3 columns; live orders has none -> 3 changes.
func threeAddsProject(t *testing.T, tmpDir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tables", "silver", "orders.yml"),
		[]byte("name: orders\nschema: silver\ncolumns:\n  - name: a\n    type: BIGINT\n  - name: b\n    type: BIGINT\n  - name: c\n    type: BIGINT\n"), 0644))
}

func TestApplyJSON_Success_StdoutOnlyJSON(t *testing.T) {
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useBackend(t, m)

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.NoError(t, err)

	var r applyJSON
	decodeSingleJSON(t, stdout, &r)
	assert.Equal(t, applyJSON{SchemaVersion: 1, Total: 3, Applied: 3}, r)
	assert.Equal(t, `{"schema_version":1,"total":3,"applied":3,"failed":null,"skipped":0}`+"\n", stdout)
	assert.NotContains(t, stdout, "Apply complete")
	assert.NotContains(t, stdout, "[1/3]")
	assert.Contains(t, stderr, "[1/3] applied")
	assert.Contains(t, stderr, "[3/3] applied")
}

func TestApplyJSON_FailureOnSecond(t *testing.T) {
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	b := newSpy(nil)
	b.failApplyOn = 2
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useSpy(t, b)

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))

	var r applyJSON
	decodeSingleJSON(t, stdout, &r)
	assert.Equal(t, 3, r.Total)
	assert.Equal(t, 1, r.Applied)
	assert.Equal(t, 1, r.Skipped)
	require.NotNil(t, r.Failed)
	assert.Equal(t, 2, r.Failed.Step)
	assert.Equal(t, "add_column", r.Failed.Op)
	assert.Equal(t, "b", r.Failed.Column)
	assert.Equal(t, "dev.silver.orders", r.Failed.Path)
	assert.Equal(t, []string{litDev, litSilver, "orders"}, []string{r.Failed.Catalog, r.Failed.Schema, r.Failed.Table})
	assert.NotEmpty(t, r.Failed.Description)
	assert.Contains(t, r.Failed.Error, "boom")
	assert.Contains(t, stderr, "[1/3] applied")
	assert.Contains(t, stderr, "apply failed after 1/3")
	assert.NotContains(t, stdout, "[1/3]")
}

func TestApplyJSON_DeclinedPrompt(t *testing.T) {
	tmpDir := setupPlanProject(t)
	m := destructivePlanMock(t, tmpDir)
	useBackend(t, m)
	useStdin(t, "no\n")

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--json")
	require.Error(t, err)

	var r applyJSON
	decodeSingleJSON(t, stdout, &r)
	assert.Equal(t, applyJSON{SchemaVersion: 1, Total: 2, Skipped: 2, Aborted: "declined"}, r, "declined: nothing applied, no failed step, everything skipped")
	assert.Contains(t, stdout, `"skipped":2,"aborted":"declined"}`)
	assert.Contains(t, stderr, "apply cancelled", "the reason is on stderr, once")
	assert.Contains(t, stderr, "destructive", "prompt must go to stderr")
	assert.NotContains(t, stdout, "destructive changes")
	assert.Empty(t, m.RecordedCalls(), "nothing may be applied after decline")
}

func TestApplyJSON_EmptyPlan(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, upToDateMock())

	stdout, _, err := executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.NoError(t, err)
	assert.Equal(t, `{"schema_version":1,"total":0,"applied":0,"failed":null,"skipped":0}`+"\n", stdout)
}

// --- errors before executor: stdout stays empty, exit 1 ---

func TestJSON_ErrorsBeforeExecutor_StdoutEmpty(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", "nope", flagJSON}, {cmdApply, "-t", "nope", flagJSON, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			setupPlanProject(t)
			useBackend(t, upToDateMock())

			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, "nope")
		})
	}
}

// --- lifecycle ---

func TestJSON_Lifecycle_CreatePlanApplyPlan(t *testing.T) {
	setupPlanProject(t)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{})
	useBackend(t, m)

	stdout, _, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)
	var p planJSON
	decodeSingleJSON(t, stdout, &p)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, "create_table", p.Changes[0].Op)
	assert.Equal(t, 1, p.Summary.Add)

	stdout, _, err = executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.NoError(t, err)
	var r applyJSON
	decodeSingleJSON(t, stdout, &r)
	assert.Equal(t, applyJSON{SchemaVersion: 1, Total: 1, Applied: 1}, r)

	// World after apply.
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id")})
	stdout, _, err = executeCmdSplit("plan", "-t", "dev", "--json", "--detailed-exitcode")
	assert.Equal(t, 0, codeOf(err))
	p = planJSON{}
	decodeSingleJSON(t, stdout, &p)
	assert.Empty(t, p.Changes)
	assert.Contains(t, stdout, `"changes":[]`)
}

func TestJSON_Lifecycle_DropTable(t *testing.T) {
	tmpDir := setupPlanProject(t)
	m := destructivePlanMock(t, tmpDir)
	// Only the drop: keep orders in sync with YAML.
	legacy := backendtest.Live("dev", "silver", "legacy")
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id"), legacy})
	useBackend(t, m)

	stdout, _, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)
	var p planJSON
	decodeSingleJSON(t, stdout, &p)
	require.Len(t, p.Changes, 1)
	assert.True(t, p.Changes[0].Destructive)
	assert.Equal(t, "drop_table", p.Changes[0].Op)

	stdout, _, err = executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.NoError(t, err)
	var r applyJSON
	decodeSingleJSON(t, stdout, &r)
	assert.Equal(t, applyJSON{SchemaVersion: 1, Total: 1, Applied: 1}, r)

	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id")})
	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)
	p = planJSON{}
	decodeSingleJSON(t, stdout, &p)
	assert.Empty(t, p.Changes)
	assert.Contains(t, stderr, "drop: true but table not in UC")
}
