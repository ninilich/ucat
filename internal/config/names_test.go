package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestFoldName_SameName(t *testing.T) {
	cases := map[string]struct {
		a, b string
		same bool
	}{
		"equal":           {litOrders, litOrders, true},
		"case only":       {"UserId", "userid", true},
		"empty both":      {"", "", true},
		"empty one":       {"", "a", false},
		"different":       {"a", "b", false},
		"unicode case":    {"Заказы", "заказы", true},
		"long s vs s":     {"ſ", "s", false}, // ToLower, not Unicode case folding
		"kelvin vs k":     {"K", "k", true},
		"dotted capital":  {"İ", "i", true}, // Go lowers U+0130 to "i"
		"sharp s vs ss":   {"ß", "ss", false},
		"trailing space":  {"a ", "a", false},
		"nfc vs nfd é":    {"é", "é", false},
		"greek final":     {"ΟΔΟΣ", "οδοσ", true},
		"greek final low": {"οδος", "οδοσ", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.same, config.SameName(c.a, c.b))
			assert.Equal(t, c.same, config.SameName(c.b, c.a), "symmetric")
			assert.Equal(t, c.same, config.FoldName(c.a) == config.FoldName(c.b))
		})
	}
}

func TestIsLowerName(t *testing.T) {
	assert.True(t, config.IsLowerName(""))
	assert.True(t, config.IsLowerName("orders_2"))
	assert.True(t, config.IsLowerName("ſ"))
	assert.True(t, config.IsLowerName("заказы"))
	assert.False(t, config.IsLowerName("Orders"))
	assert.False(t, config.IsLowerName("ЗАКАЗЫ"))
}

func TestHasDotOrSpace(t *testing.T) {
	cases := map[string]bool{
		"":            false,
		"plain":       false,
		litAB2:        true,
		litAB:         true,
		"a\tb":        true,
		"a\u00a0b":    true, // no-break space
		"a\u2003b":    true, // em space
		"имя.таблица": true,
		pathAB:        false,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) { assert.Equal(t, want, config.HasDotOrSpace(in)) })
	}
}

func TestIsFileSafeName(t *testing.T) {
	for _, ok := range []string{"core", "silver_2", "dev-eu", "Prod"} {
		assert.True(t, config.IsFileSafeName(ok), "%q", ok)
	}
	for _, bad := range []string{"", ".", "..", "../../evil", pathAB, `x\y`, "..\\x", "/root", "C:\\x", ".hidden", litAB2, litAB, "a\nb", "имя_схемы", "C:"} {
		assert.False(t, config.IsFileSafeName(bad), "%q", bad)
	}
}

// "ſ" and "s" are different names for the validator and for the differ alike:
// rename_from: ſ on column s is a real rename, not "same as name". The differ side
// of this contract is in differ.TestDiff_columnRenameFromLongSIsRealRename.
func TestValidateLocal_longSRenameFromIsRealRename(t *testing.T) {
	tbl := resolvedTable("orders")
	tbl.Columns = []config.Column{{Name: "s", Type: typBIGINT, RenameFrom: "ſ"}}
	diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
	assert.False(t, diags.HasError(), "diags: %v", diags)
	assert.False(t, hasWarningContaining(diags, "rename_from is same as name"), "diags: %v", diags)

	same := resolvedTable("orders")
	same.Columns = []config.Column{{Name: "S", Type: typBIGINT, RenameFrom: "s"}}
	diags = config.ValidateLocal(resolvedConfig([]config.Table{same}))
	assert.True(t, hasWarningContaining(diags, "rename_from is same as name"), "diags: %v", diags)
}
