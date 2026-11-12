package cmd

// interrupt_test.go covers Ctrl+C / SIGTERM: the context of the command is canceled (what the signal
// handler of Execute does) while a command runs.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ninilich/ucat/internal/backend"
)

// runWithContext runs the root command like Execute does, with ctx as the context of the command, and
// returns what the process would print and its exit code. stderr gets the progress of --json and the error.
func runWithContext(ctx context.Context, args ...string) (stdout, stderr string, code int) {
	var outBuf, errBuf bytes.Buffer
	root := newTestRoot()
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	code = runContext(ctx, root, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

// threeAddsSpy is a spy over a project that needs three add-column steps (a, b, c) on silver.orders.
func threeAddsSpy(t *testing.T) *spyBackend {
	t.Helper()
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	b := newSpy(nil)
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	return useSpy(t, b)
}

func TestInterrupt_Apply_BetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := threeAddsSpy(t)
	b.afterApply = func(n int) {
		if n == 2 {
			cancel()
		}
	}

	stdout, stderr, code := runWithContext(ctx, cmdApply, "-t", litDev, flagAutoApprove)

	assert.Equal(t, 130, code)
	assert.Equal(t, 2, b.applyCalls, "the running step is finished, the next one does not start")
	assert.Contains(t, stdout, "[1/3] applied in 1.5s:")
	assert.Contains(t, stdout, "[2/3] applied in 1.5s:")
	assert.NotContains(t, stdout, "[3/3]")
	assert.NotContains(t, stdout, "Apply complete")
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted after step 2/3; 1 step not applied\n"), "stderr=%q", stderr)
}

func TestInterrupt_Apply_BetweenSteps_ManySkipped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := threeAddsSpy(t)
	b.afterApply = func(int) { cancel() }

	_, stderr, code := runWithContext(ctx, cmdApply, "-t", litDev, flagAutoApprove)

	assert.Equal(t, 130, code)
	assert.Equal(t, 1, b.applyCalls)
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted after step 1/3; 2 steps not applied\n"), "stderr=%q", stderr)
}

// The cancel comes during the last step: everything was applied, the run is a success.
func TestInterrupt_Apply_OnLastStep_IsSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := threeAddsSpy(t)
	b.afterApply = func(n int) {
		if n == 3 {
			cancel()
		}
	}

	stdout, stderr, code := runWithContext(ctx, cmdApply, "-t", litDev, flagAutoApprove)

	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, 3, b.applyCalls)
	assert.Contains(t, stdout, "Apply complete in ")
	assert.NotContains(t, stderr, "interrupted")
}

// The cancel comes after the plan was made and before the first step (here: while the last schema is
// listed, the read itself succeeds): no step runs, exit code 130.
func TestInterrupt_Apply_BeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := threeAddsSpy(t)
	b.onList = func(context.Context) error {
		cancel()
		return nil
	}

	stdout, stderr, code := runWithContext(ctx, cmdApply, "-t", litDev, flagAutoApprove)

	assert.Equal(t, 130, code)
	assert.Zero(t, b.applyCalls)
	assert.NotContains(t, stdout, "applied in")
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted before the first step; 3 steps not applied\n"), "stderr=%q", stderr)
}

// A context that is canceled from the start never reaches UC: the connection is made (the factory
// does not take the context) but no request is sent.
func TestInterrupt_Apply_ContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := threeAddsSpy(t)
	b.onList = func(ctx context.Context) error { return ctx.Err() } // a real backend fails the request like this

	stdout, stderr, code := runWithContext(ctx, cmdApply, "-t", litDev, flagAutoApprove)

	assert.Equal(t, 130, code)
	assert.Zero(t, b.applyCalls)
	assert.Empty(t, stdout)
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted during reading the live state\n"), "stderr=%q", stderr)
}

func TestInterrupt_Apply_JSON(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := threeAddsSpy(t)
	b.afterApply = func(n int) {
		if n == 1 {
			cancel()
		}
	}

	stdout, stderr, code := runWithContext(ctx, cmdApply, "-t", litDev, flagAutoApprove, flagJSON)

	assert.Equal(t, 130, code)
	var got map[string]any
	decodeSingleJSON(t, stdout, &got)
	assert.Equal(t, map[string]any{
		"schema_version": float64(1), "total": float64(3), "applied": float64(1),
		"failed": nil, "skipped": float64(2), "aborted": "interrupted",
	}, got)
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted after step 1/3; 2 steps not applied\n"), "stderr=%q", stderr)
}

