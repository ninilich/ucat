package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeInitCmd runs the init cobra command with the given stdin content
// and returns combined stdout+stderr output and the execution error.
func executeInitCmd(t *testing.T, stdin string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	// The error is not printed (execRoot instead of runRootIn): this is the historical
	// behaviour of this helper. The input is restored when the test ends.
	useStdin(t, stdin)
	err := execRoot(buf, buf, []string{cmdInit})
	return buf.String(), err
}

// TestInit_FullFlow verifies that init with full valid input creates all expected files.
func TestInit_FullFlow(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Inputs in order:
	// project name, logical catalog, logical schema, UC schema name, targets,
	// per target: host, UC catalog name, warehouse
	stdin := "myproj\nmain\nsilver\nsilver_uc\ndev,prod\nhttps://dev.example.com\ndev_uc\nabc\nhttps://prod.example.com\nprod_uc\nxyz\n"
	out, err := executeInitCmd(t, stdin)
	require.NoError(t, err)

	createdCount := strings.Count(out, "Created")
	assert.GreaterOrEqual(t, createdCount, 5, "at least 5 'Created' lines expected")

	ucatContent, readErr := os.ReadFile(filepath.Join(tmpDir, "ucat.yml"))
	require.NoError(t, readErr)
	assert.Equal(t, "project: myproj\ntables_dir: tables\n", string(ucatContent))

	mainCatalog, readErr := os.ReadFile(filepath.Join(tmpDir, "catalogs", "main.yml"))
	require.NoError(t, readErr)
	assert.Equal(t, "name: main\nuc_name: main\nschemas:\n  silver:\n    uc_name: silver_uc\n", string(mainCatalog),
		"the catalog file keeps the logical name as uc_name: the target decides the environment")

	devTarget, readErr := os.ReadFile(filepath.Join(tmpDir, "targets", "dev.yml"))
	require.NoError(t, readErr)
	assert.Equal(t, "workspace:\n  host: https://dev.example.com\n  warehouse_id: abc\ncatalogs:\n  main:\n    uc_name: dev_uc\n", string(devTarget))
	prodTarget, readErr := os.ReadFile(filepath.Join(tmpDir, "targets", "prod.yml"))
	require.NoError(t, readErr)
	assert.Equal(t, "workspace:\n  host: https://prod.example.com\n  warehouse_id: xyz\ncatalogs:\n  main:\n    uc_name: prod_uc\n", string(prodTarget))

	assert.DirExists(t, filepath.Join(tmpDir, "tables"))
}

// TestInit_EmptyProjectName verifies that an empty project name returns an error.
func TestInit_EmptyProjectName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	out, err := executeInitCmd(t, "\n")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "project name is required")
}

