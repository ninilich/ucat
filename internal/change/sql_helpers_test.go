package change_test

// The SQL quoting helpers, tested directly (exported for tests by export_test.go):
//   - sqlIdent doubles every backtick so a name can never leave its quoted identifier (the validator
//     does not reject a backtick in a name, so escaping is the only protection);
//   - sqlStr escapes the backslash before the quote so a value can never leave its single-quoted literal;
//   - the list helpers shared by SET/UNSET TBLPROPERTIES, CREATE TABLE, CLUSTER BY and PARTITIONED BY
//     keep the escaping rule in one place.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
)

func TestSqlIdent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", litOrders, "`orders`"},
		{"one backtick", "a`b", "`a``b`"},
		{"two in a row", "a``b", "`a````b`"},
		{"backtick at start", "`ab", "```ab`"},
		{"backtick at end", "ab`", "`ab```"},
		{"only a backtick", "`", "````"},
		{"empty", "", "``"},
		{"dot and space stay in one pair", "my table.v1", "`my table.v1`"},
		{"unicode", "таблица_日本", "`таблица_日本`"},
		{"reserved word", litOrder, "`order`"},
		{"semicolon and comment", "x; DROP TABLE y --", "`x; DROP TABLE y --`"},
		{"single quote is not touched", litItS, "`it's`"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, change.SQLIdent(tc.in))
		})
	}
}

func TestSqlIdentList(t *testing.T) {
	assert.Equal(t, "", change.SQLIdentList(nil))
	assert.Equal(t, "", change.SQLIdentList([]string{}))
	assert.Equal(t, "`a`", change.SQLIdentList([]string{"a"}))
	assert.Equal(t, "`b`, `a`", change.SQLIdentList([]string{"b", "a"}), "order of columns is kept")
	assert.Equal(t, "`order`, `select`", change.SQLIdentList([]string{litOrder, "select"}), "reserved words are quoted")
	assert.Equal(t, "`we``ird`", change.SQLIdentList([]string{"we`ird"}), "backtick is doubled")
}

func TestSqlPropList(t *testing.T) {
	assert.Equal(t, "", change.SQLPropList(nil))
	assert.Equal(t, "", change.SQLPropList(map[string]string{}))
	assert.Equal(t, "'a'='1', 'b'='2', 'c'='3'", change.SQLPropList(map[string]string{"c": "3", "a": "1", "b": "2"}), "keys are sorted")
	assert.Equal(t, `'k'='it\'s'`, change.SQLPropList(map[string]string{"k": litItS}), "apostrophe in value is escaped")
	assert.Equal(t, `'it\'s'='v'`, change.SQLPropList(map[string]string{litItS: "v"}), "apostrophe in key is escaped")
	assert.Equal(t, "'select'='order'", change.SQLPropList(map[string]string{"select": "order"}), "reserved words are plain literals")
}

func TestSqlKeyList(t *testing.T) {
	keys := []string{"b", "a'x"}
	assert.Equal(t, `'a\'x', 'b'`, change.SQLKeyList(keys))
	assert.Equal(t, []string{"b", "a'x"}, keys, "input slice is not modified")
	assert.Equal(t, "", change.SQLKeyList(nil))
}

func TestSqlStr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", litAbc, `abc`},
		{"apostrophe", litItS, `it\'s`},
		{"backslash", `a\b`, `a\\b`},
		{"backslash n as two chars", `a\nb`, `a\\nb`},
		{"backslash then quote", `\'`, `\\\'`},
		{"trailing backslash", `abc\`, `abc\\`},
		{"double backslash", `\\`, `\\\\`},
		{"two quotes", `''`, `\'\'`},
		{"empty", ``, ``},
		{"real newline kept", multiline, "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, change.SQLStr(tt.in))
		})
	}
}

// parseLiteral parses a single-quoted SQL literal where backslash escapes the next
// character. It reports false if the literal is closed early or never closed, and
// otherwise returns the unescaped value.
func parseLiteral(lit string) (string, bool) {
	if len(lit) < 2 || lit[0] != '\'' {
		return "", false
	}
	var out []byte
	for i := 1; i < len(lit); i++ {
		switch lit[i] {
		case '\\':
			i++
			if i >= len(lit)-1 { // escape must not swallow the closing quote
				return "", false
			}
			out = append(out, lit[i])
		case '\'':
			return string(out), i == len(lit)-1
		default:
			out = append(out, lit[i])
		}
	}
	return "", false
}

func TestSqlStr_StaysInsideLiteralAndRoundTrips(t *testing.T) {
	inputs := []string{
		`abc`, `it's`, `a\b`, `a\nb`, `\'`, `abc\`, `\\`, `''`, ``, "a\nb",
		`'; DROP TABLE x; --`, `\'; DROP TABLE x; --`, `\\'`, `'\`, `\\\`, `\'\'\`,
		`C:\tmp\'x`, "multi\nline\\", `unicode ключ\`,
	}
	for _, in := range inputs {
		got, ok := parseLiteral("'" + change.SQLStr(in) + "'")
		require.True(t, ok, "value %q escaped to %q leaves the literal", in, change.SQLStr(in))
		assert.Equal(t, in, got, "value %q must round-trip", in)
	}
}
