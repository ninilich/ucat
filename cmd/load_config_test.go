package cmd

// load_config_test.go covers: -t is required for plan/apply and
// config loading is shared between commands.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errorLines returns the sorted "error: ..." lines of stderr.
func errorLines(stderr string) []string {
	var lines []string
	for _, l := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(l, "error: ") {
			lines = append(lines, l)
		}
	}
	sort.Strings(lines)
	return lines
}

// --- -t is required for plan and apply ---

func TestPlanApply_RequireTarget_InProject(t *testing.T) {
	for _, args := range [][]string{
		{cmdPlan},
		{cmdPlan, flagJSON},
		{cmdPlan, "-t", ""},
		{cmdApply},
		{cmdApply, flagAutoApprove},
		{cmdApply, flagAutoApprove, flagJSON},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setupPlanProject(t)
			tb := useSpy(t, newSpy(nil))

			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "requires -t")
			assert.Contains(t, err.Error(), args[0])
			assert.Contains(t, stderr, "-t")
			assert.Empty(t, stdout, "stdout must stay empty (incl. --json)")
			tb.assertUntouched(t)
		})
	}
}

// The check happens before the project lookup: outside a project the error is still about -t.
func TestPlanApply_RequireTarget_OutsideProject(t *testing.T) {
	for _, args := range [][]string{{cmdPlan}, {cmdPlan, flagJSON}, {cmdApply, flagAutoApprove}, {cmdApply, flagJSON}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			emptyDir(t)
			tb := useSpy(t, newSpy(nil))

			stdout, _, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "requires -t")
			assert.Empty(t, stdout)
			tb.assertUntouched(t)
		})
	}
}

func TestPlan_RequireTarget_DetailedExitcodeIsError(t *testing.T) {
	setupPlanProject(t)
	useSpy(t, newSpy(nil))

	_, _, err := executeCmdSplit("plan", "--detailed-exitcode")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err), "missing -t must exit 1, not 2")
}

// --- missing target file ---

func TestPlanApply_UnknownTarget_NamesTarget(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litNope}, {cmdApply, "-t", litNope, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			setupPlanProject(t)
			tb := useSpy(t, newSpy(nil))

			stdout, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Contains(t, err.Error()+stderr, "nope")
			assert.Empty(t, stdout)
			tb.assertUntouched(t)
		})
	}
}

// --- regression: -t dev keeps working ---

func TestPlanApply_WithTarget_Works(t *testing.T) {
	setupPlanProject(t)
	tb := useSpy(t, newSpy(nil))

	stdout, _, err := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, stdout, "orders")
	assert.Equal(t, 1, tb.listCalls)

	stdout, _, err = executeCmdSplit("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)
	assert.Contains(t, stdout, "orders")
	assert.NotEmpty(t, tb.RecordedCalls(), "apply must reach the backend with -t")
}

func TestPlanApply_EmptyTargetFile_Accepted(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			tmpDir := setupPlanProject(t)
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "targets", "dev.yml"), nil, 0644))
			t.Setenv("DATABRICKS_WAREHOUSE_ID", "wh123") // apply needs a warehouse; the target file has none
			tb := useSpy(t, newSpy(nil))

			stdout, stderr, err := executeCmdSplit(args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Contains(t, stdout, "orders")
			assert.Equal(t, 1, tb.factoryCalls)
		})
	}
}

// --- diagnostics from loading ---

// addCatalogFile writes catalogs/<file>.yml declaring a catalog with the given logical name.
func addCatalogFile(t *testing.T, tmpDir, file, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "catalogs", file+".yml"),
		[]byte("name: "+name+"\nuc_name: "+name+"\n"), 0644))
}

