package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers to build minimal test configs
func mainCatalog() config.CatalogConfig {
	return config.CatalogConfig{
		Name:   litMain,
		UCName: litProd,
		Schemas: map[string]config.SchemaConfig{
			litSilver2: {UCName: litSilver2},
			litGold:    {},
		},
	}
}

func silverTable() config.Table {
	return config.Table{Name: litOrders, Schema: litSilver2}
}

// The environment is the last resort: a warehouse in the target or in ucat.yml wins over it.
func TestResolve_warehousePriority(t *testing.T) {
	const env = "env-warehouse-id"
	cases := []struct {
		name         string
		project, tgt string
		want         string
	}{
		{"target wins over project and env", litProjectWh, litTargetWH, litTargetWH},
		{"project wins over env", litProjectWh, "", litProjectWh},
		{"target wins over env", "", litTargetWH, litTargetWH},
		{"env is the fallback", "", "", env},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := config.ProjectConfig{Workspace: config.WorkspaceConfig{WarehouseID: tc.project}}
			target := config.TargetConfig{Workspace: config.WorkspaceConfig{WarehouseID: tc.tgt}}
			resolved := config.Resolve(proj, []config.CatalogConfig{mainCatalog()}, target, nil, config.ResolveOptions{WarehouseID: env})
			assert.Equal(t, tc.want, resolved.WarehouseID)
		})
	}
}