// TestInit_CatalogDefault verifies that empty inputs use sensible defaults:
// catalog defaults to "main", the UC catalog of a target defaults to the logical name, targets default to
// "dev" (not "dev,prod").
// A UC schema name must be provided (no default).
func TestInit_CatalogDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Inputs:
	// 1. project name: "myproj"
	// 2. catalog name: "" → default "main"
	// 3. logical schema name: "silver"
	// 4. UC schema name: "silver"
	// 5. targets: "" → default "dev"
	// 6. dev host: ""
	// 7. dev UC catalog name: "" → default "main"
	// 8. dev warehouse: ""
	stdin := "myproj\n\nsilver\nsilver\n\n\n\n\n"
	_, err := executeInitCmd(t, stdin)
	require.NoError(t, err)

	mainCatalog, readErr := os.ReadFile(filepath.Join(tmpDir, "catalogs", "main.yml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(mainCatalog), "uc_name: main")
	devTarget, readErr := os.ReadFile(filepath.Join(tmpDir, "targets", "dev.yml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(devTarget), "catalogs:\n  main:\n    uc_name: main\n")

	// Default target is now "dev" only (not "dev,prod")
	assert.FileExists(t, filepath.Join(tmpDir, "targets", "dev.yml"))
	_, statErr := os.Stat(filepath.Join(tmpDir, "targets", "prod.yml"))
	assert.True(t, os.IsNotExist(statErr), "prod.yml must not be created when target defaults to 'dev'")
}

// TestInit_EmptySchemaName verifies that an empty UC schema name defaults to the logical schema name.
func TestInit_EmptySchemaName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Provide project name, catalog defaults, logical schema "silver", empty UC schema → defaults to "silver"
	stdin := "myproj\n\nsilver\n\n\n\n\n"
	_, err := executeInitCmd(t, stdin)
	require.NoError(t, err)

	mainCatalog, readErr := os.ReadFile(filepath.Join(tmpDir, "catalogs", "main.yml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(mainCatalog), "uc_name: silver", "empty UC schema must default to logical schema name")
}

// TestInit_UCatYmlAlreadyExists verifies that init fails when ucat.yml already exists.
func TestInit_UCatYmlAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ucat.yml"), []byte("existing\n"), 0644))

	out, err := executeInitCmd(t, "myproj\n")
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "ucat.yml already exists")
}

// TestInit_TargetFileAlreadyExists verifies that init fails when a target file already exists.
func TestInit_TargetFileAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "targets"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "targets", "dev.yml"), []byte("existing\n"), 0644))

	stdin := "myproj\nmain\nsilver\nsilver_uc\ndev,prod\nhttps://dev.example.com\ndev_uc\nabc\nhttps://prod.example.com\nprod_uc\nxyz\n"
	out, err := executeInitCmd(t, stdin)
	require.Error(t, err)
	combined := out + err.Error()
	assert.Contains(t, combined, "already exists")
	assert.Contains(t, combined, "targets/dev.yml")
}

// TestInit_SingleTarget verifies that specifying a single target creates only that target file.
func TestInit_SingleTarget(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Single target "prod": project, catalog, schema, uc_schema, targets, host, uc_catalog, warehouse
	stdin := "myproj\nmain\nsilver\nsilver_uc\nprod\nhttps://prod.example.com\nprod\nxyz\n"
	_, err := executeInitCmd(t, stdin)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(tmpDir, "targets", "prod.yml"))

	_, statErr := os.Stat(filepath.Join(tmpDir, "targets", "dev.yml"))
	assert.True(t, os.IsNotExist(statErr), "dev.yml must not be created when only prod is specified")
}

// TestInit_UCSchemaName verifies that the UC schema name is written as uc_name
// under the schemas section of the catalog file.
func TestInit_UCSchemaName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Inputs: project="p", catalog="main", schema="silver", ucSchema="silver_uc", targets="dev",
	// then host, UC catalog and warehouse of dev (defaults)
	stdin := "p\nmain\nsilver\nsilver_uc\ndev\n\n\n\n"
	_, err := executeInitCmd(t, stdin)
	require.NoError(t, err)

	mainCatalog, readErr := os.ReadFile(filepath.Join(tmpDir, "catalogs", "main.yml"))
	require.NoError(t, readErr)
	body := string(mainCatalog)
	assert.Contains(t, body, "uc_name: silver_uc", "catalog file must contain UC schema name under schemas section")
}

// TestInit_EmptyWarehouseAndHost verifies that a target with empty host and empty warehouse_id
// produces a target file with a commented warehouse_id placeholder (not "workspace: {}").
func TestInit_EmptyWarehouseAndHost(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Single target "dev" with empty host and empty warehouse_id
	// Inputs: project, catalog, schema, uc_schema, targets, dev host, dev UC catalog, dev warehouse
	stdin := "myproj\nmain\nsilver\nsilver\ndev\n\n\n\n"
	_, err := executeInitCmd(t, stdin)
	require.NoError(t, err)

	devTarget, readErr := os.ReadFile(filepath.Join(tmpDir, "targets", "dev.yml"))
	require.NoError(t, readErr)
	body := string(devTarget)
	// When warehouse_id not entered, file must contain commented placeholder
	assert.Contains(t, body, "# warehouse_id:", "missing warehouse_id must produce a commented placeholder")
	// File should NOT be the empty "workspace: {}" form anymore
	assert.NotContains(t, body, "workspace: {}", "workspace block must not be empty-inline when host is provided or placeholder written")
}

