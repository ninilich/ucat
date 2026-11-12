package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolvedConfig builds a minimal ResolvedConfig with one schema for tests.
func resolvedConfig(tables []config.Table) config.ResolvedConfig {
	return config.ResolvedConfig{
		WarehouseID: "wh",
		Schemas: []config.ResolvedSchema{
			{LogicalCatalog: litMain, LogicalSchema: litSilver2, UCCatalog: litProd, UCSchema: litSilver2},
		},
		Tables: tables,
	}
}

// resolvedTable returns a valid fully-resolved table.
func resolvedTable(name string) config.Table {
	return config.Table{
		Name:            name,
		Schema:          litSilver2,
		ResolvedCatalog: litProd,
		ResolvedSchema:  litSilver2,
		Columns: []config.Column{
			{Name: "id", Type: typBIGINT},
		},
	}
}

func hasErrorContaining(diags diag.Diagnostics, substr string) bool {
	for _, d := range diags {
		if d.Severity == diag.Error && containsStr(d.Summary, substr) {
			return true
		}
	}
	return false
}

func hasWarningContaining(diags diag.Diagnostics, substr string) bool {
	for _, d := range diags {
		if d.Severity == diag.Warning && containsStr(d.Summary, substr) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func TestValidateLocal_noErrors(t *testing.T) {
	tables := []config.Table{resolvedTable("orders")}
	diags := config.ValidateLocal(resolvedConfig(tables))
	assert.False(t, diags.HasError())
	assert.Empty(t, diags)
}

func TestValidateLocal_unknownSchemaRef(t *testing.T) {
	tbl := config.Table{
		Name:   "orphan",
		Schema: "nonexistent",
		// ResolvedCatalog and ResolvedSchema are empty - resolver couldn't resolve
	}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.True(t, diags.HasError())
}

func TestValidateLocal_duplicateTable(t *testing.T) {
	t1 := resolvedTable("orders")
	t2 := resolvedTable("orders") // same name, same resolved catalog+schema
	diags := config.ValidateLocal(resolvedConfig([]config.Table{t1, t2}))
	assert.True(t, diags.HasError())
}

func TestValidateLocal_clusteringAndPartitioning(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Clustering = &config.Clustering{Columns: []string{"id"}}
	tbl.Partitioning = &config.Partitioning{Columns: []string{litStatus}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.True(t, diags.HasError())
}

func TestValidateLocal_clusteringAutoAndColumns(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Clustering = &config.Clustering{Auto: true, Columns: []string{"id"}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.True(t, diags.HasError())
}

func TestValidateLocal_tableNameRequired(t *testing.T) {
	tbl := resolvedTable("")
	tbl.Name = ""
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.True(t, diags.HasError())
}

func TestValidateLocal_columnNameRequired(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: "", Type: typSTRING}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.True(t, diags.HasError())
}

func TestValidateLocal_validColumnTypes(t *testing.T) {
	validTypes := []string{
		typSTRING, "string", typBIGINT, typINT, typTINYINT, typSMALLINT,
		typFLOAT, typDOUBLE, typBOOLEAN, typDATE, typTIMESTAMP, litTIMESTAMPNTZ,
		typBINARY, typVARIANT,
		typDECIMAL1022, "decimal(18,4)",
		typARRAYSTRING, typMAPSTRINGINT,
		typSTRUCTIdBIGINTNameSTRING,
	}
	for _, colType := range validTypes {
		tbl := resolvedTable("orders")
		tbl.Columns = []config.Column{{Name: "col", Type: colType}}
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		assert.False(t, diags.HasError(), "type %q should be valid", colType)
	}
}

func TestValidateLocal_invalidColumnType(t *testing.T) {
	invalidTypes := []string{"NVARCHAR", "VARCHAR(100)", "NUMBER", typTEXT, typLONG, typINTEGER, "CHAR(5)"}
	for _, colType := range invalidTypes {
		tbl := resolvedTable("orders")
		tbl.Columns = []config.Column{{Name: "col", Type: colType}}
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		assert.True(t, diags.HasError(), "type %q should be invalid", colType)
	}
}

func TestValidateLocal_renameFromSameName(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.RenameFrom = litOrders // same as Name
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, diags.HasError(), "rename_from == name is a warning, not error")
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.Warning, diags[0].Severity)
}

func TestValidateLocal_renameFromEmptyName(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Name = ""
	tbl.RenameFrom = "old_orders"
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.True(t, diags.HasError())
}

func TestValidateLocal_columnRenameFromSameName(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{
		{Name: litStatus, Type: typSTRING, RenameFrom: "status"},
	}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, diags.HasError(), "column rename_from == name is a warning")
	// should have a warning
	hasWarn := false
	for _, d := range diags {
		if d.Severity == diag.Warning {
			hasWarn = true
		}
	}
	assert.True(t, hasWarn)
}

