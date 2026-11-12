package executor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failOnNthBackend fails the n-th (1-based) ApplyChange call and counts every attempt.
type failOnNthBackend struct {
	*backendtest.MockUCBackend
	n     int
	count int
}

func (f *failOnNthBackend) ApplyChange(ctx context.Context, ch change.Change) error {
	f.count++
	if f.count == f.n {
		return fmt.Errorf("boom on step %d", f.n)
	}
	return f.MockUCBackend.ApplyChange(ctx, ch)
}

func TestExecute_Applied_SuccessEqualsPlanLen(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{addColChange(), renameColChange(), addColChange()}
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{Now: frozenNow}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Equal(t, executor.Result{Total: 3, Applied: 3}, res)
	assert.Len(t, b.RecordedCalls(), 3)
}

func TestExecute_Applied_FailOnSecondOfThree(t *testing.T) {
	b := &failOnNthBackend{MockUCBackend: &backendtest.MockUCBackend{}, n: 2}
	plan := []change.Change{addColChange(), addColChange(), addColChange()}
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom on step 2")
	assert.Equal(t, 3, res.Total)
	assert.Equal(t, 1, res.Applied)
	assert.Equal(t, 1, res.Skipped, "the step after the failed one is skipped")
	require.NotNil(t, res.Failed)
	assert.Equal(t, 2, res.Failed.Step, "Step is 1-based")
	assert.Same(t, plan[1], res.Failed.Change)
	assert.EqualError(t, res.Failed.Err, "boom on step 2")
	var applyErr *executor.ApplyError
	require.ErrorAs(t, err, &applyErr)
	assert.Same(t, res.Failed.Change, applyErr.Change)
	assert.Equal(t, 2, b.count, "third step must not be attempted (fail-fast)")
	assert.Len(t, b.RecordedCalls(), 1)
}

func TestExecute_Applied_DeclinedPrompt_Zero(t *testing.T) {
	for name, input := range map[string]string{"no": "no\n", "empty": "\n", "eof": ""} {
		t.Run(name, func(t *testing.T) {
			b := &backendtest.MockUCBackend{}
			plan := []change.Change{addColChange(), dropTableChange()}
			var out bytes.Buffer
			res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{Now: frozenNow}, strings.NewReader(input), &out)
			require.Error(t, err)
			assert.Equal(t, executor.Result{Total: 2, Skipped: 2, Aborted: executor.AbortDeclined}, res, "declined: nothing ran, nothing failed")
			assert.Empty(t, b.RecordedCalls())
		})
	}
}

func TestExecute_Applied_EmptyPlan_Zero(t *testing.T) {
	for name, plan := range map[string][]change.Change{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			b := &backendtest.MockUCBackend{}
			var out bytes.Buffer
			res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{Now: frozenNow}, strings.NewReader(""), &out)
			require.NoError(t, err)
			assert.Equal(t, executor.Result{}, res)
			assert.Empty(t, b.RecordedCalls())
		})
	}
}

func TestExecute_Applied_AutoApproveDestructive_AllApplied(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{dropColChange(), dropTableChange()}
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Applied)
	assert.Len(t, b.RecordedCalls(), 2)
}

func TestExecute_Applied_ConfirmedDestructive_AllApplied(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{addColChange(), dropTableChange()}
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{Now: frozenNow}, strings.NewReader("yes\n"), &out)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Applied)
}

func TestExecute_Applied_FailOnFirstAndLast(t *testing.T) {
	for name, tc := range map[string]struct {
		n    int
		want executor.Result
	}{
		"first": {1, executor.Result{Total: 3, Applied: 0, Skipped: 2}},
		"last":  {3, executor.Result{Total: 3, Applied: 2, Skipped: 0}},
	} {
		t.Run(name, func(t *testing.T) {
			b := &failOnNthBackend{MockUCBackend: &backendtest.MockUCBackend{}, n: tc.n}
			plan := []change.Change{addColChange(), addColChange(), addColChange()}
			res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &bytes.Buffer{})
			require.Error(t, err)
			require.NotNil(t, res.Failed)
			assert.Equal(t, tc.n, res.Failed.Step)
			failed := res.Failed
			res.Failed = nil
			assert.Equal(t, tc.want, res)
			assert.Equal(t, res.Total, res.Applied+res.Skipped+1, "Total = Applied + Skipped + the failed step")
			assert.NotNil(t, failed)
		})
	}
}

func TestExecute_Applied_ConfirmReadError_AllSkipped(t *testing.T) {
	plan := []change.Change{dropTableChange()}
	res, err := executor.Execute(context.Background(), plan, &backendtest.MockUCBackend{}, executor.ExecOpts{Now: frozenNow}, errReader{err: errors.New("boom")}, &bytes.Buffer{})
	require.ErrorIs(t, err, executor.ErrConfirmRead)
	assert.Equal(t, executor.Result{Total: 1, Skipped: 1, Aborted: executor.AbortInputError}, res)
}

// A answer over the read limit is a refusal, not an input error; a successful or failed run has no Aborted.
func TestExecute_Applied_Aborted_OnlyWhenConfirmationStops(t *testing.T) {
	plan := []change.Change{dropTableChange()}
	long := strings.Repeat("a", 70*1024) + "\n"
	res, err := executor.Execute(context.Background(), plan, &backendtest.MockUCBackend{}, executor.ExecOpts{Now: frozenNow}, strings.NewReader(long), &bytes.Buffer{})
	require.ErrorIs(t, err, executor.ErrCancelled)
	assert.Equal(t, executor.AbortDeclined, res.Aborted)

	res, err = executor.Execute(context.Background(), plan, &backendtest.MockUCBackend{}, executor.ExecOpts{Now: frozenNow}, strings.NewReader("yes\n"), &bytes.Buffer{})
	require.NoError(t, err)
	assert.Empty(t, res.Aborted)

	b := &failOnNthBackend{MockUCBackend: &backendtest.MockUCBackend{}, n: 1}
	res, err = executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &bytes.Buffer{})
	require.Error(t, err)
	assert.Empty(t, res.Aborted, "a failed step is not an abort")
}
