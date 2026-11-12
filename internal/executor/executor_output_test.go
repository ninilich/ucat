package executor_test

// executor_output_test.go covers: apply output has "[i/N] applied: <table> - <Describe()>".

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addColOn(p change.TablePath, col string) change.Change {
	return &change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: col, Type: typSTRING}}
}

func appliedLine(i, n int, ch change.Change) string {
	return fmt.Sprintf("  [%d/%d] applied in 0ms: %s - %s", i, n, ch.TableName(), ch.Describe())
}

func appliedLines(out string) []string {
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "applied in ") {
			res = append(res, l)
		}
	}
	return res
}

func TestExecute_Output_InterleavedTables_PlanOrder(t *testing.T) {
	a := change.TablePath{Catalog: litProd, Schema: litSilver, Name: "a"}
	bt := change.TablePath{Catalog: "prod", Schema: "gold", Name: "b"}
	plan := []change.Change{addColOn(a, "c1"), addColOn(bt, "c2"), addColOn(a, "c3")}

	b := &backendtest.MockUCBackend{}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.NoError(t, err)

	lines := appliedLines(out.String())
	require.Len(t, lines, 3)
	for i, ch := range plan {
		assert.Equal(t, appliedLine(i+1, 3, ch), lines[i])
	}
	assert.Contains(t, lines[0], "`prod`.`silver`.`a`")
	assert.Contains(t, lines[1], "`prod`.`gold`.`b`")
	assert.Contains(t, lines[2], "`prod`.`silver`.`a`")

	calls := b.RecordedCalls()
	require.Len(t, calls, 3)
	for i, ch := range plan {
		assert.Equal(t, ch.Describe(), calls[i].Describe, "execution order must equal plan order")
	}
}

func TestExecute_Output_SingleChange(t *testing.T) {
	plan := []change.Change{addColChange()}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, &backendtest.MockUCBackend{}, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Contains(t, out.String(), appliedLine(1, 1, plan[0])+"\n")
}

func TestExecute_Output_TwoDigitTotals(t *testing.T) {
	p := tablePathFixture()
	var plan []change.Change
	for i := 0; i < 12; i++ {
		plan = append(plan, addColOn(p, fmt.Sprintf("c%d", i)))
	}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, &backendtest.MockUCBackend{}, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.NoError(t, err)

	lines := appliedLines(out.String())
	require.Len(t, lines, 12)
	for i, ch := range plan {
		assert.Equal(t, appliedLine(i+1, 12, ch), lines[i])
	}
	assert.Contains(t, lines[9], "[10/12]")
	assert.Contains(t, lines[11], "[12/12]")
}

func TestExecute_Output_FailureOnSecondOfThree(t *testing.T) {
	p := tablePathFixture()
	plan := []change.Change{addColOn(p, "c1"), addColOn(p, "c2"), addColOn(p, "c3")}
	b := &failOnNthBackend{MockUCBackend: &backendtest.MockUCBackend{}, n: 2}

	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.Error(t, err)

	lines := appliedLines(out.String())
	require.Len(t, lines, 1, "only the first change is reported as applied; got: %q", out.String())
	assert.Equal(t, appliedLine(1, 3, plan[0]), lines[0])
	// the failed change is named on out, the reason is only in the returned error
	assert.Contains(t, out.String(), "  [2/3] failed in 0ms: "+plan[1].TableName()+" - "+plan[1].Describe()+"\n")
	assert.NotContains(t, out.String(), "boom")
	assert.NotContains(t, out.String(), "[3/3]")
	assert.Less(t, strings.Index(out.String(), "[1/3]"), strings.Index(out.String(), "[2/3] failed"))
	var applyErr *executor.ApplyError
	require.ErrorAs(t, err, &applyErr)
	assert.Same(t, plan[1], applyErr.Change)
	assert.EqualError(t, applyErr.Err, "boom on step 2")
	assert.Equal(t, fmt.Sprintf("apply %q: boom on step 2", plan[1].Describe()), err.Error(), "the --json error text")
	assert.Len(t, b.RecordedCalls(), 1)
}

// The ApplyError keeps the cause of the backend: errors.Is finds it through Unwrap.
func TestExecute_ApplyError_UnwrapsToBackendError(t *testing.T) {
	cause := errors.New("warehouse unavailable")
	b := &backendtest.MockUCBackend{}
	b.SetApplyError(cause)
	_, err := executor.Execute(context.Background(), []change.Change{addColChange()}, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &bytes.Buffer{})
	require.ErrorIs(t, err, cause)
	var applyErr *executor.ApplyError
	require.ErrorAs(t, err, &applyErr)
	assert.Equal(t, cause, errors.Unwrap(applyErr))
}

func TestExecute_Output_FailureOnFirst_NoAppliedLines(t *testing.T) {
	plan := []change.Change{addColChange(), addColChange()}
	b := &failOnNthBackend{MockUCBackend: &backendtest.MockUCBackend{}, n: 1}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.Error(t, err)
	assert.Empty(t, appliedLines(out.String()))
	assert.Contains(t, out.String(), "[1/2] failed in 0ms: ")
}

func TestExecute_Output_UnusualTableNames_Verbatim(t *testing.T) {
	names := []string{"my.table", "we`ird", "таблица", "tbl-1 x"}
	var plan []change.Change
	for _, n := range names {
		plan = append(plan, addColOn(change.TablePath{Catalog: "c.at", Schema: "схема", Name: n}, "x"))
	}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, &backendtest.MockUCBackend{}, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.NoError(t, err)

	lines := appliedLines(out.String())
	require.Len(t, lines, len(plan))
	for i, ch := range plan {
		assert.Equal(t, appliedLine(i+1, len(plan), ch), lines[i])
	}
}

func TestExecute_Output_EmptyPlan(t *testing.T) {
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), nil, &backendtest.MockUCBackend{}, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Empty(t, out.String())
}
