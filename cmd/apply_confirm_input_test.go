package cmd

// End-to-end contract of the apply confirmation prompt input: long answers, binary answers,
// read errors, accepted spellings of "yes", and isolation between consecutive apply calls.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const confirmLimit = 64 * 1024

// runDestructiveApply runs `apply -t dev [--json]` on a drop-table plan with the given stdin.
func runDestructiveApply(t *testing.T, jsonMode bool, setIn func(t *testing.T)) (m *backendtest.MockUCBackend, stdout, stderr string, err error) {
	t.Helper()
	tmpDir := setupPlanProject(t)
	m = destructivePlanMock(t, tmpDir)
	useBackend(t, m)
	setIn(t)
	args := []string{cmdApply, "-t", litDev}
	if jsonMode {
		args = append(args, "--json")
	}
	stdout, stderr, err = executeCmdSplit(args...)
	return
}

func TestApplyPrompt_ReadError_NotACancelAndPrintedOnce(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%v", jsonMode), func(t *testing.T) {
			m, stdout, stderr, err := runDestructiveApply(t, jsonMode, func(t *testing.T) {
				useStdinReader(t, iotest.ErrReader(fmt.Errorf("disk on fire")))
			})
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Empty(t, m.RecordedCalls())
			const want = "failed to read confirmation: disk on fire"
			assert.NotContains(t, stdout+stderr, "cancelled")
			if jsonMode {
				var r applyJSON
				decodeSingleJSON(t, stdout, &r)
				assert.Equal(t, applyJSON{SchemaVersion: 1, Total: 2, Skipped: 2, Aborted: "input_error"}, r, "a failed prompt is no failed step")
				assert.NotContains(t, stdout, want, "the reason is on stderr only")
				assert.True(t, strings.HasSuffix(stderr, "Error: apply failed: "+want+"\n"), "stderr=%q", stderr)
				assert.Equal(t, 1, strings.Count(stderr, want))
			} else {
				assert.Equal(t, 1, strings.Count(stdout+stderr, want), "stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestApplyPrompt_LongAnswer_DeclinedWithTruncatedEcho(t *testing.T) {
	sizes := map[string]int{"64KiB": confirmLimit, "64KiB+1": confirmLimit + 1, "1MiB": 1 << 20}
	for _, jsonMode := range []bool{false, true} {
		for name, n := range sizes {
			for _, nl := range []string{"\n", ""} {
				t.Run(fmt.Sprintf("json=%v/%s/nl=%q", jsonMode, name, nl), func(t *testing.T) {
					m, stdout, stderr, err := runDestructiveApply(t, jsonMode, func(t *testing.T) {
						useStdin(t, strings.Repeat("a", n)+nl)
					})
					require.Error(t, err)
					assert.Equal(t, 1, codeOf(err))
					assert.Empty(t, m.RecordedCalls())
					want := `apply cancelled by user (got "` + strings.Repeat("a", 64) + `...")`
					assert.Contains(t, stdout+stderr, want)
					assert.Less(t, len(stdout)+len(stderr), 4096, "output must not carry the whole answer")
					if jsonMode {
						var r applyJSON
						decodeSingleJSON(t, stdout, &r)
						assert.Equal(t, applyJSON{SchemaVersion: 1, Total: 2, Skipped: 2, Aborted: "declined"}, r)
						assert.True(t, strings.HasSuffix(stderr, "Error: apply failed: "+want+"\n"), "stderr=%q", stderr)
						assert.Equal(t, 1, strings.Count(stderr, want))
					} else {
						assert.Equal(t, 1, strings.Count(stdout+stderr, "apply cancelled"))
					}
				})
			}
		}
	}
}

// "yes" followed by more spaces than the limit is not a full answer: declined, nothing applied.
func TestApplyPrompt_YesPlusOverflowSpaces_Declined(t *testing.T) {
	for _, nl := range []string{"", "\n"} {
		t.Run(fmt.Sprintf("nl=%q", nl), func(t *testing.T) {
			m, stdout, stderr, err := runDestructiveApply(t, false, func(t *testing.T) {
				useStdin(t, "yes"+strings.Repeat(" ", confirmLimit+10)+nl)
			})
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Empty(t, m.RecordedCalls())
			assert.Contains(t, stdout+stderr, "apply cancelled by user (got \"yes ")
			assert.Less(t, len(stdout)+len(stderr), 4096)
		})
	}
}

func TestApplyPrompt_BadUTF8AndSplitRune_DeclinedAndOutputValid(t *testing.T) {
	inputs := map[string]string{
		"invalid utf8":         "\xff\xfe\x00\x80" + strings.Repeat("\xc3", 200) + "\n",
		"rune cut by limit":    strings.Repeat("a", confirmLimit-1) + "я" + "\n", // 2-byte rune straddles 64 KiB
		"rune cut, no newline": strings.Repeat("a", confirmLimit-1) + "я" + strings.Repeat("я", 10),
		"4-byte rune cut":      strings.Repeat("a", confirmLimit-2) + "😀" + "\n",
		"invalid after limit":  strings.Repeat("a", confirmLimit) + "\xff\xff\n",
	}
	for _, jsonMode := range []bool{false, true} {
		for name, in := range inputs {
			t.Run(fmt.Sprintf("json=%v/%s", jsonMode, name), func(t *testing.T) {
				m, stdout, stderr, err := runDestructiveApply(t, jsonMode, func(t *testing.T) { useStdin(t, in) })
				require.Error(t, err)
				assert.Equal(t, 1, codeOf(err))
				assert.Empty(t, m.RecordedCalls())
				assert.Contains(t, stdout+stderr, "apply cancelled by user (got ")
				assert.Less(t, len(stdout)+len(stderr), 4096)
				if jsonMode {
					assert.True(t, json.Valid([]byte(stdout)), "stdout must be valid JSON: %q", stdout)
					var r applyJSON
					decodeSingleJSON(t, stdout, &r)
					assert.Equal(t, 2, r.Skipped)
					assert.Equal(t, "declined", r.Aborted)
					assert.Nil(t, r.Failed)
					assert.Contains(t, stderr, "apply cancelled by user")
				}
			})
		}
	}
}

func TestApplyPrompt_YesSpellings_Apply(t *testing.T) {
	for name, in := range map[string]string{
		"crlf": "yes\r\n", "no newline": "yes", "spaces": "  yes  \n", "spaces no newline": "  yes  ",
	} {
		for _, jsonMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("json=%v/%s", jsonMode, name), func(t *testing.T) {
				m, _, stderr, err := runDestructiveApply(t, jsonMode, func(t *testing.T) { useStdin(t, in) })
				require.NoError(t, err, stderr)
				assert.NotEmpty(t, m.RecordedCalls())
			})
		}
	}
}

