package differ_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
)

func hasClusteringChange(changes []change.Change) bool {
	for _, ch := range changes {
		if ch.Type() == change.AlterClustering {
			return true
		}
	}
	return false
}

// DiffSchema has its own path to Diff: nested live clustering without a YAML block is left alone,
// every nested spelling included; a flat one is still removed; an explicit YAML block still wins.
func TestDiffSchema_NestedLiveClustering(t *testing.T) {
	cases := []struct {
		name    string
		desired *config.Clustering
		live    *config.Clustering
		change  bool
	}{
		{"nested live, no block", nil, &config.Clustering{Columns: []string{litAB}}, false},
		{"mixed live, no block", nil, &config.Clustering{Columns: []string{"a", "b.c"}}, false},
		{"flat live, no block: CLUSTER BY NONE", nil, &config.Clustering{Columns: []string{"a"}}, true},
		{"auto live, no block: CLUSTER BY NONE", nil, &config.Clustering{Auto: true}, true},
		{"nested live, flat block differs", &config.Clustering{Columns: []string{"a"}}, &config.Clustering{Columns: []string{"a.b"}}, true},
		{"no clustering anywhere", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := makeTable("orders", "silver")
			d.Clustering = tc.desired
			l := makeTable("orders", "silver")
			l.Clustering = tc.live
			changes, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(l)})
			require.False(t, diags.HasError())
			assert.Equal(t, tc.change, hasClusteringChange(changes))
		})
	}
}
