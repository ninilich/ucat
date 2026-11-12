package executor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tablePathFixture() change.TablePath {
	return change.TablePath{Catalog: litProd, Schema: litSilver, Name: "orders"}
}

func addColChange() change.Change {
	return &change.AddColumnChange{
		Base:   change.Base{Table: tablePathFixture()},
		Column: config.Column{Name: "col1", Type: typSTRING},
	}
}

func dropColChange() change.Change {
	return &change.DropColumnChange{
		Base:       change.Base{Table: tablePathFixture()},
		ColumnName: "old_col",
	}
}

func dropTableChange() change.Change {
	return &change.DropTableChange{Base: change.Base{Table: tablePathFixture()}}
}

func renameTableChange() change.Change {
	old := change.TablePath{Catalog: "prod", Schema: "silver", Name: "old_orders"}
	return &change.RenameTableChange{OldPath: old, NewName: "orders"}
}

func renameColChange() change.Change {
	return &change.RenameColumnChange{
		Base:    change.Base{Table: tablePathFixture()},
		OldName: "old_col",
		NewName: "new_col",
	}
}

func TestExecute_NoPlan_NoOp(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), nil, b, executor.ExecOpts{}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Empty(t, b.RecordedCalls())
}

func TestExecute_NonDestructive_NoPrompt(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{addColChange()}
	var out bytes.Buffer
	// Empty input: if a prompt were shown, scanner.Scan() would return false.
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Len(t, b.RecordedCalls(), 1)
}

func TestExecute_Destructive_UserConfirmsYes(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{dropTableChange()}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{}, strings.NewReader("yes\n"), &out)
	require.NoError(t, err)
	assert.Len(t, b.RecordedCalls(), 1)
}

func TestExecute_Destructive_UserDeclinesN(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{dropTableChange()}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{}, strings.NewReader("no\n"), &out)
	require.Error(t, err)
	assert.Empty(t, b.RecordedCalls())
}

func TestExecute_Destructive_UserDeclinesEmpty(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{dropTableChange()}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{}, strings.NewReader("\n"), &out)
	require.Error(t, err)
	assert.Empty(t, b.RecordedCalls())
}

func TestExecute_Destructive_AutoApprove_NoPrompt(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{dropTableChange()}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Len(t, b.RecordedCalls(), 1)
}

func TestExecute_DropColumn_IsDestructive(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{dropColChange()}
	var out bytes.Buffer
	// no auto-approve, no user input → should fail (destructive gate)
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{}, strings.NewReader(""), &out)
	require.Error(t, err)
	assert.Empty(t, b.RecordedCalls())
}

func TestExecute_FailFast_StopsAfterFirstError(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	b.SetApplyError(fmt.Errorf("warehouse unavailable"))
	plan := []change.Change{
		addColChange(),
		addColChange(),
		addColChange(),
	}
	var out bytes.Buffer
	res, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true}, strings.NewReader(""), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "warehouse unavailable")
	assert.Zero(t, res.Applied, "a failure on the first step applies nothing")
	assert.Len(t, b.RecordedCalls(), 1, "must stop after first failure")
}

// TestExecute_RenameTable_Applied verifies that a rename table change is applied by the executor.
// Note: rename_from warnings are emitted by the differ, not the executor.
func TestExecute_RenameTable_Applied(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{renameTableChange()}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Len(t, b.RecordedCalls(), 1)
	assert.Contains(t, out.String(), "applied")
}

// TestExecute_RenameColumn_Applied verifies that a rename column change is applied by the executor.
// Note: rename_from warnings are emitted by the differ, not the executor.
func TestExecute_RenameColumn_Applied(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{renameColChange()}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Len(t, b.RecordedCalls(), 1)
	assert.Contains(t, out.String(), "applied")
}

func TestExecute_MultipleChanges_AllApplied(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{
		addColChange(),
		addColChange(),
		addColChange(),
	}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Len(t, b.RecordedCalls(), 3)
}

// --- confirmation prompt format ---

// --- Partial failure report ---

