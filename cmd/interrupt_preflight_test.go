package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
)

// cancelPropsBackend cancels the command context in GetTableProperties (the fallback read of preflight)
// and fails the way the SDK does with a canceled request.
type cancelPropsBackend struct {
	*backendtest.MockUCBackend
	cancel func()
}

func (b *cancelPropsBackend) GetTableProperties(ctx context.Context, _, _, _ string) (map[string]string, error) {
	b.cancel()
	return nil, fmt.Errorf("get table: %w", ctx.Err())
}

// The cancel comes in the preflight stage (the live table is not found by its three names, so preflight
// reads its properties from the backend): not a failed check but "interrupted during preflight checks",
// exit code 130, nothing printed on stdout, nothing applied. Kills the mutant that does not map the
// preflight stage to the interrupt error.
func TestInterrupt_PreflightStage_PlanAndApply(t *testing.T) {
	for _, cmdName := range []string{cmdPlan, cmdApply} {
		t.Run(cmdName, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			setupPlanProject(t)
			m := &backendtest.MockUCBackend{}
			live := liveOrders("id", "extra_col") // the YAML has no extra_col: a column drop is planned
			live.Catalog = ""                     // not found by (dev, silver, orders) in the live state of preflight
			m.SetFixture(jsonSilverRef, []*backend.LiveTable{live})
			useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
				return &cancelPropsBackend{MockUCBackend: m, cancel: cancel}, nil
			})

			args := []string{cmdName, "-t", litDev}
			if cmdName == cmdApply {
				args = append(args, flagAutoApprove)
			}
			stdout, stderr, code := runWithContext(ctx, args...)

			assert.Equal(t, 130, code, stderr)
			assert.Empty(t, stdout)
			assert.Empty(t, m.RecordedCalls())
			require.Contains(t, stderr, "Error:")
			assert.Equal(t, "Error: interrupted during preflight checks\n", stderr[strings.LastIndex(stderr, "Error:"):])
		})
	}
}
