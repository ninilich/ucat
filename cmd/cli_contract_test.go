package cmd

// cli_contract_test.go: behavioural safety net for the CLI layer. It pins the
// observable contract of global/local flags, defaults, required flags, the destructive-apply
// confirmation, --json/--show-sql/--detailed-exitcode and isolation between consecutive
// calls. Tests go through runRootIn/executeCmd*/--help only and were written before the
// package-level flag variables and the rootCmd singleton were replaced by a per-call command tree
// (app + newRootCmd) and must stay valid for it.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	destructivePrompt = "Plan contains destructive changes. Type 'yes' to confirm:"
)

// countApplied returns how many ApplyChange-like calls the mock recorded.
func countApplied(m *backendtest.MockUCBackend) int { return len(m.RecordedCalls()) }

// addColumnMock: live orders has no columns, desired has "id" -> a non-destructive add column.
func addColumnMock() *backendtest.MockUCBackend {
	m := &backendtest.MockUCBackend{}
	m.SetFixture(jsonSilverRef, []*backend.LiveTable{liveOrders()})
	return m
}

func subcommand(t *testing.T, name string) *cobra.Command {
	t.Helper()
	c, _, err := newRootCmd(newApp()).Find([]string{name})
	require.NoError(t, err)
	require.Equal(t, name, c.Name())
	return c
}

// --- 1. destructive confirmation ---

func TestCLIContract_Apply_Destructive_NoInput_Refused(t *testing.T) {
	for _, stdin := range []string{"", "\n"} {
		t.Run(fmt.Sprintf("stdin=%q", stdin), func(t *testing.T) {
			tmp := setupPlanProject(t)
			m := destructivePlanMock(t, tmp)
			useBackend(t, m)
			useStdin(t, stdin)

			stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, stdout+stderr, destructivePrompt)
			if stdin == "" {
				assert.Contains(t, stdout+stderr+err.Error(), "apply cancelled: no input")
			} else {
				assert.Contains(t, stdout+stderr+err.Error(), "apply cancelled")
			}
			assert.Zero(t, countApplied(m))
		})
	}
}

func TestCLIContract_Apply_Destructive_No_Refused(t *testing.T) {
	tmp := setupPlanProject(t)
	m := destructivePlanMock(t, tmp)
	useBackend(t, m)
	useStdin(t, "no\n")

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
	assert.Contains(t, stdout+stderr, destructivePrompt)
	assert.Zero(t, countApplied(m))
}

func TestCLIContract_Apply_Destructive_Yes_Applies(t *testing.T) {
	tmp := setupPlanProject(t)
	m := destructivePlanMock(t, tmp)
	useBackend(t, m)
	useStdin(t, "yes\n")

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout+stderr, destructivePrompt)
	assert.NotZero(t, countApplied(m))
}

func TestCLIContract_Apply_Destructive_AutoApprove_NoPrompt_AnyPosition(t *testing.T) {
	cases := map[string][]string{
		"flag last":  {cmdApply, "-t", litDev, flagAutoApprove},
		"flag first": {cmdApply, flagAutoApprove, "-t", litDev},
		"=true":      {cmdApply, "-t", litDev, "--auto-approve=true"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			tmp := setupPlanProject(t)
			m := destructivePlanMock(t, tmp)
			useBackend(t, m)
			useStdin(t, "no\n") // would decline if asked

			stdout, stderr, err := executeCmdSplit(args...)
			require.NoError(t, err, stderr)
			assert.NotContains(t, stdout+stderr, destructivePrompt)
			assert.NotContains(t, stdout+stderr, "cancelled")
			assert.NotZero(t, countApplied(m))
		})
	}
}

func TestCLIContract_Apply_AutoApproveFalse_StillPrompts(t *testing.T) {
	tmp := setupPlanProject(t)
	m := destructivePlanMock(t, tmp)
	useBackend(t, m)
	useStdin(t, "")

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve=false")
	require.Error(t, err)
	assert.Contains(t, stdout+stderr, destructivePrompt)
	assert.Zero(t, countApplied(m))
}

func TestCLIContract_Apply_NonDestructive_NoPrompt(t *testing.T) {
	setupPlanProject(t)
	m := addColumnMock()
	useBackend(t, m)
	useStdin(t, "") // a prompt would fail with "no input"

	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev")
	require.NoError(t, err, stderr)
	assert.NotContains(t, stdout+stderr, destructivePrompt)
	assert.NotZero(t, countApplied(m))
}

