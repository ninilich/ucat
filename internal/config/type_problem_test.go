package config

import "testing"

func TestColumnTypeProblem(t *testing.T) {
	cases := map[string]typeProblem{
		"BIGINT":                        typeOK,
		"decimal(10, 2)":                typeOK,
		"STRUCT<a:INT>":                 typeOK,
		"":                              typeUnsupported,
		"NOPE":                          typeUnsupported,
		"BIG INT":                       typeWhitespaceInWord,
		"DEC IMAL(10,2)":                typeWhitespaceInWord,
		"STRUCT<a:INT NOT NULL>":        typeStructModifier,
		"STRUCT<a:INT COMMENT x>":       typeStructModifier,
		"STRUCT<a:INT, comment:INT>":    typeOK,
		"IN T; DROP":                    typeUnsupported,
		"БИГИНТ":                        typeUnsupported,
		"BIG\u00a0INT":                  typeWhitespaceInWord,
		"ARRAY<STRUCT<a:INT NOT NULL>>": typeStructModifier,
	}
	for in, want := range cases {
		if got, _ := columnTypeProblem(in); got != want {
			t.Errorf("columnTypeProblem(%q) = %d, want %d", in, got, want)
		}
	}
	if typeOK.reason() != "" || typeUnsupported.reason() != "" {
		t.Error("typeOK and typeUnsupported have no reason text")
	}
	if typeWhitespaceInWord.reason() != "whitespace inside a word" ||
		typeStructModifier.reason() != "NOT NULL and COMMENT are not supported inside STRUCT in v1" {
		t.Error("reason text changed")
	}
}
