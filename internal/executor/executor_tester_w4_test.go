package executor_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/executor"
)

// The cancel comes after the pre-check and the prompt but before the first step (here: inside the first
// read of the clock, which Execute takes after the prompt). The loop must still look at it before step 1:
// nothing runs, all steps are skipped. Kills the mutant that checks the cancel only from step 2 on.
func TestExecute_Interrupt_AfterPromptBeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	now := func() time.Time {
		calls++
		if calls == 1 {
			cancel()
		}
		return time.Unix(0, 0)
	}
	b := &hookBackend{}
	var out bytes.Buffer
	res, err := executor.Execute(ctx, addColPlan(3), b, executor.ExecOpts{AutoApprove: true, Now: now}, strings.NewReader(""), &out)

	require.ErrorIs(t, err, executor.ErrInterrupted)
	assert.Equal(t, executor.Result{Total: 3, Skipped: 3, Aborted: executor.AbortInterrupted}, res)
	assert.Empty(t, b.RecordedCalls(), "no step may start after the cancel")
	assert.NotContains(t, out.String(), "[1/3]")
}
