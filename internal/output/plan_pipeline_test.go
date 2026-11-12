package output_test

import (
	"go/build"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/differ"
)

// Pipeline: real differ.DiffSchema -> planner -> output.PrintPlan. A table rename with column
// changes after the rename must sit under ONE header with the NEW name, next to the rename.
func TestPrintPlan_pipelineRenameTableGroupsUnderNewName(t *testing.T) {
	desired := []config.Table{
		{
			Name: "new_name", ResolvedCatalog: litCat, ResolvedSchema: litSch, RenameFrom: litOldName,
			Columns: []config.Column{{Name: "a", Type: typSTRING}, {Name: "c", Type: typSTRING}},
		},
		{
			Name: "other", ResolvedCatalog: litCat, ResolvedSchema: litSch,
			Columns: []config.Column{{Name: "x", Type: typINT}, {Name: "y", Type: "INT"}},
		},
	}
	live := []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: "old_name", ResolvedCatalog: litCat, ResolvedSchema: litSch, Columns: []config.Column{{Name: "a", Type: typSTRING}}}),
		backendtest.LiveOf(config.Table{Name: "other", ResolvedCatalog: litCat, ResolvedSchema: litSch, Columns: []config.Column{{Name: "x", Type: "INT"}}}),
	}
	changes, diags := differ.DiffSchema(desired, live)
	require.False(t, diags.HasError(), "%v", diags)

	var haveRename, haveAddAfterRename bool
	for _, ch := range changes {
		switch ch.Type() {
		case change.RenameTable:
			haveRename = true
		case change.AddColumn:
			if strings.Contains(ch.Describe(), `"c"`) {
				haveAddAfterRename = true
			}
		}
	}
	require.True(t, haveRename, "differ must emit RenameTable")
	require.True(t, haveAddAfterRename, "differ must emit AddColumn c for the renamed table")

	plan := planOf(t, changes)
	out := printPlan(plan)

	newKey := change.TablePath{Catalog: "cat", Schema: "sch", Name: "new_name"}.FullName()
	oldKey := change.TablePath{Catalog: "cat", Schema: "sch", Name: "old_name"}.FullName()
	assert.Equal(t, 1, strings.Count(out, "  "+newKey+"\n"), "one header with the new name")
	assert.NotContains(t, out, "  "+oldKey+"\n", "old name must not be a header")

	var got []string
	for _, e := range entries(out) {
		if strings.HasPrefix(e, "H:") {
			got = append(got, e)
		}
	}
	assert.Len(t, got, 2, "two tables, two headers: %v", got)
	for i, ch := range plan {
		assert.Contains(t, out, ch.Describe(), "change %d", i)
	}
	// The rename and the column add of the renamed table sit in the same block.
	block := out[strings.Index(out, "  "+newKey+"\n"):]
	if i := strings.Index(block, "\n\n"); i >= 0 {
		block = block[:i]
	}
	assert.Contains(t, block, "rename table old_name")
	assert.Contains(t, block, `add column "c"`)
}

// Invariant: the production code of internal/output must not depend on the differ package
// (it works through the change.Change interface, GroupKey included).
func TestOutputPackage_doesNotImportDiffer(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	require.NoError(t, err)
	for _, imp := range pkg.Imports {
		assert.NotContains(t, imp, "internal/differ", "production import of differ in internal/output")
	}
}
