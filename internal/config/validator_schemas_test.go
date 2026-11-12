package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
)

// --- duplicate schemas, catalog name, column rename_from, clustering, columns ---

func errorCount(diags diag.Diagnostics) int {
	n := 0
	for _, d := range diags {
		if d.Severity == diag.Error {
			n++
		}
	}
	return n
}

func schemaCfg(schemas ...config.ResolvedSchema) config.ResolvedConfig {
	return config.ResolvedConfig{WarehouseID: "wh", Schemas: schemas}
}

func rs(cat, schema, ucCat, ucSchema, file string) config.ResolvedSchema {
	return config.ResolvedSchema{LogicalCatalog: cat, LogicalSchema: schema, UCCatalog: ucCat, UCSchema: ucSchema, SourceFile: file}
}

func tableWithCols(name string, cols ...config.Column) config.Table {
	tbl := resolvedTable(name)
	tbl.Columns = cols
	return tbl
}

func TestValidateLocal_duplicateLogicalSchemaAcrossCatalogs(t *testing.T) {
	cfg := schemaCfg(
		rs("common", "silver", "common", "silver", "catalogs/common.yml"),
		rs("main", "silver", "prod", "silver", "catalogs/main.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	for _, frag := range []string{litSilver2, litMain, "common", pathCatalogMain, "catalogs/common.yml"} {
		assert.True(t, hasErrorContaining(diags, frag), "missing %q in %v", frag, diags)
	}
}

func TestValidateLocal_duplicateLogicalSchemaThreeCatalogsOneDiag(t *testing.T) {
	cfg := schemaCfg(
		rs("a", "silver", "a", "silver", "catalogs/a.yml"),
		rs("b", "silver", "b", "silver", "catalogs/b.yml"),
		rs("c", "silver", "c", "silver", "catalogs/c.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	for _, frag := range []string{pathCatalogsAYml, pathCatalogsBYml, "catalogs/c.yml"} {
		assert.True(t, hasErrorContaining(diags, frag), "missing %q in %v", frag, diags)
	}
}

func TestValidateLocal_duplicateLogicalSchemaTargetOnlyCatalog(t *testing.T) {
	cfg := schemaCfg(
		rs("extra", "silver", "extra", "silver", ""), // catalog defined only in a target
		rs("main", "silver", "prod", "silver", "catalogs/main.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "extra"), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "main"), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "catalogs/main.yml"), "diags: %v", diags)
}

func TestValidateLocal_sameSchemaInOneCatalogNotDuplicate(t *testing.T) {
	cfg := schemaCfg(
		rs("main", "silver", "prod", "silver", "catalogs/main.yml"),
		rs("main", "gold", "prod", "gold", "catalogs/main.yml"),
	)
	assert.Zero(t, errorCount(config.ValidateLocal(cfg)))
}

func TestValidateLocal_differentLogicalSchemasSameUCNameIsError(t *testing.T) {
	cfg := schemaCfg(
		rs("common", "silver_common", "prod", "silver", "catalogs/common.yml"),
		rs("main", "silver", "prod", "silver", "catalogs/main.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "physical schema prod.silver"), "diags: %v", diags)
}

func TestValidateLocal_sameTableNameInDifferentSchemasOK(t *testing.T) {
	cfg := schemaCfg(
		rs("main", "silver", "prod", "silver", "catalogs/main.yml"),
		rs("main", "gold", "prod", "gold", "catalogs/main.yml"),
	)
	a := resolvedTable("orders")
	b := resolvedTable("orders")
	b.Schema, b.ResolvedSchema = litGold, litGold
	cfg.Tables = []config.Table{a, b}
	diags := config.ValidateLocal(cfg)
	assert.Zero(t, errorCount(diags), "diags: %v", diags)
}

func TestValidateLocal_emptyCatalogName(t *testing.T) {
	cfg := schemaCfg(rs("", "silver", "prod", "silver", "catalogs/x.yml"))
	diags := config.ValidateLocal(cfg)
	assert.True(t, hasErrorContaining(diags, "catalog name is empty"), "diags: %v", diags)

	assert.False(t, hasErrorContaining(config.ValidateLocal(resolvedConfig(nil)), "catalog name is empty"))
}

func TestValidateLocal_columnRenameFromDuplicate(t *testing.T) {
	tbl := tableWithCols("orders",
		config.Column{Name: "x", Type: typINT, RenameFrom: "a"},
		config.Column{Name: "y", Type: typINT, RenameFrom: "A"},
	)
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	for _, frag := range []string{"rename_from", `"x"`, `"y"`} {
		assert.True(t, hasErrorContaining(diags, frag), "missing %q in %v", frag, diags)
	}
}

func TestValidateLocal_columnRenameSwap(t *testing.T) {
	tbl := tableWithCols("orders",
		config.Column{Name: "a", Type: typINT, RenameFrom: "b"},
		config.Column{Name: "b", Type: typINT, RenameFrom: "a"},
	)
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.GreaterOrEqual(t, errorCount(diags), 1, "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "rename_from"), "diags: %v", diags)
}

func TestValidateLocal_columnRenameChain(t *testing.T) {
	tbl := tableWithCols("orders",
		config.Column{Name: "a", Type: typINT, RenameFrom: "b"},
		config.Column{Name: "b", Type: typINT, RenameFrom: "c"},
	)
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.GreaterOrEqual(t, errorCount(diags), 1, "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "rename_from"), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, `"a"`), "diags: %v", diags)
}

func TestValidateLocal_columnRenameNoFalsePositives(t *testing.T) {
	cases := map[string][]config.Column{
		"independent renames": {
			{Name: "a", Type: typINT, RenameFrom: "x"},
			{Name: "b", Type: typINT, RenameFrom: "y"},
		},
		"case-only rename": {
			{Name: "A", Type: typINT, RenameFrom: "a"},
		},
		"single rename next to plain column": {
			{Name: "id", Type: typBIGINT},
			{Name: "b", Type: typINT, RenameFrom: "a"},
		},
	}
	for name, cols := range cases {
		t.Run(name, func(t *testing.T) {
			diags := config.ValidateLocal(resolvedConfig([]config.Table{tableWithCols("orders", cols...)}))
			assert.Zero(t, errorCount(diags), "diags: %v", diags)
		})
	}
}

func TestValidateLocal_columnRenameDuplicateAcrossTablesOK(t *testing.T) {
	a := tableWithCols("t1", config.Column{Name: "b", Type: typINT, RenameFrom: "a"})
	b := tableWithCols("t2", config.Column{Name: "b", Type: typINT, RenameFrom: "a"})
	diags := config.ValidateLocal(resolvedConfig([]config.Table{a, b}))
	assert.Zero(t, errorCount(diags), "diags: %v", diags)
}

func TestValidateLocal_clusteringEmptyIsError(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Clustering = &config.Clustering{}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "clustering"), "diags: %v", diags)
}

func TestValidateLocal_clusteringValidForms(t *testing.T) {
	cases := map[string]*config.Clustering{
		"columns": {Columns: []string{"id"}},
		"auto":    {Auto: true},
		"absent":  nil,
	}
	for name, cl := range cases {
		t.Run(name, func(t *testing.T) {
			tbl := resolvedTable("orders")
			tbl.Clustering = cl
			diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
			assert.Zero(t, errorCount(diags), "diags: %v", diags)
		})
	}
}

func TestValidateLocal_columnsRequired(t *testing.T) {
	for name, cols := range map[string][]config.Column{"nil": nil, litEmpty: {}} {
		t.Run(name, func(t *testing.T) {
			tbl := resolvedTable("orders")
			tbl.Columns = cols
			diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
			assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
			assert.True(t, hasErrorContaining(diags, "columns"), "diags: %v", diags)
		})
	}
}

func TestValidateLocal_dropTableWithoutColumnsOK(t *testing.T) {
	tbl := resolvedTable("legacy")
	tbl.Columns = nil
	tbl.Drop = true
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.Zero(t, errorCount(diags), "diags: %v", diags)
}

// D: diagnostics order is identical across runs for a project with several violations.
func TestValidateLocal_diagnosticsOrderStable(t *testing.T) {
	build := func() config.ResolvedConfig {
		cfg := schemaCfg(
			rs("main", "silver", "prod", "silver", "catalogs/main.yml"),
			rs("common", "silver", "common", "silver", "catalogs/common.yml"),
			rs("", "gold", "prod", "gold", "catalogs/x.yml"),
		)
		swap := tableWithCols("swap",
			config.Column{Name: "a", Type: typINT, RenameFrom: "b"},
			config.Column{Name: "b", Type: typINT, RenameFrom: "a"},
		)
		dup := tableWithCols("dup",
			config.Column{Name: "x", Type: typINT, RenameFrom: "a"},
			config.Column{Name: "y", Type: typINT, RenameFrom: "a"},
		)
		cl := resolvedTable("cl")
		cl.Clustering = &config.Clustering{}
		nocols := resolvedTable("nocols")
		nocols.Columns = nil
		cfg.Tables = []config.Table{swap, dup, cl, nocols}
		return cfg
	}
	first := errorSummaries(config.ValidateLocal(build()))
	assert.GreaterOrEqual(t, len(first), 6, "diags: %v", first)
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, errorSummaries(config.ValidateLocal(build())), "run %d", i)
	}
}