// TestInit_WarehousePromptText verifies that the warehouse prompt text is informative.
func TestInit_WarehousePromptText(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Provide all inputs up to (and including) the warehouse prompt, then EOF.
	// The prompt text should appear in output before the warehouse_id input.
	stdin := "myproj\nmain\nsilver\nsilver\ndev\nhttps://dev.example.com\nmain\n\n"
	out, err := executeInitCmd(t, stdin)
	require.NoError(t, err)
	assert.Contains(t, out, "SQL warehouse ID", "warehouse prompt must mention 'SQL warehouse ID'")
	assert.Contains(t, out, "Databricks UI", "warehouse prompt must mention 'Databricks UI'")
}

// --- OS errors without a duplicated path; long and failing answers ---

const initFullAnswers = "proj\nmain\nsilver\nsilver\ndev\nhttps://h.example.com\nmain\nwh1\n"

// A write error in init shows "<op>: <reason>" with the relative file, no absolute path, no repeated path.
func TestInit_WriteError_NoAbsolutePathNoDuplicate(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	emptyDir(t)
	dir, err := os.Getwd()
	require.NoError(t, err)
	denyAccess(t, dir, 0o755)
	out, err := executeInitCmd(t, initFullAnswers)
	require.Error(t, err)
	msg := err.Error()
	assert.Equal(t, "write ucat.yml: open: permission denied", msg)
	assert.Equal(t, 1, strings.Count(msg, "ucat.yml"), "path must not repeat: %s", msg)
	assertNoAbsolutePath(t, dir, msg+out)
}

// A target file that cannot be written: error names the relative file once.
func TestInit_TargetWriteError_RelativePathOnce(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	emptyDir(t)
	dir, err := os.Getwd()
	require.NoError(t, err)
	// targets/ exists and is read-only, so targets/dev.yml cannot be created
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "targets"), 0o755))
	denyAccess(t, filepath.Join(dir, "targets"), 0o755)
	out, err := executeInitCmd(t, initFullAnswers)
	require.Error(t, err)
	msg := err.Error()
	assert.Equal(t, "write targets/dev.yml: open: permission denied", msg)
	assertNoAbsolutePath(t, dir, msg+out)
}

// EOF is not an error: the remaining answers take their defaults (schema is required, so it stops there).
func TestInit_EOF_UsesDefaults(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, "proj\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "logical schema name")
	assert.NotContains(t, err.Error(), "failed to read answer")
}

// The files init writes are what the target resolution reads: each target gets its own UC catalog for the
// same logical catalog, the schemas stay the same (uc_name of the catalog file is the logical name).
func TestInit_TargetsResolveToTheirOwnCatalogs(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, "p\nmain\nsilver\nsilver_uc\ndev,prod\nhttps://d\ndev_cat\nw\nhttps://p\nprod_cat\nw\n")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll("tables/silver", 0o755))
	require.NoError(t, os.WriteFile("tables/silver/t.yml", []byte(tableYAMLOf("t", "silver")), 0o644))

	for target, ref := range map[string]string{litDev: "dev_cat", litProd: "prod_cat"} {
		m := &backendtest.MockUCBackend{}
		m.SetFixture(backend.SchemaRef{UCCatalog: ref, UCSchema: "silver_uc"}, []*backend.LiveTable{})
		useBackend(t, m)
		stdout, stderr, runErr := executeCmdSplit("plan", "-t", target, "--json")
		require.NoError(t, runErr, stderr)
		assert.Contains(t, stdout, `"path":"`+ref+`.silver_uc.t"`, target)
		assert.NotContains(t, stderr, "share the UC catalog", "targets with their own catalogs")
	}
}

