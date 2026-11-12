package cmd

// load_warnings_test.go: warnings the loading stage prints on stderr: ignored *.yaml files, a second YAML
// document, a project file that overrides an environment variable, targets that share a catalog.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// warnArgs are the commands that run the whole loading stage with tables: plan, apply, validate (+ -t).
var warnArgs = [][]string{
	{cmdPlan, "-t", litDev},
	{cmdApply, "-t", litDev, flagAutoApprove},
	{cmdValidate},
	{cmdValidate, "-t", litDev},
}

func emptyLiveBackend(t *testing.T) *backendtest.MockUCBackend {
	t.Helper()
	m := &backendtest.MockUCBackend{}
	m.SetFixture(silverRef, []*backend.LiveTable{})
	useBackend(t, m)
	return m
}

// --- *.yaml next to *.yml ---

func TestIgnoredYAML_WarnedByEveryCommand(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "tables", "silver", "extra.yaml"), "name: extra\nschema: silver\n")
	writeFile(t, filepath.Join(dir, "catalogs", "more.yaml"), "name: more\n")
	const want = "warning: 2 file(s) are ignored (ucat reads only files named *.yml with a lower-case extension, not *.yaml or *.YML): catalogs/more.yaml, tables/silver/extra.yaml; rename them to .yml\n"
	for _, args := range warnArgs {
		emptyLiveBackend(t)
		_, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, "%v: %s", args, stderr)
		assert.Equal(t, 1, strings.Count(stderr, want), "%v: %s", args, stderr)
	}
	// pull loads tables too; drop does not (only catalogs and targets are looked at)
	emptyLiveBackend(t)
	_, stderr, err := executeCmdSplit("pull", "-t", litDev)
	require.NoError(t, err)
	assert.Contains(t, stderr, want)
	_, stderr, err = executeCmdSplit("drop", "-t", litDev, "--schema", litSilver, "--table", "x", "--auto-approve")
	require.NoError(t, err)
	assert.Contains(t, stderr, "1 file(s) are ignored (ucat reads only files named *.yml with a lower-case extension, not *.yaml or *.YML): catalogs/more.yaml;")
	assert.NotContains(t, stderr, "tables/silver/extra.yaml")
}

func TestIgnoredYAML_NoWarningWithoutYAMLFiles(t *testing.T) {
	setupPlanProject(t)
	emptyLiveBackend(t)
	_, stderr, err := executeCmdSplit("validate")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "*.yaml")
}

// --- a second YAML document ---

func TestSecondDocument_WarnedWithoutFailing(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "tables", "silver", "orders.yml"), tableYAMLOf("orders", "silver")+"---\nname: ignored\nschema: silver\n")
	const want = "warning: the file has a second YAML document (after ---): it is ignored, only the first document is read (tables/silver/orders.yml:6)\n"
	for _, args := range [][]string{{cmdPlan, "-t", litDev}, {cmdPlan, "-t", litDev, flagJSON}, {cmdValidate}} {
		useBackend(t, addColumnMock())
		stdout, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, "%v: %s", args, stderr)
		assert.Equal(t, 1, strings.Count(stderr, want), "%v: %s", args, stderr)
		assert.NotContains(t, stdout, "warning")
	}
}

// --- a file value wins over the environment ---

func warehouseOf(t *testing.T, env string, target, project string) (got, stderr string) {
	t.Helper()
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), target)
	writeFile(t, filepath.Join(dir, "ucat.yml"), project)
	t.Setenv("DATABRICKS_WAREHOUSE_ID", env)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(silverRef, []*backend.LiveTable{})
	useBackendFactory(t, func(_, wh string) (backend.UCBackend, error) { got = wh; return m, nil })
	_, stderr, err := executeCmdSplit("apply", "-t", litDev, flagAutoApprove)
	require.NoError(t, err, stderr)
	return got, stderr
}

func TestWarehousePriority_FileWinsOverEnv(t *testing.T) {
	const noWH = "workspace:\n  host: https://h\n"
	const projectWH = "project: p\ntables_dir: tables\nworkspace:\n  warehouse_id: proj-wh\n"
	const projectNoWH = "project: p\ntables_dir: tables\n"
	cases := []struct {
		name            string
		env             string
		target, project string
		want            string
		warning         string // "" = none
	}{
		{"target beats env", litEnvWH, noWH + "  warehouse_id: tgt-wh\n", projectWH, litTgtWH,
			`warning: workspace.warehouse_id "tgt-wh" from targets/dev.yml is used; DATABRICKS_WAREHOUSE_ID ("env-wh") is ignored: a value in a project file wins over the environment`},
		{"project beats env", litEnvWH, noWH, projectWH, "proj-wh",
			`warning: workspace.warehouse_id "proj-wh" from ucat.yml is used; DATABRICKS_WAREHOUSE_ID ("env-wh") is ignored: a value in a project file wins over the environment`},
		{"env equals the file: no warning", litTgtWH, noWH + "  warehouse_id: tgt-wh\n", projectNoWH, litTgtWH, ""},
		{"env is the fallback: no warning", litEnvWH, noWH, projectNoWH, litEnvWH, ""},
		{"env unset: no warning", "", noWH + "  warehouse_id: tgt-wh\n", projectNoWH, litTgtWH, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, stderr := warehouseOf(t, tc.env, tc.target, tc.project)
			assert.Equal(t, tc.want, got)
			if tc.warning == "" {
				assert.NotContains(t, stderr, "DATABRICKS_WAREHOUSE_ID")
				return
			}
			assert.Equal(t, 1, strings.Count(stderr, tc.warning+"\n"), stderr)
		})
	}
}

