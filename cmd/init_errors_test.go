package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dirNames lists the entries of the current directory (recursively, slash paths), sorted.
func dirNames(t *testing.T) []string {
	t.Helper()
	var names []string
	require.NoError(t, filepath.WalkDir(".", func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." {
			names = append(names, filepath.ToSlash(p))
		}
		return nil
	}))
	sort.Strings(names)
	return names
}

// A file with the name of a directory init must create: "mkdir <name>: <reason>", no absolute path.
// Files already written at the moment of the error stay (fact: init does not roll back).
func TestInit_MkdirObstacle_NoAbsolutePath(t *testing.T) {
	cases := []struct {
		obstacle string
		wantMsg  string
		wantLeft []string
		partial  string // what stderr lists as created
	}{
		{litCatalogs, "mkdir catalogs: mkdir: not a directory", []string{litCatalogs, pathProjectFile}, pathProjectFile},
		{litTargets, "mkdir targets: mkdir: not a directory", []string{"catalogs", pathCatalogMain, "targets", pathProjectFile}, "ucat.yml, catalogs/, catalogs/main.yml"},
		{litTables, "mkdir tables: mkdir: not a directory", []string{"catalogs", pathCatalogMain, litTables, "targets", pathTargetDev, "ucat.yml"}, "ucat.yml, catalogs/, catalogs/main.yml, targets/, targets/dev.yml"},
	}
	for _, tc := range cases {
		t.Run(tc.obstacle, func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(tc.obstacle, []byte("x"), 0o644))
			useStdin(t, initFullAnswers)
			stdout, stderr, err := executeCmdSplit("init")
			out := stdout + stderr
			require.Error(t, err)
			assert.Equal(t, "project was created partially: "+tc.partial+"; remove them or fix the cause and rerun init in an empty directory\nError: "+err.Error()+"\n", stderr)
			msg := err.Error()
			if os.PathSeparator == '/' {
				assert.Equal(t, tc.wantMsg, msg)
			}
			assert.True(t, strings.HasPrefix(msg, "mkdir "+tc.obstacle+": "), msg)
			assert.Equal(t, 1, strings.Count(msg, tc.obstacle), "path must not repeat: %s", msg)
			assertNoAbsolutePath(t, dir, msg+out)
			assert.Equal(t, 1, codeOf(err))
			assert.Equal(t, tc.wantLeft, dirNames(t))
		})
	}
}

// catalogs/ exists but is read-only: "write catalogs/main.yml: ...", no absolute path, once.
func TestInit_CatalogWriteError_RelativePathOnce(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	emptyDir(t)
	dir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll("catalogs", 0o755))
	denyAccess(t, filepath.Join(dir, "catalogs"), 0o755)
	out, err := executeInitCmd(t, initFullAnswers)
	require.Error(t, err)
	msg := err.Error()
	assert.Equal(t, "write catalogs/main.yml: open: permission denied", msg)
	assert.Equal(t, 1, strings.Count(msg, "main.yml"))
	assertNoAbsolutePath(t, dir, msg+out)
	// ucat.yml was already written (no rollback); nothing is created under targets/
	_, statErr := os.Stat("ucat.yml")
	assert.NoError(t, statErr)
	_, statErr = os.Stat("targets")
	assert.True(t, os.IsNotExist(statErr))
}

// An answer over 64 KiB to ANY question: its name is in the message, code 1, no files, no scanner jargon.
func TestInit_AnswerTooLong_EveryQuestion(t *testing.T) {
	good := []string{"proj", litMain, litSilver, litSilver, litDev, "https://h.example.com", litMain, "wh1"}
	questions := []string{
		"Project name",
		"Logical catalog name [main]",
		"Logical schema name",
		"UC schema name [silver]",
		"Target names (comma-separated) [dev]",
		"Workspace host for 'dev'",
		"UC catalog name for target 'dev' [main]",
		"SQL warehouse ID for target 'dev' (from Databricks UI > SQL Warehouses, optional)",
	}
	for i, q := range questions {
		t.Run(q, func(t *testing.T) {
			emptyDir(t)
			stdin := strings.Join(good[:i], "\n")
			if i > 0 {
				stdin += "\n"
			}
			stdin += strings.Repeat("x", 70*1024) + "\n"
			useStdin(t, stdin)
			stdout, stderr, err := executeCmdSplit("init")
			require.Error(t, err)
			assert.Equal(t, fmt.Sprintf("answer to %q is too long (max 64 KiB)", q), err.Error())
			assert.NotContains(t, err.Error(), "bufio")
			assert.NotContains(t, err.Error(), "token too long")
			assert.Equal(t, 1, codeOf(err))
			assert.NotContains(t, stdout+stderr, "✓", "nothing may be reported as created")
			assert.Empty(t, dirNames(t), "no file may be created")
		})
	}
}

