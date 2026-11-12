package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApply_ExecutionErrorPrintedOnce(t *testing.T) {
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	b := newSpy(nil)
	b.failApplyOn = 2
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useSpy(t, b)

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, 1, strings.Count(stdout+stderr, "boom"),
		"execution error must be printed once; stdout=%q stderr=%q", stdout, stderr)
}

// A failed change in text mode: progress (applied and failed lines) on stdout, the error with
// the position, the change and the reason on stderr (what Execute prints), exit code 1.
func TestApply_ExecutionError_OnStderrWithChangeAndReason(t *testing.T) {
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	b := newSpy(nil)
	b.failApplyOn = 2
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useSpy(t, b)

	stdout, _, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.Error(t, err)
	var stderr bytes.Buffer
	assert.Equal(t, 1, exitCode(err, &stderr))

	assert.Contains(t, stdout, "[1/3] applied in 1.5s:")
	assert.Contains(t, stdout, "[2/3] failed in 1.5s: ")
	assert.NotContains(t, stdout, "boom", "the reason is not on stdout")
	assert.NotContains(t, stdout, "apply failed")
	assert.NotContains(t, stdout, "[3/3]")
	assert.Regexp(t, `^Error: apply failed after 1/3 changes: \S+ - add column "b" BIGINT: .*boom.*\n$`, stderr.String())
}

func TestApplyJSON_ExecutionErrorPrintedOnceOnStderr(t *testing.T) {
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	b := newSpy(nil)
	b.failApplyOn = 2
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useSpy(t, b)

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--json", "--auto-approve")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Equal(t, 1, strings.Count(stderr, "boom"), "stderr=%q", stderr)
	assert.Equal(t, 1, strings.Count(stdout, "boom"), "the reason is in failed.error of the result JSON")

	var r applyJSON
	decodeSingleJSON(t, stdout, &r)
	require.NotNil(t, r.Failed)
	assert.Contains(t, r.Failed.Error, "boom")
	assert.Equal(t, 1, r.Applied)
}

// End-to-end contract of a declined prompt: exit code 1, nothing applied, the message once per
// stream (text and --json). All answer variants are covered by the executor unit tests.
func TestApplyPromptDecline_NothingAppliedMessageOnce(t *testing.T) {
	inputs := map[string]string{"no": "no", "invisible char": "yes\u200b"}
	for _, jsonMode := range []bool{false, true} {
		for name, in := range inputs {
			t.Run(fmt.Sprintf("json=%v/%s", jsonMode, name), func(t *testing.T) {
				tmpDir := setupPlanProject(t)
				m := destructivePlanMock(t, tmpDir)
				useBackend(t, m)
				useStdin(t, in+"\n")

				args := []string{cmdApply, "-t", litDev}
				if jsonMode {
					args = append(args, "--json")
				}
				stdout, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				assert.Equal(t, 1, codeOf(err))
				assert.Empty(t, m.RecordedCalls())
				want := fmt.Sprintf("apply cancelled by user (got %q)", in)
				if jsonMode {
					// The reason is on stderr once; the result JSON has no failed step for a declined prompt.
					assert.NotContains(t, stdout, "apply cancelled by user", "stdout=%q", stdout)
					assert.Equal(t, 1, strings.Count(stderr, "apply cancelled by user"), "stderr=%q", stderr)
				} else {
					assert.Equal(t, 1, strings.Count(stdout+stderr, "apply cancelled by user"),
						"stdout=%q stderr=%q", stdout, stderr)
				}
				assert.Contains(t, stdout+stderr, want)
			})
		}
	}
}