func TestEnvOverrideWarnings(t *testing.T) {
	ws := func(host, wh string) config.WorkspaceConfig {
		return config.WorkspaceConfig{Host: host, WarehouseID: wh}
	}
	cases := []struct {
		name    string
		project config.WorkspaceConfig
		target  config.WorkspaceConfig
		opts    config.ResolveOptions
		want    []string
	}{
		{"nothing set", ws("", ""), ws("", ""), config.ResolveOptions{}, nil},
		{"env only: it is the fallback, no warning", ws("", ""), ws("", ""),
			config.ResolveOptions{WarehouseID: "e", Host: litEnvHost}, nil},
		{"env equals the file", ws("https://p", "p"), ws("", ""),
			config.ResolveOptions{WarehouseID: "p", Host: "https://p"}, nil},
		{"files only", ws("https://p", "p"), ws("https://t", "t"), config.ResolveOptions{}, nil},
		{"warehouse: target file differs from env", ws("", "p"), ws("", "t"),
			config.ResolveOptions{WarehouseID: "e", TargetName: litDev},
			[]string{`workspace.warehouse_id "t" from targets/dev.yml is used; DATABRICKS_WAREHOUSE_ID ("e") is ignored: a value in a project file wins over the environment`}},
		{"warehouse: project file differs from env", ws("", "p"), ws("", ""),
			config.ResolveOptions{WarehouseID: "e", TargetName: litDev},
			[]string{`workspace.warehouse_id "p" from ucat.yml is used; DATABRICKS_WAREHOUSE_ID ("e") is ignored: a value in a project file wins over the environment`}},
		{"warehouse: env equals the project value but the target differs", ws("", "p"), ws("", "t"),
			config.ResolveOptions{WarehouseID: "p", TargetName: litDev},
			[]string{`workspace.warehouse_id "t" from targets/dev.yml is used; DATABRICKS_WAREHOUSE_ID ("p") is ignored: a value in a project file wins over the environment`}},
		{"host: file differs from env", ws("https://p", ""), ws("https://t", ""),
			config.ResolveOptions{Host: litEnvHost, TargetName: litProd},
			[]string{`workspace.host "https://t" from targets/prod.yml is used; DATABRICKS_HOST ("https://e") is ignored: a value in a project file wins over the environment`}},
		{"both differ: warehouse first, then host", ws("https://p", "p"), ws("", ""),
			config.ResolveOptions{Host: litEnvHost, WarehouseID: "e"},
			[]string{
				`workspace.warehouse_id "p" from ucat.yml is used; DATABRICKS_WAREHOUSE_ID ("e") is ignored: a value in a project file wins over the environment`,
				`workspace.host "https://p" from ucat.yml is used; DATABRICKS_HOST ("https://e") is ignored: a value in a project file wins over the environment`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := config.EnvOverrideWarnings(config.ProjectConfig{Workspace: tc.project}, config.TargetConfig{Workspace: tc.target}, tc.opts)
			var got []string
			for _, d := range diags {
				assert.Equal(t, diag.Warning, d.Severity)
				got = append(got, d.Summary)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// Resolve must not read the environment itself: only the option counts.
func TestResolve_ignoresEnvironment(t *testing.T) {
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "env-warehouse-id")
	proj := config.ProjectConfig{Workspace: config.WorkspaceConfig{WarehouseID: litProjectWh}}

	resolved := config.Resolve(proj, nil, config.TargetConfig{}, nil, config.ResolveOptions{})
	assert.Equal(t, "project-wh", resolved.WarehouseID)
}

func TestResolve_noTargetFollowsHasTarget(t *testing.T) {
	without := config.Resolve(config.ProjectConfig{}, nil, config.TargetConfig{}, nil, config.ResolveOptions{})
	assert.True(t, without.NoTarget)
	with := config.Resolve(config.ProjectConfig{}, nil, config.TargetConfig{}, nil, config.ResolveOptions{HasTarget: true})
	assert.False(t, with.NoTarget)
}

func TestResolve_warehouseFromTarget(t *testing.T) {
	proj := config.ProjectConfig{Workspace: config.WorkspaceConfig{WarehouseID: "project-wh"}}
	target := config.TargetConfig{Workspace: config.WorkspaceConfig{WarehouseID: litTargetWH}}

	resolved := config.Resolve(proj, []config.CatalogConfig{mainCatalog()}, target, nil, config.ResolveOptions{})
	assert.Equal(t, litTargetWH, resolved.WarehouseID)
}

func TestResolve_warehouseFromProject(t *testing.T) {
	proj := config.ProjectConfig{Workspace: config.WorkspaceConfig{WarehouseID: "project-wh"}}
	target := config.TargetConfig{}

	resolved := config.Resolve(proj, []config.CatalogConfig{mainCatalog()}, target, nil, config.ResolveOptions{})
	assert.Equal(t, "project-wh", resolved.WarehouseID)
}

func TestResolve_warehouseMissing(t *testing.T) {
	proj := config.ProjectConfig{}
	target := config.TargetConfig{}

	resolved := config.Resolve(proj, []config.CatalogConfig{mainCatalog()}, target, nil, config.ResolveOptions{})
	assert.Empty(t, resolved.WarehouseID)
}

func TestResolve_catalogUCNameOverride(t *testing.T) {
	target := config.TargetConfig{
		Catalogs: map[string]config.CatalogConfig{
			litMain: {UCName: litDev},
		},
	}

	resolved := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{mainCatalog()}, target, nil, config.ResolveOptions{})

	// all schemas from "main" catalog should now show "dev" as UC catalog
	for _, s := range resolved.Schemas {
		if s.LogicalCatalog == litMain {
			assert.Equal(t, litDev, s.UCCatalog)
		}
	}
}

func TestResolve_schemaUCNameDefault(t *testing.T) {
	// "gold" schema has empty UCName in mainCatalog() - should default to key "gold"
	resolved := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{mainCatalog()}, config.TargetConfig{}, nil, config.ResolveOptions{})

	var goldSchema *config.ResolvedSchema
	for i, s := range resolved.Schemas {
		if s.LogicalSchema == litGold {
			goldSchema = &resolved.Schemas[i]
		}
	}
	require.NotNil(t, goldSchema, "gold schema not found in resolved schemas")
	assert.Equal(t, "gold", goldSchema.UCSchema)
}

func TestResolve_schemaUCNameOverride(t *testing.T) {
	target := config.TargetConfig{
		Catalogs: map[string]config.CatalogConfig{
			litMain: {
				Schemas: map[string]config.SchemaConfig{
					litSilver2: {UCName: "silver_dev"},
				},
			},
		},
	}

	resolved := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{mainCatalog()}, target, nil, config.ResolveOptions{})

	var silverSchema *config.ResolvedSchema
	for i, s := range resolved.Schemas {
		if s.LogicalSchema == litSilver2 {
			silverSchema = &resolved.Schemas[i]
		}
	}
	require.NotNil(t, silverSchema)
	assert.Equal(t, "silver_dev", silverSchema.UCSchema)
}

func TestResolve_tableResolution(t *testing.T) {
	tables := []config.Table{silverTable()}
	resolved := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{mainCatalog()}, config.TargetConfig{}, tables, config.ResolveOptions{})

	require.Len(t, resolved.Tables, 1)
	assert.Equal(t, "prod", resolved.Tables[0].ResolvedCatalog)
	assert.Equal(t, "silver", resolved.Tables[0].ResolvedSchema)
}

func TestResolve_unknownSchemaRef(t *testing.T) {
	tables := []config.Table{{Name: "orphan", Schema: "nonexistent"}}
	resolved := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{mainCatalog()}, config.TargetConfig{}, tables, config.ResolveOptions{})
	// unknown schema ref is not an error from resolver - validator catches it
	require.Len(t, resolved.Tables, 1)
	assert.Empty(t, resolved.Tables[0].ResolvedCatalog)
	assert.Empty(t, resolved.Tables[0].ResolvedSchema)
}

func TestResolve_workspaceHost(t *testing.T) {
	proj := config.ProjectConfig{Workspace: config.WorkspaceConfig{Host: "https://proj.azuredatabricks.net"}}
	target := config.TargetConfig{Workspace: config.WorkspaceConfig{Host: "https://target.azuredatabricks.net"}}

	resolved := config.Resolve(proj, nil, target, nil, config.ResolveOptions{})
	assert.Equal(t, "https://target.azuredatabricks.net", resolved.WorkspaceHost)
}

