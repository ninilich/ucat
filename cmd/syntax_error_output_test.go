package cmd

// a YAML syntax error is printed like the other load errors, in every command.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyntaxError_AllCommands_FormatAndNoBackend(t *testing.T) {
	type src struct {
		file, content, want string
		needsTarget         bool // the file is read only when -t is given
	}
	sources := map[string]src{
		pathProjectFile: {pathProjectFile, "project: p\ntables_dir: tables\nx: y: z\n",
			"error: failed to parse: mapping values are not allowed in this context (ucat.yml:3)", false},
		litCatalog: {pathCatalogMain, "name: main\nuc_name: dev\nschemas: [a\n",
			"error: failed to parse: did not find expected ',' or ']' (catalogs/main.yml:2)", false},
		litTarget: {pathTargetDev, "workspace:\n  host: https://x\ncatalogs: {a: 1\n",
			"error: failed to parse: did not find expected ',' or '}' (targets/dev.yml:2)", true},
		litTable: {pathSilverOrders, "name: orders\nschema: silver\ncolumns: [a, b\n",
			"error: failed to parse: did not find expected ',' or ']' (tables/silver/orders.yml:2)", false},
	}
	commands := [][]string{
		{cmdValidate},
		{cmdValidate, "-t", litDev},
		{cmdPlan, "-t", litDev},
		{cmdApply, "-t", litDev, flagAutoApprove},
		{cmdPull, "-t", litDev},
		{cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove},
	}
	for sname, s := range sources {
		for _, args := range commands {
			t.Run(sname+"/"+strings.Join(args, " "), func(t *testing.T) {
				if s.needsTarget && args[0] == cmdValidate && len(args) == 1 {
					t.Skip("validate without -t does not read targets")
				}
				if sname == litTable && args[0] == cmdDrop {
					t.Skip("drop does not read table files (see TestDrop_IgnoresBrokenTableFile)")
				}
				tmpDir := setupPlanProject(t)
				writeFile(t, filepath.Join(tmpDir, filepath.FromSlash(s.file)), s.content)
				tb := useSpy(t, newSpy(nil))

				stdout, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				assert.Equal(t, 1, codeOf(err))
				all := stdout + stderr + err.Error()
				assert.Contains(t, stdout+stderr, s.want)
				assert.NotContains(t, all, "\r")
				assert.NotContains(t, all, tmpDir)
				assert.NotContains(t, all, "yaml: line")
				tb.assertUntouched(t)
			})
		}
	}
}

// Without a line number from yaml.v3 the old format stays: the file name is in the text.
func TestSyntaxError_NoLine_AllCommands_OldFormat(t *testing.T) {
	cases := map[string]struct{ file, content string }{
		pathProjectFile: {pathProjectFile, brokenYAML},
		"catalog":       {"catalogs/main.yml", brokenYAML},
		litTarget:       {pathTargetDev, brokenYAML},
		"table":         {pathSilverOrders, "a: b: c\n"},
		"binary":        {pathProjectFile, "\x00\xff"},
	}
	for name, c := range cases {
		for _, args := range [][]string{{cmdValidate, "-t", litDev}, {cmdPlan, "-t", litDev}, {cmdPull, "-t", litDev}} {
			t.Run(name+"/"+args[0], func(t *testing.T) {
				tmpDir := setupPlanProject(t)
				writeFile(t, filepath.Join(tmpDir, filepath.FromSlash(c.file)), c.content)
				tb := useSpy(t, newSpy(nil))

				stdout, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				assert.Equal(t, 1, codeOf(err))
				out := stdout + stderr
				assert.Contains(t, out, "error: failed to parse "+c.file+": yaml: ")
				assert.NotContains(t, out, "yaml: line")
				assert.NotContains(t, out, tmpDir)
				for _, r := range out {
					assert.False(t, r < 0x20 && r != '\n' || r == 0x7f || r == 0xfffd, "raw control char %q in %q", r, out)
				}
				tb.assertUntouched(t)
			})
		}
	}
}

func TestSyntaxError_Commands_LFEqualsCRLF(t *testing.T) {
	bodies := map[string]string{
		"unclosed quote":   "name: orders\nschema: silver\ncomment: \"x\n",
		"unclosed bracket": "name: orders\nschema: silver\ncolumns: [a, b\n",
		"tab indent":       "name: orders\nschema: silver\ncolumns:\n\t- name: id\n",
		"no line":          "a: b: c\n",
	}
	for name, body := range bodies {
		for _, args := range [][]string{{cmdValidate}, {cmdPlan, "-t", litDev}, {cmdPull, "-t", litDev}} {
			t.Run(name+"/"+args[0], func(t *testing.T) {
				var outs [2]string
				for i, content := range []string{body, crlf(body)} {
					tmpDir := setupPlanProject(t)
					writeFile(t, filepath.Join(tmpDir, "tables", "silver", "orders.yml"), content)
					useSpy(t, newSpy(nil))
					stdout, stderr, err := executeCmdSplit(args...)
					require.Error(t, err)
					outs[i] = stdout + stderr
				}
				assert.NotEmpty(t, outs[0])
				assert.NotContains(t, outs[1], "\r")
				assert.Equal(t, outs[0], outs[1])
			})
		}
	}
}

// Neighbouring formats stay as they were.
func TestSyntaxError_NeighbourFormatsUnchanged_Commands(t *testing.T) {
	cases := map[string]struct{ file, content, want string }{
		"ucat.yml unknown field": {"ucat.yml", "project: p\nfoo: 1\n", `error: field "foo" is not supported; if the file was written for a newer ucat, upgrade ucat (ucat.yml:2)`},
		"catalog unknown field":  {"catalogs/main.yml", "name: main\nuc_name: dev\nfoo: 1\n", `error: field "foo" is not supported; if the file was written for a newer ucat, upgrade ucat (catalogs/main.yml:3)`},
		"target unknown field":   {"targets/dev.yml", "workspace:\n  host: https://x\nfoo: 1\n", `error: field "foo" is not supported; if the file was written for a newer ucat, upgrade ucat (targets/dev.yml:3)`},
		"table unknown field":    {"tables/silver/orders.yml", "name: orders\nschema: silver\nfoo: 1\n", `error: field "foo" is not supported; if the file was written for a newer ucat, upgrade ucat (tables/silver/orders.yml:3)`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tmpDir := setupPlanProject(t)
			writeFile(t, filepath.Join(tmpDir, filepath.FromSlash(c.file)), c.content)
			tb := useSpy(t, newSpy(nil))
			stdout, stderr, err := executeCmdSplit("validate", "-t", "dev")
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, stdout+stderr, c.want)
			tb.assertUntouched(t)
		})
	}
}
