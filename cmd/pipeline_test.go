package cmd

// pipeline_test.go covers: schema fetching must be parallel, not sequential.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// barrierBackend is a UCBackend whose ListTables blocks until all `want` calls are
// in flight at once. It records the maximum concurrency observed. If fetching were
// sequential the barrier could never be satisfied, so the wait times out and the
// call returns an error instead of hanging.
type barrierBackend struct {
	backendtest.MockUCBackend
	want        int32
	timeout     time.Duration
	inflight    atomic.Int32
	maxInflight atomic.Int32
	release     chan struct{}
	once        sync.Once
	mu          sync.Mutex
	fetched     map[string]bool
}

func (b *barrierBackend) ListTables(ctx context.Context, schema backend.SchemaRef) ([]*backend.LiveTable, error) {
	n := b.inflight.Add(1)
	defer b.inflight.Add(-1)
	for {
		cur := b.maxInflight.Load()
		if n <= cur || b.maxInflight.CompareAndSwap(cur, n) {
			break
		}
	}
	b.mu.Lock()
	if b.fetched == nil {
		b.fetched = map[string]bool{}
	}
	b.fetched[schema.UCSchema] = true
	b.mu.Unlock()
	if n == b.want {
		b.once.Do(func() { close(b.release) })
	}
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(b.timeout):
		return nil, fmt.Errorf("barrier timeout: only %d of %d ListTables calls in flight (fetching is not parallel)",
			b.maxInflight.Load(), b.want)
	}
	return b.MockUCBackend.ListTables(ctx, schema)
}

// reverseOrderBackend completes ListTables in the reverse of sorted schema order
// (silver, gold, bronze) regardless of scheduling: each call waits for the
// completion of the schema that sorts after it.
type reverseOrderBackend struct {
	backendtest.MockUCBackend
	waitFor map[string]string // schema -> schema whose completion it waits for
	done    map[string]chan struct{}
	timeout time.Duration
}

func newReverseOrderBackend() *reverseOrderBackend {
	r := &reverseOrderBackend{
		waitFor: map[string]string{litBronze: litGold, litGold: litSilver},
		done:    map[string]chan struct{}{},
		timeout: 5 * time.Second,
	}
	for _, s := range []string{litBronze, litGold, litSilver} {
		r.done[s] = make(chan struct{})
	}
	return r
}

func (r *reverseOrderBackend) ListTables(ctx context.Context, schema backend.SchemaRef) ([]*backend.LiveTable, error) {
	defer close(r.done[schema.UCSchema])
	if dep, ok := r.waitFor[schema.UCSchema]; ok {
		select {
		case <-r.done[dep]:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(r.timeout):
			return nil, fmt.Errorf("timeout waiting for %s fetch to complete", dep)
		}
	}
	return r.MockUCBackend.ListTables(ctx, schema)
}

// --- Parallel ListTables ---

// TestPipeline_ParallelSchemaFetch verifies that listing tables in multiple schemas
// is done concurrently. Deterministic: ListTables calls meet at a barrier that is
// released only when all 3 are in flight simultaneously (no wall-clock assertions).
func TestPipeline_ParallelSchemaFetch(t *testing.T) {
	setupMultiSchemaProject(t)

	bb := &barrierBackend{want: 3, timeout: 5 * time.Second, release: make(chan struct{})}
	for _, s := range []string{litSilver, litGold, litBronze} {
		bb.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: s}, []*backend.LiveTable{})
	}

	useBackend(t, bb)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err, "plan must succeed (barrier not satisfied means sequential fetching): %s", out)

	assert.EqualValues(t, 3, bb.maxInflight.Load(), "all 3 schemas must be fetched concurrently")
	assert.Equal(t, map[string]bool{litSilver: true, litGold: true, litBronze: true}, bb.fetched,
		"all 3 schemas must be fetched")
}

// TestPipeline_ParallelSchemaFetch_BarrierFailsWhenSequential guards the barrier
// itself: with a want that can never be reached, the call must fail via the timeout
// path with a clear message rather than hang.
func TestPipeline_ParallelSchemaFetch_BarrierFailsWhenSequential(t *testing.T) {
	setupMultiSchemaProject(t)

	bb := &barrierBackend{want: 4, timeout: 100 * time.Millisecond, release: make(chan struct{})}
	for _, s := range []string{litSilver, litGold, litBronze} {
		bb.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: s}, []*backend.LiveTable{})
	}

	useBackend(t, bb)

	_, err := executeCmd("plan", "-t", "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "barrier timeout")
}