func TestIsValidColumnType(t *testing.T) {
	// Valid exact types (case-insensitive)
	exact := []string{
		typTINYINT, "tinyint", typSMALLINT, typINT, typBIGINT,
		"FLOAT", "DOUBLE", typBOOLEAN, typSTRING, "DATE",
		"TIMESTAMP", "TIMESTAMP_NTZ", "BINARY", typVARIANT,
	}
	for _, typ := range exact {
		tbl := resolvedTable("t")
		tbl.Columns = []config.Column{{Name: "c", Type: typ}}
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		assert.False(t, diags.HasError(), "exact type %q must be valid", typ)
	}

	// Valid prefix types
	prefix := []string{
		typDECIMAL1022, "DECIMAL(38,10)", "decimal(5,0)",
		"ARRAY<STRING>", "ARRAY<BIGINT>", typArrayString,
		"MAP<STRING,INT>", "MAP<STRING,ARRAY<BIGINT>>",
		"STRUCT<id:BIGINT,name:STRING>",
	}
	for _, typ := range prefix {
		tbl := resolvedTable("t")
		tbl.Columns = []config.Column{{Name: "c", Type: typ}}
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		assert.False(t, diags.HasError(), "prefix type %q must be valid", typ)
	}

	// Invalid types
	invalid := []string{"NVARCHAR", "VARCHAR(100)", "NUMBER", "TEXT", "LONG", "CHAR", "INTEGER", "REAL", ""}
	for _, typ := range invalid {
		tbl := resolvedTable("t")
		tbl.Columns = []config.Column{{Name: "c", Type: typ}}
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		assert.True(t, diags.HasError(), "type %q must be invalid", typ)
	}
}

// --- canonical column types only ---

func validateColType(typ string) diag.Diagnostics {
	tbl := resolvedTable("t")
	tbl.Columns = []config.Column{{Name: "c", Type: typ}}
	return config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
}

func TestValidateLocal_ColumnTypes_Canonical(t *testing.T) {
	valid := []string{
		"TINYINT", "SMALLINT", typINT, typBIGINT, "FLOAT", "DOUBLE", "BOOLEAN", typSTRING,
		"BINARY", "DATE", "TIMESTAMP", "TIMESTAMP_NTZ", "VARIANT",
		"tinyint", "Timestamp_Ntz", "variant", "  STRING  ",
		typDECIMAL, typDECIMAL1022, "ARRAY<INT>", "array<string>", "MAP<STRING,INT>",
		"STRUCT<a:INT,b:STRING>", typGEOGRAPHY4326, "GEOMETRY(ANY)",
		"INTERVAL YEAR TO MONTH", typARRAYSTRUCTAINT,
		"INTERVAL DAY TO SECOND", "INTERVAL  DAY", "ARRAY<INTERVAL DAY TO SECOND>",
		"DECIMAL(10, 2)", typDECIMAL102, "STRUCT<a: INT, b: STRING>", "ARRAY< STRING >",
		"MAP<STRING, INT>", "STRUCT<a: INT, b: ARRAY<STRING>>", "\tSTRING\t",
		// what UC returns as type_text
		"struct<a:int,b:string>", "decimal(10,2)", "map<string,array<int>>",
		litIntervalDayToSecond, "array<struct<a:int,b:varchar(5)>>",
	}
	for _, typ := range valid {
		assert.False(t, validateColType(typ).HasError(), "type %q must be valid", typ)
	}
}

