package differ_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- column types are compared without case and whitespace ---

func typeDiff(yamlType, liveType string) []change.Change {
	changes, _ := typeDiffAll(yamlType, liveType)
	return changes
}

func typeDiffAll(yamlType, liveType string) ([]change.Change, diag.Diagnostics) {
	d, l := caseTables(
		[]config.Column{{Name: "c", Type: yamlType}},
		[]config.Column{{Name: "c", Type: liveType}},
	)
	return differ.Diff(d, backendtest.LiveOf(*l))
}

// assertTypeChangeRejected checks that the two types differ for real and the change is not
// supported in v1: one planning error about the column type, no changes.
func assertTypeChangeRejected(t *testing.T, yamlType, liveType string) {
	t.Helper()
	changes, diags := typeDiffAll(yamlType, liveType)
	assert.Empty(t, changes)
	errs := summaries(diags, diag.Error)
	require.Len(t, errs, 1, "%q vs %q", yamlType, liveType)
	assert.Contains(t, errs[0], `column "c": changing the type from `+liveType+" to "+yamlType+" is not supported in v1")
}

func TestDiff_typeSpellingNoChanges(t *testing.T) {
	pairs := [][2]string{
		{typDECIMAL1022, typDecimal102},
		{"DECIMAL (10,2)", typDecimal102},
		{"STRUCT<a: INT, b: STRING>", typStructAIntBString},
		{"ARRAY< STRING >", typArrayString},
		{"MAP<STRING, INT>", "map<string,int>"},
		{"TIMESTAMP_NTZ", "timestamp_ntz"},
		{"Decimal(10,2)", typDecimal102},
		{"DECIMAL(10,\t2)", typDecimal102},
		{typDecimal102, typDECIMAL1022},
		{typArrayString, "ARRAY< STRING >"}, // spaced live side against a compact YAML type
		{"STRUCT<`My Field`: INT>", "struct<`my field`:int>"},
	}
	for _, p := range pairs {
		t.Run(p[0]+" vs "+p[1], func(t *testing.T) {
			assert.Empty(t, typeDiff(p[0], p[1]))
		})
	}
}

func TestDiff_typeRealDifferencesStillDetected(t *testing.T) {
	pairs := [][2]string{
		{"DECIMAL(10, 2)", "decimal(12,2)"},
		{typINT, "bigint"},
		{"ARRAY<STRING>", "array<int>"},
		{"STRUCT<a: INT>", "struct<b:int>"},
		{"STRUCT<`my field`: INT>", "struct<`myfield`:int>"}, // whitespace inside backticks is significant
	}
	for _, p := range pairs {
		t.Run(p[0]+" vs "+p[1], func(t *testing.T) {
			assertTypeChangeRejected(t, p[0], p[1])
		})
	}
}

// ALTER and its description use the original desired type from YAML, not the normalized one.
func TestDiff_typeAlterKeepsOriginalSpelling(t *testing.T) {
	changes := typeDiff("String", "VARCHAR( 10 )")
	require.Len(t, changes, 1)
	ch := changes[0].(*change.AlterColumnChange)
	assert.Equal(t, "String", ch.NewValue)
	assert.Equal(t, "VARCHAR( 10 )", ch.OldValue)
	assert.Contains(t, ch.Describe(), "String")
	assert.Contains(t, ch.SQL(), "TYPE String")
}

// The error text uses the original spellings: the desired type from YAML and the live type as UC returned it.
func TestDiff_typeChangeErrorKeepsOriginalSpelling(t *testing.T) {
	changes, diags := typeDiffAll("DECIMAL(12, 2)", "decimal(10,2)")
	assert.Empty(t, changes)
	assert.Equal(t, []string{`table "silver.orders", column "c": changing the type from decimal(10,2) to DECIMAL(12, 2) is not supported in v1 (only varchar(n)/char(n) to STRING); type widening is planned for v2`}, summaries(diags, diag.Error))
}