// --- 2. flag visibility ---

// flagOwners maps every flag to the commands that have it: a flag is bound to its commands, the
// root has none (so a flag a command does not use is an error, not silently ignored).
var flagOwners = map[string][]string{
	litTarget:           {cmdPlan, cmdApply, cmdValidate, cmdPull, cmdDrop},
	flagNameAutoApprove: {cmdApply, cmdDrop},
	flagNameShowSQL:     {cmdPlan, cmdApply},
	litJSON:             {cmdPlan, cmdApply},
	"detailed-exitcode": {cmdPlan},
	litForce:            {cmdPull},
	litCatalog:          {cmdPull},
	"tblproperties":     {cmdPull},
	litSchema:           {cmdPlan, cmdApply, cmdPull, cmdDrop},
	litTable:            {cmdPlan, cmdApply, cmdDrop},
}

var allCommands = []string{cmdPlan, cmdApply, cmdValidate, cmdPull, cmdDrop, cmdInit, litVersion}

func owns(owners []string, name string) bool {
	for _, o := range owners {
		if o == name {
			return true
		}
	}
	return false
}

func TestCLIContract_FlagsOnlyOnOwningCommands(t *testing.T) {
	for flag, own := range flagOwners {
		for _, name := range allCommands {
			t.Run(flag+"@"+name, func(t *testing.T) {
				c := subcommand(t, name)
				assert.Equal(t, owns(own, name), c.Flags().Lookup(flag) != nil, "--%s on %s", flag, name)
				assert.Nil(t, c.InheritedFlags().Lookup(flag), "--%s must not be inherited by %s", flag, name)
			})
		}
	}
}

func TestCLIContract_RootHasNoPersistentFlags(t *testing.T) {
	root := newRootCmd(newApp())
	assert.False(t, root.PersistentFlags().HasFlags())
	for flag := range flagOwners {
		assert.Nil(t, root.Flags().Lookup(flag), "--%s must not be on the root", flag)
	}
}

// A flag on a command that does not own it is the usual cobra error (exit code 1), also before the
// subcommand name; --help of a command lists exactly the flags it owns.
func TestCLIContract_FlagOnForeignCommand_UnknownFlag(t *testing.T) {
	cases := [][]string{
		{cmdValidate, flagJSON}, {cmdValidate, flagShowSQL}, {cmdValidate, flagAutoApprove},
		{cmdPull, flagJSON}, {cmdPull, flagAutoApprove}, {cmdPull, flagShowSQL},
		{cmdPlan, flagAutoApprove}, {cmdDrop, flagJSON}, {cmdDrop, flagShowSQL},
		{cmdApply, "--detailed-exitcode"},
		{cmdInit, "-t", litDev}, {cmdInit, flagJSON}, {cmdInit, flagAutoApprove}, {cmdInit, flagShowSQL},
		{litVersion, "-t", litDev}, {litVersion, flagJSON},
		{flagJSON, cmdPlan, "-t", litDev}, {flagAutoApprove, cmdApply, "-t", litDev},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, stderr, err := executeCmdSplit(args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown ") // "unknown flag: --json" or "unknown shorthand flag: 't' in -t"
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, stderr, "Error: unknown ")
		})
	}
}

func TestCLIContract_HelpListsOwnFlagsOnly(t *testing.T) {
	for _, name := range allCommands {
		t.Run(name, func(t *testing.T) {
			out, err := executeCmd(name, "--help")
			require.NoError(t, err)
			for flag, own := range flagOwners {
				assert.Equal(t, owns(own, name), strings.Contains(out, "--"+flag), "--%s in help of %s", flag, name)
			}
			assert.Equal(t, owns(flagOwners[litTarget], name), strings.Contains(out, "-t, --target"))
		})
	}
}

