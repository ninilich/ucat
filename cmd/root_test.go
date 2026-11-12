package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelpContainsAllSubcommands verifies all 7 subcommands appear in --help output.
func TestHelpContainsAllSubcommands(t *testing.T) {
	expected := []string{cmdApply, cmdDrop, cmdInit, cmdPlan, cmdPull, cmdValidate, litVersion}

	out, err := executeCommand("--help")
	require.NoError(t, err)

	for _, sub := range expected {
		assert.Contains(t, out, sub, "expected subcommand %q in --help output", sub)
	}
}

// executeCommand runs the root command with args and returns combined output + error.
// Uses newRootCmd(newApp()) to avoid exec overhead.
func executeCommand(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root := newRootCmd(newApp())
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}
