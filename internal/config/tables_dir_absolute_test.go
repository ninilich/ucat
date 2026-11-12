package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadWithUcatYml(t *testing.T, content string) (config.ProjectConfig, int, []string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ucat.yml"), []byte(content), 0o644))
	proj, diags := config.LoadProject(root)
	var summaries []string
	for _, d := range diags {
		summaries = append(summaries, d.Summary)
	}
	return proj, len(diags), summaries
}

// Absolute forms are strings checked on every OS (no filepath dependence); quoted and,
// where YAML allows it, plain scalars.
func TestLoadProject_AbsoluteTablesDir_Forms(t *testing.T) {
	forms := []string{
		"/abs", `\rooted`, litCX, pathCX, "C:x", "C:", "c:relative", "x:y",
		"//server/share/t", `\\server\share`,
	}
	check := func(t *testing.T, val, want string) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "ucat.yml"), []byte("project: p\ntables_dir: "+val+"\n"), 0o644))
		_, diags := config.LoadProject(root)
		require.Len(t, diags, 1, "%v", diags)
		assert.True(t, diags.HasError())
		assert.Equal(t, `tables_dir must be relative to the project root (got "`+want+`")`, diags[0].Summary)
		assert.Equal(t, "ucat.yml:2", diags[0].Location)
	}
	for _, f := range forms {
		t.Run("quoted/"+f, func(t *testing.T) { check(t, "'"+f+"'", f) })
	}
	// plain scalars ("C:" alone would be a YAML mapping and is not tested unquoted)
	for _, f := range []string{"/abs", `\rooted`, `C:\x`, "C:/x", "c:relative", "x:y", "//server/share/t"} {
		t.Run("plain/"+f, func(t *testing.T) { check(t, f, f) })
	}
}

func TestLoadProject_RelativeTablesDir_NoError(t *testing.T) {
	for _, f := range []string{litTables, "./tables", pathAB, `tables\prod`, `..\tables`, "../tables", `''`} {
		t.Run(f, func(t *testing.T) {
			root := t.TempDir()
			v := f
			if f != `''` {
				v = "'" + f + "'"
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, "ucat.yml"), []byte("project: p\ntables_dir: "+v+"\n"), 0o644))
			_, diags := config.LoadProject(root)
			assert.Empty(t, diags)
		})
	}
}

// The location carries the real line of the key, not a fixed one.
func TestLoadProject_AbsoluteTablesDir_LocationLine(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ucat.yml"),
		[]byte("# comment\nproject: p\n\nworkspace:\n  warehouse_id: w\ntables_dir: /abs\n"), 0o644))
	_, diags := config.LoadProject(root)
	require.Len(t, diags, 1)
	assert.Equal(t, "ucat.yml:6", diags[0].Location)
}

// CRLF files: no "\r" in the location or the message.
func TestLoadProject_AbsoluteTablesDir_CRLF(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ucat.yml"),
		[]byte("project: p\r\ntables_dir: /abs/t\r\n"), 0o644))
	_, diags := config.LoadProject(root)
	require.Len(t, diags, 1)
	assert.Equal(t, "ucat.yml:2", diags[0].Location)
	assert.Equal(t, `tables_dir must be relative to the project root (got "/abs/t")`, diags[0].Summary)
	assert.NotContains(t, diags[0].Summary+diags[0].Location, "\r")
}

// Non-string tables_dir, fixed by fact (contract does not define it).
func TestLoadProject_TablesDirNotString_FixedByFact(t *testing.T) {
	// a list cannot be decoded into a string: a decode error, not the absolute-path error
	_, n, sums := loadWithUcatYml(t, "project: p\ntables_dir: [a, b]\n")
	require.Equal(t, 1, n)
	assert.NotContains(t, sums[0], "must be relative")
	// a number is decoded as the string "5" by yaml.v3 and is a relative path: no error
	_, n, _ = loadWithUcatYml(t, "project: p\ntables_dir: 5\n")
	assert.Equal(t, 0, n)
}

// Order with other load errors, fixed by fact: a decode error (unknown field) is reported alone,
// the absolute-path check runs only after a successful decode.
func TestLoadProject_AbsoluteTablesDir_WithDecodeError_FixedByFact(t *testing.T) {
	_, n, sums := loadWithUcatYml(t, "project: p\nbogus: 1\ntables_dir: /abs\n")
	require.Equal(t, 1, n)
	assert.Contains(t, sums[0], "bogus")
}