// With one target the default UC catalog is the logical name, with several it is <logical>_<target>, in lower
// case: accepting the defaults gives every target its own catalog and no shared-catalog warning.
func TestInit_DefaultUCCatalog(t *testing.T) {
	cases := []struct {
		name, stdin string
		want        map[string]string // target file -> uc_name
	}{
		{"one target", "p\nmain\nsilver\nsilver\ndev\nh\n\nw\n", map[string]string{litDev: litMain}},
		{"two targets", "p\nmain\nsilver\nsilver\ndev,prod\nh\n\nw\nh\n\nw\n", map[string]string{litDev: litMainDev, "prod": "main_prod"}},
		{"upper-case logical name, one target", "p\nMain\nsilver\nsilver\ndev\nh\n\nw\n", map[string]string{litDev: "main"}},
		{"upper-case names, two targets", "p\nMain\nsilver\nsilver\nDev,PROD\nh\n\nw\nh\n\nw\n", map[string]string{"Dev": litMainDev, "PROD": "main_prod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emptyDir(t)
			_, err := executeInitCmd(t, tc.stdin)
			require.NoError(t, err)
			for target, uc := range tc.want {
				body, readErr := os.ReadFile("targets/" + target + ".yml")
				require.NoError(t, readErr)
				assert.Contains(t, string(body), "uc_name: "+uc+"\n", target)
			}
		})
	}
}

// The prompt shows the default it will use.
func TestInit_DefaultUCCatalog_PromptText(t *testing.T) {
	emptyDir(t)
	out, err := executeInitCmd(t, "p\nMain\nsilver\nsilver\nDev,prod\nh\n\nw\nh\n\nw\n")
	require.NoError(t, err)
	assert.Contains(t, out, "UC catalog name for target 'Dev' [main_dev]: ")
	assert.Contains(t, out, "UC catalog name for target 'prod' [main_prod]: ")
}

func TestDefaultUCCatalog(t *testing.T) {
	assert.Equal(t, "main", defaultUCCatalog("main", "dev", 1))
	assert.Equal(t, "main", defaultUCCatalog("Main", "dev", 1))
	assert.Equal(t, litMainDev, defaultUCCatalog("main", "dev", 2))
	assert.Equal(t, litMainDev, defaultUCCatalog("MAIN", "Dev", 3))
}

// Two targets that the user gave the same catalog share it: the first plan says so.
func TestInit_SameCatalogForTwoTargets_SharedCatalogWarning(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, "p\nmain\nsilver\nsilver\ndev,prod\nh\nsame\nw\nh\nsame\nw\n")
	require.NoError(t, err)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(backend.SchemaRef{UCCatalog: "same", UCSchema: litSilver}, []*backend.LiveTable{})
	useBackend(t, m)
	_, stderr, runErr := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, runErr, stderr)
	assert.Contains(t, stderr, `warning: targets dev, prod share the UC catalog "same" of the logical catalog "main"; give each its own catalogs.main.uc_name in targets/<name>.yml`)
}

// Default answers for two targets do not trigger the warning.
func TestInit_DefaultCatalogsForTwoTargets_NoSharedCatalogWarning(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, "p\nmain\nsilver\nsilver\ndev,prod\nh\n\nw\nh\n\nw\n")
	require.NoError(t, err)
	m := &backendtest.MockUCBackend{}
	m.SetFixture(backend.SchemaRef{UCCatalog: litMainDev, UCSchema: "silver"}, []*backend.LiveTable{})
	useBackend(t, m)
	_, stderr, runErr := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, runErr, stderr)
	assert.NotContains(t, stderr, "share the UC catalog")
}

// A project init wrote with an upper-case logical catalog validates, with and without a target, and Enter on
// the UC catalog prompt is accepted (the default is lower case).
func TestInit_UpperCaseLogicalCatalog_ProjectValidates(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, "p\nMain\nsilver\nsilver\ndev\nh\n\nw\n")
	require.NoError(t, err)
	useBackend(t, &backendtest.MockUCBackend{})
	out, runErr := executeCmd("validate")
	require.NoError(t, runErr, out)
	out, runErr = executeCmd("validate", "-t", "dev")
	require.NoError(t, runErr, out)
}