// The warning is printed by every command that loads the config, once.
func TestEnvOverrideWarning_PerCommand(t *testing.T) {
	setupPlanProject(t) // target dev has warehouse wh123 and host https://dev.example.com
	t.Setenv("DATABRICKS_WAREHOUSE_ID", "other-wh")
	t.Setenv("DATABRICKS_HOST", "https://other")
	const whWarn = `warning: workspace.warehouse_id "wh123" from targets/dev.yml is used; DATABRICKS_WAREHOUSE_ID ("other-wh") is ignored`
	const hostWarn = `warning: workspace.host "https://dev.example.com" from targets/dev.yml is used; DATABRICKS_HOST ("https://other") is ignored`
	for _, args := range [][]string{
		{cmdPlan, "-t", litDev}, {cmdApply, "-t", litDev, flagAutoApprove}, {cmdValidate, "-t", litDev},
		{cmdValidate}, {cmdPull, "-t", litDev},
		{cmdDrop, "-t", litDev, flagSchema, litSilver, "--table", "x", flagAutoApprove},
	} {
		emptyLiveBackend(t)
		_, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, "%v: %s", args, stderr)
		wantWH, wantHost := 1, 1
		if args[0] == cmdValidate && len(args) == 1 {
			// without -t the target file is not loaded: the project file (no warehouse, no host) decides
			wantWH, wantHost = 0, 0
		}
		assert.Equal(t, wantWH, strings.Count(stderr, whWarn), "%v: %s", args, stderr)
		assert.Equal(t, wantHost, strings.Count(stderr, hostWarn), "%v: %s", args, stderr)
	}
}

// The host of a file is what the backend gets, whatever DATABRICKS_HOST says.
func TestHostPriority_FileHostPassedToBackend(t *testing.T) {
	setupPlanProject(t)
	t.Setenv("DATABRICKS_HOST", "https://other")
	var gotHost string
	m := &backendtest.MockUCBackend{}
	m.SetFixture(silverRef, []*backend.LiveTable{})
	useBackendFactory(t, func(host, _ string) (backend.UCBackend, error) { gotHost = host; return m, nil })
	_, _, err := executeCmdSplit("plan", "-t", litDev)
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.com", gotHost)
}

// --- targets that share a catalog ---

const sharedWarning = "warning: targets dev, prod share the UC catalog \"dev\" of the logical catalog \"main\"; give each its own catalogs.main.uc_name in targets/<name>.yml\n"

func TestSharedCatalogWarning_PlanApplyValidate(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "prod.yml"), "workspace:\n  warehouse_id: p\n")
	for _, args := range warnArgs {
		emptyLiveBackend(t)
		_, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, "%v: %s", args, stderr)
		assert.Equal(t, 1, strings.Count(stderr, sharedWarning), "%v: %s", args, stderr)
	}
	// not for pull and drop
	emptyLiveBackend(t)
	_, stderr, err := executeCmdSplit("pull", "-t", litDev)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "share the UC catalog")
	_, stderr, err = executeCmdSplit("drop", "-t", litDev, "--schema", litSilver, "--table", "x", flagAutoApprove)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "share the UC catalog")
}

func TestSharedCatalogWarning_NotWhenTargetsHaveOwnCatalogs(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), "workspace:\n  warehouse_id: w\ncatalogs:\n  main:\n    uc_name: dev\n")
	writeFile(t, filepath.Join(dir, "targets", "prod.yml"), "catalogs:\n  main:\n    uc_name: prod\n")
	emptyLiveBackend(t)
	_, stderr, err := executeCmdSplit("plan", "-t", litDev)
	require.NoError(t, err, stderr)
	assert.NotContains(t, stderr, "share the UC catalog")
}

// A single target, and a target next to one with its own catalog: no warning.
func TestSharedCatalogWarning_NeedsTwoTargetsWithoutCatalog(t *testing.T) {
	dir := setupPlanProject(t) // targets/dev.yml has no catalog
	emptyLiveBackend(t)
	_, stderr, err := executeCmdSplit("plan", "-t", litDev)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "share the UC catalog", "a single target")

	writeFile(t, filepath.Join(dir, "targets", "prod.yml"), "catalogs:\n  main:\n    uc_name: prod\n")
	emptyLiveBackend(t)
	_, stderr, err = executeCmdSplit("plan", "-t", litDev)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "share the UC catalog", "dev and prod resolve to different catalogs")
}

// The warning does not change exit codes and does not reach stdout (plan --json stays one document).
func TestSharedCatalogWarning_DoesNotBreakJSON(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "prod.yml"), "workspace: {}\n")
	useBackend(t, addColumnMock())
	stdout, stderr, err := executeCmdSplit("plan", "-t", litDev, flagJSON)
	require.NoError(t, err)
	assert.Contains(t, stderr, sharedWarning)
	var doc map[string]any
	decodeSingleJSON(t, stdout, &doc)
}

// Two targets that type the same explicit catalog (not the catalog file's value) share it too.
func TestSharedCatalogWarning_SameExplicitValue(t *testing.T) {
	dir := setupPlanProject(t)
	writeFile(t, filepath.Join(dir, "targets", "dev.yml"), "workspace:\n  warehouse_id: w\ncatalogs:\n  main:\n    uc_name: same\n")
	writeFile(t, filepath.Join(dir, "targets", "prod.yml"), "catalogs:\n  main:\n    uc_name: same\n")
	for _, args := range warnArgs {
		emptyLiveBackend(t)
		_, stderr, err := executeCmdSplit(args...)
		require.NoError(t, err, "%v: %s", args, stderr)
		assert.Equal(t, 1, strings.Count(stderr, `warning: targets dev, prod share the UC catalog "same" of the logical catalog "main"; give each its own catalogs.main.uc_name in targets/<name>.yml`+"\n"), "%v: %s", args, stderr)
	}
}
