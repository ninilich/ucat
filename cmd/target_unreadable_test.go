package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An unreadable target file is reported as "failed to read targets/dev.yml: ..." by every
// command that loads a target, with a relative path only.
func TestLoadErrors_UnreadableTargetFile(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdValidate, "-t", litDev}, {cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			tmpDir := setupPlanProject(t)
			useSpy(t, listFailSpy().forbid(t))
			denyAccess(t, filepath.Join(tmpDir, "targets", "dev.yml"), 0o644)

			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			all := stdout + stderr + err.Error()
			assert.Contains(t, all, "failed to read targets/dev.yml: ")
			assert.Contains(t, all, "permission denied")
			assert.NotContains(t, all, `failed to read target "dev"`)
			assertNoAbsolutePath(t, tmpDir, all)
		})
	}
}
