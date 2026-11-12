package config_test

// syntax errors of YAML files across all four loaders.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syntaxLoader struct {
	name string
	file string
	load func(t *testing.T, content string) diag.Diagnostics
}

func syntaxLoaders() []syntaxLoader {
	return []syntaxLoader{
		{litProject, "ucat.yml", func(t *testing.T, c string) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "ucat.yml", c)
			_, d := config.LoadProject(root)
			return d
		}},
		{litCatalog, pathCatalogMain, func(t *testing.T, c string) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "catalogs/main.yml", c)
			_, d := config.LoadCatalogs(root)
			return d
		}},
		{litTarget, "targets/dev.yml", func(t *testing.T, c string) diag.Diagnostics {
			root := t.TempDir()
			writeFile(t, root, "targets/dev.yml", c)
			_, d := config.LoadTarget(root, "dev")
			return d
		}},
		{litTable, pathSilverOrders, func(t *testing.T, c string) diag.Diagnostics {
			_, d := loadTablesFrom(t, c)
			return d
		}},
	}
}

func oneError(t *testing.T, d diag.Diagnostics) diag.Diagnostic {
	t.Helper()
	errs := errDiags(d)
	require.Len(t, errs, 1, errs)
	return errs[0]
}

// Reasons are pinned to the actual yaml.v3 behaviour: with a line number the new format, without it the old one.
func TestSyntaxError_reasons_withAndWithoutLine(t *testing.T) {
	pre := "name: orders\nschema: silver\n"
	cases := []struct {
		name    string
		content string
		line    int // 0: yaml.v3 gives no line, old format
		reason  string
	}{
		{"unclosed quote", pre + "comment: \"x\n", 3, litFoundUnexpectedEndOfStream},
		{"unclosed quote in first key", "name: \"x\n", 2, "found unexpected end of stream"},
		{"tab indent", pre + "columns:\n\t- name: id\n", 4, "found character that cannot start any token"},
		{"unclosed bracket", pre + "columns: [a, b\n", 2, "did not find expected ',' or ']'"},
		{"unclosed brace", pre + "columns: {a: 1\n", 2, "did not find expected ',' or '}'"},
		{"bad list indent", pre + "columns:\n  - name: id\n   type: INT\n", 3, "did not find expected '-' indicator"},
		{"expected key", pre + "? a\n- b\n", 3, "did not find expected key"},
		{"block seq not allowed", pre + "comment: !!str\nx: - \n", 4, "block sequence entries are not allowed in this context"},
		{"bad token", pre + "comment: @x\n", 3, "found character that cannot start any token"},
		{"mapping values line 4", "# c\n" + pre + "columns: x: y\n", 4, "mapping values are not allowed in this context"},
		{"unknown anchor", pre + "comment: *x\n", 0, "unknown anchor 'x' referenced"},
		{"control char", pre + "comment: a\x01b\n", 0, "control characters are not allowed"},
		{"mapping values line 1", "a: b: c\n", 0, "mapping values are not allowed in this context"},
		{"multiline string unterminated", pre + "comment: 'abc\n  def\n", 3, "found unexpected end of stream"},
	}
	for _, l := range syntaxLoaders() {
		for _, tc := range cases {
			for _, eol := range []string{"\n", "\r\n"} {
				t.Run(l.name+"/"+tc.name+"/"+map[string]string{"\n": "LF", "\r\n": "CRLF"}[eol], func(t *testing.T) {
					e := oneError(t, l.load(t, strings.ReplaceAll(tc.content, "\n", eol)))
					if tc.line > 0 {
						assert.Equal(t, "failed to parse: "+tc.reason, e.Summary)
						assert.Equal(t, l.file+":"+strconv.Itoa(tc.line), e.Location)
					} else {
						assert.Equal(t, "failed to parse "+l.file+": yaml: "+tc.reason, e.Summary)
						assert.Equal(t, l.file, e.Location)
					}
					assert.NotContains(t, e.Summary, "\n")
					assert.NotContains(t, e.Summary, "\r")
					assert.NotContains(t, e.Location, "\r")
					assert.NotContains(t, e.Summary, "yaml: line")
					assert.NotContains(t, e.Location, `\`)
				})
			}
		}
	}
}

// A single-line `{` is reported by yaml.v3 with line 1, so it gets the location with a line.
func TestSyntaxError_singleBraceLine1_allLoaders(t *testing.T) {
	for _, l := range syntaxLoaders() {
		t.Run(l.name, func(t *testing.T) {
			e := oneError(t, l.load(t, "{"))
			assert.Equal(t, "failed to parse: did not find expected node content", e.Summary)
			assert.Equal(t, l.file+":1", e.Location)
		})
	}
}

func TestSyntaxError_noLineOnFirstLine_allLoaders(t *testing.T) {
	for _, l := range syntaxLoaders() {
		t.Run(l.name, func(t *testing.T) {
			e := oneError(t, l.load(t, "a: b: c\n"))
			assert.Equal(t, "failed to parse "+l.file+": yaml: mapping values are not allowed in this context", e.Summary)
			assert.Equal(t, l.file, e.Location)
		})
	}
}

func TestSyntaxError_binaryFile_allLoaders(t *testing.T) {
	for _, l := range syntaxLoaders() {
		t.Run(l.name, func(t *testing.T) {
			var d diag.Diagnostics
			require.NotPanics(t, func() { d = l.load(t, "\x00\xff") })
			e := oneError(t, d)
			assert.Equal(t, "failed to parse "+l.file+": yaml: control characters are not allowed", e.Summary)
			assert.Equal(t, l.file, e.Location)
			for _, r := range e.Summary + e.Location {
				assert.False(t, r < 0x20 || r == 0x7f || r == 0xfffd, "raw control char %q", r)
			}
		})
	}
}

func TestSyntaxError_emptyFile_noError_allLoaders(t *testing.T) {
	for _, l := range syntaxLoaders() {
		t.Run(l.name, func(t *testing.T) {
			assert.Empty(t, errDiags(l.load(t, "")), l.name)
		})
	}
}

func TestSyntaxError_lfAndCRLF_sameDiagnostic(t *testing.T) {
	bodies := []string{
		"# c\nname: x\nschema: s\ncolumns: x: y\n",
		"name: x\nschema: s\ncolumns: [a, b\n",
		"name: x\nschema: s\ncomment: \"q\n",
		"name: x\nschema: s\ncolumns:\n\t- a\n",
		"a: b: c\n",
	}
	for _, l := range syntaxLoaders() {
		for i, b := range bodies {
			t.Run(l.name+"/"+strconv.Itoa(i), func(t *testing.T) {
				lf := oneError(t, l.load(t, b))
				cr := oneError(t, l.load(t, strings.ReplaceAll(b, "\n", "\r\n")))
				assert.Equal(t, lf.Summary, cr.Summary)
				assert.Equal(t, lf.Location, cr.Location)
				assert.NotContains(t, cr.Summary+cr.Location, "\r")
			})
		}
	}
}

// Neighbouring formats are untouched: TypeError-based diagnostics keep their text and location.
func TestSyntaxError_typeErrorsUnchanged_allLoaders(t *testing.T) {
	cases := []struct {
		loader  string
		content string
		summary string
		line    int
	}{
		{"project", "project: p\nfoo: 1\n", litFieldFooIsNotSupported, 2},
		{"catalog", "name: main\nuc_name: dev\nfoo: 1\n", litFieldFooIsNotSupported, 3},
		{"target", "workspace:\n  host: https://x\nfoo: 1\n", litFieldFooIsNotSupported, 3},
		{"table", "name: orders\nschema: silver\nfoo: 1\n", litFieldFooIsNotSupported, 3},
	}
	byName := map[string]syntaxLoader{}
	for _, l := range syntaxLoaders() {
		byName[l.name] = l
	}
	for _, tc := range cases {
		t.Run(tc.loader, func(t *testing.T) {
			l := byName[tc.loader]
			e := oneError(t, l.load(t, tc.content))
			assert.Equal(t, tc.summary, e.Summary)
			assert.Equal(t, l.file+":"+strconv.Itoa(tc.line), e.Location)
		})
	}
	t.Run("wrong type and duplicate key keep line locations", func(t *testing.T) {
		for _, c := range []string{
			"name: orders\nschema: silver\ncolumns: 5\n",
			"name: orders\nname: orders\nschema: silver\n",
		} {
			e := oneError(t, byName["table"].load(t, c))
			assert.NotContains(t, e.Summary, "failed to parse")
			assert.Regexp(t, `^tables/silver/orders\.yml:\d+$`, e.Location)
		}
		e := oneError(t, byName["project"].load(t, "project: p\ntables_dir: [a]\n"))
		assert.Equal(t, "ucat.yml:2", e.Location)
		assert.NotContains(t, e.Summary, "failed to parse")
	})
}
