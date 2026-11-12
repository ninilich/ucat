package config_test

import (
	"fmt"
	"testing"

	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Whitespace inside a word of a column type (columnTypeProblem/hasWhitespaceInWord).

const (
	wsMsg      = "whitespace inside a word"
	structMsg  = "NOT NULL and COMMENT are not supported inside STRUCT in v1"
	generalMsg = "is not supported; supported types:"
)

func typeErrors(t *testing.T, typ string) []string {
	t.Helper()
	var out []string
	for _, d := range validateColType(typ) {
		if d.Severity == diag.Error {
			out = append(out, d.Summary)
		}
	}
	return out
}

func wantWhitespaceError(t *testing.T, typ string) {
	t.Helper()
	errs := typeErrors(t, typ)
	require.Len(t, errs, 1, "type %q", typ)
	assert.Equal(t, fmt.Sprintf(`table "t", column "c": type %q is not supported: %s`, typ, wsMsg), errs[0])
}

func wantGeneralError(t *testing.T, typ string) {
	t.Helper()
	errs := typeErrors(t, typ)
	require.Len(t, errs, 1, "type %q", typ)
	assert.Contains(t, errs[0], fmt.Sprintf(`table "t", column "c": type %q`, typ))
	assert.Contains(t, errs[0], generalMsg)
}

// Unicode and control whitespace between two identifier characters: rejected with the whitespace message.
func TestColumnTypes_UnicodeWhitespaceInsideWord(t *testing.T) {
	for name, typ := range map[string]string{
		"NBSP":         "BIG INT",
		"ideographic":  "BIG　INT",
		"em space":     "BIG INT",
		"tab":          "BIG\tINT",
		"nbsp nested":  "ARRAY<IN T>",
		"vertical tab": "BIG\vINT",
		"form feed":    "BIG\fINT",
		"NEL":          "BIG\u0085INT",
		"struct field": "STRUCT<a b:INT>",
		"map key":      "MAP<STR ING, INT>",
	} {
		t.Run(name, func(t *testing.T) { wantWhitespaceError(t, typ) })
	}
}

// By fact: the glued form is well-formed, so \n and \r\n inside a word give the whitespace
// message too (the contract text suggests the general one; both reject the type).
func TestColumnTypes_NewlineInsideWord_Rejected(t *testing.T) {
	for _, typ := range []string{"BIG\nINT", "BIG\r\nINT", "BIG\rINT", "ARRAY<IN\nT>"} {
		errs := typeErrors(t, typ)
		require.Len(t, errs, 1, "type %q", typ)
		assert.Contains(t, errs[0], "is not supported")
	}
	wantWhitespaceError(t, "BIG\nINT") // fact pinned
}

// By fact: newline next to punctuation or at the edge is rejected with the general message
// (validShape forbids \n and \r everywhere, unlike a tab at the edge).
func TestColumnTypes_NewlineNearPunctuation_General(t *testing.T) {
	for _, typ := range []string{"ARRAY<INT>\n", "ARRAY<\nINT>", "\nSTRING", "STRING\r\n", "STRING\nX"} {
		wantGeneralError(t, typ)
	}
}

// U+200B is not unicode.IsSpace and not an identifier char: by fact it is rejected with the
// general message (not as whitespace). Open question in the report.
func TestColumnTypes_ZeroWidthSpace_ByFact(t *testing.T) {
	wantGeneralError(t, "BIG\u200bINT")
	wantGeneralError(t, "STRING\u200b")
}

// STRING\tX and STRING<NBSP>X: gluing is not a valid type: general message.
func TestColumnTypes_WhitespaceGlueInvalid_General(t *testing.T) {
	wantGeneralError(t, "STRING\tX")
	wantGeneralError(t, "STRING X")
	wantGeneralError(t, "STRING　X")
}

func TestColumnTypes_DeepNesting(t *testing.T) {
	wantWhitespaceError(t, "ARRAY<MAP<STRING, STRUCT<a: INT, b: ARRAY<IN T>>>>")
	assert.Empty(t, typeErrors(t, "ARRAY<MAP<STRING, STRUCT<a: INT, b: ARRAY<INT>>>>"))
	wantWhitespaceError(t, "ARRAY<MAP<STRING, STRUCT<a: INT, b: ARRAY<STRUCT<c: BIG INT>>>>>")
}

func TestColumnTypes_Interval(t *testing.T) {
	for _, typ := range []string{
		"INTERVAL DAY TO SECOND", typINTERVALYEARTOMONTH, "INTERVAL  DAY", litIntervalDayToSecond,
		"ARRAY<INTERVAL DAY TO SECOND>", "STRUCT<a:INTERVAL DAY TO SECOND>", "STRUCT<a: INTERVAL YEAR TO MONTH, b: INT>",
		"MAP<STRING, INTERVAL DAY TO SECOND>",
	} {
		assert.Empty(t, typeErrors(t, typ), "type %q", typ)
	}
	// By fact: letters inside an INTERVAL qualifier are not checked ("DA Y" passes: the qualifier
	// is not parsed until v2).
	assert.Empty(t, typeErrors(t, "INTERVAL DA Y"))
	// A non-INTERVAL word after an interval atom (after punctuation) is checked again.
	wantWhitespaceError(t, "STRUCT<a:INTERVAL DAY TO SECOND, b:IN T>")
}

func TestColumnTypes_Geo(t *testing.T) {
	for _, typ := range []string{typGEOGRAPHY4326, "GEOMETRY(4326)", "GEOGRAPHY( 4326 )", "geometry( ANY )", "ARRAY<GEOGRAPHY(4326)>"} {
		assert.Empty(t, typeErrors(t, typ), "type %q", typ)
	}
	wantWhitespaceError(t, "GEO GRAPHY(4326)")
	wantWhitespaceError(t, "GEOMETRY(43 26)")
}

func TestColumnTypes_Decimal(t *testing.T) {
	for _, typ := range []string{typDECIMAL, "DECIMAL(10)", typDECIMAL1022, "DECIMAL( 10 , 2 )", typDECIMAL102, "DECIMAL(10, 2)"} {
		assert.Empty(t, typeErrors(t, typ), "type %q", typ)
	}
	// By fact: a space inside a number is whitespace inside a word.
	wantWhitespaceError(t, "DECIMAL(1 0,2)")
	wantWhitespaceError(t, "DEC IMAL(10,2)")
}

// Types as UC returns them in type_text pass the validator, including nested varchar/char.
func TestColumnTypes_UCTypeText_RoundTrip(t *testing.T) {
	for _, typ := range []string{
		"struct<a:int,b:string>", "decimal(10,2)", "map<string,array<int>>", "interval day to second",
		"array<varchar(5)>", "array<char(3)>", "struct<a:array<char(3)>,b:varchar(10)>",
		"map<string,struct<a:decimal(10,2),b:array<varchar(5)>>>",
		"array<interval year to month>", "struct<a:interval day to second,b:int>",
	} {
		assert.Empty(t, typeErrors(t, typ), "type %q", typ)
	}
}

func TestColumnTypes_BacktickRejected(t *testing.T) {
	for _, typ := range []string{"`INT`", "ARRAY<`INT`>", "STRUCT<`a`:INT>", "INT`"} {
		wantGeneralError(t, typ)
	}
}

// STRUCT field modifiers are not supported (full type parsing is v2).
// NOT NULL / COMMENT x get the STRUCT-specific reason (not "whitespace inside a word");
// COMMENT 'x' has a quote, so the message is the general one. The type is rejected either way.
func TestColumnTypes_StructModifiersRejected(t *testing.T) {
	for _, typ := range []string{"STRUCT<a:INT NOT NULL>", "STRUCT<a:INT COMMENT 'x'>", typSTRUCTAINTCOMMENTX} {
		assert.NotEmpty(t, typeErrors(t, typ), "type %q", typ)
	}
	for _, typ := range []string{"STRUCT<a:INT NOT NULL>", "STRUCT<a:INT COMMENT x>", "ARRAY<STRUCT<a:INT not  null>>", "STRUCT<a:STRUCT<b:INT NOT NULL>>"} {
		errs := typeErrors(t, typ)
		require.Len(t, errs, 1, "type %q", typ)
		assert.Equal(t, fmt.Sprintf(`table "t", column "c": type %q is not supported: %s`, typ, structMsg), errs[0])
	}
	wantGeneralError(t, "STRUCT<a:INT COMMENT 'x'>")
}

// The STRUCT reason is only for STRUCT field modifiers: other whitespace keeps the old message.
func TestColumnTypes_StructReasonNarrow(t *testing.T) {
	for _, typ := range []string{"BIG\nINT", typBIGINT2, "STRUCT<comment:BIG INT>", "STRUCT<a:IN T>", "NOT NULL", "INT NOT NULL"} {
		errs := typeErrors(t, typ)
		require.Len(t, errs, 1, "type %q", typ)
		assert.NotContains(t, errs[0], structMsg, "type %q", typ)
	}
	wantWhitespaceError(t, "STRUCT<comment:BIG INT>")
	wantWhitespaceError(t, "BIG INT")
}

// Whitespace next to punctuation and at the edges stays valid, with any depth.
func TestColumnTypes_PunctuationSpacingAccepted(t *testing.T) {
	for _, typ := range []string{
		"  DECIMAL ( 10 , 2 )  ", "STRUCT < a : INT , b : ARRAY < STRING > >", "MAP < STRING , MAP < STRING , INT > >",
		"ARRAY<\tINT\t>", " STRING ",
	} {
		assert.Empty(t, typeErrors(t, typ), "type %q", typ)
	}
}
