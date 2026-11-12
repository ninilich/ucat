package cmd

// With stdin that is not a terminal (a pipe, CI) nobody types Enter, so ucat ends each prompt line
// itself: the prompts and the output after them do not stick together. From a terminal the Enter of the
// user ends the line and ucat prints nothing extra.

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_NonTerminal_EachPromptOnOwnLine(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := executeInitCmd(t, "proj\nmain\nsilver\nsilver\ndev\nhttps://h\nmain\nwh\n")
	require.NoError(t, err)
	want := "Project name: \n" +
		"Logical catalog name [main]: \n" +
		"Logical schema name: \n" +
		"UC schema name [silver]: \n" +
		"Target names (comma-separated) [dev]: \n" +
		"Workspace host for 'dev': \n" +
		"UC catalog name for target 'dev' [main]: \n" +
		"SQL warehouse ID for target 'dev' (from Databricks UI > SQL Warehouses, optional): \n" +
		"✓ Created ucat.yml\n"
	assert.Contains(t, out, want)
}

// The last answer without a trailing newline and EOF (no answer) also end the prompt line.
func TestInit_NonTerminal_LastAnswerWithoutNewlineAndEOF(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := executeInitCmd(t, "proj\nmain\nsilver\nsilver\ndev\nhttps://h\nmain\nwh")
	require.NoError(t, err)
	assert.Contains(t, out, "(from Databricks UI > SQL Warehouses, optional): \n✓ Created ucat.yml\n")

	t.Chdir(t.TempDir())
	out, err = executeInitCmd(t, "")
	require.Error(t, err)
	assert.Equal(t, "Project name: \n", out, "EOF: the prompt line is ended, the error is printed on its own line")
}

func TestInit_Terminal_NoExtraNewline(t *testing.T) {
	t.Chdir(t.TempDir())
	tty, err := os.Open(os.DevNull) // a character device, like a terminal; reads as EOF
	require.NoError(t, err)
	t.Cleanup(func() { _ = tty.Close() })
	useStdinReader(t, tty)
	var buf bytes.Buffer
	_ = execRoot(&buf, &buf, []string{cmdInit})
	assert.Equal(t, "Project name: ", buf.String())
}
