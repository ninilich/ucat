package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeCmdSplit is like executeCmd but keeps stdout and stderr in separate buffers.
// It resets package-level flag variables the same way executeCmd does.
func executeCmdSplit(args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	err = runRootIn(&outBuf, &errBuf, args)
	return outBuf.String(), errBuf.String(), err
}

// codeOf is the exit code Execute() would produce for err (the same function, output discarded).
func codeOf(err error) int { return exitCode(err, io.Discard) }

// decodeSingleJSON requires s to be exactly one JSON document on one line.
func decodeSingleJSON(t *testing.T, s string, v any) {
	t.Helper()
	require.Equal(t, 1, strings.Count(s, "\n"), "stdout must be a single line: %q", s)
	require.True(t, strings.HasSuffix(s, "\n"), "stdout must end with newline: %q", s)
	dec := json.NewDecoder(strings.NewReader(s))
	require.NoError(t, dec.Decode(v), "stdout must be valid JSON: %q", s)
	require.False(t, dec.More(), "stdout has trailing data after JSON: %q", s)
}

// useBackend installs b as the backend factory for the duration of the test.
func useBackend(t *testing.T, b backend.UCBackend) {
	t.Helper()
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) { return b, nil })
}

// useBackendFactory is the single point where tests replace the backend factory. The previous
// factory is restored with t.Cleanup (not defer), so it also works inside subtests and loops.
func useBackendFactory(t *testing.T, f func(host, wh string) (backend.UCBackend, error)) {
	t.Helper()
	orig := testBackendFactory
	testBackendFactory = f
	t.Cleanup(func() { testBackendFactory = orig })
}

// failClosedBackend is the default test factory: a test that did not install a backend gets an
// error instead of a real network connection.
func failClosedBackend(_, _ string) (backend.UCBackend, error) {
	return nil, errors.New("no test backend installed")
}

// testBackendFactory is the factory runRootIn gives the app; see useBackendFactory.
var testBackendFactory func(host, wh string) (backend.UCBackend, error) = failClosedBackend

// testGOOS is the operating system the test app reports; see useGOOS. Defaults to the real one.
var testGOOS = runtime.GOOS

// useGOOS makes the test app behave as on the given operating system until the end of the test.
func useGOOS(t *testing.T, goos string) {
	t.Helper()
	orig := testGOOS
	testGOOS = goos
	t.Cleanup(func() { testGOOS = orig })
}

// newTestRoot builds a fresh command tree on an app with the test backend factory.
func newTestRoot() *cobra.Command {
	a := newApp()
	a.newBackend = testBackendFactory
	a.goos = testGOOS
	a.now = (&stepClock{}).Now
	return newRootCmd(a)
}

// testStep is how far every reading of the test clock moves it.
const testStep = 1500 * time.Millisecond

// stepClock is the clock of the test app: every Now moves it forward by testStep, so the two readings
// around one apply step are exactly testStep apart ("applied in 1.5s") and no test depends on real time.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the time after moving the clock forward by testStep.
func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(testStep)
	return c.t
}

// testStdin is the single point where tests set the root command input (test code only).
// nil means "leave the root command input untouched". runRootIn applies it before Execute.
var testStdin io.Reader

// runRootIn is the common internal runner: it builds a fresh command tree (flag defaults on every call), applies
// testStdin, runs the root command with the given writers and mirrors Execute()'s error
// printing to errW. executeCmd and executeCmdSplit are thin wrappers over it.
func runRootIn(outW, errW io.Writer, args []string) error {
	err := execRoot(outW, errW, args)
	// SilenceErrors=true suppresses cobra's auto-print; exitCode prints the error exactly as
	// Execute() does (exitCodeError is not printed, it only sets the exit code).
	exitCode(err, errW)
	return err
}

// execRoot builds a fresh tree, applies testStdin and executes the root command without printing the error.
func execRoot(outW, errW io.Writer, args []string) error {
	root := newTestRoot()
	if testStdin != nil {
		root.SetIn(testStdin)
	}
	root.SetOut(outW)
	root.SetErr(errW)
	root.SetArgs(args)
	return root.Execute()
}

// useStdin sets content as the root command input (apply reads cmd.InOrStdin()).
func useStdin(t *testing.T, content string) {
	t.Helper()
	useStdinReader(t, strings.NewReader(content))
}

// useStdinReader is useStdin for an arbitrary reader (for example iotest.ErrReader).
// The reader is shared by every command run until the end of the test.
func useStdinReader(t *testing.T, r io.Reader) {
	t.Helper()
	testStdin = r
	t.Cleanup(func() { testStdin = nil })
}

// spyBackend is the single configurable test double over a MockUCBackend: it counts calls, records the SQL of every
// applied change and injects failures. Zero value of every knob = pass-through to the mock.
type spyBackend struct {
	*backendtest.MockUCBackend
	forbidden                                       *testing.T         // non-nil: ListTables, ApplyChange and CheckWarehouse fail this test
	listErr                                         error              // every ListTables call fails with this error
	failList                                        *backend.SchemaRef // ListTables fails (assert.AnError) for this schema only
	failApplyOn                                     int                // the n-th (1-based) ApplyChange call fails
	mu                                              sync.Mutex
	factoryCalls, listCalls, applyCalls, checkCalls int
	propsCalls                                      int // GetTableProperties calls (preflight)
	sqls                                            []string
	listed                                          []backend.SchemaRef             // every schema ListTables was called for, in call order
	onList                                          func(ctx context.Context) error // runs in every ListTables call; a non-nil error is its result
	onApply                                         func(ctx context.Context)       // runs at the start of every ApplyChange call, with its context
	afterApply                                      func(n int)                     // runs after the n-th (1-based) ApplyChange call
}