func TestPlanApply_LoadWarnings_PrintedToStderr(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}} {
		t.Run(args[0], func(t *testing.T) {
			tmpDir := setupPlanProject(t)
			addCatalogFile(t, tmpDir, "zz_file", "extra")
			useSpy(t, newSpy(nil))

			stdout, stderr, err := executeCmdSplit(args...)
			require.NoError(t, err)
			assert.Contains(t, stderr, "warning: ")
			assert.Contains(t, stderr, `catalog name "extra" does not match filename "zz_file"`)
			assert.NotContains(t, stdout, "does not match filename")
		})
	}
}

func TestPlan_LoadWarnings_OrderDeterministic(t *testing.T) {
	tmpDir := setupPlanProject(t)
	addCatalogFile(t, tmpDir, "b_file", "bbb")
	addCatalogFile(t, tmpDir, "a_file", "aaa")
	addCatalogFile(t, tmpDir, "c_file", "ccc")
	useSpy(t, newSpy(nil))

	var first string
	for i := 0; i < 20; i++ {
		_, stderr, err := executeCmdSplit("plan", "-t", "dev")
		require.NoError(t, err, "run %d", i)
		var warns []string
		for _, l := range strings.Split(stderr, "\n") {
			if strings.HasPrefix(l, "warning: ") {
				warns = append(warns, l)
			}
		}
		require.Len(t, warns, 3, "run %d: %s", i, stderr)
		joined := strings.Join(warns, "\n")
		if i == 0 {
			first = joined
			assertOrdered(t, stderr, "run 0", `"aaa"`, `"bbb"`, `"ccc"`)
		}
		require.Equal(t, first, joined, "run %d", i)
	}
}

func TestPlanApply_LoadError_StopsBeforeBackend(t *testing.T) {
	cases := map[string]func(t *testing.T, tmpDir string){
		"broken catalog yaml": func(t *testing.T, tmpDir string) {
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "catalogs", "bad.yml"), []byte("name: [oops\n"), 0644))
		},
		"broken target yaml": func(t *testing.T, tmpDir string) {
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "targets", "dev.yml"), []byte("workspace: [oops\n"), 0644))
		},
		"broken table yaml": func(t *testing.T, tmpDir string) {
			addTableFile(t, tmpDir, "silver", "bad", "name: [oops\n")
		},
	}
	for name, mutate := range cases {
		for _, cmdName := range []string{cmdPlan, cmdApply} {
			t.Run(name+"/"+cmdName, func(t *testing.T) {
				tmpDir := setupPlanProject(t)
				mutate(t, tmpDir)
				tb := useSpy(t, newSpy(nil))

				args := []string{cmdName, "-t", litDev}
				if cmdName == cmdApply {
					args = append(args, "--auto-approve")
				}
				stdout, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				assert.Contains(t, stderr, "error: ")
				assert.Empty(t, stdout)
				tb.assertUntouched(t)
			})
		}
	}
}

// --- validate without -t stays local ---

func TestValidate_WithoutTarget_StaysLocal(t *testing.T) {
	setupPlanProject(t)
	tb := useSpy(t, newSpy(nil))

	stdout, _, err := executeCmdSplit("validate")
	require.NoError(t, err)
	assert.Contains(t, stdout, "all local checks passed")
	tb.assertUntouched(t)
}

func TestValidate_WithoutTarget_NoTargetFileNeeded(t *testing.T) {
	tmpDir := setupPlanProject(t)
	require.NoError(t, os.RemoveAll(filepath.Join(tmpDir, "targets")))
	tb := useSpy(t, newSpy(nil))

	stdout, _, err := executeCmdSplit("validate")
	require.NoError(t, err)
	assert.Contains(t, stdout, "all local checks passed")
	tb.assertUntouched(t)
}

// --- parity: the same violation yields the same errors in plan and apply ---

