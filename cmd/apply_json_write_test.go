package cmd

// The result JSON of "apply --json" that cannot be written to stdout.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errStdoutClosed = errors.New("stdout closed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errStdoutClosed }

// A successful apply whose JSON cannot be written is an error (exit 1): CI must not read an empty stdout as success.
func TestApplyJSON_Success_StdoutWriteErrorIsReported(t *testing.T) {
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	b := newSpy(nil)
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useSpy(t, b)

	var errBuf bytes.Buffer
	err := execRoot(failingWriter{}, &errBuf, []string{"apply", "-t", litDev, flagJSON, flagAutoApprove})
	require.Error(t, err)
	assert.ErrorIs(t, err, errStdoutClosed)
	assert.Equal(t, 1, codeOf(err))
}

// A failed apply reports the failed change, not the write error of the JSON.
func TestApplyJSON_Failure_StdoutWriteErrorDoesNotHideApplyError(t *testing.T) {
	tmpDir := setupPlanProject(t)
	threeAddsProject(t, tmpDir)
	b := newSpy(nil)
	b.failApplyOn = 2
	b.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	useSpy(t, b)

	var errBuf bytes.Buffer
	err := execRoot(failingWriter{}, &errBuf, []string{"apply", "-t", "dev", "--json", "--auto-approve"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, errStdoutClosed)
	assert.Contains(t, err.Error(), "apply failed after 1/3")
}
