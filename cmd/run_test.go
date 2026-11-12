package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cmdBoom is the extra command of the run tests.
const cmdBoom = "boom"

// runWith runs run() on a test tree extended with one extra command returning err.
func runWith(t *testing.T, retErr error, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := newTestRoot()
	root.AddCommand(&cobra.Command{
		Use:  cmdBoom,
		RunE: func(*cobra.Command, []string) error { return retErr },
	})
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	code = run(root, &errBuf)
	return code, outBuf.String(), errBuf.String()
}

func TestRun_ExitCodeError_ReturnsItsCodeAndPrintsNothing(t *testing.T) {
	for _, c := range []int{1, 2, 3, 42} {
		t.Run(fmt.Sprint(c), func(t *testing.T) {
			code, stdout, stderr := runWith(t, &exitCodeError{code: c}, cmdBoom)
			assert.Equal(t, c, code)
			assert.Empty(t, stderr)
			assert.Empty(t, stdout)
		})
	}
}

func TestRun_WrappedExitCodeError_KeepsItsCode(t *testing.T) {
	code, _, stderr := runWith(t, fmt.Errorf("wrapped: %w", &exitCodeError{code: 2}), cmdBoom)
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
}

func TestRun_PlainError_Code1AndPrintedExactlyOnce(t *testing.T) {
	code, stdout, stderr := runWith(t, errors.New("something broke"), cmdBoom)
	assert.Equal(t, 1, code)
	assert.Equal(t, "Error: something broke\n", stderr)
	assert.Equal(t, 1, strings.Count(stderr+stdout, "something broke"))
}

func TestRun_UnknownCommand_Code1(t *testing.T) {
	code, _, stderr := runWith(t, nil, "no-such-command")
	assert.Equal(t, 1, code)
	assert.Equal(t, 1, strings.Count(stderr, "Error: "), "stderr=%q", stderr)
	assert.Contains(t, stderr, "no-such-command")
}

func TestRun_HelpAndVersion_Code0(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {cmdPlan, "--help"}, {litVersion}, {"help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runWith(t, nil, args...)
			assert.Equal(t, 0, code)
			assert.Empty(t, stderr)
			assert.NotEmpty(t, stdout)
		})
	}
}

func TestRun_Error_Code1AndPrinted(t *testing.T) {
	setupTestProject(t)

	code, _, stderr := runWith(t, nil, "plan") // --target is required
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Error: ")
}

func TestRun_DetailedExitCode_Code2NothingPrinted(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders",
		"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")
	useBackend(t, &backendtest.MockUCBackend{})

	code, _, stderr := runWith(t, nil, "plan", "-t", "dev", "--detailed-exitcode")
	require.Equal(t, 2, code)
	assert.NotContains(t, stderr, "Error")
}

// A panic must not end the process with the Go runtime's code 2 (plan --detailed-exitcode: "changes present").
func TestRun_Panic_Code1AndMessageOnStderr(t *testing.T) {
	for name, val := range map[string]any{"string": "kaboom", "error": errors.New("nil map write")} {
		t.Run(name, func(t *testing.T) {
			var outBuf, errBuf bytes.Buffer
			root := newTestRoot()
			root.AddCommand(&cobra.Command{Use: cmdBoom, Run: func(*cobra.Command, []string) { panic(val) }})
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs([]string{cmdBoom})

			code := run(root, &errBuf)

			assert.Equal(t, 1, code)
			assert.Equal(t, fmt.Sprintf("Error: internal error: %v\n", val), errBuf.String())
			assert.Empty(t, outBuf.String())
		})
	}
}
