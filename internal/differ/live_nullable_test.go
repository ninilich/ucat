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

func nullableAlters(changes []change.Change) []*change.AlterColumnChange {
	var out []*change.AlterColumnChange
	for _, c := range changes {
		if a, ok := c.(*change.AlterColumnChange); ok && a.Kind == change.AlterNullable {
			out = append(out, a)
		}
	}
	return out
}

// The live Nullable bool is read as UC reports it: NOT NULL live + no key in YAML -> alter to
// nullable; nullable live + nullable: false -> alter to NOT NULL; equal states -> nothing.
func TestDiffSchema_liveNullableBothWays(t *testing.T) {
	no := false
	yes := true
	tbl := func(n *bool) config.Table {
		return config.Table{Name: "t", ResolvedCatalog: "c", ResolvedSchema: "s",
			Columns: []config.Column{{Name: "a", Type: typINT, Nullable: n}}}
	}
	liveNotNull := []*backend.LiveTable{backendtest.Live("c", "s", "t", backendtest.NotNull(backendtest.Col("a", "INT")))}
	liveNull := []*backend.LiveTable{backendtest.Live("c", "s", "t", backendtest.Col("a", "INT"))}

	ch, _ := differ.DiffSchema([]config.Table{tbl(nil)}, liveNotNull)
	alters := nullableAlters(ch)
	require.Len(t, alters, 1)
	assert.False(t, alters[0].OldNullable)
	assert.True(t, alters[0].NewNullable)

	ch, _ = differ.DiffSchema([]config.Table{tbl(&no)}, liveNull)
	alters = nullableAlters(ch)
	require.Len(t, alters, 1)
	assert.True(t, alters[0].OldNullable)
	assert.False(t, alters[0].NewNullable)

	for _, n := range []*bool{nil, &yes} {
		ch, _ = differ.DiffSchema([]config.Table{tbl(n)}, liveNull)
		assert.Empty(t, ch)
	}
	ch, _ = differ.DiffSchema([]config.Table{tbl(&no)}, liveNotNull)
	assert.Empty(t, ch)
}
