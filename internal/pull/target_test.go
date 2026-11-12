package pull_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/pull"
)

func never(string) bool  { return false }
func always(string) bool { return true }

func defined() []config.Table {
	return []config.Table{
		{Name: litOrders, Schema: litSilver, SourceFile: "tables/silver/orders.yml"},
		{Name: "moved", Schema: litSilver, SourceFile: "tables/misc/moved.yml"},
		{Name: "new_name", Schema: litSilver, RenameFrom: litOldName, SourceFile: "tables/silver/new_name.yml"},
		{Name: "same", Schema: litSilver, RenameFrom: "same", SourceFile: "tables/silver/same.yml"},
		{Name: "squatter", Schema: "gold", SourceFile: "tables/silver/fresh.yml"},
	}
}

func TestDecideCreate(t *testing.T) {
	idx := pull.NewIndex(defined())
	got := idx.Decide("/proj", "tables", "silver", "brand_new", false, never)
	assert.Empty(t, got.SkipReason)
	assert.Equal(t, "created", got.Verb)
	assert.False(t, got.InPlace)
	assert.Equal(t, "tables/silver/brand_new.yml", got.RelPath)
	assert.Equal(t, filepath.Join("/proj", "tables", "silver", "brand_new.yml"), got.FilePath)
}

func TestDecideSkipReasons(t *testing.T) {
	idx := pull.NewIndex(defined())
	tests := []struct {
		name, schema, table string
		force               bool
		exists              func(string) bool
		reason              string
	}{
		{"rename source", litSilver, "old_name", false, never, `silver.old_name (rename_from source of "new_name", rename not applied yet)`},
		{"rename source wins over force", litSilver, "old_name", true, never, `silver.old_name (rename_from source of "new_name", rename not applied yet)`},
		{"defined", litSilver, "orders", false, never, "tables/silver/orders.yml (defined in tables/silver/orders.yml)"},
		{"defined in another file", litSilver, "moved", false, never, "tables/misc/moved.yml (defined in tables/misc/moved.yml)"},
		{"canonical path holds another table", litSilver, "fresh", false, never, `silver.fresh (tables/silver/fresh.yml defines table "squatter")`},
		{"canonical path holds another table, force", "silver", "fresh", true, never, `silver.fresh (tables/silver/fresh.yml defines table "squatter")`},
		{"file exists", "silver", "other", false, always, "tables/silver/other.yml (already exists, use --force)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.Decide("/proj", "tables", tt.schema, tt.table, tt.force, tt.exists)
			assert.Equal(t, tt.reason, got.SkipReason)
		})
	}
}

func TestDecideRenameToSelfIsNotRenameSource(t *testing.T) {
	idx := pull.NewIndex(defined())
	got := idx.Decide("/proj", "tables", "silver", "same", false, never)
	assert.Equal(t, "tables/silver/same.yml (defined in tables/silver/same.yml)", got.SkipReason)
}

func TestDecideForceUpdatesDefiningFileInPlace(t *testing.T) {
	idx := pull.NewIndex(defined())
	got := idx.Decide("/proj", "tables", "silver", "moved", true, never)
	assert.Empty(t, got.SkipReason)
	assert.Equal(t, "updated", got.Verb)
	assert.True(t, got.InPlace)
	assert.Equal(t, "tables/misc/moved.yml", got.RelPath)
	assert.Equal(t, filepath.Join("/proj", "tables", "misc", "moved.yml"), got.FilePath)
}

func TestDecideForceOverwritesExistingUndefinedFile(t *testing.T) {
	idx := pull.NewIndex(nil)
	got := idx.Decide("/proj", "tables", "silver", "other", true, always)
	assert.Empty(t, got.SkipReason)
	assert.Equal(t, "created", got.Verb)
	assert.False(t, got.InPlace)
}

func TestDecideAbsoluteSourceFileIsNotJoinedWithRoot(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "x", "t.yml")
	idx := pull.NewIndex([]config.Table{{Name: "t", Schema: "s", SourceFile: abs}})
	got := idx.Decide("/proj", "tables", "s", "t", true, never)
	assert.Equal(t, abs, got.FilePath)
	assert.Equal(t, "updated", got.Verb)
}

func TestDecideCustomTablesDir(t *testing.T) {
	got := pull.NewIndex(nil).Decide("/proj", "defs/t", "silver", "x", false, never)
	assert.Equal(t, "defs/t/silver/x.yml", got.RelPath)
}

// Whatever (file-safe) logical schema and table names are, the file is inside the project root:
// "C:" and Windows device names are plain path parts on any OS; the names that could climb
// out ("..", separators, absolute paths) never reach Decide (config.IsFileSafeName).
func TestDecideStaysInsideRoot(t *testing.T) {
	idx := pull.NewIndex(nil)
	root := filepath.Join(string(filepath.Separator), "proj")
	for _, name := range []string{"C:", "con", "NUL", "aux", "COM1", "a-b", "заказы", "-", "_"} {
		got := idx.Decide(root, "tables", name, name, false, never)
		rel, err := filepath.Rel(root, got.FilePath)
		require.NoError(t, err)
		assert.NotContains(t, filepath.ToSlash(rel), "..", name)
		assert.Equal(t, "tables/"+name+"/"+name+".yml", got.RelPath)
	}
}
