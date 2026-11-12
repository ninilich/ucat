package live_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/live"
)

// gateBackend measures how many ListTables calls are in flight at once. Every call waits at a gate that
// opens settle after MaxParallel calls are in flight, so a limit that is too low shows as a
// stuck test (the gate also opens by timeout) and a missing limit as more calls in flight than allowed:
// the extra calls have settle to pile up.
type gateBackend struct {
	*backendtest.MockUCBackend
	inFlight, maxInFlight, calls atomic.Int32
	gate                         chan struct{}
	once                         sync.Once
	settle                       time.Duration
	delay                        func(ref backend.SchemaRef) time.Duration
}

func newGateBackend() *gateBackend {
	return &gateBackend{MockUCBackend: &backendtest.MockUCBackend{}, gate: make(chan struct{}), settle: 50 * time.Millisecond}
}

func (g *gateBackend) ListTables(ctx context.Context, ref backend.SchemaRef) ([]*backend.LiveTable, error) {
	g.calls.Add(1)
	n := g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	for {
		m := g.maxInFlight.Load()
		if n <= m || g.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	if n >= live.MaxParallel {
		g.once.Do(func() {
			go func() {
				time.Sleep(g.settle) // let more calls pile up if nothing holds them back
				close(g.gate)
			}()
		})
	}
	select {
	case <-g.gate:
	case <-time.After(10 * time.Second): // fewer than MaxParallel calls in total
	}
	if g.delay != nil {
		time.Sleep(g.delay(ref))
	}
	return g.MockUCBackend.ListTables(ctx, ref)
}

// schemaRefs makes n schemas s00, s01, ... in one catalog.
func schemaRefs(n int) []backend.SchemaRef {
	refs := make([]backend.SchemaRef, n)
	for i := range refs {
		refs[i] = backend.SchemaRef{UCCatalog: "c", UCSchema: fmt.Sprintf("s%02d", i)}
	}
	return refs
}

func TestMaxParallelValue(t *testing.T) {
	assert.Equal(t, 8, live.MaxParallel, "the documented limit (docs/contributing/architecture.md)")
}

func TestFetchTablesParallelLimit(t *testing.T) {
	refs := schemaRefs(3 * live.MaxParallel)
	g := newGateBackend()

	_, err := live.FetchTables(context.Background(), g, refs, true)
	require.NoError(t, err)

	assert.EqualValues(t, len(refs), g.calls.Load(), "every schema is listed")
	assert.EqualValues(t, live.MaxParallel, g.maxInFlight.Load(), "parallel up to the limit and not above it")
}

func TestFetchTablesSequentialIsOneAtATime(t *testing.T) {
	g := newGateBackend()
	g.settle = 0
	close(g.gate)
	_, err := live.FetchTables(context.Background(), g, schemaRefs(5), false)
	require.NoError(t, err)
	assert.EqualValues(t, 1, g.maxInFlight.Load())
}

// The limit must not change the result: the order is the order of refs, whatever finishes first.
func TestFetchTablesParallelKeepsOrderAboveTheLimit(t *testing.T) {
	refs := schemaRefs(2*live.MaxParallel + 3)
	g := newGateBackend()
	g.settle = 0
	// the first schemas are the slowest, so they finish last
	g.delay = func(ref backend.SchemaRef) time.Duration {
		var i int
		_, _ = fmt.Sscanf(ref.UCSchema, "s%d", &i)
		return time.Duration(len(refs)-i) * time.Millisecond
	}
	for i, ref := range refs {
		g.SetFixture(ref, []*backend.LiveTable{backendtest.Live(ref.UCCatalog, ref.UCSchema, fmt.Sprintf("t%02d", i))})
	}

	got, err := live.FetchTables(context.Background(), g, refs, true)
	require.NoError(t, err)
	require.Len(t, got, len(refs))
	for i := range refs {
		require.Len(t, got[i], 1)
		assert.Equal(t, fmt.Sprintf("t%02d", i), got[i][0].Name, "result[%d] belongs to refs[%d]", i, i)
	}
}

// A failure among more schemas than the limit is still reported, once, with the schema name.
func TestFetchTablesParallelFailureAboveTheLimit(t *testing.T) {
	refs := schemaRefs(2 * live.MaxParallel)
	bad := refs[len(refs)-1] // starts after the first batch: the error must still come out
	fb := &failingBackend{MockUCBackend: &backendtest.MockUCBackend{}, failFor: map[backend.SchemaRef]bool{bad: true}}

	_, err := live.FetchTables(context.Background(), fb, refs, true)
	require.Error(t, err)
	assert.Equal(t, "list tables in c."+bad.UCSchema+": boom "+bad.UCSchema, err.Error())
}

func TestCheckLiveParallelLimitAndOrder(t *testing.T) {
	n := 3 * live.MaxParallel
	refs := schemaRefs(n)
	schemas := make([]string, n)
	for i, ref := range refs {
		schemas[i] = ref.UCSchema
	}
	g := newGateBackend()
	g.delay = func(ref backend.SchemaRef) time.Duration { // the first schemas finish last
		var i int
		_, _ = fmt.Sscanf(ref.UCSchema, "s%d", &i)
		return time.Duration(n-i) * time.Millisecond
	}

	res := live.CheckLive(context.Background(), g, resolvedWith("wh1", schemas...))

	assert.EqualValues(t, n, g.calls.Load())
	assert.EqualValues(t, live.MaxParallel, g.maxInFlight.Load(), "parallel up to the limit and not above it")
	require.Len(t, res, n+1)
	for i, ref := range refs {
		assert.Equal(t, fmt.Sprintf("schema c.%s exists", ref.UCSchema), res[i].Line, "result %d", i)
	}
	assert.Equal(t, "warehouse wh1 reachable", res[n].Line, "the warehouse result comes last")
}

// One failed schema among many does not stop the others, and its result stays at its own place.
func TestCheckLiveParallelFailureKeepsPlace(t *testing.T) {
	n := 2 * live.MaxParallel
	refs := schemaRefs(n)
	schemas := make([]string, n)
	for i, ref := range refs {
		schemas[i] = ref.UCSchema
	}
	fb := &failingBackend{MockUCBackend: &backendtest.MockUCBackend{}, failFor: map[backend.SchemaRef]bool{refs[n-2]: true}}

	res := live.CheckLive(context.Background(), fb, resolvedWith("", schemas...))

	assert.EqualValues(t, n, fb.calls.Load(), "no schema is skipped after a failure")
	require.Len(t, res, n)
	for i := range res {
		assert.Equal(t, i != n-2, res[i].OK, "result %d", i)
	}
	assert.Equal(t, "schema c.s14: boom s14", res[n-2].Line)
}