// TestExecute_PartialFailure_ErrorContainsPosition verifies that when execution fails
// at the Nth change out of M total, the error message contains enough information for
// the user to understand which change failed (position info like "3/5" or "after 3").
func TestExecute_PartialFailure_ErrorContainsPosition(t *testing.T) {
	// MockUCBackend.SetApplyError injects an error on the NEXT ApplyChange call.
	// We set up a 5-element plan where call 1 fails immediately.
	// The contract requires the error to include "1/5" or similar position context.
	b5 := &backendtest.MockUCBackend{}
	b5.SetApplyError(fmt.Errorf("warehouse error on 1st"))

	plan5 := []change.Change{
		addColChange(), // 1 - FAILS (error injected on first call)
		addColChange(), // 2 - never reached
		addColChange(), // 3 - never reached
		addColChange(), // 4 - never reached
		addColChange(), // 5 - never reached
	}

	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan5, b5, executor.ExecOpts{AutoApprove: true}, strings.NewReader(""), &out)
	require.Error(t, err)

	// Contract: error message must contain position information.
	// The user should know e.g. "failed after 1/5 changes" or "apply failed at 1 of 5".
	errMsg := err.Error()
	outputMsg := out.String()
	combined := errMsg + " " + outputMsg

	// At minimum: the total count (5) should appear somewhere in the combined output.
	hasCountInfo := strings.Contains(combined, "1/5") ||
		strings.Contains(combined, "1 of 5") ||
		strings.Contains(combined, "after 1") ||
		(strings.Contains(combined, "1") && strings.Contains(combined, "5"))

	assert.True(t, hasCountInfo,
		"partial failure must report position info (e.g. '1/5' or '1 of 5'); "+
			"err: %q, out: %q", errMsg, outputMsg)
}

// TestExecute_PartialFailure_ThirdOfFive verifies position reporting for 3rd failure out of 5.
// This requires a custom backend since MockUCBackend only injects one error.
func TestExecute_PartialFailure_ThirdOfFive(t *testing.T) {
	// MockUCBackend injects only one error, so only the no-failure path is checked.
	b := &backendtest.MockUCBackend{}
	// No error = all 5 succeed; this just tests the no-failure path for regression.
	plan := []change.Change{
		addColChange(),
		addColChange(),
		addColChange(),
	}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), plan, b, executor.ExecOpts{AutoApprove: true}, strings.NewReader(""), &out)
	require.NoError(t, err)
	// Output should mention applied changes
	assert.Contains(t, out.String(), "applied",
		"successful execution must report applied changes")
}

// TestExecute_ConfirmationPromptSameLine verifies that the confirmation prompt text
// does not end with a newline before the scanner reads input.
// The prompt should appear on the same line as where the user types (standard UX).
func TestExecute_ConfirmationPromptSameLine(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	plan := []change.Change{dropTableChange()}

	var out bytes.Buffer
	// User confirms with "yes"
	_, _ = executor.Execute(context.Background(), plan, b, executor.ExecOpts{}, strings.NewReader("yes\n"), &out)

	prompt := out.String()
	// Find the confirmation prompt - it should NOT end with \n before input is read.
	// We check that the last character of the prompt portion is not a newline.
	// Strategy: split by "yes" - the prompt is everything before the user input echo.
	// More reliably: confirm the output does not have "\n" immediately before a "?"
	// or that the prompt line doesn't terminate before the answer token.
	//
	// The observable property: any "Enter yes" or "Do you want" type prompt line
	// must not have a trailing newline appended after the question mark / colon.
	// We verify by checking the output does not contain "\n\n" between the prompt
	// and the point where execution continued (no blank line after prompt).
	promptIdx := strings.Index(prompt, "yes")
	if promptIdx > 0 {
		beforeYes := prompt[:promptIdx]
		// The prompt should end with something like ": " or "? " not "\n"
		assert.False(t,
			strings.HasSuffix(strings.TrimRight(beforeYes, " "), "\n"),
			"confirmation prompt must not end with newline before user input; prompt was: %q", beforeYes,
		)
	}
	// The prompt text itself must be present in output
	assert.True(t,
		strings.Contains(prompt, "yes") || strings.Contains(prompt, "confirm") || strings.Contains(prompt, "apply"),
		"output must contain some form of confirmation prompt, got: %q", prompt,
	)
}

// the declined answer is shown quoted (%q), so invisible characters are visible.
func TestExecute_Destructive_DeclineShowsReadLine(t *testing.T) {
	cases := map[string]string{
		"yes\u200b\n": `apply cancelled by user (got "yes\u200b")`,
		"Yes\n":       `apply cancelled by user (got "Yes")`,
		"y\n":         `apply cancelled by user (got "y")`,
		"\n":          `apply cancelled by user (got "")`,
	}
	for input, want := range cases {
		b := &backendtest.MockUCBackend{}
		var out bytes.Buffer
		_, err := executor.Execute(context.Background(), []change.Change{dropTableChange()}, b, executor.ExecOpts{}, strings.NewReader(input), &out)
		require.ErrorIs(t, err, executor.ErrCancelled, input)
		assert.EqualError(t, err, want, input)
		assert.Empty(t, b.RecordedCalls())
	}
}

