package cmd

// With stdin that is not a terminal (a pipe, CI) nobody types Enter, so the confirmation prompt line
// is ended by ucat: the next output does not stick to it.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const answerYes = "yes\n"

func TestApplyPrompt_NonTerminal_NextOutputOnNewLine(t *testing.T) {
	const prompt = "Type 'yes' to confirm: "
	for _, jsonMode := range []bool{false, true} {
		for _, answer := range []string{answerYes, "no\n", ""} {
			t.Run(strings.TrimSpace(answer)+map[bool]string{false: "/text", true: "/json"}[jsonMode], func(t *testing.T) {
				_, stdout, stderr, err := runDestructiveApply(t, jsonMode, func(t *testing.T) { useStdin(t, answer) })
				// the prompt and the progress/error text are on stdout (text) or stderr (--json)
				text := stdout
				if jsonMode {
					text = stderr
					var r applyJSON
					decodeSingleJSON(t, stdout, &r)
					assert.NotContains(t, stdout, prompt, "stdout is the result JSON only")
				}
				i := strings.Index(text, prompt)
				require.GreaterOrEqual(t, i, 0, "text=%q", text)
				rest := text[i+len(prompt):]
				assert.True(t, strings.HasPrefix(rest, "\n"), "the line after the prompt starts on a new line: %q", rest)
				assert.False(t, strings.HasPrefix(rest, "\n\n"), "one newline only: %q", rest)
				if answer == answerYes {
					require.NoError(t, err)
					assert.True(t, strings.HasPrefix(rest, "\n  [1/2] applied"), "%q", rest)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

// Declined: the error line is on its own line, not glued to the prompt (text and --json).
func TestApplyPrompt_NonTerminal_ErrorOnOwnLine(t *testing.T) {
	_, stdout, stderr, err := runDestructiveApply(t, false, func(t *testing.T) { useStdin(t, "no\n") })
	require.Error(t, err)
	assert.Contains(t, stdout, "Type 'yes' to confirm: \n")
	assert.NotContains(t, stdout+stderr, "confirm: Error")
}
