package config_test

// validator_injection_test.go covers the simplified rule: column types containing
// SQL-breaking characters are rejected, everything accepted before stays accepted.

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateLocal_ColumnTypes_RejectsSQLInjection(t *testing.T) {
	bad := []string{
		"STRING;",
		"INTERVAL DAY; DROP TABLE x",
		"ARRAY<INT>) LOCATION 's3://x' (a ARRAY<INT>",
		"STRUCT<a:INT> -- x",
		"ARRAY<INT>/*x*/",
		"ARRAY</*x*/INT>",
		`STRING\`,
		`STRING"`,
		"STRING`",
		"STRING'",
		"ARRAY<\nINT>",
		"ARRAY<INT>\nLOCATION 'x'",
		"INTERVAL DAY\r\nTO SECOND",
		"ARRAY<INT>\rx",
		"DECIMAL(10,2) -- c",
	}
	for _, typ := range bad {
		diags := validateColType(typ)
		assert.True(t, diags.HasError(), "type %q must be rejected", typ)
	}
}

// Inside "(...)" of DECIMAL/GEOGRAPHY the text is not parsed, so the forbidden characters are the only guard there.
func TestValidateLocal_ColumnTypes_ForbiddenInsideParens(t *testing.T) {
	for _, sep := range []string{";", `\`, "'", `"`, "`", "\n", "\r", "--", "/*"} {
		for _, typ := range []string{"DECIMAL(10" + sep + "2)", "ARRAY<GEOGRAPHY(43" + sep + "26)>"} {
			assert.True(t, validateColType(typ).HasError(), "type %q must be rejected", typ)
		}
	}
	assert.False(t, validateColType("DECIMAL(10,2)").HasError())
}

func TestValidateLocal_ColumnTypes_PreviouslyValidStillAccepted(t *testing.T) {
	good := []string{
		typSTRING, "string", typBIGINT, typINT, "TINYINT", "SMALLINT", typFLOAT, typDOUBLE,
		typBOOLEAN, typDATE, typTIMESTAMP, litTIMESTAMPNTZ, typBINARY, "VARIANT",
		"Timestamp_Ntz", "  STRING  ",
		typDECIMAL, typDECIMAL1022, "decimal(18,4)", "decimal(5,0)", typDECIMAL102,
		typARRAYSTRING, "ARRAY<BIGINT>", "array<string>", "array <int>",
		typMAPSTRINGINT, "MAP<STRING,ARRAY<BIGINT>>",
		typSTRUCTIdBIGINTNameSTRING, "STRUCT<a:INT,b:STRING>",
		typARRAYSTRUCTAINT, "GEOGRAPHY(4326)", "GEOMETRY(ANY)",
		typINTERVALYEARTOMONTH, litIntervalDayToSecond,
	}
	for _, typ := range good {
		assert.False(t, validateColType(typ).HasError(), "type %q must stay valid", typ)
	}
}