func TestApplyPrompt_YesIsCaseSensitive(t *testing.T) {
	m, _, _, err := runDestructiveApply(t, false, func(t *testing.T) { useStdin(t, "YES\n") })
	require.Error(t, err)
	assert.Contains(t, err.Error(), `apply cancelled by user (got "YES")`)
	assert.Empty(t, m.RecordedCalls())
}

func TestApplyPrompt_EmptyInput_NoInput(t *testing.T) {
	m, stdout, stderr, err := runDestructiveApply(t, false, func(t *testing.T) { useStdin(t, "") })
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Equal(t, 1, strings.Count(stdout+stderr, "apply cancelled: no input"), "stdout=%q stderr=%q", stdout, stderr)
	assert.Empty(t, m.RecordedCalls())
}

// With --auto-approve (destructive plan) or a non-destructive plan the prompt is never read.
func TestApplyPrompt_NotReadWhenNotNeeded(t *testing.T) {
	failing := func(t *testing.T) {
		useStdinReader(t, iotest.ErrReader(fmt.Errorf("must not be read")))
	}
	t.Run("auto-approve destructive", func(t *testing.T) {
		tmpDir := setupPlanProject(t)
		m := destructivePlanMock(t, tmpDir)
		useBackend(t, m)
		failing(t)
		stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
		require.NoError(t, err, stderr)
		assert.NotEmpty(t, m.RecordedCalls())
		assert.NotContains(t, stdout+stderr, "must not be read")
	})
	t.Run("non-destructive plan", func(t *testing.T) {
		tmpDir := setupPlanProject(t)
		threeAddsProject(t, tmpDir)
		m := &backendtest.MockUCBackend{}
		m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
		useBackend(t, m)
		failing(t)
		stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
		require.NoError(t, err, stderr)
		assert.NotEmpty(t, m.RecordedCalls())
		assert.NotContains(t, stdout+stderr, "must not be read")
	})
}

// Two applies in one process: a declined/failed first call must not leak input or flags into the second.
func TestApplyPrompt_ConsecutiveCallsIndependent(t *testing.T) {
	for _, first := range []string{"no\n", strings.Repeat("a", confirmLimit+10)} {
		tmpDir := setupPlanProject(t)
		m := destructivePlanMock(t, tmpDir)
		useBackend(t, m)

		useStdinReader(t, strings.NewReader(first))
		_, _, err := executeCmdSplit("apply", "-t", "dev", "--json")
		require.Error(t, err)
		assert.Empty(t, m.RecordedCalls())

		useStdinReader(t, strings.NewReader("yes\n"))
		stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
		require.NoError(t, err, stderr)
		assert.NotEmpty(t, m.RecordedCalls())
		assert.False(t, json.Valid([]byte(stdout)), "--json from the first call must not leak: %q", stdout)
	}
}
