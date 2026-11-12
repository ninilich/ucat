package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every YAML loader reports an unreadable file in one format: "failed to read <rel>: <reason>",
// Location = <rel>, with no absolute path.
func TestLoaders_unreadableFileUnifiedText(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod does not deny access on Windows or as root")
	}
	root := t.TempDir()
	files := map[string]string{
		pathProjectFile:        "project: p\n",
		pathCatalogMain:        "name: main\nuc_name: prod\n",
		pathTargetDev:          "workspace:\n  host: h\n",
		"tables/s/orders.yml":  "schema: s\nname: orders\n",
		"tables/s/orders2.yml": "schema: s\nname: orders2\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	for _, rel := range []string{"ucat.yml", pathCatalogMain, "targets/dev.yml", "tables/s/orders.yml"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.Chmod(p, 0o000))
		t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	}

	want := func(rel string) string { return "failed to read " + rel + ": open: permission denied" }

	_, diags := config.LoadProject(root)
	require.Len(t, diags, 1)
	assert.Equal(t, want("ucat.yml"), diags[0].Summary)
	assert.Equal(t, "ucat.yml", diags[0].Location)

	_, diags = config.LoadCatalogs(root)
	require.Len(t, diags, 1)
	assert.Equal(t, want("catalogs/main.yml"), diags[0].Summary)
	assert.Equal(t, "catalogs/main.yml", diags[0].Location)

	_, diags = config.LoadTarget(root, "dev")
	require.Len(t, diags, 1)
	assert.Equal(t, want("targets/dev.yml"), diags[0].Summary)
	assert.Equal(t, "targets/dev.yml", diags[0].Location)

	tables, diags := config.LoadTables(root, config.ProjectConfig{TablesDir: litTables})
	require.Len(t, diags, 1)
	assert.Equal(t, want("tables/s/orders.yml"), diags[0].Summary)
	assert.Equal(t, "tables/s/orders.yml", diags[0].Location)
	require.Len(t, tables, 1, "the readable file is still loaded")
	assert.Equal(t, "orders2", tables[0].Name)
}

// A missing target file is a read error in the same format.
func TestLoadTarget_missingFileUnifiedText(t *testing.T) {
	_, diags := config.LoadTarget(t.TempDir(), "nope")
	require.Len(t, diags, 1)
	assert.Equal(t, "targets/nope.yml", diags[0].Location)
	assert.Contains(t, diags[0].Summary, "failed to read targets/nope.yml: ")
}
