package pull

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
)

// The converted table does not share slices with the live one: editing it leaves the live table as it was.
func TestLiveToDesired_doesNotAliasLiveSlices(t *testing.T) {
	live := &backend.LiveTable{Name: "t", Clustering: []string{"a", "b"}, Partitioning: []string{"p"}}
	d, _ := liveToDesired(live, "s", "x.yml", filterOf(t, ""))
	require.NotNil(t, d.Clustering)
	d.Clustering.Columns[0] = "changed"
	assert.Equal(t, []string{"a", "b"}, live.Clustering)

	live = &backend.LiveTable{Name: "t", Partitioning: []string{"p", "q"}}
	d, _ = liveToDesired(live, "s", "x.yml", filterOf(t, ""))
	require.NotNil(t, d.Partitioning)
	d.Partitioning.Columns[0] = "changed"
	assert.Equal(t, []string{"p", "q"}, live.Partitioning)
}