func TestPlanApply_ErrorParity(t *testing.T) {
	const body = "\ncolumns:\n  - name: id\n    type: BIGINT\n"
	cases := map[string]struct {
		files map[string]string // "<schema dir>/<file>" -> content
		want  string
	}{
		"uppercase table name": {map[string]string{"silver/orders": "name: Orders\nschema: silver" + body}, "lowercase"},
		litUnknownSchema:       {map[string]string{"nonexistent/ghost": "name: ghost\nschema: nonexistent" + body}, "unknown schema ref"},
		litDuplicateTable: {map[string]string{
			"silver/orders_a": "name: orders\nschema: silver" + body,
			"silver/orders_b": "name: orders\nschema: silver" + body,
		}, litOrders},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			for path, content := range tc.files {
				dir, file, _ := strings.Cut(path, "/")
				addTableFile(t, tmpDir, dir, file, content)
			}
			tb := useSpy(t, newSpy(nil))

			planOut, planErr, err := executeCmdSplit("plan", "-t", "dev")
			require.Error(t, err)
			applyOut, applyErr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve")
			require.Error(t, err)

			planLines, applyLines := errorLines(planErr), errorLines(applyErr)
			require.NotEmpty(t, planLines)
			assert.Contains(t, strings.Join(planLines, "\n"), tc.want)
			assert.Equal(t, planLines, applyLines, "plan and apply must report identical errors")
			assert.Empty(t, planOut)
			assert.Empty(t, applyOut)
			tb.assertUntouched(t)
		})
	}
}

// --- relative paths in load errors ---

func TestLoadErrors_PathsRelativeInAllCommands(t *testing.T) {
	bad := map[string]string{
		"unknown field": "name: orders\nschema: silver\nbogus: 1\ncolumns:\n  - name: id\n    type: BIGINT\n",
		"parse error":   "name: [oops\n",
	}
	cmds := [][]string{
		{cmdValidate}, {cmdValidate, "-t", litDev}, {cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove},
		{cmdPull, "-t", litDev}, {cmdDrop, "-t", litDev, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove},
	}
	for name, body := range bad {
		for _, args := range cmds {
			t.Run(name+"/"+strings.Join(args, "_"), func(t *testing.T) {
				tmpDir := setupPlanProject(t)
				useBackend(t, upToDateMock())
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tables", "silver", "orders.yml"), []byte(body), 0644))
				cwd, err := os.Getwd()
				require.NoError(t, err)

				stdout, stderr, err := executeCmdSplit(args...)
				all := stdout + stderr
				if err != nil {
					all += err.Error()
				}
				require.NotEmpty(t, all)
				if args[0] != cmdDrop { // drop works from --schema/--table and never reads table files
					assert.Contains(t, all, "tables/silver/orders.yml", "relative path of the broken file, always with /")
				}
				for _, root := range []string{tmpDir, cwd} {
					assert.NotContains(t, all, root)
					assert.NotContains(t, all, config.SlashPath(root))
				}
			})
		}
	}
}

func TestLoadErrors_ShowRelativeTablePath(t *testing.T) {
	tmpDir := setupPlanProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tables", "silver", "orders.yml"),
		[]byte("name: orders\nschema: silver\nbogus: 1\n"), 0644))
	stdout, stderr, err := executeCmdSplit("validate")
	require.Error(t, err)
	assert.Contains(t, stdout+stderr+err.Error(), "tables/silver/orders.yml")
}

// skipUnlessPermissionsEnforced skips tests that rely on chmod denying access (Windows, root).
func skipUnlessPermissionsEnforced(t *testing.T) {
	t.Helper()
	if runtime.GOOS == litWindows {
		t.Skip("chmod does not deny access on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod does not deny access")
	}
}

// denyAccess removes all permissions from path and restores them on cleanup (so TempDir can be removed).
func denyAccess(t *testing.T, path string, restore os.FileMode) {
	t.Helper()
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, restore) })
}

// assertNoAbsolutePath fails if the output mentions the project directory (as given or symlink-resolved).
func assertNoAbsolutePath(t *testing.T, tmpDir, out string) {
	t.Helper()
	resolved, _ := filepath.EvalSymlinks(tmpDir)
	for _, p := range []string{tmpDir, resolved} {
		assert.NotContains(t, out, p)
		assert.NotContains(t, out, config.SlashPath(p)) // Windows: both spellings of the path
	}
}

