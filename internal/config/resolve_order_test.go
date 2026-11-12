package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
)

// Two logical catalogs on one physical catalog: the schemas are ordered by UC names, then by
// the logical catalog name, whatever the order of the input.
func TestResolve_schemasTieBreakByLogicalCatalog(t *testing.T) {
	mk := func(name string) config.CatalogConfig {
		return config.CatalogConfig{Name: name, UCName: litProd, Schemas: map[string]config.SchemaConfig{"s_" + name: {UCName: "same"}}}
	}
	for _, cats := range [][]config.CatalogConfig{{mk("a"), mk("b")}, {mk("b"), mk("a")}} {
		rc := config.Resolve(config.ProjectConfig{}, cats, config.TargetConfig{}, nil, config.ResolveOptions{})
		require.Len(t, rc.Schemas, 2)
		assert.Equal(t, "a", rc.Schemas[0].LogicalCatalog)
		assert.Equal(t, "b", rc.Schemas[1].LogicalCatalog)
	}
}

// Resolve returns resolved copies: the tables the caller passed keep empty ResolvedCatalog/Schema.
func TestResolve_doesNotModifyInputTables(t *testing.T) {
	in := []config.Table{silverTable()}
	rc := config.Resolve(config.ProjectConfig{}, []config.CatalogConfig{mainCatalog()}, config.TargetConfig{}, in, config.ResolveOptions{})
	require.Len(t, rc.Tables, 1)
	assert.Equal(t, "prod", rc.Tables[0].ResolvedCatalog)
	assert.Empty(t, in[0].ResolvedCatalog)
	assert.Empty(t, in[0].ResolvedSchema)
}

// Several collisions in one catalog are reported sorted by physical schema name.
func TestValidateLocal_physicalCollisionsSortedBySchemaInOneCatalog(t *testing.T) {
	cfg := schemaCfg(
		rs("x", "a", "main", "zz", "catalogs/x.yml"),
		rs("y", "b", "main", "zz", "catalogs/y.yml"),
		rs("p", "c", "main", "aa", "catalogs/p.yml"),
		rs("q", "d", "main", "aa", "catalogs/q.yml"),
	)
	diags := config.ValidateLocal(cfg)
	var errs []string
	for _, d := range diags {
		errs = append(errs, d.Summary)
	}
	require.Len(t, errs, 2, "diags: %v", diags)
	assert.Contains(t, errs[0], "main.aa")
	assert.Contains(t, errs[1], "main.zz")
}