func TestValidateLocal_ColumnTypes_Unsupported(t *testing.T) {
	invalid := []string{
		"TEXT", "VARCHAR(10)", "CHAR(5)", "INTEGER", "LONG", "VOID", "OBJECT", "FILE", "TIME",
		"DECIMALFOO", "ARRAY", "garbage", "!!!", "'; DROP TABLE x; --",
	}
	for _, typ := range invalid {
		diags := validateColType(typ)
		require.True(t, diags.HasError(), "type %q must be invalid", typ)
		found := false
		for _, d := range diags {
			if d.Severity == diag.Error && containsStr(d.Summary, "is not supported") && containsStr(d.Summary, typ) {
				found = true
			}
		}
		assert.True(t, found, "type %q: error must contain 'is not supported' and the type; got %v", typ, diags)
	}
}

// --- names with dots or whitespace ---

func TestValidateLocal_NameWarnings(t *testing.T) {
	tblWarn := func(name string) string { return fmt.Sprintf("table %q: name contains a dot or whitespace", name) }
	colWarn := func(tbl, col string) string {
		return fmt.Sprintf("table %q, column %q: name contains a dot or whitespace", tbl, col)
	}
	count := func(diags diag.Diagnostics, summary string) int {
		n := 0
		for _, d := range diags {
			if d.Severity == diag.Warning && d.Summary == summary {
				n++
			}
		}
		return n
	}
	withCol := func(tblName, colName string) diag.Diagnostics {
		tbl := resolvedTable(tblName)
		tbl.Columns = []config.Column{{Name: colName, Type: "STRING"}}
		return config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	}

	t.Run("dot in table name", func(t *testing.T) {
		d := withCol("my.orders", "id")
		assert.Equal(t, 1, count(d, tblWarn("my.orders")))
		assert.False(t, d.HasError())
	})
	t.Run("space in table name", func(t *testing.T) {
		d := withCol("my orders", "id")
		assert.Equal(t, 1, count(d, tblWarn("my orders")))
		assert.False(t, d.HasError())
	})
	t.Run("dot in column name", func(t *testing.T) {
		d := withCol("orders", "a.b")
		assert.Equal(t, 1, count(d, colWarn("orders", "a.b")))
		assert.False(t, d.HasError())
	})
	t.Run("tab in column name", func(t *testing.T) {
		d := withCol("orders", "a\tb")
		assert.Equal(t, 1, count(d, colWarn("orders", "a\tb")))
		assert.False(t, d.HasError())
	})
	t.Run("clean names no warning", func(t *testing.T) {
		d := withCol("orders", "order_id")
		for _, x := range d {
			assert.NotContains(t, x.Summary, "name contains a dot or whitespace")
		}
	})
	t.Run("empty names do not trigger", func(t *testing.T) {
		tbl := resolvedTable("")
		tbl.Columns = []config.Column{{Name: "", Type: "STRING"}}
		d := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		for _, x := range d {
			assert.NotContains(t, x.Summary, "name contains a dot or whitespace")
		}
	})
	t.Run("dot and space gives one warning per name", func(t *testing.T) {
		d := withCol("my.or ders", "a. b")
		assert.Equal(t, 1, count(d, tblWarn("my.or ders")))
		assert.Equal(t, 1, count(d, colWarn("my.or ders", "a. b")))
		assert.False(t, d.HasError())
	})
}

func TestIsValidColumnType_MalformedParameterized(t *testing.T) {
	valid := func(typ string) bool {
		tbl := resolvedTable("t")
		tbl.Columns = []config.Column{{Name: "c", Type: typ}}
		return !config.ValidateLocal(resolvedConfig([]config.Table{tbl})).HasError()
	}
	for _, bad := range []string{
		"DECIMAL foo", "DECIMAL<x", "DECIMAL(", "GEOGRAPHY", "GEOMETRY()", "INTERVAL",
		"ARRAY<", "ARRAY<>", "STRUCT<", "MAP", "ARRAY<INT> foo", "DECIMALFOO",
	} {
		assert.False(t, valid(bad), bad)
	}
	for _, good := range []string{typDECIMAL102, "array <int>", "ARRAY<STRUCT<a:INT>>", "decimal(38,0)"} {
		assert.True(t, valid(good), good)
	}
}

func TestValidateLocal_NameWarnings_AnyWhitespace(t *testing.T) {
	for _, name := range []string{"a\nb", "a\u00a0b", "a\tb"} {
		tbl := resolvedTable("t")
		tbl.Columns = []config.Column{{Name: name, Type: typINT}}
		diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
		assert.False(t, diags.HasError(), "%q", name)
		found := false
		for _, d := range diags {
			if d.Severity == diag.Warning && strings.Contains(d.Summary, "name contains a dot or whitespace") {
				found = true
			}
		}
		assert.True(t, found, "%q", name)
	}
}