func TestResolve_schemaSourceFile(t *testing.T) {
	cats := []config.CatalogConfig{
		{Name: litMain, UCName: litProd, SourceFile: pathCatalogMain,
			Schemas: map[string]config.SchemaConfig{litSilver2: {}}},
		{Name: "raw", UCName: "raw_prod", SourceFile: pathCatalogsRawYml,
			Schemas: map[string]config.SchemaConfig{litLanding: {}}},
	}
	rc := config.Resolve(config.ProjectConfig{}, cats, config.TargetConfig{}, nil, config.ResolveOptions{})
	got := map[string]string{}
	for _, rs := range rc.Schemas {
		got[rs.LogicalSchema] = rs.SourceFile
	}
	assert.Equal(t, map[string]string{litSilver2: pathCatalogMain, "landing": "catalogs/raw.yml"}, got)
}

func TestResolve_schemaSourceFile_targetOnlyCatalogEmpty(t *testing.T) {
	target := config.TargetConfig{Catalogs: map[string]config.CatalogConfig{
		litExtra: {UCName: "x", Schemas: map[string]config.SchemaConfig{"s": {}}},
	}}
	rc := config.Resolve(config.ProjectConfig{}, nil, target, nil, config.ResolveOptions{})
	require.Len(t, rc.Schemas, 1)
	assert.Empty(t, rc.Schemas[0].SourceFile)
}

func TestResolve_schemasOrderDeterministic(t *testing.T) {
	cats := []config.CatalogConfig{
		{Name: litMain, UCName: litProd, SourceFile: "catalogs/main.yml",
			Schemas: map[string]config.SchemaConfig{litSilver2: {}, litGold: {}}},
		{Name: "raw", UCName: "raw_prod", SourceFile: "catalogs/raw.yml",
			Schemas: map[string]config.SchemaConfig{"landing": {}, "stage": {}}},
	}
	first := config.Resolve(config.ProjectConfig{}, cats, config.TargetConfig{}, nil, config.ResolveOptions{})
	for i := 0; i < 20; i++ {
		rc := config.Resolve(config.ProjectConfig{}, cats, config.TargetConfig{}, nil, config.ResolveOptions{})
		assert.Equal(t, first.Schemas, rc.Schemas)
	}
}

func TestResolvedConfig_SchemaByLogical(t *testing.T) {
	rc := config.ResolvedConfig{Schemas: []config.ResolvedSchema{
		{LogicalCatalog: "a", LogicalSchema: litSilver2, UCCatalog: litProd, UCSchema: "silver_uc"},
		{LogicalCatalog: "b", LogicalSchema: litSilver2, UCCatalog: litDev, UCSchema: "silver_dev"},
		{LogicalCatalog: "a", LogicalSchema: "Gold", UCCatalog: litProd, UCSchema: litGold},
		{LogicalCatalog: "a", LogicalSchema: "", UCCatalog: litProd, UCSchema: litEmpty},
	}}

	rs, ok := rc.SchemaByLogical("silver")
	require.True(t, ok)
	assert.Equal(t, "silver_uc", rs.UCSchema, "first match wins")

	rs, ok = rc.SchemaByLogical("Gold")
	require.True(t, ok)
	assert.Equal(t, "gold", rs.UCSchema)

	_, ok = rc.SchemaByLogical("gold")
	assert.False(t, ok, "logical names match exactly; case collisions are the validator's job")

	rs, ok = rc.SchemaByLogical("")
	require.True(t, ok, "an empty name matches an empty logical schema, like the validator's exact comparison")
	assert.Equal(t, "empty", rs.UCSchema)

	_, ok = rc.SchemaByLogical("nope")
	assert.False(t, ok)
	_, ok = config.ResolvedConfig{}.SchemaByLogical("silver")
	assert.False(t, ok)
}

func TestResolve_doesNotMutateInputCatalog(t *testing.T) {
	cat := mainCatalog()
	target := config.TargetConfig{Catalogs: map[string]config.CatalogConfig{
		litMain: {Schemas: map[string]config.SchemaConfig{
			litSilver2: {UCName: litSilverOverride},
			"extra":    {},
		}},
	}}

	resolved := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{cat}, target, nil, config.ResolveOptions{})

	assert.Len(t, resolved.Schemas, 3)
	assert.Equal(t, map[string]config.SchemaConfig{litSilver2: {UCName: litSilver2}, litGold: {}}, cat.Schemas)
}

func TestResolve_doesNotMutateInputCatalogSchemas(t *testing.T) {
	cat := config.CatalogConfig{
		Name:   litMain,
		UCName: litProd,
		Schemas: map[string]config.SchemaConfig{
			litSilver2: {UCName: litSilver2},
		},
	}
	target := config.TargetConfig{Catalogs: map[string]config.CatalogConfig{
		"main": {Schemas: map[string]config.SchemaConfig{
			litSilver2: {UCName: "silver_override"},
			"extra":    {UCName: "extra_uc"},
		}},
	}}

	resolved := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{cat}, target, nil, config.ResolveOptions{})

	assert.Equal(t, map[string]config.SchemaConfig{"silver": {UCName: "silver"}}, cat.Schemas)
	var ucNames []string
	for _, s := range resolved.Schemas {
		ucNames = append(ucNames, s.UCSchema)
	}
	assert.ElementsMatch(t, []string{"silver_override", "extra_uc"}, ucNames)
}