// failAfterReader returns data and then a read error instead of EOF.
type failAfterReader struct {
	data string
	err  error
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

var _ io.Reader = (*failAfterReader)(nil)

// A read error in the middle of the dialogue (after some good answers, at any point): no files, code 1.
func TestInit_ReadErrorMidDialogue_NoFiles(t *testing.T) {
	good := []string{"proj\n", "main\n", "silver\n", "silver\n", "dev\n", "https://h.example.com\n", "main\n"}
	for n := 0; n <= len(good); n++ {
		t.Run(fmt.Sprintf("after_%d_answers", n), func(t *testing.T) {
			emptyDir(t)
			useStdinReader(t, &failAfterReader{data: strings.Join(good[:n], ""), err: fmt.Errorf("disk on fire")})
			_, _, err := executeCmdSplit("init")
			require.Error(t, err)
			assert.Equal(t, "failed to read answer: disk on fire", err.Error())
			assert.Equal(t, 1, codeOf(err))
			assert.Empty(t, dirNames(t))
		})
	}
}

// A read error right after a partial last line (no newline): bufio delivers the partial token only at EOF,
// a non-EOF error is reported instead. Fact fixed here: the partial line is NOT used as an answer.
func TestInit_ReadErrorAfterPartialLine(t *testing.T) {
	emptyDir(t)
	useStdinReader(t, &failAfterReader{data: "proj\nmai", err: fmt.Errorf("disk on fire")})
	_, _, err := executeCmdSplit("init")
	require.Error(t, err)
	assert.Equal(t, "failed to read answer: disk on fire", err.Error())
	assert.Empty(t, dirNames(t))
}

// The bufio.Scanner boundary (MaxScanTokenSize = 64 KiB = 65536) for the answer to the second question.
// Fact: the answer may be at most 65535 bytes, with or without a trailing "\n" (65536 is already too long).
// The answer is a long logical catalog name, so being read whole is visible as a different error
// (the name rule: over 100 characters), not "too long".
func TestInit_AnswerLengthBoundary(t *testing.T) {
	const maxAnswer = 64 * 1024
	cases := []struct {
		name    string
		n       int
		nl      bool
		tooLong bool
	}{
		{"max-1_with_newline", maxAnswer - 1, true, false},
		{"max_with_newline", maxAnswer, true, true},
		{"max+1_with_newline", maxAnswer + 1, true, true},
		{"max-1_no_newline", maxAnswer - 1, false, false},
		{"max_no_newline", maxAnswer, false, true},
		{"max+1_no_newline", maxAnswer + 1, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emptyDir(t)
			stdin := "proj\n" + strings.Repeat("x", tc.n)
			if tc.nl {
				stdin += "\n"
			}
			useStdin(t, stdin)
			_, _, err := executeCmdSplit("init")
			require.Error(t, err)
			if tc.tooLong {
				assert.Equal(t, `answer to "Logical catalog name [main]" is too long (max 64 KiB)`, err.Error())
			} else {
				// the answer was read whole; as a catalog name it is far over the 100 characters
				assert.Contains(t, err.Error(), nameRuleText)
			}
			assert.Empty(t, dirNames(t))
		})
	}
}

// the second target cannot be written (a dangling symlink in its place): the first one
// is listed as created, in creation order; the pre-existing targets/ is not listed.
func TestInit_SecondTargetWriteError_PartialListed(t *testing.T) {
	if runtime.GOOS == litWindows {
		t.Skip("symlinks need privileges on Windows")
	}
	emptyDir(t)
	require.NoError(t, os.MkdirAll("targets", 0o755))
	require.NoError(t, os.Symlink("no-such-dir/x.yml", "targets/b.yml"))
	useStdin(t, "proj\nmain\nsilver\nsilver\na,b\nh1\nc1\nw1\nh2\nc2\nw2\n")
	_, stderr, err := executeCmdSplit("init")
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "write targets/b.yml: "), err.Error())
	assert.Equal(t, 1, codeOf(err))
	assert.Equal(t, "project was created partially: ucat.yml, catalogs/, catalogs/main.yml, targets/a.yml; remove them or fix the cause and rerun init in an empty directory\nError: "+err.Error()+"\n", stderr)
	assert.Equal(t, 1, strings.Count(stderr, err.Error()), "the error text is printed once, not repeated in the addition")
}

// nothing created yet (ucat.yml cannot be written) or no error at all: no addition.
func TestInit_NoPartialMessage(t *testing.T) {
	t.Run("first write fails", func(t *testing.T) {
		skipUnlessPermissionsEnforced(t)
		emptyDir(t)
		dir, err := os.Getwd()
		require.NoError(t, err)
		denyAccess(t, dir, 0o755)
		useStdin(t, initFullAnswers)
		_, stderr, err := executeCmdSplit("init")
		require.Error(t, err)
		assert.NotContains(t, stderr, "partially")
	})
	t.Run("success", func(t *testing.T) {
		emptyDir(t)
		useStdin(t, initFullAnswers)
		stdout, stderr, err := executeCmdSplit("init")
		require.NoError(t, err)
		assert.NotContains(t, stdout+stderr, "partially")
	})
}
