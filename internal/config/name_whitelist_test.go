package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
)

// The white list of logical names: A-Z a-z 0-9 _ - , 1..100 characters, not a reserved Windows name.

const nameRuleText = "must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name"

func goodNames() []string {
	return []string{litSilver2, "Main_1", "a-b", "x", "_", "-", "0", strings.Repeat("a", 100), "COM10", "console", "nul_", "com", "LPT0",
		"AZaz09_-", "silver9", "Z9", "a0123456789z"}
}

// badNames are non-empty names the rule must reject; the empty name is checked on its own.
func badNames() []string {
	return []string{
		strings.Repeat("a", 101), "con", "CON", "Nul", "COM1", "lpt9", "AUX", "prn", "Com5", "C:evil", "a*b", litAB2, litAB,
		pathAB, bsAB, "..", ".", "схема", ctrlAB, "a\x00b", tabAB, nlAB, " a", "a ", "a?b", "a<b", "a|b", nbspAB,
	}
}

func TestIsFileSafeName_WhiteList(t *testing.T) {
	for _, name := range goodNames() {
		assert.True(t, config.IsFileSafeName(name), "%q", name)
	}
	for _, name := range append(badNames(), "") {
		assert.False(t, config.IsFileSafeName(name), "%q", name)
	}
}

func TestIsReservedWindowsName(t *testing.T) {
	for _, name := range []string{"CON", "con", "Prn", "aux", "NUL", "com1", "COM9", "lpt1", "LPT9"} {
		assert.True(t, config.IsReservedWindowsName(name), name)
	}
	for _, name := range []string{"", "COM0", "COM10", "LPT0", "CONSOLE", "con.txt", "nul "} {
		assert.False(t, config.IsReservedWindowsName(name), name)
	}
}

func TestLoadCatalogs_SchemaKeyWhiteList(t *testing.T) {
	for _, name := range append(badNames(), "") {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			got := summaries(t, "catalogs/main.yml", "name: main\nuc_name: dev\nschemas:\n  "+strconv.Quote(name)+":\n    uc_name: silver\n", loadCatalogErrors)
			require.NotEmpty(t, got)
			assert.Contains(t, got[0], "error: ")
			if name != "" && !strings.Contains(name, "\x00") {
				assert.Contains(t, got[0], "logical schema name "+strconv.Quote(name)+" "+nameRuleText)
			}
		})
	}
	for _, name := range goodNames() {
		got := summaries(t, "catalogs/main.yml", "name: main\nuc_name: dev\nschemas:\n  "+name+": {}\n", loadCatalogErrors)
		assert.Empty(t, got, name)
	}
}

func TestLoadCatalogs_CatalogNameWhiteList(t *testing.T) {
	for _, name := range badNames() {
		if strings.Contains(name, "\x00") {
			continue
		}
		got := summaries(t, "catalogs/main.yml", "uc_name: dev\nname: "+strconv.Quote(name)+"\n", loadCatalogErrors)
		require.Len(t, got, 1, "%q", name)
		assert.Contains(t, got[0], "logical catalog name "+strconv.Quote(name)+" "+nameRuleText)
	}
}

func TestLoadTarget_KeysWhiteList(t *testing.T) {
	for _, name := range badNames() {
		if strings.Contains(name, "\x00") {
			continue
		}
		root := t.TempDir()
		writeFile(t, root, "targets/dev.yml", "catalogs:\n  "+strconv.Quote(name)+":\n    uc_name: c\n    schemas:\n      "+strconv.Quote(name)+":\n        uc_name: s\n")
		_, diags := config.LoadTarget(root, "dev")
		var got []string
		for _, d := range diags {
			got = append(got, d.Summary)
		}
		assert.Equal(t, []string{
			"logical catalog name " + strconv.Quote(name) + " " + nameRuleText,
			"logical schema name " + strconv.Quote(name) + " " + nameRuleText,
		}, got, "%q", name)
	}
}

func TestLoadTarget_NameWhiteList(t *testing.T) {
	root := t.TempDir()
	for _, name := range append(badNames(), "") {
		_, diags := config.LoadTarget(root, name)
		require.Len(t, diags, 1, "%q", name)
		assert.True(t, diags.HasError(), "%q", name)
		assert.Contains(t, diags[0].Summary, "target name ", "%q", name)
	}
	for _, name := range goodNames() {
		_, diags := config.LoadTarget(root, name)
		for _, d := range diags { // the file is missing, but not because of the name
			assert.NotContains(t, d.Summary, "target name ", name)
		}
	}
}

// Physical (UC) names have their own rule: unicode is fine, whitespace, dots, slashes and control characters are not.
func TestUCNameError(t *testing.T) {
	for _, name := range []string{litMain, "каталог_1", "схема-2", "a-b", strings.Repeat("я", 255)} {
		assert.Empty(t, config.UCNameError(name), "%q", name)
	}
	for _, name := range []string{"", litAB, litAB2, pathAB, bsAB, tabAB, nlAB, nbspAB, ctrlAB, "a\x00b", "a\x7fb", strings.Repeat("я", 256)} {
		assert.Equal(t, config.UCNameRule, config.UCNameError(name), "%q", name)
	}
}

func TestValidateLocal_UCNames(t *testing.T) {
	check := func(ucCatalog, ucSchema string) string {
		cfg := resolvedConfig(nil)
		cfg.Schemas = []config.ResolvedSchema{{LogicalCatalog: litMain, LogicalSchema: litSilver2, UCCatalog: ucCatalog, UCSchema: ucSchema}}
		for _, d := range config.ValidateLocal(cfg) {
			return d.String()
		}
		return ""
	}
	assert.Empty(t, check("каталог_1", "схема-2"), "unicode UC names are accepted")
	for _, bad := range []string{litAB, litAB2, pathAB, bsAB, ctrlAB} {
		assert.Contains(t, check(bad, "silver"), "catalog "+strconv.Quote(bad)+" "+config.UCNameRule, "%q", bad)
		assert.Contains(t, check("prod", bad), "schema "+strconv.Quote(bad)+" "+config.UCNameRule, "%q", bad)
	}
}
