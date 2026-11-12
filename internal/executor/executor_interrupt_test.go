package executor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hookBackend is a MockUCBackend whose ApplyChange runs a hook before the mock records the call: the
// hook gets the 1-based number of the step and the context the executor passed.
type hookBackend struct {
	backendtest.MockUCBackend
	hook func(step int, ctx context.Context) error
	n    int
}

func (h *hookBackend) ApplyChange(ctx context.Context, ch change.Change) error {
	h.n++
	if h.hook != nil {
		if err := h.hook(h.n, ctx); err != nil {
			return err
		}
	}
	return h.MockUCBackend.ApplyChange(ctx, ch)
}

var _ backend.UCBackend = (*hookBackend)(nil)

func addColPlan(n int) []change.Change {
	plan := make([]change.Change, n)
	for i := range plan {
		plan[i] = &change.AddColumnChange{Base: change.Base{Table: tablePathFixture()}, Column: config.Column{Name: fmt.Sprintf("c%d", i), Type: typSTRING}}
	}
	return plan
}

// A cancel during step 2 of 4: the step is finished on a context that is not canceled, step 3 and 4 never start.
func TestExecute_Interrupt_BetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var seenErrInStep []error
	b := &hookBackend{hook: func(step int, stepCtx context.Context) error {
		if step == 2 {
			cancel() // Ctrl+C arrives while the DDL runs
			seenErrInStep = append(seenErrInStep, stepCtx.Err())
		}
		return nil
	}}
	var out bytes.Buffer
	res, err := executor.Execute(ctx, addColPlan(4), b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)

	require.ErrorIs(t, err, executor.ErrInterrupted)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, executor.Result{Total: 4, Applied: 2, Skipped: 2, Aborted: executor.AbortInterrupted}, res)
	assert.Equal(t, []error{nil}, seenErrInStep, "the running step must not see the cancel")
	assert.Len(t, b.RecordedCalls(), 2)
	assert.Len(t, appliedLines(out.String()), 2)
	assert.NotContains(t, out.String(), "[3/4]")
}

// The cancel comes while the last step runs: nothing is left to skip, so the apply is complete.
func TestExecute_Interrupt_OnLastStepIsComplete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &hookBackend{hook: func(step int, _ context.Context) error {
		if step == 3 {
			cancel()
		}
		return nil
	}}
	res, err := executor.Execute(ctx, addColPlan(3), b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), io.Discard)

	require.NoError(t, err)
	assert.Equal(t, executor.Result{Total: 3, Applied: 3}, res)
	assert.Len(t, b.RecordedCalls(), 3)
}

// The cancel comes while the first step runs: that step is applied, the rest is skipped.
func TestExecute_Interrupt_OnFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &hookBackend{hook: func(step int, _ context.Context) error {
		if step == 1 {
			cancel()
		}
		return nil
	}}
	res, err := executor.Execute(ctx, addColPlan(2), b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), io.Discard)

	require.ErrorIs(t, err, executor.ErrInterrupted)
	assert.Equal(t, executor.Result{Total: 2, Applied: 1, Skipped: 1, Aborted: executor.AbortInterrupted}, res)
}

// A context canceled before the first step: no step runs, every step is skipped.
func TestExecute_Interrupt_BeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &hookBackend{}
	var out bytes.Buffer
	res, err := executor.Execute(ctx, addColPlan(3), b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), &out)

	require.ErrorIs(t, err, executor.ErrInterrupted)
	assert.Equal(t, executor.Result{Total: 3, Skipped: 3, Aborted: executor.AbortInterrupted}, res)
	assert.Empty(t, b.RecordedCalls())
	assert.Empty(t, out.String())
}

// Before the first step with a destructive plan: the prompt is not even shown.
func TestExecute_Interrupt_BeforePromptShowsNoPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	res, err := executor.Execute(ctx, []change.Change{dropTableChange()}, &hookBackend{}, executor.ExecOpts{Now: frozenNow}, strings.NewReader("yes\n"), &out)

	require.ErrorIs(t, err, executor.ErrInterrupted)
	assert.Equal(t, executor.Result{Total: 1, Skipped: 1, Aborted: executor.AbortInterrupted}, res)
	assert.Empty(t, out.String())
}

// A cancel while the prompt waits for the answer ends the wait: nothing runs, the answer is not needed.
func TestExecute_Interrupt_DuringPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	reading := make(chan struct{})
	var once sync.Once
	in := readerFunc(func(p []byte) (int, error) {
		once.Do(func() { close(reading) })
		return pr.Read(p)
	})
	b := &hookBackend{}

	type outcome struct {
		res executor.Result
		err error
	}
	done := make(chan outcome, 1)
	var out bytes.Buffer // written by Execute only; read after it returned
	go func() {
		res, err := executor.Execute(ctx, []change.Change{dropTableChange()}, b, executor.ExecOpts{Now: frozenNow}, in, &out)
		done <- outcome{res, err}
	}()
	<-reading // the prompt is waiting for input
	cancel()

	select {
	case o := <-done:
		require.ErrorIs(t, o.err, executor.ErrInterrupted)
		assert.Equal(t, executor.Result{Total: 1, Skipped: 1, Aborted: executor.AbortInterrupted}, o.res)
		assert.Empty(t, b.RecordedCalls())
		assert.True(t, strings.HasSuffix(out.String(), "Type 'yes' to confirm: \n"), "the prompt line is ended before the error: %q", out.String())
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after the cancel")
	}
}

// A step that fails while the cancel arrives is a failure: the reason is not hidden behind "interrupted".
func TestExecute_Interrupt_FailedStepWins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boom := errors.New("boom")
	b := &hookBackend{hook: func(step int, _ context.Context) error {
		if step == 2 {
			cancel()
			return boom
		}
		return nil
	}}
	res, err := executor.Execute(ctx, addColPlan(4), b, executor.ExecOpts{AutoApprove: true, Now: frozenNow}, strings.NewReader(""), io.Discard)

	var applyErr *executor.ApplyError
	require.ErrorAs(t, err, &applyErr)
	assert.NotErrorIs(t, err, executor.ErrInterrupted)
	assert.Equal(t, 2, res.Failed.Step)
	assert.Equal(t, 1, res.Applied)
	assert.Equal(t, 2, res.Skipped)
	assert.Empty(t, res.Aborted)
}

// A confirmed destructive plan is interrupted between steps like any other.
func TestExecute_Interrupt_AfterConfirmation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &hookBackend{hook: func(_ int, _ context.Context) error {
		cancel()
		return nil
	}}
	plan := append([]change.Change{dropTableChange()}, addColPlan(2)...)
	res, err := executor.Execute(ctx, plan, b, executor.ExecOpts{Now: frozenNow}, strings.NewReader("yes\n"), io.Discard)

	require.ErrorIs(t, err, executor.ErrInterrupted)
	assert.Equal(t, executor.Result{Total: 3, Applied: 1, Skipped: 2, Aborted: executor.AbortInterrupted}, res)
}

type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
