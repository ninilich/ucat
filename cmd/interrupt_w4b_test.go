package cmd

// interrupt_w4b_test.go: Ctrl+C in validate -t and drop.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
)

// validate -t: the failures of canceled requests are not printed as findings, the run is interrupted.
func TestInterrupt_ValidateLive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := threeAddsSpy(t)
	b.onList = func(ctx context.Context) error {
		cancel()
		return fmt.Errorf("failed in rate limiter: %w", ctx.Err())
	}

	stdout, stderr, code := runWithContext(ctx, cmdValidate, "-t", litDev)

	assert.Equal(t, 130, code)
	assert.NotContains(t, stdout, "\u2717")
	assert.NotContains(t, stdout+stderr, "rate limiter")
	assert.NotContains(t, stdout+stderr, "context canceled")
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted during live checks\n"), "stderr=%q", stderr)
}

// Without a cancel the same failure is a finding: "\u2717" and code 1 (the check above is not a blanket silence).
func TestInterrupt_ValidateLive_OtherFailureIsReported(t *testing.T) {
	b := threeAddsSpy(t)
	b.onList = func(context.Context) error { return fmt.Errorf("failed in rate limiter") }

	stdout, _, code := runWithContext(context.Background(), cmdValidate, "-t", litDev)

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "\u2717")
}

func dropArgsOf() []string {
	return []string{cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove}
}

// drop: a signal before the statement is sent drops nothing.
func TestInterrupt_Drop_BeforeStatement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	setupTestProject(t)
	b := useSpy(t, newSpy(nil))

	stdout, stderr, code := runWithContext(ctx, dropArgsOf()...)

	assert.Equal(t, 130, code)
	assert.Zero(t, b.applyCalls)
	assert.NotContains(t, stdout, "dropped")
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted before the drop; nothing was dropped\n"), "stderr=%q", stderr)
}

// drop: a signal during connect (client construction is not cancellable) drops nothing either.
func TestInterrupt_Drop_DuringConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupTestProject(t)
	m := &backendtest.MockUCBackend{}
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		cancel() // Ctrl+C arrives while the client is being built
		return m, nil
	})

	stdout, stderr, code := runWithContext(ctx, dropArgsOf()...)

	assert.Equal(t, 130, code)
	assert.Empty(t, m.RecordedCalls())
	assert.NotContains(t, stdout, "dropped")
	assert.True(t, strings.HasSuffix(stderr, "Error: interrupted before the drop; nothing was dropped\n"), "stderr=%q", stderr)
}

// drop: a signal while the statement runs does not cut it (the context of the statement is not canceled)
// and the drop is reported as done.
func TestInterrupt_Drop_StatementIsAwaited(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupTestProject(t)
	b := newSpy(nil)
	seen := context.DeadlineExceeded
	b.onApply = func(stmtCtx context.Context) {
		cancel() // Ctrl+C arrives while the DROP runs
		seen = stmtCtx.Err()
	}
	useSpy(t, b)

	stdout, stderr, code := runWithContext(ctx, dropArgsOf()...)

	assert.Equal(t, 0, code, stderr)
	assert.NoError(t, seen, "the statement must not see the cancel")
	assert.Equal(t, 1, b.applyCalls)
	assert.Contains(t, stdout, "dropped")
}

// The diagnostics of a checker whose request was canceled are an effect of the cancel: none of them is printed.
func TestInterrupt_PreflightStage_NoDiagnosticsOfCanceledChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupPlanProject(t)
	m := &backendtest.MockUCBackend{}
	live := liveOrders("id", "extra_col")
	live.Catalog = "" // not in the live state of preflight: its properties are requested from the backend
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{live})
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		return &cancelPropsBackend{MockUCBackend: m, cancel: cancel}, nil
	})

	stdout, stderr, code := runWithContext(ctx, cmdPlan, "-t", litDev)

	assert.Equal(t, 130, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: interrupted during preflight checks\n", stderr)
}

var _ backend.UCBackend = (*spyBackend)(nil)