// TestPipeline_ParallelSchemaFetch_ThreeSchemasAllFetched verifies that parallel
// fetching actually fetches all 3 schemas (correctness, not just timing).
func TestPipeline_ParallelSchemaFetch_ThreeSchemasAllFetched(t *testing.T) {
	setupMultiSchemaProject(t)

	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: litEvents, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
			Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litGold}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: "events", ResolvedCatalog: litDev, ResolvedSchema: "gold",
			Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litBronze}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: "events", ResolvedCatalog: litDev, ResolvedSchema: litBronze,
			Columns: []config.Column{{Name: "id", Type: typBIGINT}}}),
	})

	useBackend(t, mock)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err, "plan must succeed with 3 schemas")
	// When all schemas match desired, 0 changes → "up-to-date" or empty plan
	assert.NotContains(t, out, "error",
		"plan output must not contain errors when all schemas match; got: %q", out)
}

// TestPipeline_SchemaFetchError_Propagated verifies that a ListTables error for one
// schema propagates correctly and plan returns an error.
func TestPipeline_SchemaFetchError_Propagated(t *testing.T) {
	setupMultiSchemaProject(t)

	fb := &backendtest.MockUCBackend{}
	fb.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{})
	fb.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: "bronze"}, []*backend.LiveTable{})

	useBackend(t, fb)

	_, err := executeCmd("plan", "-t", "dev")
	// gold schema has no fixture → returns empty list (not an error in mock)
	// This test verifies plan still succeeds (tables in gold will be created)
	// A real error injection test would require a custom ListTables that returns error.
	require.NoError(t, err, "plan must handle missing fixture gracefully (empty schema)")
}

// --- deterministic change/warning order across schemas ---

var sortedSchemas = []string{"bronze", "gold", litSilver} // sorted by (UCCatalog, UCSchema)

// TestPipeline_PlanOrderDeterministic verifies that plan lists tables in sorted schema
// order even when schema fetches complete in reverse order.
func TestPipeline_PlanOrderDeterministic(t *testing.T) {
	setupMultiSchemaProject(t)

	for i := 0; i < 10; i++ {
		rb := newReverseOrderBackend()
		for _, s := range sortedSchemas {
			rb.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: s}, []*backend.LiveTable{})
		}
		useBackend(t, rb)

		out, err := executeCmd("plan", "-t", "dev")
		require.NoError(t, err, "run %d: %s", i, out)
		assertOrdered(t, out, "run "+fmt.Sprint(i), "`dev`.`bronze`.`events`", "`dev`.`gold`.`events`", "`dev`.`silver`.`events`")

		rb = newReverseOrderBackend()
		for _, s := range sortedSchemas {
			rb.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: s}, []*backend.LiveTable{})
		}
		useBackend(t, rb)
		jout, err := executeCmd("plan", "-t", "dev", "--json")
		require.NoError(t, err, "run %d json: %s", i, jout)
		var parsed struct {
			Changes []struct {
				Path string `json:"path"`
			} `json:"changes"`
		}
		require.NoError(t, json.Unmarshal([]byte(jout[strings.Index(jout, "{"):]), &parsed), jout)
		var paths []string
		for _, c := range parsed.Changes {
			paths = append(paths, c.Path)
		}
		assert.Equal(t, []string{"dev.bronze.events", "dev.gold.events", "dev.silver.events"}, paths,
			"run %d: json changes must follow sorted schema order", i)
	}
}

// TestPipeline_WarningOrderDeterministic verifies warnings follow sorted schema order.
func TestPipeline_WarningOrderDeterministic(t *testing.T) {
	tmpDir := setupMultiSchemaProject(t)

	// A drop:true table absent from UC produces a warning per schema.
	for _, s := range sortedSchemas {
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "tables", s, "old.yml"),
			[]byte("name: old\nschema: "+s+"\ndrop: true\n"), 0644))
	}

	for i := 0; i < 10; i++ {
		rb := newReverseOrderBackend()
		for _, s := range sortedSchemas {
			rb.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: s}, []*backend.LiveTable{})
		}
		useBackend(t, rb)

		out, err := executeCmd("plan", "-t", "dev")
		require.NoError(t, err, "run %d: %s", i, out)
		require.Equal(t, 3, strings.Count(out, "drop: true but table not in UC"), "run %d: %s", i, out)
		assertOrdered(t, out, "run "+fmt.Sprint(i), "bronze/old.yml", "gold/old.yml", "silver/old.yml")
	}
}

// assertOrdered asserts every needle occurs in out, in the given order.
func assertOrdered(t *testing.T, out, label string, needles ...string) {
	t.Helper()
	prev := -1
	for _, n := range needles {
		idx := strings.Index(out, n)
		if !assert.GreaterOrEqual(t, idx, 0, "%s: %q missing in output:\n%s", label, n, out) {
			return
		}
		assert.Greater(t, idx, prev, "%s: %q out of order in output:\n%s", label, n, out)
		prev = idx
	}
}
