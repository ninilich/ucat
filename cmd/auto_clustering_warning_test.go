package cmd

// auto_clustering_warning_test.go covers the warning for a table file without a clustering block
// over a live table with automatic clustering (plan, apply and validate -t; stderr only).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
)

const wantAutoClusteringWarn = `warning: table "silver.orders": Unity Catalog assigned automatic clustering and the file has no clustering block, so apply will drop it; add "clustering: {auto: true}" to keep it (tables/silver/orders.yml)`

func autoClusterMock(auto bool, cols ...string) *backendtest.MockUCBackend {
	live := liveOrders("id")
	live.ClusterAuto = auto
	live.Clustering = cols
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{live})
	return m
}

// partitionedAutoMock is a live table that has both automatic clustering and a partition column.
func partitionedAutoMock() *backendtest.MockUCBackend {
	live := liveOrders("id")
	live.ClusterAuto = true
	live.Partitioning = []string{"id"}
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{live})
	return m
}

func TestAutoClusteringWarning_PlanApplyValidate(t *testing.T) {
	for _, args := range [][]string{
		{cmdPlan, "-t", litDev},
		{cmdApply, "-t", litDev, flagAutoApprove},
		{cmdValidate, "-t", litDev},
	} {
		t.Run(args[0], func(t *testing.T) {
			setupPlanProject(t)
			useBackend(t, autoClusterMock(true))

			stdout, stderr, err := executeCmdSplit(args...)
			require.NoError(t, err)
			assert.Contains(t, stderr, wantAutoClusteringWarn)
			assert.NotContains(t, stdout, "automatic clustering")
		})
	}
}

func TestAutoClusteringWarning_NotEmitted(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		live *backendtest.MockUCBackend
	}{
		{"auto block", "clustering:\n  auto: true\n", autoClusterMock(true)},
		{"columns block on auto live", "clustering:\n  columns: [id]\n", autoClusterMock(true)},
		{"partitioning block", "partitioning:\n  columns: [id]\n", partitionedAutoMock()},
		{"manual live, no block", "", autoClusterMock(false, "id")},
		{"no clustering live", "", autoClusterMock(false)},
		{"table to create", "", &backendtest.MockUCBackend{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newProject(t, projectOpts{Files: map[string]string{
				pathSilverOrders: tableYAML("silver", "orders", tc.yaml),
			}})
			useBackend(t, tc.live)

			for _, cmdName := range []string{cmdPlan, cmdValidate} {
				_, stderr, err := executeCmdSplit(cmdName, "-t", litDev)
				require.NoError(t, err)
				assert.NotContains(t, stderr, "automatic clustering")
			}
		})
	}
}

// The plan and the JSON are the same with the warning: it adds nothing to stdout, and a warning
// changes no exit code.
func TestAutoClusteringWarning_JSONAndExitCodeUnchanged(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, autoClusterMock(true))

	stdout, stderr, err := executeCmdSplit(cmdPlan, "-t", litDev, flagJSON, flagDetailedExitcode)
	assert.Equal(t, 2, codeOf(err))
	assert.Contains(t, stderr, wantAutoClusteringWarn)
	assert.NotContains(t, stdout, "automatic clustering")
	assert.Contains(t, stdout, "alter_clustering")

	useBackend(t, autoClusterMock(false, "id"))
	jsonManual, _, err := executeCmdSplit(cmdPlan, "-t", litDev, flagJSON, flagDetailedExitcode)
	assert.Equal(t, 2, codeOf(err))
	assert.Equal(t, stdout, jsonManual, "same plan, same JSON (the manual live table also drops its clustering)")

	_, _, err = executeCmdSplit(cmdPlan, "-t", litDev)
	require.NoError(t, err)
	assert.Zero(t, codeOf(err))
}
