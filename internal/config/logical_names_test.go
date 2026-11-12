package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
)

// Logical catalog and schema names and target names become path parts (catalogs/<n>.yml,
// targets/<n>.yml, tables/<schema>/...): the loader rejects names that could leave the project.

func summaries(t *testing.T, name, content string, load func(root string) []string) []string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, name, content)
	return load(root)
}

func loadCatalogErrors(root string) []string {
	_, diags := config.LoadCatalogs(root)
	var out []string
	for _, d := range errDiags(diags) {
		out = append(out, d.String())
	}
	return out
}

func TestLoadCatalogs_UnsafeSchemaName_Rejected(t *testing.T) {
	for name, key := range map[string]string{
		"dotdot path": `"../../evil"`, "slash": `"a/b"`, "backslash": `'a\b'`, "dotdot": `".."`,
		"dot": `"."`, "hidden": `".hidden"`, "space": `"a b"`, "empty": `""`, "drive": `'C:\x'`, "absolute": `"/etc"`,
	} {
		t.Run(name, func(t *testing.T) {
			got := summaries(t, "catalogs/main.yml", "name: main\nuc_name: dev\nschemas:\n  "+key+":\n    uc_name: silver\n", loadCatalogErrors)
			require.Len(t, got, 1)
			assert.Contains(t, got[0], "error: logical schema name ")
			assert.Contains(t, got[0], "(catalogs/main.yml:4)")
		})
	}
}

func TestLoadCatalogs_UnsafeCatalogName_Rejected(t *testing.T) {
	got := summaries(t, "catalogs/main.yml", "uc_name: dev\nname: ../x\nschemas:\n  silver: {}\n", loadCatalogErrors)
	assert.Equal(t, []string{`error: logical catalog name "../x" must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name (catalogs/main.yml:2)`}, got)
}

// An empty catalog name stays check 20 of the validator, the loader does not report it.
func TestLoadCatalogs_EmptyCatalogName_LeftToValidator(t *testing.T) {
	assert.Empty(t, summaries(t, "catalogs/main.yml", "name: \"\"\nuc_name: dev\n", loadCatalogErrors))
}

func TestLoadCatalogs_ExistingValidNamesStillLoad(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "catalogs/main.yml", "name: Main-1\nuc_name: dev\nschemas:\n  silver_2: {}\n  gold-eu: {}\n  UPPER: {}\n")
	cats, diags := config.LoadCatalogs(root)
	require.Empty(t, errDiags(diags))
	require.Len(t, cats, 1)
	assert.Len(t, cats[0].Schemas, 3)
}

func TestLoadTarget_UnsafeName_RejectedBeforeReading(t *testing.T) {
	root := t.TempDir()
	// the file outside targets/ exists: it must not be read
	writeFile(t, root, "x.yml", "workspace:\n  host: https://x\n")
	for _, name := range []string{"../x", "..", pathAB, `p\q`, "", ".dev", litAB, "dev.eu"} {
		cfg, diags := config.LoadTarget(root, name)
		require.Len(t, diags, 1, "%q", name)
		assert.True(t, diags.HasError())
		assert.Contains(t, diags[0].Summary, "target name ")
		assert.Empty(t, cfg.Workspace.Host, "%q", name)
	}
}

func TestLoadTarget_UnsafeCatalogAndSchemaNames_Rejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "targets/dev.yml", "catalogs:\n  main:\n    schemas:\n      \"../evil\":\n        uc_name: s\n  \"a/b\":\n    uc_name: c\n")
	_, diags := config.LoadTarget(root, "dev")
	var got []string
	for _, d := range diags {
		got = append(got, d.String())
	}
	assert.Equal(t, []string{
		`error: logical catalog name "a/b" must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name (targets/dev.yml:6)`,
		`error: logical schema name "../evil" must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name (targets/dev.yml:4)`,
	}, got)
}

func TestLoadTarget_EmptyCatalogName_Rejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "targets/dev.yml", "catalogs:\n  \"\":\n    uc_name: c\n")
	_, diags := config.LoadTarget(root, "dev")
	require.Len(t, diags, 1)
	assert.Equal(t, "error: logical catalog name is empty (targets/dev.yml:2)", diags[0].String())
}

func TestLoadTarget_ValidNames_Load(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "targets/dev-eu.yml", "workspace:\n  host: https://x\ncatalogs:\n  main:\n    schemas:\n      silver: {}\n")
	cfg, diags := config.LoadTarget(root, "dev-eu")
	require.Empty(t, diags)
	assert.Equal(t, "https://x", cfg.Workspace.Host)
}
