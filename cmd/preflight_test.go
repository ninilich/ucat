package cmd

// preflight_test.go covers preflight diagnostics as seen through plan and apply.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dropColumnNoMappingMock() *backendtest.MockUCBackend {
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id", "extra_col")})
	return m
}

const wantDropColumnMsg = `table "silver.orders": column drop "extra_col" requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)`

func TestPreflightDropColumnNoMapping_PlanAndApply(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			setupPlanProject(t)
			m := dropColumnNoMappingMock()
			useBackend(t, m)

			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, stdout+stderr+err.Error(), wantDropColumnMsg)
			assert.Contains(t, err.Error(), "preflight checks failed")
			assert.NotContains(t, stdout+stderr, "applied")
			assert.Empty(t, m.RecordedCalls())
		})
	}
}

// two tables with the same short name in different schemas, both dropping a column without
// mapping: both labels appear, exit code 1, nothing applied.
func TestPreflightSameShortNameDifferentSchemas_PlanAndApply(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			tmpDir := setupProjectWithCatalog(t,
				"name: main\nuc_name: dev\nschemas:\n  silver:\n    uc_name: silver\n  gold:\n    uc_name: gold\n")
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "tables", "silver"), 0755))
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "tables", "gold"), 0755))
			for _, s := range []string{litSilver, litGold} {
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tables", s, "orders.yml"),
					[]byte("name: orders\nschema: "+s+"\ncolumns:\n  - name: id\n    type: BIGINT\n"), 0644))
			}
			m := &backendtest.MockUCBackend{}
			m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id", "col_silver")})
			goldLive := liveOrders("id", "col_gold")
			goldLive.Schema = litGold
			m.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litGold}, []*backend.LiveTable{goldLive})
			useBackend(t, m)

			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			all := stdout + stderr + err.Error()
			assert.Contains(t, all, `table "silver.orders": column drop "col_silver"`)
			assert.Contains(t, all, `table "gold.orders": column drop "col_gold"`)
			assert.NotContains(t, all, "preflight:")
			assert.NotRegexp(t, `\[\d+/\d+\] applied`, all)
			assert.Empty(t, m.RecordedCalls())
		})
	}
}

// The column mapping of a table is read from the live state the plan was made from: the pipeline
// asks the backend for no table properties at all (one request per table before).
func TestPreflight_UsesLiveStateNotARequestPerTable(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			tmpDir := setupPlanProject(t)
			b := useSpy(t, newSpy(destructivePlanMock(t, tmpDir))) // drops column extra_col, mapping is set live

			stdout, stderr, err := executeCmdSplit(args...)
			require.NoError(t, err, stderr)
			assert.Contains(t, stdout, `drop column "extra_col"`)
			assert.Zero(t, b.propsCalls, "GetTableProperties calls")
		})
	}
}

// ... and it is the live state that decides: no mapping there is an error, again without a request.
func TestPreflight_LiveStateWithoutMappingFailsWithoutRequest(t *testing.T) {
	setupPlanProject(t)
	b := useSpy(t, newSpy(dropColumnNoMappingMock()))

	_, stderr, err := executeCmdSplit(cmdPlan, "-t", litDev)
	require.Error(t, err)
	assert.Contains(t, stderr, wantDropColumnMsg)
	assert.Zero(t, b.propsCalls)
}