func TestCLIContract_LocalFlagsUnknownOnOtherCommands(t *testing.T) {
	cases := map[string][]string{
		"apply --detailed-exitcode": {cmdApply, "-t", litDev, flagDetailedExitcode},
		"apply --force":             {cmdApply, "-t", litDev, flagForce},
		"plan --force":              {cmdPlan, "-t", litDev, flagForce},
		"plan --catalog":            {cmdPlan, "-t", litDev, flagCatalog, "x"},
		"drop --force":              {cmdDrop, "-t", litDev, flagForce},
		"drop --catalog":            {cmdDrop, "-t", litDev, flagCatalog, "x"},
		"drop --detailed-exitcode":  {cmdDrop, "-t", litDev, flagDetailedExitcode},
		"pull --table":              {cmdPull, "-t", litDev, flagTable, "x"},
		"validate --schema":         {cmdValidate, flagSchema, "x"},
		"pull --detailed-exitcode":  {cmdPull, "-t", litDev, flagDetailedExitcode},
		"validate --force":          {cmdValidate, flagForce},
		"version --schema":          {litVersion, flagSchema, "x"},
		"init --force":              {cmdInit, flagForce},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := executeCmd(args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown flag")
		})
	}
}

// --- 3. defaults ---

func TestCLIContract_FlagDefaults(t *testing.T) {
	defaults := map[string]string{
		litTarget: "", flagNameAutoApprove: litFalse, flagNameShowSQL: litFalse, litJSON: litFalse,
		"detailed-exitcode": litFalse, litForce: litFalse, litCatalog: "", "tblproperties": "", litSchema: "", litTable: "",
	}
	require.Len(t, defaults, len(flagOwners))
	for flag, own := range flagOwners {
		for _, name := range own {
			t.Run(name+"/"+flag, func(t *testing.T) {
				f := subcommand(t, name).Flags().Lookup(flag)
				require.NotNil(t, f, "--%s on %s", flag, name)
				want := defaults[flag]
				if (flag == litSchema || flag == litTable) && (name == cmdPlan || name == cmdApply) {
					want = "[]" // the selectors of plan and apply are repeatable: a list, empty by default
				}
				assert.Equal(t, want, f.DefValue)
			})
		}
	}
}

func TestCLIContract_TargetShorthand(t *testing.T) {
	for _, name := range flagOwners[litTarget] {
		t.Run(name, func(t *testing.T) {
			f := subcommand(t, name).Flags().Lookup(litTarget)
			require.NotNil(t, f)
			assert.Equal(t, "t", f.Shorthand)
		})
	}
	// -t on a command without a target is not a different flag, it is an error.
	for _, name := range []string{cmdInit, litVersion} {
		assert.Nil(t, subcommand(t, name).Flags().ShorthandLookup("t"), name)
	}
}

// --- 4. -t required ---

func TestCLIContract_TargetRequired_BeforeBackend(t *testing.T) {
	cases := map[string][]string{
		cmdPlan:  {cmdPlan},
		cmdApply: {cmdApply},
		cmdPull:  {cmdPull},
		cmdDrop:  {cmdDrop, flagSchema, litSilver, flagTable, litOrders, flagAutoApprove},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			setupPlanProject(t)
			calls := 0
			installCounting(t, &calls)

			out, err := executeCmd(args...)
			require.Error(t, err)
			assert.Equal(t, 1, codeOf(err))
			assert.Contains(t, out+err.Error(), name+" requires -t <target>")
			assert.Zero(t, calls, "backend must not be created")
		})
	}
}

// installCounting replaces the backend factory with one counting constructions.
func installCounting(t *testing.T, calls *int) {
	t.Helper()
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		*calls++
		return &backendtest.MockUCBackend{}, nil
	})
}

func TestCLIContract_Validate_WithoutTarget_RunsLocally(t *testing.T) {
	setupPlanProject(t)
	calls := 0
	installCounting(t, &calls)

	out, err := executeCmd("validate")
	require.NoError(t, err, out)
	assert.Zero(t, calls)
}

// --- 5. drop required flags ---

