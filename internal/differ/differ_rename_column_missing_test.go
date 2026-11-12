package differ_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
)

// --- column rename_from typo (source and target both missing in live) is an Error ---

func TestDiff_renameColumn_sourceAndTargetMissing_error(t *testing.T) {
	changes, diags := diffCols(
		[]config.Column{col("id", ""), col("y", "x")},
		[]config.Column{col("id", "")},
	)
	require.True(t, diags.HasError())
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], `column "y"`)
	assert.Contains(t, errs[0], `rename_from "x"`)
	assert.Contains(t, errs[0], "orders")
	assert.Equal(t, "orders.yaml", diags[0].Location)
	assert.Empty(t, summaries(diags, diag.Warning))
	for _, c := range changes {
		_, isRename := c.(*change.RenameColumnChange)
		assert.False(t, isRename)
	}
}

func TestDiff_renameColumn_sourceAndTargetMissing_caseInsensitive(t *testing.T) {
	_, diags := diffCols(
		[]config.Column{col("id", ""), col("Y", "X")},
		[]config.Column{col("ID", "")},
	)
	require.True(t, diags.HasError())
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], `rename_from "X"`)
}

func TestDiff_renameColumn_sourceMissingTargetInLive_stillWarning(t *testing.T) {
	_, diags := diffCols(
		[]config.Column{col("y", "x")},
		[]config.Column{col("Y", "")},
	)
	require.False(t, diags.HasError())
	warns := summaries(diags, diag.Warning)
	require.Len(t, warns, 1)
	assert.Contains(t, warns[0], "can be removed after apply")
}

func TestDiff_renameColumn_sourceAndTargetInLive_stillError(t *testing.T) {
	_, diags := diffCols(
		[]config.Column{col("y", "x")},
		[]config.Column{col("x", ""), col("y", "")},
	)
	require.True(t, diags.HasError())
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "cannot rename")
}

func TestDiffSchema_renameColumn_sourceAndTargetMissing_error(t *testing.T) {
	d := makeTable("orders", "silver")
	d.Columns = []config.Column{col("id", ""), col("y", "x")}
	l := makeTable("orders", "silver")
	l.Columns = []config.Column{col("id", "")}
	_, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(l)})
	require.True(t, diags.HasError())
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], `rename_from "x"`)
}
