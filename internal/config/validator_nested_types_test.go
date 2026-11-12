package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Words inside ARRAY<...>, MAP<..., ...>, STRUCT<name: ...> follow the same rules as a top-level type.

func TestColumnTypes_NestedCanonical_Accepted(t *testing.T) {
	for _, typ := range []string{
		"ARRAY<STRING>", typMAPSTRINGINT, "STRUCT<a:INT,b:ARRAY<DECIMAL(10,2)>>", "ARRAY<ARRAY<TIMESTAMP_NTZ>>",
		"ARRAY<DECIMAL>", "MAP<STRING, DECIMAL(38, 18)>", "ARRAY<GEOGRAPHY(4326)>", "ARRAY<VARIANT>",
		"MAP<TIMESTAMP, STRUCT<x: BIGINT, y: MAP<STRING, ARRAY<BINARY>>>>",
		// field names may look like type words
		"STRUCT<decimal:INT>", "STRUCT<array:ARRAY<INT>,map:MAP<STRING,INT>,struct:STRUCT<int:INT>>",
		"STRUCT<integer:INT, text: STRING>", "STRUCT<1a:INT>", "STRUCT<имя:STRING>",
		"STRUCT<a:INTERVAL DAY TO SECOND, b:INT>",
	} {
		assert.Empty(t, typeErrors(t, typ), "type %q", typ)
	}
}

func TestColumnTypes_NestedNonCanonical_Rejected(t *testing.T) {
	for typ, nested := range map[string]string{
		"ARRAY<INTEGER>":                       typINTEGER,
		"MAP<STRING, DEC(10,2)>":               "DEC",
		"STRUCT<a:TEXT>":                       typTEXT,
		"ARRAY<ARRAY<LONG>>":                   typLONG,
		"MAP<TIMESTAMP_LTZ, INT>":              "TIMESTAMP_LTZ",
		"STRUCT<a:INT, b:MAP<STRING, FOO>>":    "FOO",
		"array<integer>":                       "integer",
		"ARRAY<VARCHAR>":                       "",
		"ARRAY<ARRAY>":                         "",
		"ARRAY<STRUCT<a:INT, b:BOOL>>":         "BOOL",
		"MAP<STRING, STRUCT<a:ARRAY<FLOATS>>>": "FLOATS",
	} {
		errs := typeErrors(t, typ)
		require.Len(t, errs, 1, "type %q", typ)
		assert.Contains(t, errs[0], `type "`+typ+`" is not supported; supported types:`)
		if nested != "" {
			assert.Contains(t, errs[0], `unsupported nested type: "`+nested+`"`, "type %q", typ)
		}
	}
}

func TestColumnTypes_NestedMalformed_Rejected(t *testing.T) {
	for _, typ := range []string{
		"ARRAY<>", "MAP<STRING>", "ARRAY<STRING", "ARRAY<STRING>>", "ARRAY<STRING>>>", "MAP<STRING,>", "MAP<,INT>",
		"MAP<STRING,INT,INT>", "ARRAY<INT,INT>", "STRUCT<>", "STRUCT<a>", "STRUCT<:INT>", "STRUCT<a:INT,>",
		"STRUCT<a:>", "STRUCT<a:INT,,b:INT>", "ARRAY<STRING>X", "ARRAY<DECIMAL(10,2>", "ARRAY<DECIMAL()>",
		"ARRAY<GEOMETRY>", "ARRAY<INTERVAL>", "ARRAY<<STRING>>", "<STRING>", "ARRAY<STRING>,INT",
		"STRUCT<`my field`:INT>", "STRUCT<a.b:INT>",
	} {
		wantGeneralError(t, typ)
	}
}

