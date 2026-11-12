package pull_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/pull"
)

func s(v string) *string { return &v }

func TestParseFilterErrors(t *testing.T) {
	tests := map[string]string{
		"a,,b":     "empty mask",
		" ":        "empty mask",
		"*":        "lone *",
		"all,x":    "cannot be combined",
		"a*b":      "only allowed at the end",
		"x,a*b,y*": "only allowed at the end",
	}
	for spec, msg := range tests {
		_, err := pull.ParseFilter(spec)
		require.Error(t, err, spec)
		assert.Contains(t, err.Error(), msg, spec)
	}
}

func TestFilterSelect(t *testing.T) {
	props := map[string]string{litOwner: "a", litOwnerTeam: "b", litQualityX: "c", "delta.minReaderVersion": "1"}

	f, err := pull.ParseFilter("")
	require.NoError(t, err)
	assert.Nil(t, f.Select(props), "no filter writes nothing")

	f, _ = pull.ParseFilter("owner")
	assert.Equal(t, map[string]*string{litOwner: s("a")}, f.Select(props), "exact mask does not take a longer key")

	f, _ = pull.ParseFilter("owner*, quality.*, owner")
	assert.Equal(t, map[string]*string{"owner": s("a"), "owner.team": s("b"), "quality.x": s("c")}, f.Select(props))

	f, _ = pull.ParseFilter("all")
	assert.Equal(t, map[string]*string{"owner": s("a"), "owner.team": s("b"), "quality.x": s("c"), "delta.minReaderVersion": s("1")}, f.Select(props))
	assert.Nil(t, f.Select(nil))
}

func TestFilterWarnings(t *testing.T) {
	f, _ := pull.ParseFilter("all")
	w := f.Warnings()
	require.Len(t, w, 1)
	assert.Contains(t, w[0].Summary, "writes system properties")

	f, _ = pull.ParseFilter("d*,delta.*,owner*,quality")
	w = f.Warnings()
	require.Len(t, w, 2, "one warning per risky mask, in mask order")
	assert.Contains(t, w[0].Summary, `"d*"`)
	assert.Contains(t, w[1].Summary, `"delta.*"`)

	f, _ = pull.ParseFilter("owner")
	assert.Empty(t, f.Warnings())
}

func TestFilterUnmatchedWarnings(t *testing.T) {
	f, _ := pull.ParseFilter("a,b*,c")
	f.Select(map[string]string{"a": "1", "bx": "2"})
	w := f.UnmatchedWarnings()
	require.Len(t, w, 1)
	assert.Equal(t, `--tblproperties mask "c" matched no property`, w[0].Summary)
}

func TestFilterSchemas(t *testing.T) {
	all := []config.ResolvedSchema{
		{LogicalCatalog: "a", LogicalSchema: "x"}, {LogicalCatalog: "a", LogicalSchema: "y"}, {LogicalCatalog: "b", LogicalSchema: "x"},
	}
	got, err := pull.FilterSchemas(all, "", "")
	require.NoError(t, err)
	assert.Len(t, got, 3)
	got, _ = pull.FilterSchemas(all, "a", "")
	assert.Len(t, got, 2)
	got, _ = pull.FilterSchemas(all, "a", "y")
	assert.Equal(t, []config.ResolvedSchema{all[1]}, got)

	_, err = pull.FilterSchemas(all, "zz", "")
	assert.EqualError(t, err, `unknown catalog "zz"`)
	_, err = pull.FilterSchemas(all, "a", "zz")
	assert.EqualError(t, err, `unknown schema "zz" in catalog "a"`)
}
