package config_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Valid STRUCT types whose field names or neighbours look like modifiers must not get the STRUCT-modifier message.
func TestColumnTypes_StructLookalikesAccepted(t *testing.T) {
	for _, typ := range []string{
		"STRUCT<a:INT>", "STRUCT<comment:STRING>", "STRUCT<not_null:INT>", "STRUCT<notnull:INT>",
		"STRUCT<x_comment:INT>", "STRUCT<comment_x:INT>", "STRUCT<a:STRING,b:ARRAY<INT>>",
		"STRUCT<a:INT, comment:STRING>", "STRUCT<a:INT,comment:STRING>", "struct<comment:string,not:int,null:int>",
		"STRUCT<a: INT, not_null: STRING>", "ARRAY<STRUCT<comment:STRING>>", "MAP<STRING,STRUCT<comment:INT>>",
		"STRUCT<comment:STRUCT<comment:INT>>", "STRUCT<comment : INT>", "STRUCT<a:INT,\tcomment:INT>",
	} {
		assert.Empty(t, typeErrors(t, typ), "type %q", typ)
	}
}

func TestColumnTypes_StructModifierAdversarial(t *testing.T) {
	// Case-insensitive, nested, extra spacing: the STRUCT-specific message.
	for _, typ := range []string{
		"struct<a:int not null>", "Struct<a:Int Not Null>", "STRUCT<a:STRUCT<b:INT NOT NULL>>",
		"STRUCT<a:INT, b:INT NOT NULL>", "STRUCT<a:INT\tNOT\tNULL>", "STRUCT<a:DECIMAL(10,2) NOT NULL>",
		"STRUCT<a:ARRAY<INT> NOT NULL>", typSTRUCTAINTCOMMENTX, "struct<a:int comment x>",
		"STRUCT<comment:INT NOT NULL>", "STRUCT<a:INT NOT NULL, comment:INT>",
	} {
		errs := typeErrors(t, typ)
		require.Len(t, errs, 1, "type %q", typ)
		assert.Equal(t, fmt.Sprintf(`table "t", column "c": type %q is not supported: %s`, typ, structMsg), errs[0], "type %q", typ)
	}
	// Quote / backtick make the shape invalid: the general message, never the STRUCT one.
	for _, typ := range []string{"STRUCT<a:INT COMMENT 'x'>", "STRUCT<`cmnt`:INT>", "STRUCT<`comment`:INT>"} {
		wantGeneralError(t, typ)
	}
}

// COMMENT / NOT NULL outside STRUCT keep their old messages (nothing about STRUCT).
func TestColumnTypes_ModifiersOutsideStruct_NotStructMessage(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"INT COMMENT 'x'":           wantGeneralError,
		"INT COMMENT x":             wantGeneralError,
		"INT NOT NULL":              wantGeneralError,
		"ARRAY<INT NOT NULL>":       wantWhitespaceError,
		"MAP<STRING,INT NOT NULL>":  wantWhitespaceError,
		"ARRAY<INT> COMMENT x":      wantGeneralError,
		"STRUCT<a:INT> NOT NULL":    wantGeneralError,
		"STRUCT<a:INT> COMMENT 'x'": wantGeneralError,
	}
	for typ, want := range cases {
		t.Run(typ, func(t *testing.T) {
			for _, e := range typeErrors(t, typ) {
				assert.NotContains(t, e, structMsg)
			}
			want(t, typ)
		})
	}
}
