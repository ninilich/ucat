package cmd

// Characterization (golden) tests for the whole `init` dialog: for each input the exact stdout and
// stderr, the exit code, the sorted list of created files and the bytes of every file are pinned in
// testdata/init_<scenario>.golden. They guard a refactoring of runInit (the order of the questions, the
// text of the prompts, the error messages and the content of the files must not change).
//
// Regenerate after an intended change:
//
//	go test ./cmd -run TestInit_Golden -update

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

// goldenDir is the absolute path of testdata, fixed at package init: tests change the working directory.
var goldenDir = func() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(wd, "testdata")
}()

// assertGolden compares got with testdata/<name>; with -update it rewrites the file.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *update {
		require.NoError(t, os.MkdirAll(goldenDir, 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file %s; run with -update", name)
	assert.Equal(t, string(want), got, "golden mismatch for %s (run with -update if the change is intended)", name)
}

// goldenBlock renders text as lines prefixed with "| ", marking a missing final newline.
func goldenBlock(s string) string {
	if s == "" {
		return "(empty)\n"
	}
	var b strings.Builder
	noNL := !strings.HasSuffix(s, "\n")
	for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		b.WriteString("| " + l + "\n")
	}
	if noNL {
		b.WriteString("\\ no newline at end\n")
	}
	return b.String()
}

// treeSnapshot lists every file and directory under root (relative, slash-separated, sorted;
// directories end with "/") and returns the content of each file.
func treeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var entries []string
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		entries = append(entries, rel)
		return nil
	}))
	sort.Strings(entries)
	var b strings.Builder
	b.WriteString("-- files\n")
	if len(entries) == 0 {
		b.WriteString("(none)\n")
	}
	for _, e := range entries {
		b.WriteString(e + "\n")
	}
	for _, e := range entries {
		if strings.HasSuffix(e, "/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e)))
		require.NoError(t, err)
		fmt.Fprintf(&b, "-- content %s\n%s", e, goldenBlock(string(data)))
	}
	return b.String()
}

// normTmp replaces the temporary directory with <tmp> and Windows separators with slashes in paths.
func normTmp(s, tmp string) string {
	return strings.ReplaceAll(s, tmp, "<tmp>")
}

// osMkdirReason matches the OS reason at the end of "mkdir <name>: mkdir: <reason>". The reason is the
// text of an OS error and differs per OS ("not a directory" on Unix, "The system cannot find the path
// specified." on Windows), so only it is replaced; the rest of the line stays compared strictly.
var osMkdirReason = regexp.MustCompile(`(?m)(mkdir [^\s:]+: mkdir: ).*$`)

// normOSError replaces the OS reason of a mkdir error with a placeholder.
func normOSError(s string) string {
	return osMkdirReason.ReplaceAllString(s, "${1}<os error>")
}

type initScenario struct {
	name   string
	stdin  string
	stdinD string // description of stdin for the golden file when the input is huge
	setup  func(t *testing.T)
}