// A wrapper type nested at the limit or very wide is read once: no quadratic behaviour (deeper than the limit: TestColumnTypes_DepthLimit).
func TestColumnTypes_NestedLarge_Linear(t *testing.T) {
	const depth = 100
	typ := strings.Repeat("ARRAY<", depth) + "STRING" + strings.Repeat(">", depth)
	assert.Empty(t, typeErrors(t, typ))
	fields := make([]string, 5000)
	for i := range fields {
		fields[i] = "f" + strings.Repeat("x", i%7) + ":MAP<STRING,DECIMAL(10,2)>"
	}
	assert.Empty(t, typeErrors(t, "STRUCT<"+strings.Join(fields, ",")+">"))
	assert.NotEmpty(t, typeErrors(t, "STRUCT<"+strings.Join(fields, ",")+",z:INTEGER>"))
}

// Only the first unsupported nested type is named, in the order of the text.
func TestColumnTypeError_FirstNestedTypeNamed(t *testing.T) {
	msg := config.ColumnTypeError("STRUCT<a:INTEGER, b:MAP<STRING, LONG>>")
	assert.Contains(t, msg, `unsupported nested type: "INTEGER"`)
	assert.NotContains(t, msg, `nested type: "LONG"`)
	assert.True(t, strings.HasSuffix(msg, `unsupported nested type: "INTEGER"`), msg)
}

func TestColumnTypeError(t *testing.T) {
	assert.Empty(t, config.ColumnTypeError("ARRAY<STRING>"))
	assert.Contains(t, config.ColumnTypeError("ARRAY<INTEGER>"), `type "ARRAY<INTEGER>" is not supported; supported types:`)
	assert.Equal(t, `type "BIG INT" is not supported: whitespace inside a word`, config.ColumnTypeError("BIG INT"))
}

// --- depth limit (maxTypeDepth = 100) ---

func nestedArrays(depth int) string {
	return strings.Repeat("ARRAY<", depth) + "INT" + strings.Repeat(">", depth)
}

func TestColumnTypes_DepthLimit(t *testing.T) {
	assert.Empty(t, config.ColumnTypeError(nestedArrays(100)), "depth 100 is accepted")
	assert.Empty(t, config.ColumnTypeError(strings.Repeat("STRUCT<a:", 50)+strings.Repeat("ARRAY<", 50)+"INT"+strings.Repeat(">", 100)))

	msg := config.ColumnTypeError(nestedArrays(101))
	assert.Contains(t, msg, `ARRAY<ARRAY<`)
	assert.True(t, strings.HasSuffix(msg, `..." is not supported; nesting is deeper than 100 levels`), msg)
	assert.Less(t, len(msg), 200, "a long type is cut in the message")

	for _, typ := range []string{
		strings.Repeat("MAP<STRING,", 101) + "INT" + strings.Repeat(">", 101),
		strings.Repeat("STRUCT<a:", 101) + "INT" + strings.Repeat(">", 101),
		strings.Repeat("ARRAY<", 101), // malformed as well: the depth is reported first
	} {
		assert.Contains(t, config.ColumnTypeError(typ), "nesting is deeper than 100 levels", typ[:20])
	}
}

func TestColumnTypes_DepthLimit_MessageCutAt80Characters(t *testing.T) {
	typ := strings.Repeat("ARRAY<", 101)
	assert.Equal(t, `type "`+typ[:80]+`..." is not supported; nesting is deeper than 100 levels`, config.ColumnTypeError(typ))
}

// A type of two million levels is rejected at once: the reader stops at level 101, no deep recursion.
func TestColumnTypes_DepthLimit_HugeTypeRejectedFast(t *testing.T) {
	typ := nestedArrays(2_000_000)
	start := time.Now()
	msg := config.ColumnTypeError(typ)
	assert.Contains(t, msg, "nesting is deeper than 100 levels")
	assert.Less(t, len(msg), 200)
	assert.Less(t, time.Since(start), 2*time.Second)

	// with whitespace inside (the classification path reads the type a second time)
	start = time.Now()
	assert.Contains(t, config.ColumnTypeError(typ+" X"), "nesting is deeper than 100 levels")
	assert.Less(t, time.Since(start), 2*time.Second)
}