// plan -> apply -> plan = 0 for a column with a spaced type.
func TestDiff_typeIdempotentAfterApply(t *testing.T) {
	d, l := caseTables(
		[]config.Column{{Name: "c", Type: typINT}},
		[]config.Column{{Name: "c", Type: typINT}},
	)
	d.Columns = []config.Column{{Name: "c", Type: typSTRING}, {Name: "s", Type: "STRUCT<a: INT, b: STRING>"}, {Name: "n", Type: "DECIMAL(10, 2)"}}
	l.Columns = []config.Column{{Name: "c", Type: typVarchar10}}

	changes, _ := differ.Diff(d, backendtest.LiveOf(*l))
	require.Len(t, changes, 3) // alter type + two add columns

	applied := &config.Table{
		Name: litOrders, Schema: litSilver, ResolvedCatalog: litProd, ResolvedSchema: litSilver,
		Columns: []config.Column{{Name: "c", Type: typStringLower}, {Name: "s", Type: "struct<a:int,b:string>"}, {Name: "n", Type: "decimal(10,2)"}},
	}
	changes, diags := differ.Diff(d, backendtest.LiveOf(*applied))
	assert.False(t, diags.HasError())
	assert.Empty(t, changes, "second plan must be empty")
}

// Edge cases of the type comparison, through the public Diff.

func TestDiff_typeEdgeNoChanges(t *testing.T) {
	pairs := map[string][2]string{
		"tab and newline":         {"STRUCT<a:\tINT,\n b: STRING>", "struct<a:int,b:string>"},
		"CRLF":                    {"ARRAY<\r\nSTRING\r\n>", "array<string>"},
		"non-ASCII spaces":        {"DECIMAL(10,\u00a02)", "decimal(10,2)"},
		"ideographic space":       {"MAP<STRING,\u3000INT>", "map<string,int>"},
		"both sides spaced":       {typDECIMAL102, "decimal(10,  2)"},
		"case inside backticks":   {"STRUCT<`Name`: INT>", "struct<`name`:int>"},
		"space inside backticks":  {"STRUCT<`a b`: INT>", typStructABInt},
		"spaces around backticks": {"STRUCT< `a b` : INT >", typStructABInt},
		"doubled backtick":        {"STRUCT<`a``b c`: INT>", "struct<`a``b c`:int>"},
		"empty both":              {"", ""},
		"spaces only vs empty":    {"  \t", ""},
	}
	for name, p := range pairs {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, typeDiff(p[0], p[1]))
		})
	}
}

func TestDiff_typeEdgeStillDetected(t *testing.T) {
	pairs := map[string][2]string{
		"space inside backticks is significant":   {"STRUCT<`a b`: INT>", "struct<`ab`:int>"},
		"extra space inside backticks":            {"STRUCT<`a  b`: INT>", "struct<`a b`:int>"},
		"tab inside backticks is not a space":     {"STRUCT<`a\tb`: INT>", "struct<`a b`:int>"},
		"empty vs type":                           {"", "int"},
		"type vs empty":                           {typINT, ""},
		"whitespace must not merge identifiers":   {"STRUCT<a: INT>", "struct<ai:nt>"},
		"different case-insensitive names differ": {"STRUCT<`a`: INT>", "struct<`b`:int>"},
		"precision differs despite spaces":        {"DECIMAL( 10 , 2 )", "decimal(10,3)"},
	}
	for name, p := range pairs {
		t.Run(name, func(t *testing.T) {
			assertTypeChangeRejected(t, p[0], p[1])
		})
	}
}

// --- Spark DECIMAL defaults: bare DECIMAL is decimal(10,0), DECIMAL(p) is decimal(p,0) ---

