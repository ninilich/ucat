package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validate -t with an unreachable warehouse: exact stdout, one error line on stderr, exit code 1,
// and the schema line is still printed before the warehouse failure.
func TestValidateTarget_WarehouseUnreachable_OutputAndExitCode(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"),
		"workspace:\n  host: https://dev.example.com\n  warehouse_id: wh1\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "orders")})
	mock.SetWarehouseError(errors.New("warehouse stopped"))
	useSpy(t, newSpy(mock))

	var out, errOut bytes.Buffer
	err := runRootIn(&out, &errOut, []string{cmdValidate, "-t", litDev})
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Contains(t, out.String(), "✓ schema dev.silver exists\n")
	assert.Contains(t, out.String(), "✗ warehouse wh1: warehouse stopped\n")
	assert.Equal(t, "✓ all local checks passed\n✓ schema dev.silver exists\n✗ warehouse wh1: warehouse stopped\n", out.String())
	assert.Equal(t, "Error: live validation failed for warehouse wh1\n", errOut.String(), "single error line on stderr")
}

// A reachable warehouse never prints the failure line and exits 0.
func TestValidateTarget_WarehouseReachable_ExitZero(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"),
		"workspace:\n  host: https://dev.example.com\n  warehouse_id: wh1\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(silverRef, []*backend.LiveTable{liveTable("silver", "orders")})
	useSpy(t, newSpy(mock))

	stdout, stderr, err := executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err)
	assert.Equal(t, 0, codeOf(err))
	assert.Contains(t, stdout, "✓ warehouse wh1 reachable\n")
	assert.NotContains(t, stdout, "✗")
	assert.Empty(t, stderr)
}