func initScenarios() []initScenario {
	return []initScenario{
		{name: "defaults", stdin: "demo\n\nsilver\n\n\n\n\n\n"},
		{name: "defaults_eof", stdin: "demo\n\nsilver\n"},
		{name: "custom_names", stdin: "my proj: x\nsales\nraw\nraw_uc\nstage\nhttps://stage.example.com\nSALES_UC\nwh-1\n"},
		{name: "custom_lowercase", stdin: "my proj: x\nSales\nraw\nraw_uc\nstage\nhttps://stage.example.com\nsales_uc\nwh-1\n"},
		{name: "two_targets", stdin: "proj\nmain\nsilver\nsilver_uc\ndev, prod\nhttps://dev.example.com\ndev_cat\nabc\n\nprod_cat\n\n"},
		{name: "two_targets_hash_and_quote", stdin: "p#1\nmain\ns\ns\ndev,prod\nhttps://dev.example.com/#x\ndev_cat\n'wh'\nhttps://p\nprod_cat\n: wh\n"},
		{name: "two_targets_same_catalog", stdin: "proj\nmain\nsilver\nsilver\ndev,prod\nh1\nshared\nw1\nh2\nshared\nw2\n"},
		{name: "two_targets_default_catalogs", stdin: "proj\nmain\nsilver\nsilver\ndev,prod\nh1\n\nw1\nh2\n\nw2\n"},
		{name: "uppercase_logical_catalog_defaults", stdin: "proj\nMain\nsilver\nsilver\ndev\nh\n\nw\n"},
		{name: "uppercase_logical_catalog_two_targets_defaults", stdin: "proj\nMain\nsilver\nsilver\nDev,Prod\nh\n\nw\nh\n\nw\n"},
		{name: "targets_empty_list", stdin: "proj\nmain\nsilver\nsilver\n , ,\n"},
		{name: "targets_duplicate", stdin: "proj\nmain\nsilver\nsilver\ndev,DEV\n"},
		{name: "target_bad_name", stdin: "proj\nmain\nsilver\nsilver\ndev,a/b\n"},
		{name: "empty_project_name", stdin: "\n"},
		{name: "eof_immediately", stdin: ""},
		{name: "catalog_with_dot", stdin: "proj\nma.in\n"},
		{name: "target_uc_catalog_with_dot", stdin: "proj\nmain\nsilver\nsilver\ndev,prod\nh\nok\nw\nh\na.b\n"},
		{name: "schema_required", stdin: "proj\nmain\n\n"},
		{name: "schema_with_space", stdin: "proj\nmain\nsil ver\n"},
		{name: "uc_schema_uppercase", stdin: "proj\nmain\nsilver\nSilver\n"},
		{name: "long_answer", stdin: strings.Repeat("a", 70000) + "\n", stdinD: "70000 x 'a' + newline"},
		{name: "ucat_yml_exists", stdin: "proj\n", setup: func(t *testing.T) {
			require.NoError(t, os.WriteFile("ucat.yml", []byte("x"), 0o644))
		}},
		{name: "catalog_file_exists", stdin: initAnswersDevOnly, setup: func(t *testing.T) {
			require.NoError(t, os.MkdirAll("catalogs", 0o755))
			require.NoError(t, os.WriteFile("catalogs/main.yml", []byte("x"), 0o644))
		}},
		{name: "target_file_exists", stdin: "proj\nmain\nsilver\nsilver\ndev,prod\n\n\n\n\n\n\n", setup: func(t *testing.T) {
			require.NoError(t, os.MkdirAll("targets", 0o755))
			require.NoError(t, os.WriteFile("targets/prod.yml", []byte("x"), 0o644))
		}},
		{name: "write_error_tables_is_file", stdin: "proj\nmain\nsilver\nsilver\ndev,prod\nh1\nc1\nw1\nh2\nc2\nw2\n", setup: func(t *testing.T) {
			require.NoError(t, os.WriteFile("tables", []byte("x"), 0o644))
		}},
		{name: "write_error_catalogs_is_file", stdin: "proj\nmain\nsilver\nsilver\ndev\n\n\n\n", setup: func(t *testing.T) {
			require.NoError(t, os.WriteFile("catalogs", []byte("x"), 0o644))
		}},
		{name: "write_error_targets_is_file", stdin: "proj\nmain\nsilver\nsilver\ndev\n\n\n\n", setup: func(t *testing.T) {
			require.NoError(t, os.WriteFile("targets", []byte("x"), 0o644))
		}},
		{name: "crlf_input", stdin: "proj\r\nmain\r\nsilver\r\nsilver\r\ndev\r\nhttps://h\r\nmain\r\nwh\r\n"},
	}
}

// TestInit_GoldenDialog runs every scenario in an empty temporary directory and pins the whole result.
// Update: go test ./cmd -run TestInit_Golden -update
func TestInit_GoldenDialog(t *testing.T) {
	for _, sc := range initScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			emptyDir(t)
			tmp, err := os.Getwd()
			require.NoError(t, err)
			if sc.setup != nil {
				sc.setup(t)
			}
			useStdin(t, sc.stdin)
			stdout, stderr, runErr := executeCmdSplit("init")

			var b strings.Builder
			in := sc.stdinD
			if in == "" {
				in = fmt.Sprintf("%q", sc.stdin)
			}
			fmt.Fprintf(&b, "-- stdin\n%s\n", in)
			fmt.Fprintf(&b, "-- exit code\n%d\n", codeOf(runErr))
			fmt.Fprintf(&b, "-- stdout\n%s", goldenBlock(normOSError(normTmp(stdout, tmp))))
			fmt.Fprintf(&b, "-- stderr\n%s", goldenBlock(normOSError(normTmp(stderr, tmp))))
			b.WriteString(treeSnapshot(t, tmp))
			assertGolden(t, "init_"+sc.name+".golden", b.String())
		})
	}
}
