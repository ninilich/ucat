package differ_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
)

const autoClusteringFile = "tables/silver/auto_orders.yml"

const wantAutoClusteringWarning = `table "silver.orders": Unity Catalog assigned automatic clustering and the file has no clustering block, so apply will drop it; add "clustering: {auto: true}" to keep it`

// A table without clustering and partitioning blocks over a live table with automatic clustering
// still plans the drop, and warns about it; no other combination warns.
func TestDiff_AutoClusteringDropWarning(t *testing.T) {
	cases := []struct {
		name         string
		desiredClust *config.Clustering
		desiredPart  *config.Partitioning
		liveAuto     bool
		liveCols     []string
		livePart     []string
		missingLive  bool
		warn         bool
	}{
		{name: "auto live, no blocks", liveAuto: true, warn: true},
		{name: "auto live, auto block", desiredClust: &config.Clustering{Auto: true}, liveAuto: true},
		{name: "auto live, columns block", desiredClust: &config.Clustering{Columns: []string{"id"}}, liveAuto: true},
		{name: "auto live, partitioning block", desiredPart: &config.Partitioning{Columns: []string{"id"}}, liveAuto: true, livePart: []string{"id"}},
		{name: "manual live, no blocks", liveCols: []string{"id"}},
		{name: "no clustering live, no blocks"},
		{name: "table to create", missingLive: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := makeTable("orders", "silver")
			d.SourceFile = autoClusteringFile
			d.Columns = []config.Column{{Name: "id", Type: typBIGINT}}
			d.Clustering = tc.desiredClust
			d.Partitioning = tc.desiredPart
			var live *backend.LiveTable
			if !tc.missingLive {
				live = backendtest.LiveOf(d)
				live.ClusterAuto = tc.liveAuto
				live.Clustering = tc.liveCols
				live.Partitioning = tc.livePart
			}
			changes, diags := differ.Diff(d, live)
			require.False(t, diags.HasError())
			var warns []string
			for _, dg := range diags {
				warns = append(warns, dg.Summary)
			}
			if !tc.warn {
				assert.NotContains(t, warns, wantAutoClusteringWarning)
				return
			}
			assert.Equal(t, []string{wantAutoClusteringWarning}, warns)
			assert.Equal(t, autoClusteringFile, diags[0].Location)
			assert.True(t, hasClusteringChange(changes), "the drop stays in the plan")
		})
	}
}