// OS read errors never show the absolute path of the project: the path is project-relative
// (Location) and the text is "<op>: <OS reason>".
func TestLoadErrors_UnreadableInputs_NoAbsolutePath(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	cases := []struct {
		name     string
		deny     string // path relative to the project root
		isDir    bool
		wantPath string
	}{
		{pathProjectFile, pathProjectFile, false, pathProjectFile},
		{"catalogs dir", litCatalogs, true, litCatalogs},
		{"catalog file", filepath.Join("catalogs", "main.yml"), false, pathCatalogMain},
		{"table file", filepath.Join("tables", "silver", "orders.yml"), false, pathSilverOrders},
	}
	for _, tc := range cases {
		for _, args := range [][]string{{cmdValidate}, {cmdPlan, "-t", litDev}} {
			t.Run(tc.name+"/"+args[0], func(t *testing.T) {
				tmpDir := setupPlanProject(t)
				useBackend(t, upToDateMock())
				restore := os.FileMode(0o644)
				if tc.isDir {
					restore = 0o755
				}
				denyAccess(t, filepath.Join(tmpDir, tc.deny), restore)

				stdout, stderr, err := executeCmdSplit(args...)
				require.Error(t, err)
				all := stdout + stderr + err.Error()
				assert.Contains(t, all, "permission denied")
				assert.Contains(t, all, tc.wantPath)
				assertNoAbsolutePath(t, tmpDir, all)
			})
		}
	}
}

// ucat.yml missing is the one case that shows an absolute path (where the search started).
func TestLoadErrors_ProjectNotFound_ShowsSearchStart(t *testing.T) {
	emptyDir(t)

	stdout, stderr, err := executeCmdSplit("validate")
	require.Error(t, err)
	all := stdout + stderr + err.Error()
	assert.Contains(t, all, "ucat.yml not found: searched from")
}

// pull into a tables directory without write permission: the mkdir/write error shows the
// project-relative path and the OS reason, never the absolute path.
func TestPull_UnwritableTablesDir_NoAbsolutePath(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	cases := map[string]string{
		"mkdir (tables/ read-only)":        litTables,
		"write (tables/silver/ read-only)": filepath.Join("tables", "silver"),
	}
	for name, deny := range cases {
		t.Run(name, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "tables", "silver"), 0o755))
			if deny == litTables {
				require.NoError(t, os.RemoveAll(filepath.Join(tmpDir, "tables", "silver")))
			}
			m := &backendtest.MockUCBackend{}
			m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders("id")})
			useBackend(t, m)
			ro := filepath.Join(tmpDir, deny)
			require.NoError(t, os.Chmod(ro, 0o555))
			t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })

			stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
			all := stdout + stderr
			if err != nil {
				all += err.Error()
			}
			require.Error(t, err, all)
			assert.Contains(t, all, "permission denied")
			assert.Contains(t, all, deny)
			assertNoAbsolutePath(t, tmpDir, all)
		})
	}
}

// A working directory that cannot be determined is an ordinary command error, not a silent ".".
func TestLoadConfig_WorkingDirectoryError(t *testing.T) {
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev}, {cmdValidate}, {cmdPull, "-t", litDev}, {cmdDrop, "-t", litDev, flagSchema, "s", flagTable, "t", flagAutoApprove}} {
		a := newApp()
		a.getwd = func() (string, error) { return "", errors.New("cwd gone") }
		root := newRootCmd(a)
		var out, errOut bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(args)

		err := root.Execute()
		require.Error(t, err, args)
		assert.EqualError(t, err, "failed to get the working directory: cwd gone", args)
		assert.Empty(t, out.String(), args)
		assert.Empty(t, errOut.String(), args)
	}
}