// --- duplicate rename_from ---

func renameTable(name, schema, from string) config.Table {
	t := resolvedTable(name)
	t.Schema = schema
	t.ResolvedSchema = schema
	t.RenameFrom = from
	return t
}

func twoSchemaConfig(tables []config.Table) config.ResolvedConfig {
	cfg := resolvedConfig(tables)
	cfg.Schemas = append(cfg.Schemas,
		config.ResolvedSchema{LogicalCatalog: litMain, LogicalSchema: litGold, UCCatalog: litProd, UCSchema: litGold})
	return cfg
}

func TestValidateLocal_DuplicateRenameFrom_SameSchema(t *testing.T) {
	diags := config.ValidateLocal(resolvedConfig([]config.Table{
		renameTable("a", "silver", "old"),
		renameTable("b", "silver", "old"),
	}))
	assert.True(t, hasErrorContaining(diags, `rename_from "old" is used by both table "a" and table "b"`), "diags: %v", diags)
}

func TestValidateLocal_DuplicateRenameFrom_DifferentSchemas(t *testing.T) {
	diags := config.ValidateLocal(twoSchemaConfig([]config.Table{
		renameTable("a", "silver", "old"),
		renameTable("b", "gold", "old"),
	}))
	assert.False(t, hasErrorContaining(diags, "rename_from"), "diags: %v", diags)
}

func TestValidateLocal_DuplicateRenameFrom_EmptyIgnored(t *testing.T) {
	diags := config.ValidateLocal(resolvedConfig([]config.Table{
		renameTable("a", "silver", ""),
		renameTable("b", "silver", ""),
	}))
	assert.False(t, diags.HasError(), "diags: %v", diags)
}

func TestValidateLocal_DuplicateRenameFrom_Three(t *testing.T) {
	diags := config.ValidateLocal(resolvedConfig([]config.Table{
		renameTable("a", "silver", "old"),
		renameTable("b", "silver", "old"),
		renameTable("c", "silver", "old"),
	}))
	assert.True(t, hasErrorContaining(diags, `rename_from "old"`), "diags: %v", diags)
}

// --- rename_from pointing at a table defined in the same schema ---

func renamingTable(name, renameFrom string) config.Table {
	tbl := resolvedTable(name)
	tbl.RenameFrom = renameFrom
	return tbl
}

func countErrorsContaining(diags diag.Diagnostics, substr string) int {
	n := 0
	for _, d := range diags {
		if d.Severity == diag.Error && strings.Contains(d.Summary, substr) {
			n++
		}
	}
	return n
}

func TestValidateLocal_renameFromDefinedTable(t *testing.T) {
	a := resolvedTable("a")
	a.SourceFile = "tables/silver/a.yml"
	b := renamingTable("b", "a")
	b.SourceFile = "tables/silver/b.yml"

	diags := config.ValidateLocal(resolvedConfig([]config.Table{a, b}))
	require.True(t, diags.HasError())

	want := `table "b": rename_from "a" refers to a table that is also defined in tables/silver/a.yml; remove one of them`
	found := false
	for _, d := range diags {
		if d.Severity == diag.Error && d.Summary == want {
			found = true
			assert.Equal(t, "tables/silver/b.yml", d.Location)
		}
	}
	assert.True(t, found, "expected exact error, got: %v", diags)
}

func TestValidateLocal_renameFromDefinedTable_orderIndependent(t *testing.T) {
	a := resolvedTable("a")
	b := renamingTable("b", "a")
	// referenced table defined AFTER the renaming one
	diags := config.ValidateLocal(resolvedConfig([]config.Table{b, a}))
	assert.True(t, hasErrorContaining(diags, `rename_from "a" refers to a table that is also defined`))
}

func TestValidateLocal_renameFromDefinedTable_emptySourceFile(t *testing.T) {
	a := resolvedTable("a")
	b := renamingTable("b", "a")
	diags := config.ValidateLocal(resolvedConfig([]config.Table{a, b}))
	want := `table "b": rename_from "a" refers to a table that is also defined in table:a; remove one of them`
	found := false
	for _, d := range diags {
		if d.Summary == want {
			found = true
			assert.Equal(t, "table:b", d.Location)
		}
	}
	assert.True(t, found, "got: %v", diags)
}

