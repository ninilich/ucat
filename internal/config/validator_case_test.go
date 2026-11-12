package config_test

import (
	"fmt"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
)

// --- name case rules ---

func countErrorsWithSummary(diags diag.Diagnostics, summary string) int {
	n := 0
	for _, d := range diags {
		if d.Severity == diag.Error && d.Summary == summary {
			n++
		}
	}
	return n
}

func TestValidateLocal_uppercaseTableName(t *testing.T) {
	for _, name := range []string{"Orders", "ORDERS", "orders_V2", "ordersÜ", "Ünits"} {
		t.Run(name, func(t *testing.T) {
			diags := config.ValidateLocal(resolvedConfig([]config.Table{resolvedTable(name)}))
			want := fmt.Sprintf("table name %q must be lowercase: Unity Catalog stores table names in lowercase", name)
			assert.Equal(t, 1, countErrorsWithSummary(diags, want), "diags: %v", diags)
		})
	}
}

func TestValidateLocal_uppercaseTableRenameFrom(t *testing.T) {
	for _, old := range []string{"Orders", "old_ORDERS"} {
		tbl := renamingTable("orders_v2", old)
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		want := fmt.Sprintf("table %q: rename_from %q must be lowercase: Unity Catalog stores table names in lowercase", "orders_v2", old)
		assert.Equal(t, 1, countErrorsWithSummary(diags, want), "diags: %v", diags)
	}
}

func TestValidateLocal_uppercaseResolvedSchemaReportedOnce(t *testing.T) {
	var tables []config.Table
	for _, n := range []string{"a", "b", "c"} {
		tbl := resolvedTable(n)
		tbl.ResolvedSchema = litSilver
		tables = append(tables, tbl)
	}
	diags := config.ValidateLocal(resolvedConfig(tables))
	want := `schema "Silver" must be lowercase: Unity Catalog stores schema names in lowercase`
	assert.Equal(t, 1, countErrorsWithSummary(diags, want), "diags: %v", diags)
}

func TestValidateLocal_uppercaseResolvedCatalogReportedOnce(t *testing.T) {
	var tables []config.Table
	for _, n := range []string{"a", "b", "c"} {
		tbl := resolvedTable(n)
		tbl.ResolvedCatalog = litProd2
		tables = append(tables, tbl)
	}
	diags := config.ValidateLocal(resolvedConfig(tables))
	want := `catalog "Prod" must be lowercase: Unity Catalog stores catalog names in lowercase`
	assert.Equal(t, 1, countErrorsWithSummary(diags, want), "diags: %v", diags)
}

func TestValidateLocal_twoDistinctUppercaseSchemasReportedSeparately(t *testing.T) {
	a := resolvedTable("a")
	a.ResolvedSchema = litSilver
	b := resolvedTable("b")
	b.ResolvedSchema = "GOLD"
	diags := config.ValidateLocal(resolvedConfig([]config.Table{a, b}))
	assert.True(t, hasErrorContaining(diags, `schema "Silver" must be lowercase`), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, `schema "GOLD" must be lowercase`), "diags: %v", diags)
}

func TestValidateLocal_uppercaseLogicalSchemaKeyWithLowercaseUCNameOK(t *testing.T) {
	cfg := config.ResolvedConfig{
		WarehouseID: "wh",
		Schemas: []config.ResolvedSchema{
			{LogicalCatalog: "Main", LogicalSchema: litSilver, UCCatalog: litProd, UCSchema: litSilver2},
		},
	}
	tbl := resolvedTable("orders")
	tbl.Schema = "Silver"
	cfg.Tables = []config.Table{tbl}
	diags := config.ValidateLocal(cfg)
	assert.False(t, diags.HasError(), "diags: %v", diags)
	assert.False(t, hasErrorContaining(diags, "must be lowercase"))
}

func TestValidateLocal_lowercaseNamesNoCaseError(t *testing.T) {
	tbl := resolvedTable("orders_2024_v1")
	tbl.RenameFrom = "old_orders_1"
	tbl.ResolvedCatalog = "prod_1"
	tbl.ResolvedSchema = "silver_2"
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, hasErrorContaining(diags, "must be lowercase"), "diags: %v", diags)
}

func TestValidateLocal_caseRuleEmptyValuesNoPanic(t *testing.T) {
	empty := config.Table{}
	noRes := resolvedTable("a")
	noRes.ResolvedCatalog = ""
	noRes.ResolvedSchema = ""
	emptyRename := renamingTable("b", "")
	assert.NotPanics(t, func() {
		diags := config.ValidateLocal(resolvedConfig([]config.Table{empty, noRes, emptyRename}))
		assert.False(t, hasErrorContaining(diags, "must be lowercase"), "diags: %v", diags)
	})
}

func TestValidateLocal_mixedCaseColumnNamesOK(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: "userId", Type: typBIGINT}, {Name: "OrderDate", Type: typDATE}, {Name: "ID2", Type: typINT}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.Empty(t, diags)
}

func TestValidateLocal_duplicateColumnCaseInsensitive(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: "id", Type: typBIGINT}, {Name: "Id", Type: typBIGINT}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	want := `table "orders": duplicate column "Id" (column names are case-insensitive)`
	assert.Equal(t, 1, countErrorsWithSummary(diags, want), "diags: %v", diags)
}

func TestValidateLocal_duplicateColumnNonASCII(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: "ÜBER", Type: typINT}, {Name: "über", Type: typINT}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.True(t, hasErrorContaining(diags, "duplicate column"), "diags: %v", diags)
}

func TestValidateLocal_distinctColumnsNoDuplicateError(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: "id", Type: typBIGINT}, {Name: "id2", Type: "BIGINT"}, {Name: "Id_", Type: "BIGINT"}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, hasErrorContaining(diags, "duplicate column"), "diags: %v", diags)

	// same column name in different tables is fine
	a, b := resolvedTable("a"), resolvedTable("b")
	a.Columns = []config.Column{{Name: "Id", Type: typINT}}
	b.Columns = []config.Column{{Name: "id", Type: typINT}}
	diags = config.ValidateLocal(resolvedConfig([]config.Table{a, b}))
	assert.False(t, hasErrorContaining(diags, "duplicate column"), "diags: %v", diags)
}

func TestValidateLocal_columnRenameFromSameNameCaseInsensitive(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: litStatus, Type: typSTRING, RenameFrom: "STATUS"}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, diags.HasError(), "same-name rename_from stays a warning: %v", diags)
	assert.True(t, hasWarningContaining(diags, "rename_from is same as name"), "diags: %v", diags)

	tbl.Columns = []config.Column{{Name: "status", Type: typSTRING, RenameFrom: "state"}}
	diags = config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, hasWarningContaining(diags, "rename_from is same as name"), "diags: %v", diags)
}
