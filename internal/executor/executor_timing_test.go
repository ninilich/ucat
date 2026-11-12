package executor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timedBackend advances the fake clock by durs[i] in the i-th ApplyChange, like a step that takes that long.
const zeroMs = "0ms"

func timedBackend(c *fakeClock, durs ...time.Duration) *hookBackend {
	return &hookBackend{hook: func(step int, _ context.Context) error {
		c.advance(durs[step-1])
		return nil
	}}
}

func TestExecute_Timing_StepLinesAndElapsed(t *testing.T) {
	clock := newFakeClock()
	durs := []time.Duration{1600 * time.Millisecond, 250 * time.Millisecond, 0, 65 * time.Second}
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), addColPlan(4), timedBackend(clock, durs...),
		executor.ExecOpts{AutoApprove: true, Now: clock.Now}, strings.NewReader(""), &out)
	require.NoError(t, err)

	lines := appliedLines(out.String())
	require.Len(t, lines, 4)
	for i, want := range []string{"1.6s", "250ms", zeroMs, "1m5s"} {
		prefix := fmt.Sprintf("  [%d/4] applied in %s: ", i+1, want)
		assert.True(t, strings.HasPrefix(lines[i], prefix), "line %d: %q, want prefix %q", i+1, lines[i], prefix)
	}
	assert.Equal(t, 1600*time.Millisecond+250*time.Millisecond+65*time.Second, res.Elapsed)
}

func TestExecute_Timing_FailedStepShowsItsDuration(t *testing.T) {
	clock := newFakeClock()
	b := &hookBackend{hook: func(step int, _ context.Context) error {
		clock.advance(2 * time.Second)
		if step == 2 {
			return errors.New("boom")
		}
		return nil
	}}
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), addColPlan(3), b,
		executor.ExecOpts{AutoApprove: true, Now: clock.Now}, strings.NewReader(""), &out)
	require.Error(t, err)

	assert.Contains(t, out.String(), "  [1/3] applied in 2s: ")
	assert.Contains(t, out.String(), "  [2/3] failed in 2s: ")
	assert.NotContains(t, out.String(), "[3/3]")
	assert.Equal(t, 4*time.Second, res.Elapsed, "the failed step counts")
}

// The wait for the confirmation is not part of the elapsed time or of the first step.
func TestExecute_Timing_PromptWaitNotCounted(t *testing.T) {
	clock := newFakeClock()
	in := readerFunc(func(p []byte) (int, error) {
		clock.advance(time.Hour) // the user thinks for an hour
		return copy(p, "yes\n"), nil
	})
	b := timedBackend(clock, time.Second)
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), []change.Change{dropTableChange()}, b,
		executor.ExecOpts{Now: clock.Now}, in, &out)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "[1/1] applied in 1s: ")
	assert.Equal(t, time.Second, res.Elapsed)
}

// An interrupted apply reports the time of the steps that ran.
func TestExecute_Timing_InterruptedElapsed(t *testing.T) {
	clock := newFakeClock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &hookBackend{hook: func(step int, _ context.Context) error {
		clock.advance(3 * time.Second)
		if step == 2 {
			cancel()
		}
		return nil
	}}
	res, err := executor.Execute(ctx, addColPlan(5), b, executor.ExecOpts{AutoApprove: true, Now: clock.Now}, strings.NewReader(""), io.Discard)
	require.ErrorIs(t, err, executor.ErrInterrupted)
	assert.Equal(t, 6*time.Second, res.Elapsed)
}

// Without a clock the executor uses the real one: the line has a duration of the same shape.
func TestExecute_Timing_NilClockUsesRealTime(t *testing.T) {
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), addColPlan(1), &hookBackend{}, executor.ExecOpts{AutoApprove: true}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^  \[1/1\] applied in (\d+ms|\d+(\.\d)?s): `), out.String())
}

func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, zeroMs}, // a clock that went back
		{0, zeroMs},
		{400 * time.Microsecond, zeroMs},
		{time.Millisecond, "1ms"},
		{999 * time.Millisecond, "999ms"},
		{999600 * time.Microsecond, "1s"}, // rounds up to a second
		{time.Second, "1s"},
		{1049 * time.Millisecond, "1s"},
		{1050 * time.Millisecond, "1.1s"},
		{1600 * time.Millisecond, "1.6s"},
		{59*time.Second + 960*time.Millisecond, "1m0s"},
		{62300 * time.Millisecond, "1m2.3s"},
		{time.Hour, "1h0m0s"},
	} {
		assert.Equal(t, tc.want, executor.FormatDuration(tc.d), "%v", tc.d)
	}
}
