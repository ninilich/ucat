package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
)

// Awkward values (quotes, hash, YAML keywords, colons) must round-trip through the strict loaders.
var scaffoldValues = []string{"plain", "with # hash", `with "quote"`, "it's", "yes", "123", "a: b", "- dash", "*star", "ünï"}

func TestScaffold_ReadBackByStrictLoaders(t *testing.T) {
	for _, v := range scaffoldValues {
		t.Run(v, func(t *testing.T) {
			root := t.TempDir()

			proj, err := config.ScaffoldProject(v)
			require.NoError(t, err)
			writeFile(t, root, config.ProjectFile, proj)

			cat, err := config.ScaffoldCatalog("main", v, "silver", v)
			require.NoError(t, err)
			writeFile(t, root, config.CatalogFile("main"), cat)

			for _, ws := range []config.WorkspaceConfig{{}, {Host: v}, {WarehouseID: v}, {Host: v, WarehouseID: v}} {
				tgt, err := config.ScaffoldTarget(ws, "main", v)
				require.NoError(t, err)
				writeFile(t, root, config.TargetFile("dev"), tgt)

				got, diags := config.LoadTarget(root, "dev")
				require.False(t, diags.HasError(), "%v\n%s", diags, tgt)
				assert.Equal(t, ws, got.Workspace, tgt)
				require.Len(t, got.Catalogs, 1, tgt)
				assert.Equal(t, v, got.Catalogs["main"].UCName, tgt)
			}

			project, diags := config.LoadProject(root)
			require.False(t, diags.HasError(), "%v\n%s", diags, proj)
			assert.Equal(t, v, project.Project)
			assert.Equal(t, config.DefaultTablesDir, project.TablesDir)
			assert.Equal(t, config.WorkspaceConfig{}, project.Workspace)

			catalogs, diags := config.LoadCatalogs(root)
			require.False(t, diags.HasError(), "%v\n%s", diags, cat)
			require.Len(t, catalogs, 1)
			assert.Equal(t, "main", catalogs[0].Name)
			assert.Equal(t, v, catalogs[0].UCName)
			assert.Equal(t, config.SchemaConfig{UCName: v}, catalogs[0].Schemas["silver"])
		})
	}
}

// The exact text is part of the init contract (the init golden files cover it end to end).
func TestScaffold_Text(t *testing.T) {
	proj, err := config.ScaffoldProject("demo")
	require.NoError(t, err)
	assert.Equal(t, "project: demo\ntables_dir: tables\n", proj)

	cat, err := config.ScaffoldCatalog("main", "prod", "silver", "silver_uc")
	require.NoError(t, err)
	assert.Equal(t, "name: main\nuc_name: prod\nschemas:\n  silver:\n    uc_name: silver_uc\n", cat)

	for ws, want := range map[config.WorkspaceConfig]string{
		{}:                            "workspace:\n  # warehouse_id: <your-sql-warehouse-id>\n",
		{Host: "h"}:                   "workspace:\n  host: h\n  # warehouse_id: <your-sql-warehouse-id>\n",
		{WarehouseID: "w"}:            "workspace:\n  warehouse_id: w\n",
		{Host: "h", WarehouseID: "w"}: "workspace:\n  host: h\n  warehouse_id: w\n",
	} {
		got, err := config.ScaffoldTarget(ws, "main", "dev_uc")
		want += "catalogs:\n  main:\n    uc_name: dev_uc\n"
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}