func TestDiff_decimalDefaultsEqual(t *testing.T) {
	pairs := [][2]string{
		{"DECIMAL", typDecimal100},
		{"decimal", typDecimal100},
		{"Decimal", typDecimal100},
		{"DECIMAL(10)", typDecimal100},
		{"DECIMAL(10,0)", typDecimal100},
		{"DECIMAL( 10 )", typDecimal100},
		{typDECIMAL1022, typDecimal102},
		{"DECIMAL(5)", "decimal(5,0)"},
		{"DECIMAL(9)", "decimal(9,0)"},
		{"DECIMAL(19)", "decimal(19,0)"},
		{"DECIMAL(38)", "decimal(38,0)"},
		{"DECIMAL(0)", "decimal(0,0)"},
		{"ARRAY<DECIMAL>", "array<decimal(10,0)>"},
		{"ARRAY<ARRAY<DECIMAL(7)>>", "array<array<decimal(7,0)>>"},
		{"MAP<STRING,DECIMAL(5)>", "map<string,decimal(5,0)>"},
		{"MAP<DECIMAL, DECIMAL>", "map<decimal(10,0),decimal(10,0)>"},
		{"STRUCT<a:DECIMAL,b:INT>", "struct<a:decimal(10,0),b:int>"},
		{"STRUCT<a: DECIMAL(3), b: DECIMAL(4,1)>", "struct<a:decimal(3,0),b:decimal(4,1)>"},
		{typStructDecimalInt, "struct<decimal:int>"},
		{"STRUCT<decimal:DECIMAL>", "struct<decimal:decimal(10,0)>"},
		{"STRUCT<`decimal`:DECIMAL>", "struct<`decimal`:decimal(10,0)>"},
		{typStructQDecimalInt, "struct<`decimal`:int>"},
		{"STRUCT<decimals:INT,mydecimal:DECIMAL>", "struct<decimals:int,mydecimal:decimal(10,0)>"},
		{"STRUCT<decimal_x:INT,x_decimal:INT>", "struct<decimal_x:int,x_decimal:int>"},
	}
	for _, p := range pairs {
		t.Run(p[0]+" vs "+p[1], func(t *testing.T) {
			assert.Empty(t, typeDiff(p[0], p[1]))
			assert.Empty(t, typeDiff(p[1], p[0]))
		})
	}
}

func TestDiff_decimalDefaultsDoNotHideRealDifferences(t *testing.T) {
	pairs := [][2]string{
		{"DECIMAL(10,2)", typDecimal100},
		{"DECIMAL", typDecimal102},
		{"DECIMAL(10)", typDecimal102},
		{"DECIMAL(12)", typDecimal100},
		{"decimal()", "decimal(,0)"},
		{"ARRAY<DECIMAL(5)>", "array<decimal(10,0)>"},
		{typStructDecimalInt, "struct<decimal:decimal(10,0)>"},
		{typStructDecimalInt, "struct<decimals:int>"},
		{typStructQDecimalInt, "struct<decimal:int>"},
	}
	for _, p := range pairs {
		t.Run(p[0]+" vs "+p[1], func(t *testing.T) {
			assertTypeChangeRejected(t, p[0], p[1])
		})
	}
}

// Words that merely contain "decimal", field names and quoted identifiers are not DECIMAL types:
// they must not be expanded, so each pair below is a real difference (checked in both directions).
func TestDiff_decimalLookalikesAreNotExpanded(t *testing.T) {
	pairs := map[string][2]string{
		"decimals as a type word":      {"decimals", typDecimal100},
		"suffix after underscore":      {"decimal_x", "decimal(10,0)_x"},
		"prefix before underscore":     {"x_decimal", "x_decimal(10,0)"},
		"suffix digit":                 {"decimal2", "decimal(10,0)2"},
		"prefix digit":                 {"2decimal", "2decimal(10,0)"},
		"suffix non-ASCII letter":      {"decimalé", "decimal(10,0)é"},
		"prefix non-ASCII letter":      {"édecimal", "édecimal(10,0)"},
		"field name before colon":      {typStructDecimalInt, "struct<decimal(10,0):int>"},
		"field name with precision":    {typStructDecimalInt, "struct<decimal(10):int>"},
		"quoted name is not expanded":  {typStructQDecimalInt, "struct<`decimal(10,0)`:int>"},
		"quoted name with precision":   {typStructQDecimalInt, "struct<`decimal(10)`:int>"},
		"quoted decimal, decimal type": {"STRUCT<`decimal`:DECIMAL>", "struct<`decimal(10,0)`:decimal(10,0)>"},
	}
	for name, p := range pairs {
		t.Run(name, func(t *testing.T) {
			for _, ps := range [][2]string{p, {p[1], p[0]}} {
				assertTypeChangeRejected(t, ps[0], ps[1])
			}
		})
	}
}