// newSpy wraps m (nil = a fresh MockUCBackend).
func newSpy(m *backendtest.MockUCBackend) *spyBackend {
	if m == nil {
		m = &backendtest.MockUCBackend{}
	}
	return &spyBackend{MockUCBackend: m}
}

// listFailSpy is a spy whose every ListTables call fails.
func listFailSpy() *spyBackend {
	b := newSpy(nil)
	b.listErr = errors.New("list tables boom")
	return b
}

// forbid makes any backend call (ListTables, ApplyChange, CheckWarehouse) fail t.
func (b *spyBackend) forbid(t *testing.T) *spyBackend {
	b.forbidden = t
	return b
}

// useSpy installs s as the backend; every factory call is counted in s.factoryCalls.
func useSpy(t *testing.T, s *spyBackend) *spyBackend {
	t.Helper()
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		s.mu.Lock()
		s.factoryCalls++
		s.mu.Unlock()
		return s, nil
	})
	return s
}

func (b *spyBackend) ListTables(ctx context.Context, ref backend.SchemaRef) ([]*backend.LiveTable, error) {
	b.mu.Lock()
	b.listCalls++
	b.listed = append(b.listed, ref)
	b.mu.Unlock()
	if b.forbidden != nil {
		b.forbidden.Error("ListTables must not be called")
	}
	if b.onList != nil {
		if err := b.onList(ctx); err != nil {
			return nil, err
		}
	}
	if b.listErr != nil {
		return nil, b.listErr
	}
	if b.failList != nil && ref == *b.failList {
		return nil, assert.AnError
	}
	return b.MockUCBackend.ListTables(ctx, ref)
}

func (b *spyBackend) ApplyChange(ctx context.Context, ch change.Change) error {
	b.mu.Lock()
	b.applyCalls++
	n := b.applyCalls
	b.sqls = append(b.sqls, ch.SQL())
	b.mu.Unlock()
	if b.forbidden != nil {
		b.forbidden.Error("ApplyChange must not be called")
	}
	if b.onApply != nil {
		b.onApply(ctx)
	}
	if n == b.failApplyOn {
		return errors.New("statement FAILED: boom")
	}
	err := b.MockUCBackend.ApplyChange(ctx, ch)
	if b.afterApply != nil {
		b.afterApply(n)
	}
	return err
}

func (b *spyBackend) GetTableProperties(ctx context.Context, cat, schema, table string) (map[string]string, error) {
	b.mu.Lock()
	b.propsCalls++
	b.mu.Unlock()
	return b.MockUCBackend.GetTableProperties(ctx, cat, schema, table)
}

func (b *spyBackend) CheckWarehouse(ctx context.Context) error {
	b.mu.Lock()
	b.checkCalls++
	b.mu.Unlock()
	if b.forbidden != nil {
		b.forbidden.Error("CheckWarehouse must not be called")
	}
	return b.MockUCBackend.CheckWarehouse(ctx)
}

// assertUntouched requires that the factory and no backend method was called.
func (b *spyBackend) assertUntouched(t *testing.T) {
	t.Helper()
	assert.Zero(t, b.factoryCalls, "newBackend must not be called")
	assert.Zero(t, b.listCalls, "ListTables must not be called")
	assert.Zero(t, b.applyCalls, "ApplyChange must not be called")
	assert.Zero(t, b.checkCalls, "CheckWarehouse must not be called")
}

// executeCmd is a shared test helper (accessible from all package cmd test files)
// that runs the root cobra command with the given args and returns combined
// stdout+stderr output and the execution error.
//
// Every call builds a fresh command tree (see runRootIn), so no flag state carries over between calls.
func executeCmd(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	err := runRootIn(buf, buf, args)
	return buf.String(), err
}

func liveOrders(cols ...string) *backend.LiveTable {
	t := backendtest.Live("dev", "silver", "orders")
	for _, c := range cols {
		t.Columns = append(t.Columns, backendtest.Col(c, "BIGINT"))
	}
	return t
}

func upToDateMock() *backendtest.MockUCBackend {
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id")})
	return m
}

// destructivePlanMock: legacy table with drop:true exists live, orders has an extra column.
func destructivePlanMock(t *testing.T, tmpDir string) *backendtest.MockUCBackend {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tables", "silver", "legacy.yml"),
		[]byte("name: legacy\nschema: silver\ndrop: true\n"), 0644))
	m := &backendtest.MockUCBackend{}
	m.TableProperties = map[string]map[string]string{litDevSilverOrders: {propColumnMapping: litName}}
	legacy := backendtest.Live("dev", "silver", "legacy", backendtest.Col("id", "BIGINT"))
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id", "extra_col"), legacy})
	return m
}

// executorResult is the result of an apply of total steps that stopped after applied of them.
func executorResult(applied, total int) executor.Result {
	return executor.Result{Total: total, Applied: applied, Skipped: total - applied, Aborted: executor.AbortInterrupted}
}

const cmdProbe = "probe"

// probeCmd is a command "probe" that hands its context to f.
func probeCmd(f func(context.Context)) *cobra.Command {
	return &cobra.Command{Use: cmdProbe, RunE: func(c *cobra.Command, _ []string) error {
		f(c.Context())
		return nil
	}}
}
