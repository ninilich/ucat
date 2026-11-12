package executor_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const promptText = "\nPlan contains destructive changes. Type 'yes' to confirm: "

// Without a terminal nobody types Enter, so Execute ends the prompt line itself: the progress line
// or the next output does not stick to the prompt.
func TestExecute_Prompt_NonTerminal_EndsPromptLine(t *testing.T) {
	cases := map[string]struct {
		in      func() *strings.Reader
		wantOut string
	}{
		"yes":        {func() *strings.Reader { return strings.NewReader("yes\n") }, promptText + "\n  [1/1] applied in 0ms: `prod`.`silver`.`orders` - drop table `prod`.`silver`.`orders`\n"},
		"no":         {func() *strings.Reader { return strings.NewReader("no\n") }, promptText + "\n"},
		"no input":   {func() *strings.Reader { return strings.NewReader("") }, promptText + "\n"},
		"yes no EOL": {func() *strings.Reader { return strings.NewReader("yes") }, promptText + "\n  [1/1] applied in 0ms: `prod`.`silver`.`orders` - drop table `prod`.`silver`.`orders`\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			_, _ = executor.Execute(context.Background(), []change.Change{dropTableChange()}, &backendtest.MockUCBackend{},
				executor.ExecOpts{Now: frozenNow}, tc.in(), &out)
			assert.Equal(t, tc.wantOut, out.String())
		})
	}
}

func TestExecute_Prompt_ReadError_EndsPromptLine(t *testing.T) {
	var out bytes.Buffer
	_, err := executor.Execute(context.Background(), []change.Change{dropTableChange()}, &backendtest.MockUCBackend{},
		executor.ExecOpts{Now: frozenNow}, iotest.ErrReader(fmt.Errorf("boom")), &out)
	require.ErrorIs(t, err, executor.ErrConfirmRead)
	assert.Equal(t, promptText+"\n", out.String())
}

// From a terminal the Enter of the user ends the line: Execute prints nothing extra.
func TestExecute_Prompt_Terminal_NoExtraNewline(t *testing.T) {
	tty, err := os.Open(os.DevNull) // a character device, like a terminal; reads as EOF
	require.NoError(t, err)
	defer func() { _ = tty.Close() }()
	var out bytes.Buffer
	_, err = executor.Execute(context.Background(), []change.Change{dropTableChange()}, &backendtest.MockUCBackend{},
		executor.ExecOpts{Now: frozenNow}, tty, &out)
	require.ErrorIs(t, err, executor.ErrCancelled)
	assert.Equal(t, promptText, out.String())
}