func TestValidateLocal_renameFromSwap(t *testing.T) {
	a := renamingTable("a", "b")
	b := renamingTable("b", "a")
	diags := config.ValidateLocal(resolvedConfig([]config.Table{a, b}))
	assert.True(t, hasErrorContaining(diags, `table "a": rename_from "b" refers to`))
	assert.True(t, hasErrorContaining(diags, `table "b": rename_from "a" refers to`))
}

func TestValidateLocal_renameFromChain(t *testing.T) {
	t.Run("a defined", func(t *testing.T) {
		diags := config.ValidateLocal(resolvedConfig([]config.Table{
			resolvedTable("a"), renamingTable("b", "a"), renamingTable("c", "b"),
		}))
		assert.True(t, hasErrorContaining(diags, `table "b": rename_from "a" refers to`))
		assert.True(t, hasErrorContaining(diags, `table "c": rename_from "b" refers to`))
	})
	t.Run("only b and c defined", func(t *testing.T) {
		diags := config.ValidateLocal(resolvedConfig([]config.Table{
			renamingTable("b", "a"), renamingTable("c", "b"),
		}))
		assert.Equal(t, 1, countErrorsContaining(diags, "refers to a table that is also defined"))
		assert.True(t, hasErrorContaining(diags, `table "c": rename_from "b" refers to`))
	})
}

func TestValidateLocal_renameFromDropTrueTarget(t *testing.T) {
	a := resolvedTable("a")
	a.Drop = true
	diags := config.ValidateLocal(resolvedConfig([]config.Table{a, renamingTable("b", "a")}))
	assert.True(t, hasErrorContaining(diags, `rename_from "a" refers to a table that is also defined`))
}

func TestValidateLocal_renameFromOtherSchemaOK(t *testing.T) {
	cfg := resolvedConfig(nil)
	cfg.Schemas = append(cfg.Schemas,
		config.ResolvedSchema{LogicalCatalog: litMain, LogicalSchema: litGold, UCCatalog: litProd, UCSchema: litGold})
	a := resolvedTable("a")
	b := renamingTable("b", "a")
	b.Schema = litGold
	b.ResolvedSchema = "gold"
	cfg.Tables = []config.Table{a, b}
	diags := config.ValidateLocal(cfg)
	assert.False(t, hasErrorContaining(diags, "refers to a table that is also defined"), "got: %v", diags)

	// different catalog, same schema name
	c := renamingTable("b", "a")
	c.ResolvedCatalog = "other"
	cfg.Tables = []config.Table{resolvedTable("a"), c}
	diags = config.ValidateLocal(cfg)
	assert.False(t, hasErrorContaining(diags, "refers to a table that is also defined"), "got: %v", diags)
}

func TestValidateLocal_renameFromNormalAndStaleOK(t *testing.T) {
	t.Run("rename from undefined table", func(t *testing.T) {
		diags := config.ValidateLocal(resolvedConfig([]config.Table{renamingTable("b", "a")}))
		assert.False(t, diags.HasError(), "got: %v", diags)
	})
	t.Run("rename_from equal to own name is not this error", func(t *testing.T) {
		diags := config.ValidateLocal(resolvedConfig([]config.Table{renamingTable("a", "a")}))
		assert.False(t, hasErrorContaining(diags, "refers to a table that is also defined"))
	})
}

func TestValidateLocal_renameFromEmptyNamesNoPanic(t *testing.T) {
	noSchema := renamingTable("b", "a")
	noSchema.ResolvedSchema = ""
	noCatalog := resolvedTable("a")
	noCatalog.ResolvedCatalog = ""
	emptyName := renamingTable("", "a")
	emptyRename := renamingTable("c", "")
	emptyBoth := config.Table{}

	assert.NotPanics(t, func() {
		diags := config.ValidateLocal(resolvedConfig([]config.Table{noSchema, noCatalog, emptyName, emptyRename, emptyBoth}))
		assert.False(t, hasErrorContaining(diags, "refers to a table that is also defined"), "got: %v", diags)
	})
	assert.NotPanics(t, func() {
		diags := config.ValidateLocal(resolvedConfig([]config.Table{emptyName, emptyRename}))
		assert.False(t, hasErrorContaining(diags, "refers to a table that is also defined"), "got: %v", diags)
	})
}