func TestCLIContract_Drop_OnlyOneRequiredFlag(t *testing.T) {
	cases := map[string]struct {
		args    []string
		missing string
		present string
	}{
		"only schema": {[]string{cmdDrop, "-t", litDev, flagSchema, litSilver, flagAutoApprove}, litQuotedTable, litQuotedSchema},
		"only table":  {[]string{cmdDrop, "-t", litDev, "--table", litOrders, flagAutoApprove}, `"schema"`, `"table"`},
		"neither":     {[]string{cmdDrop, "-t", litDev, flagAutoApprove}, `"schema"`, `"table"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setupTestProject(t)
			m := &backendtest.MockUCBackend{}
			useBackend(t, m)

			_, err := executeCmd(tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "required flag(s)")
			assert.Contains(t, err.Error(), tc.missing)
			if name != "neither" {
				assert.NotContains(t, err.Error(), tc.present)
			}
			assert.Zero(t, countApplied(m))
		})
	}
}

func TestCLIContract_Drop_RequiresAutoApprove(t *testing.T) {
	setupTestProject(t)
	m := &backendtest.MockUCBackend{}
	useBackend(t, m)

	out, err := executeCmd("drop", "-t", "dev", "--schema", "silver", "--table", "orders")
	require.Error(t, err)
	assert.Contains(t, out+err.Error(), "drop requires --auto-approve")
	assert.Zero(t, countApplied(m))
}

// --- 6. --json / --show-sql ---

func TestCLIContract_PlanJSON_StdoutJSON_WarningOnStderr(t *testing.T) {
	dir := setupPlanProject(t)
	addCatalogFile(t, dir, "zz_file", "extra") // loader warning
	useBackend(t, addColumnMock())

	stdout, stderr, err := executeCmdSplit("plan", "-t", "dev", "--json")
	require.NoError(t, err)
	var doc map[string]any
	decodeSingleJSON(t, stdout, &doc)
	for _, k := range []string{"summary", "changes"} {
		assert.Contains(t, doc, k)
	}
	assert.Contains(t, stderr, "warning: ", "warning goes to stderr")
	assert.NotContains(t, stdout, "warning")
}

func TestCLIContract_PlanShowSQL_PrintsSQL(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, addColumnMock())

	stdout, _, err := executeCmdSplit("plan", "-t", "dev", "--show-sql")
	require.NoError(t, err)
	assert.Contains(t, stdout, "ALTER TABLE")
}

func TestCLIContract_PlanJSONShowSQL_JSONWithoutText(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, addColumnMock())

	stdout, _, err := executeCmdSplit("plan", "-t", "dev", "--json", "--show-sql")
	require.NoError(t, err)
	var doc map[string]any
	decodeSingleJSON(t, stdout, &doc)
	assert.True(t, strings.HasPrefix(stdout, "{"))
}

// apply --show-sql prints the same SQL as plan --show-sql for the same plan and still applies.
func TestCLIContract_ApplyShowSQL_PrintsSQLAndApplies(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, addColumnMock())
	planOut, _, err := executeCmdSplit("plan", "-t", "dev", "--show-sql")
	require.NoError(t, err)
	require.Contains(t, planOut, "ALTER TABLE")

	m := addColumnMock()
	useBackend(t, m)
	useStdin(t, "")
	stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve", "--show-sql")
	require.NoError(t, err, stderr)
	assert.NotZero(t, countApplied(m))
	for _, line := range strings.Split(planOut, "\n") {
		if strings.Contains(line, "ALTER TABLE") {
			assert.Contains(t, stdout, strings.TrimSpace(line), "apply must print the SQL plan prints")
		}
	}
}

// --- 7. --detailed-exitcode ---

func TestCLIContract_DetailedExitcode(t *testing.T) {
	setupPlanProject(t)

	useBackend(t, addColumnMock())
	_, _, err := executeCmdSplit("plan", "-t", "dev", "--detailed-exitcode")
	assert.Equal(t, 2, codeOf(err), "changes present")

	useBackend(t, upToDateMock())
	_, _, err = executeCmdSplit("plan", "-t", "dev", "--detailed-exitcode")
	assert.Equal(t, 0, codeOf(err), "no changes")

	useBackend(t, listFailSpy())
	_, _, err = executeCmdSplit("plan", "-t", "dev", "--detailed-exitcode")
	assert.Equal(t, 1, codeOf(err), "error")
}

// --- state isolation between consecutive calls ---

// leakRun is the result of one CLI call against a fresh spy backend.
type leakRun struct {
	stdout, stderr string
	err            error
	spy            *spyBackend
}

// TestCLIContract_NoStateLeak: for every flag of app.flags, a call that uses it is followed by a call
// without it in the same process; the second call must behave as if the flag had never been set
// (one fresh command tree per call). The drop rows check the required flags: a successful
// drop with --schema/--table must not satisfy them in the next call.
func TestCLIContract_NoStateLeak(t *testing.T) {
	plain := func(_ *testing.T, _ string) *backendtest.MockUCBackend { return addColumnMock() }
	destructive := func(t *testing.T, tmp string) *backendtest.MockUCBackend { return destructivePlanMock(t, tmp) }
	upToDate := func(_ *testing.T, _ string) *backendtest.MockUCBackend { return upToDateMock() }
	notJSON := func(t *testing.T, r leakRun) {
		require.NoError(t, r.err, r.stderr)
		assert.False(t, strings.HasPrefix(strings.TrimSpace(r.stdout), "{"), "text output expected: %q", r.stdout)
		assert.Contains(t, r.stdout, "orders")
	}
	dropArgs := func(extra ...string) []string {
		return append([]string{"drop", "-t", litDev, flagAutoApprove}, extra...)
	}
	requiredFlag := func(name string) func(t *testing.T, r leakRun) {
		return func(t *testing.T, r leakRun) {
			require.Error(t, r.err)
			assert.Contains(t, r.err.Error(), "required flag(s)")
			assert.Contains(t, r.err.Error(), `"`+name+`"`)
			assert.Zero(t, r.spy.applyCalls, "no backend call without the required flags")
		}
	}
	dropOK := func(t *testing.T, r leakRun) {
		require.NoError(t, r.err, r.stderr)
		assert.NotZero(t, r.spy.applyCalls)
	}
	exitCodeIs := func(want int) func(t *testing.T, r leakRun) {
		return func(t *testing.T, r leakRun) { assert.Equal(t, want, codeOf(r.err)) }
	}
	hasSQL := func(want bool) func(t *testing.T, r leakRun) {
		return func(t *testing.T, r leakRun) {
			require.NoError(t, r.err, r.stderr)
			assert.Equal(t, want, strings.Contains(r.stdout, "ALTER TABLE"), "stdout: %q", r.stdout)
		}
	}
	unknownFlag := func(t *testing.T, r leakRun) {
		require.Error(t, r.err)
		assert.Contains(t, r.err.Error(), "unknown flag")
	}
	pullErr := func(want string) func(t *testing.T, r leakRun) {
		return func(t *testing.T, r leakRun) {
			require.Error(t, r.err)
			assert.Contains(t, r.err.Error(), want)
		}
	}
	pullSkipped := func(t *testing.T, r leakRun) {
		require.NoError(t, r.err, r.stderr)
		// orders.yml exists (project fixture): without --force it is skipped.
		assert.Contains(t, r.stdout, "skipped")
	}

	cases := []struct {
		name          string
		mock          func(t *testing.T, tmp string) *backendtest.MockUCBackend
		with, without []string
		checkWith     func(t *testing.T, r leakRun)
		checkWithout  func(t *testing.T, r leakRun)
	}{
		{"-t/--target", plain, []string{cmdValidate, "-t", litDev}, []string{"validate"},
			func(t *testing.T, r leakRun) {
				require.NoError(t, r.err, r.stderr)
				assert.NotZero(t, r.spy.listCalls, "live validation with -t")
			},
			func(t *testing.T, r leakRun) {
				require.NoError(t, r.err, r.stderr)
				assert.Zero(t, r.spy.listCalls, "local validation without -t")
				assert.Zero(t, r.spy.factoryCalls)
			}},
		{flagAutoApprove, destructive, []string{cmdApply, "-t", litDev, flagAutoApprove}, []string{cmdApply, "-t", litDev},
			func(t *testing.T, r leakRun) {
				require.NoError(t, r.err, r.stderr)
				assert.NotZero(t, r.spy.applyCalls)
			},
			func(t *testing.T, r leakRun) {
				require.Error(t, r.err)
				assert.Contains(t, r.stdout+r.stderr, destructivePrompt)
				assert.Zero(t, r.spy.applyCalls)
			}},
		{flagJSON, plain, []string{cmdPlan, "-t", litDev, flagJSON}, []string{cmdPlan, "-t", litDev},
			func(t *testing.T, r leakRun) {
				require.NoError(t, r.err)
				assert.True(t, strings.HasPrefix(r.stdout, "{"))
			},
			func(t *testing.T, r leakRun) {
				notJSON(t, r)
				assert.NotContains(t, r.stdout, "ALTER TABLE", "--show-sql must not stick either")
			}},
		{"--show-sql (plan)", plain, []string{cmdPlan, "-t", litDev, "--show-sql"}, []string{cmdPlan, "-t", litDev},
			hasSQL(true), hasSQL(false)},
		{"--show-sql (apply)", plain, []string{cmdApply, "-t", litDev, flagAutoApprove, "--show-sql"}, []string{cmdApply, "-t", litDev, flagAutoApprove},
			func(t *testing.T, r leakRun) {
				hasSQL(true)(t, r)
				assert.NotZero(t, r.spy.applyCalls)
			},
			func(t *testing.T, r leakRun) {
				hasSQL(false)(t, r)
				assert.NotZero(t, r.spy.applyCalls)
			}},
		{flagDetailedExitcode, plain, []string{cmdPlan, "-t", litDev, flagDetailedExitcode}, []string{cmdPlan, "-t", litDev},
			exitCodeIs(2), exitCodeIs(0)},
		{"drop --schema", plain, dropArgs("--schema", "silver", "--table", "orders"), dropArgs("--table", "orders"),
			dropOK, requiredFlag("schema")},
		{"drop --table", plain, dropArgs("--schema", "silver", "--table", "orders"), dropArgs("--schema", "silver"),
			dropOK, requiredFlag("table")},
		{"drop --schema --table", plain, dropArgs("--schema", "silver", "--table", "orders"), dropArgs(),
			dropOK, func(t *testing.T, r leakRun) {
				requiredFlag("schema")(t, r)
				assert.Contains(t, r.err.Error(), `"table"`)
				assert.NotContains(t, r.err.Error(), "unknown schema")
			}},
		{"pull --force", upToDate, []string{cmdPull, "-t", litDev, flagForce}, []string{cmdPull, "-t", litDev},
			func(t *testing.T, r leakRun) {
				require.NoError(t, r.err, r.stderr)
				assert.NotContains(t, r.stdout, "skipped")
			}, pullSkipped},
		{"pull --force (known only to pull)", upToDate, []string{cmdPull, "-t", litDev, "--force"}, []string{cmdApply, "-t", litDev, "--force"},
			func(t *testing.T, r leakRun) { require.NoError(t, r.err, r.stderr) }, unknownFlag},
		{"pull --catalog", upToDate, []string{cmdPull, "-t", litDev, flagCatalog, litNope}, []string{cmdPull, "-t", litDev},
			pullErr(`unknown catalog "nope"`), pullSkipped},
		{"pull --schema", upToDate, []string{cmdPull, "-t", litDev, "--catalog", litMain, "--schema", litNope}, []string{cmdPull, "-t", litDev, "--catalog", litMain},
			pullErr(`unknown schema "nope" in catalog "main"`), pullSkipped},
		{"pull --tblproperties", upToDate, []string{cmdPull, "-t", litDev, flagTblproperties, "a,,b"}, []string{cmdPull, "-t", litDev},
			pullErr("--tblproperties"), pullSkipped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := setupPlanProject(t)
			run := func(args []string) leakRun {
				spy := useSpy(t, newSpy(tc.mock(t, tmp)))
				useStdin(t, "")
				var r leakRun
				r.stdout, r.stderr, r.err = executeCmdSplit(args...)
				r.spy = spy
				return r
			}
			tc.checkWith(t, run(tc.with))
			tc.checkWithout(t, run(tc.without))
		})
	}
}

// --- 8. removed --debug ---

// --debug was removed (it logged nothing): it is an unknown flag for every command, exit code 1.
func TestCLIContract_DebugFlagIsUnknown(t *testing.T) {
	for _, name := range []string{"plan", cmdApply, "validate", cmdPull, "drop", "init", "version"} {
		t.Run(name, func(t *testing.T) {
			_, _, err := executeCmdSplit(name, "--debug")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown flag: --debug")
			assert.Equal(t, 1, codeOf(err))
		})
	}
}

// --- 9. version ---

func TestCLIContract_Version(t *testing.T) {
	cmdOut, _, err := executeCmdSplit("version")
	require.NoError(t, err)
	flagOut, _, err := executeCmdSplit("--version")
	require.NoError(t, err)
	assert.Equal(t, cmdOut, flagOut, "`ucat version` and `ucat --version` print the same text")
	assert.Regexp(t, `^ucat \S+ \(commit: \S+, built: \S+\)\n$`, cmdOut)
}

// -v is not a shorthand of --version (nor of anything else).
func TestCLIContract_VersionHasNoShorthand(t *testing.T) {
	_, _, err := executeCmdSplit("-v")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown shorthand flag: 'v' in -v")
	assert.Equal(t, 1, codeOf(err))
	f := newRootCmd(newApp()).Flags().Lookup(litVersion)
	require.NotNil(t, f)
	assert.Empty(t, f.Shorthand)
}

// --- 10. independence of consecutive calls ---

func permutations(n int) [][]int {
	var res [][]int
	var rec func(a []int, k int)
	rec = func(a []int, k int) {
		if k == len(a) {
			res = append(res, append([]int(nil), a...))
			return
		}
		for i := k; i < len(a); i++ {
			a[k], a[i] = a[i], a[k]
			rec(a, k+1)
			a[k], a[i] = a[i], a[k]
		}
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	rec(idx, 0)
	return res
}

func TestCLIContract_ConsecutiveCallsAreIndependent(t *testing.T) {
	steps := []struct {
		name string
		run  func(t *testing.T, tmp string)
	}{
		{"apply --auto-approve --json", func(t *testing.T, _ string) {
			m := addColumnMock()
			useBackend(t, m)
			useStdin(t, "")
			stdout, stderr, err := executeCmdSplit("apply", "-t", "dev", "--auto-approve", "--json")
			require.NoError(t, err, stderr)
			var r applyJSON
			decodeSingleJSON(t, stdout, &r)
			assert.NotZero(t, countApplied(m))
		}},
		{"plan", func(t *testing.T, _ string) {
			useBackend(t, addColumnMock())
			stdout, _, err := executeCmdSplit("plan", "-t", "dev")
			require.NoError(t, err)
			assert.False(t, strings.HasPrefix(strings.TrimSpace(stdout), "{"), stdout)
			assert.NotContains(t, stdout, "ALTER TABLE")
			assert.Contains(t, stdout, "orders")
		}},
		{"pull --force --catalog main", func(t *testing.T, _ string) {
			useBackend(t, upToDateMock())
			_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--force", "--catalog", "main")
			require.NoError(t, err, stderr)
		}},
		{"pull", func(t *testing.T, _ string) {
			useBackend(t, upToDateMock())
			stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
			require.NoError(t, err, stderr)
			// orders.yml exists (project fixture): without --force it must be skipped, not overwritten.
			assert.Contains(t, stdout, "skipped")
			assert.NotContains(t, stdout, "overwritten")
		}},
	}
	for _, perm := range permutations(len(steps)) {
		var names []string
		for _, i := range perm {
			names = append(names, fmt.Sprint(i))
		}
		t.Run(strings.Join(names, "-"), func(t *testing.T) {
			tmp := setupPlanProject(t)
			for _, i := range perm {
				t.Log("step:", steps[i].name)
				steps[i].run(t, tmp)
			}
		})
	}
}

// Every newRootCmd call builds a new tree: no shared commands, no flag state carried over.
func TestRootCmd_FreshTreePerCall(t *testing.T) {
	a, b := newRootCmd(newApp()), newRootCmd(newApp())
	require.NotSame(t, a, b)
	ac, bc := a.Commands(), b.Commands()
	require.Len(t, bc, len(ac))
	for i := range ac {
		assert.NotSame(t, ac[i], bc[i], "subcommand %s must not be shared", ac[i].Name())
	}
	for _, root := range []*cobra.Command{a, b} {
		eachFlag(root, func(c *cobra.Command, f *pflag.Flag) {
			assert.False(t, f.Changed, "%s --%s", c.CommandPath(), f.Name)
		})
	}
}

// A flag before the command name belongs to that command: cobra passes the arguments before the name on.
func TestCLIContract_FlagBeforeCommandName(t *testing.T) {
	setupPlanProject(t)
	useBackend(t, addColumnMock())
	stdout, stderr, err := executeCmdSplit("-t", litDev, cmdPlan)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "orders")

	for _, args := range [][]string{{flagJSON, cmdValidate}, {flagAutoApprove, cmdPlan, "-t", litDev}} {
		_, _, err = executeCmdSplit(args...)
		require.Error(t, err, args)
		assert.Contains(t, err.Error(), "unknown flag", args)
	}
}

func TestVersionHelp_DescribesTagAndDev(t *testing.T) {
	out, err := executeCmd(litVersion, "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "reports\nthat tag")
	assert.Contains(t, out, `reports "dev"`)
	assert.Contains(t, out, "pseudo-version")
}
