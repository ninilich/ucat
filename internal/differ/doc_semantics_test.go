package differ_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A missing comment/nullable in YAML means "empty"/"true": the plan texts the documentation promises.
func TestDiff_MissingCommentAndNullable_DescribeTexts(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Columns = []config.Column{{Name: "id", Type: typBIGINT}, {Name: litName, Type: typSTRING}}

	live := makeTable("orders", "silver")
	live.Comment = "has a comment"
	live.Columns = []config.Column{
		{Name: "id", Type: typBIGINT, Comment: "key", Nullable: boolPtr(false)},
		{Name: litName, Type: typSTRING, Comment: "full name"},
	}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	require.False(t, diags.HasError())
	var got []string
	for _, c := range changes {
		assert.False(t, c.IsDestructive())
		got = append(got, c.Describe())
	}
	assert.ElementsMatch(t, []string{
		"unset comment",
		`alter column "id" comment → ""`,
		`alter column "name" comment → ""`,
		`alter column "id" nullable false → true`,
	}, got)
}

// A changed partitioning on an existing table: exact warning, no change.
func TestDiff_PartitioningChange_WarningText(t *testing.T) {
	desired := makeTable("orders", "silver")
	desired.Partitioning = &config.Partitioning{Columns: []string{"a"}}
	live := makeTable("orders", "silver")
	live.Partitioning = &config.Partitioning{Columns: []string{"b"}}

	changes, diags := differ.Diff(desired, backendtest.LiveOf(live))
	assert.Empty(t, changes)
	require.Len(t, diags, 1)
	assert.Equal(t, `table "silver.orders": partitioning cannot be changed after creation; ignoring`, diags[0].Summary)
}