func decimalSchemaDiff(yamlType string) ([]change.Change, bool) {
	d, l := caseTables(
		[]config.Column{{Name: "d", Type: yamlType}},
		[]config.Column{{Name: "d", Type: typDecimal100}},
	)
	changes, diags := differ.DiffSchema([]config.Table{d}, []*backend.LiveTable{backendtest.LiveOf(*l)})
	return changes, diags.HasError()
}

func TestDiffSchema_decimalWithoutParamsNoChanges(t *testing.T) {
	changes, hasErr := decimalSchemaDiff("DECIMAL")
	assert.False(t, hasErr)
	assert.Empty(t, changes)
}

func TestDiffSchema_decimalOtherParamsIsUnsupportedTypeChange(t *testing.T) {
	changes, hasErr := decimalSchemaDiff("DECIMAL(12,3)")
	assert.True(t, hasErr)
	assert.Empty(t, changes)
}

func TestDiff_decimalMalformedLeftAsIs(t *testing.T) {
	for _, ty := range []string{
		"decimal(abc)", "decimal()", "decimal(10,)", "decimal(,2)", "ARRAY<decimal(", "decimal(10", "decimal(",
		"ARRAY<DECIMAL(10)", "STRUCT<a:decimal(1,>",
	} {
		t.Run(ty, func(t *testing.T) {
			assert.Empty(t, typeDiff(ty, ty))
			assertTypeChangeRejected(t, ty, strings.ReplaceAll(strings.ToLower(ty), "(", "(0"))
		})
	}
	assert.Empty(t, typeDiff("ARRAY<DECIMAL(10)>", "array<decimal(10,0)>"))
	assert.Empty(t, typeDiff("STRUCT<a:DECIMAL(10),b:INT>", "struct<a:decimal(10,0),b:int>"))
	assertTypeChangeRejected(t, "decimal(abc)", "decimal(abc,0)")
}

// The scan must be linear: a long run of unclosed "decimal(" must not take quadratic time. The test
// does not compare against a wall-clock limit (that depends on the machine): it times the same input
// at size n and 16n and compares the ratio. Linear growth gives about 16, quadratic about 256; the
// limit 100 leaves room for a loaded machine (noise stays within a few times), and the minimum of
// several runs removes outliers.
func TestDiff_decimalNormalizationLinear(t *testing.T) {
	const (
		units = 4000
		runs  = 3
		limit = 100.0
	)
	best := func(long string) time.Duration {
		fastest := time.Duration(1<<63 - 1)
		for range runs {
			start := time.Now()
			assert.Empty(t, typeDiff(long, long))
			if d := time.Since(start); d < fastest {
				fastest = d
			}
		}
		return fastest
	}
	for _, unit := range []string{"decimal(", "decimal(1,"} {
		small := best(strings.Repeat(unit, units))
		large := best(strings.Repeat(unit, 16*units))
		// A clock that does not resolve the small run cannot show growth: count it as 1ns.
		ratio := float64(large) / float64(max(small, time.Nanosecond))
		assert.Less(t, ratio, limit, "unit %q: 16x input took %v vs %v (%.1fx); linear is ~16x", unit, large, small, ratio)
	}
}