// The JSON result of an interrupt before the first step: the same shape, everything skipped.
func TestInterrupt_Apply_JSON_BeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := threeAddsSpy(t)
	b.onList = func(context.Context) error {
		cancel()
		return nil
	}

	stdout, _, code := runWithContext(ctx, cmdApply, "-t", litDev, flagAutoApprove, flagJSON)

	assert.Equal(t, 130, code)
	var res applyJSON
	decodeSingleJSON(t, stdout, &res)
	assert.Equal(t, 3, res.Total)
	assert.Zero(t, res.Applied)
	assert.Nil(t, res.Failed)
	assert.Equal(t, 3, res.Skipped)
	assert.Equal(t, "interrupted", res.Aborted)
}

// Reading the live state is cut by the cancel: no panic, the same error and the same code for plan.
func TestInterrupt_ReadStage_PlanAndApply(t *testing.T) {
	for _, cmdName := range []string{cmdPlan, cmdApply} {
		t.Run(cmdName, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := threeAddsSpy(t)
			b.onList = func(ctx context.Context) error {
				cancel()
				return fmt.Errorf("get schema: %w", ctx.Err()) // what the SDK does with a canceled request
			}

			stdout, stderr, code := runWithContext(ctx, cmdName, "-t", litDev)

			assert.Equal(t, 130, code)
			assert.Empty(t, stdout)
			assert.Zero(t, b.applyCalls)
			assert.Equal(t, "Error: interrupted during reading the live state\n", stderr[strings.LastIndex(stderr, "Error:"):])
		})
	}
}

// A read error that is not a cancel stays an error with code 1 (the cancel handling must not hide it).
func TestInterrupt_ReadStage_OtherErrorIsNotInterrupt(t *testing.T) {
	b := threeAddsSpy(t)
	b.onList = func(context.Context) error { return errors.New("boom") }

	_, stderr, code := runWithContext(context.Background(), cmdPlan, "-t", litDev)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "boom")
	assert.NotContains(t, stderr, "interrupted")
}

// An interrupt before the confirmation: the prompt is not shown, nothing is applied, code 130 (an interrupt
// while the prompt waits for the answer is covered in internal/executor).
func TestInterrupt_Apply_BeforePrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tmpDir := setupPlanProject(t)
	b := useSpy(t, newSpy(destructivePlanMock(t, tmpDir)))
	cancel() // the cancel is already there when the plan is ready: the prompt is not shown

	stdout, stderr, code := runWithContext(ctx, cmdApply, "-t", litDev)

	assert.Equal(t, 130, code)
	assert.Zero(t, b.applyCalls)
	assert.NotContains(t, stdout+stderr, "Type 'yes'")
}

func TestExitCode_Interrupt(t *testing.T) {
	var out bytes.Buffer
	assert.Equal(t, 130, exitCode(&interruptedError{msg: "interrupted after step 1/2; 1 step not applied"}, &out))
	assert.Equal(t, "Error: interrupted after step 1/2; 1 step not applied\n", out.String())

	// any wrapped cancel, from a command without its own message (pull, drop, validate)
	out.Reset()
	assert.Equal(t, 130, exitCode(fmt.Errorf("list tables in a.b: %w", context.Canceled), &out))
	assert.Equal(t, "Error: interrupted\n", out.String())

	// 130 is its own code: not the error code 1, not the "changes present" code 2
	assert.NotEqual(t, 1, exitInterrupted)
	assert.NotEqual(t, 2, exitInterrupted)

	out.Reset()
	assert.Equal(t, 1, exitCode(errors.New("boom"), &out))
	assert.Equal(t, 2, exitCode(&exitCodeError{code: 2}, &out))
}

func TestInterruptedText(t *testing.T) {
	for _, tc := range []struct {
		applied, total int
		want           string
	}{
		{0, 1, "interrupted before the first step; 1 step not applied"},
		{0, 12, "interrupted before the first step; 12 steps not applied"},
		{3, 12, "interrupted after step 3/12; 9 steps not applied"},
		{11, 12, "interrupted after step 11/12; 1 step not applied"},
	} {
		res := executorResult(tc.applied, tc.total)
		assert.Equal(t, tc.want, interruptedText(res))
	}
}

// runContext hands the context to the command: a command sees the cancel of the context it was run with.
func TestRunContext_PassesContextToCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var seen error
	root := newTestRoot()
	root.AddCommand(probeCmd(func(c context.Context) { seen = c.Err() }))
	root.SetArgs([]string{cmdProbe})
	code := runContext(ctx, root, &bytes.Buffer{})

	assert.Equal(t, 0, code)
	assert.ErrorIs(t, seen, context.Canceled)
}

// Without a signal handler (run) the context is never canceled.
func TestRun_ContextNotCanceled(t *testing.T) {
	seen := errors.New("not run")
	root := newTestRoot()
	root.AddCommand(probeCmd(func(c context.Context) { seen = c.Err() }))
	root.SetArgs([]string{cmdProbe})

	assert.Equal(t, 0, run(root, &bytes.Buffer{}))
	assert.NoError(t, seen)
}