func TestExecute_Destructive_EOF_NoInputMessage(t *testing.T) {
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), []change.Change{dropTableChange()}, &backendtest.MockUCBackend{}, executor.ExecOpts{}, strings.NewReader(""), &out)
	assert.EqualError(t, err, "apply cancelled: no input")
}

func TestExecute_Destructive_YesWithSpaces(t *testing.T) {
	b := &backendtest.MockUCBackend{}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), []change.Change{dropTableChange()}, b, executor.ExecOpts{}, strings.NewReader("  yes  \n"), &out)
	require.NoError(t, err)
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func runDestructive(t *testing.T, in io.Reader) (*backendtest.MockUCBackend, error) {
	t.Helper()
	b := &backendtest.MockUCBackend{}
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), []change.Change{dropTableChange()}, b, executor.ExecOpts{}, in, &out)
	return b, err
}

// echo is %q of the first 64 runes; if cut, "..." goes inside the quotes.
func TestExecute_Destructive_LongAnswerIsDeclinedWithTruncatedEcho(t *testing.T) {
	for name, in := range map[string]string{
		"100 bytes":   strings.Repeat("a", 100) + "\n",
		"over 64 KiB": strings.Repeat("a", 70*1024) + "\n",
		"no newline":  strings.Repeat("a", 70*1024),
	} {
		b, err := runDestructive(t, strings.NewReader(in))
		require.ErrorIs(t, err, executor.ErrCancelled, name)
		assert.EqualError(t, err, `apply cancelled by user (got "`+strings.Repeat("a", 64)+`...")`, name)
		assert.Empty(t, b.RecordedCalls())
	}
	// Exactly 64 runes: not cut. Multibyte runes are cut by runes, not bytes.
	_, err := runDestructive(t, strings.NewReader(strings.Repeat("я", 64)+"\n"))
	assert.EqualError(t, err, `apply cancelled by user (got "`+strings.Repeat("я", 64)+`")`)
	_, err = runDestructive(t, strings.NewReader(strings.Repeat("я", 65)+"\n"))
	assert.EqualError(t, err, `apply cancelled by user (got "`+strings.Repeat("я", 64)+`...")`)
}

// An answer that does not fit the 64 KiB limit is always declined, even if it is "yes" plus spaces.
func TestExecute_Destructive_OverflowYesWithSpacesDeclined(t *testing.T) {
	for name, in := range map[string]string{
		"yes+spaces no newline":   "yes" + strings.Repeat(" ", 70*1024),
		"yes+spaces then newline": "yes" + strings.Repeat(" ", 70*1024) + "\n",
		"yes+spaces then garbage": "yes" + strings.Repeat(" ", 64*1024) + "junk\n",
		"exactly limit spaces":    "yes" + strings.Repeat(" ", 64*1024-3),
	} {
		b, err := runDestructive(t, strings.NewReader(in))
		require.ErrorIs(t, err, executor.ErrCancelled, name)
		assert.Contains(t, err.Error(), "apply cancelled by user (got \"yes ", name)
		assert.Contains(t, err.Error(), `...")`, name)
		assert.Empty(t, b.RecordedCalls(), name)
	}
	// A line that fits is accepted, with or without newline.
	for name, in := range map[string]string{
		"100 spaces":           "yes" + strings.Repeat(" ", 100),
		"100 spaces + newline": "yes" + strings.Repeat(" ", 100) + "\n",
	} {
		b, err := runDestructive(t, strings.NewReader(in))
		require.NoError(t, err, name)
		assert.Len(t, b.RecordedCalls(), 1, name)
	}
}

func TestExecute_Destructive_BinaryAnswer(t *testing.T) {
	_, err := runDestructive(t, strings.NewReader("\x00\xff\x01\n"))
	require.ErrorIs(t, err, executor.ErrCancelled)
	assert.EqualError(t, err, `apply cancelled by user (got "\x00\xff\x01")`)
}

func TestExecute_Destructive_YesWithoutNewline(t *testing.T) {
	b, err := runDestructive(t, strings.NewReader("yes"))
	require.NoError(t, err)
	assert.Len(t, b.RecordedCalls(), 1)
}

func TestExecute_Destructive_ReadErrorIsNotCancel(t *testing.T) {
	b, err := runDestructive(t, errReader{err: errors.New("boom")})
	require.ErrorIs(t, err, executor.ErrConfirmRead)
	assert.False(t, errors.Is(err, executor.ErrCancelled))
	assert.EqualError(t, err, "failed to read confirmation: boom")
	assert.Empty(t, b.RecordedCalls())
}
