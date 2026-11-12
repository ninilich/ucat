package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var pullDevSilver = backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}

// pullYAMLColTypes reads a table file as raw YAML (no validation) and returns column name -> type.
func pullYAMLColTypes(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc struct {
		Columns []struct{ Name, Type string } `yaml:"columns"`
	}
	require.NoError(t, yaml.Unmarshal(b, &doc))
	m := map[string]string{}
	for _, c := range doc.Columns {
		m[c.Name] = c.Type
	}
	return m
}

func pullLive(name string, cols []config.Column, props map[string]*string) *backend.LiveTable {
	return backendtest.LiveOf(config.Table{
		Name: name, ResolvedCatalog: litDev, ResolvedSchema: litSilver,
		Columns: cols, TblProperties: props,
	})
}

func pullTableFile(dir, name string) string {
	return filepath.Join(dir, "tables", "silver", name+".yml")
}

func pullReadTable(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(pullTableFile(dir, name))
	require.NoError(t, err)
	return string(b)
}

// pullLoadTables loads all tables of the project with the strict loader, keyed by name.
func pullLoadTables(t *testing.T, dir string) map[string]config.Table {
	t.Helper()
	proj, diags := config.LoadProject(dir)
	require.False(t, diags.HasError(), diags)
	tables, diags := config.LoadTables(dir, proj)
	require.Empty(t, diags, diags)
	out := map[string]config.Table{}
	for _, tbl := range tables {
		out[tbl.Name] = tbl
	}
	return out
}

func pullColTypes(tbl config.Table) map[string]string {
	m := map[string]string{}
	for _, c := range tbl.Columns {
		m[c.Name] = c.Type
	}
	return m
}

// liveColTypes maps the column names of a live table to their types.
func liveColTypes(tbl *backend.LiveTable) map[string]string {
	m := map[string]string{}
	for _, c := range tbl.Columns {
		m[c.Name] = c.Type
	}
	return m
}

func pullTypesFixture() []*backend.LiveTable {
	return []*backend.LiveTable{pullLive("t", []config.Column{
		{Name: litCVarchar, Type: typVarchar10},
		{Name: litCChar, Type: typCHAR5},
		{Name: litCSpaces, Type: "VARCHAR( 7 )"},
		{Name: litCArray, Type: typArrayVarchar5},
		{Name: litCStruct, Type: typStructAChar2BInt},
		{Name: litCMap, Type: typMapStringVarchar3},
		{Name: litCName, Type: "struct<varchar_col:int>"},
		{Name: litCDec, Type: typDecimal102},
		{Name: litCStr, Type: typSTRING},
	}, nil)}
}

var pullTypesWant = map[string]string{
	"c_varchar": typSTRING,
	"c_char":    typSTRING,
	"c_spaces":  typSTRING,
	"c_array":   typArrayVarchar5,
	"c_struct":  typStructAChar2BInt,
	"c_map":     typMapStringVarchar3,
	"c_name":    "struct<varchar_col:int>",
	"c_dec":     typDecimal102,
	"c_str":     typSTRING,
}

// --- CHAR/VARCHAR ---

func TestPull_Types_CharVarcharReplaced(t *testing.T) {
	dir := setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullTypesFixture())
	useBackend(t, mock)

	_, _, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)

	assert.Equal(t, pullTypesWant, pullColTypes(pullLoadTables(t, dir)["t"]))
}

func TestPull_Types_WarningsOnlyForChangedColumns(t *testing.T) {
	setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullTypesFixture())
	useBackend(t, mock)

	stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)

	for col, orig := range map[string]string{
		"c_varchar": typVarchar10, "c_char": typCHAR5, "c_spaces": "VARCHAR( 7 )",
	} {
		want := `warning: table "t", column "` + col + `": ` + orig + ` pulled as ` + pullTypesWant[col] + ` (length limit is dropped) (tables/silver/t.yml)`
		assert.Contains(t, stderr, want)
	}
	for _, col := range []string{"c_array", "c_struct", "c_map", "c_name", "c_dec", "c_str"} {
		assert.NotContains(t, stderr, `column "`+col+`"`)
		assert.NotContains(t, stdout, `column "`+col+`": `)
	}
}

func TestPull_Types_NoWarningsWithoutVarchar(t *testing.T) {
	setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, strictFixtureTables())
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "pulled as")
	assert.NotContains(t, stderr, "tblproperties")
}

// Pull must not mutate the backend objects: plan reads the same objects (types and filtered tblproperties).
func TestPull_DoesNotMutateBackendObject(t *testing.T) {
	cases := []struct {
		name    string
		fixture func() []*backend.LiveTable
		args    []string
		check   func(t *testing.T, live []*backend.LiveTable)
	}{
		{"types", pullTypesFixture, []string{cmdPull, "-t", litDev}, func(t *testing.T, live []*backend.LiveTable) {
			require.Len(t, live, 1)
			got := liveColTypes(live[0])
			assert.Equal(t, "varchar(10)", got["c_varchar"], "plan reads live types from the same objects")
			assert.Equal(t, "CHAR(5)", got["c_char"])
			assert.Equal(t, "array<varchar(5)>", got["c_array"])
		}},
		{"tblproperties filter", pullPropsFixture, []string{cmdPull, "-t", litDev, flagTblproperties, litCustomNote}, func(t *testing.T, live []*backend.LiveTable) {
			assert.Len(t, live[0].Properties, len(pullPropsFixture()[0].Properties))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTestProject(t)
			mock := &backendtest.MockUCBackend{}
			mock.SetFixture(pullDevSilver, tc.fixture())
			useBackend(t, mock)

			_, _, err := executeCmdSplit(tc.args...)
			require.NoError(t, err)
			live, err := mock.ListTables(context.Background(), pullDevSilver)
			require.NoError(t, err)
			tc.check(t, live)
		})
	}
}

func TestPull_Types_ForceOverExistingFile(t *testing.T) {
	dir := setupTestProject(t)
	writeFile(t, pullTableFile(dir, "t"), "name: t\nschema: silver\ncolumns:\n  - name: c_varchar\n    type: INT\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullTypesFixture())
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--force")
	require.NoError(t, err)

	assert.Equal(t, pullTypesWant, pullColTypes(pullLoadTables(t, dir)["t"]))
	assert.Contains(t, stderr, `column "c_varchar"`)
}

// TestPull_Types_Pipeline: pull -> validate -> validate -t -> plan shows alters -> live becomes STRING -> No changes.
func TestPull_Types_Pipeline(t *testing.T) {
	setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, []*backend.LiveTable{pullLive("t", []config.Column{
		{Name: "v", Type: "varchar(10)"},
		{Name: "c", Type: "CHAR(5)"},
	}, nil)})
	useBackend(t, mock)

	_, _, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)

	out, errOut, err := executeCmdSplit("validate")
	require.NoError(t, err, out+errOut)
	out, errOut, err = executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err, out+errOut)

	out, errOut, err = executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err, out+errOut)
	assert.Equal(t, 1, strings.Count(out, `alter column "v" type varchar(10) → STRING`), out)
	assert.Equal(t, 1, strings.Count(out, `alter column "c" type CHAR(5) → STRING`), out)

	// the real table was altered: backend now reports STRING
	mock.SetFixture(pullDevSilver, []*backend.LiveTable{pullLive("t", []config.Column{
		{Name: "v", Type: typSTRING},
		{Name: "c", Type: typSTRING},
	}, nil)})
	out, errOut, err = executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err, out+errOut)
	assert.Contains(t, out, "No changes")
}

// Validator rejection of these types is also pinned by the validator unit tests;
// this checks the same through the validate command on a project file.
func TestValidate_StillRejectsLengthAndUnsupportedTypes(t *testing.T) {
	for _, typ := range []string{"VARCHAR(10)", "CHAR(5)", "TIME", "VOID", "OBJECT", "FILE"} {
		t.Run(typ, func(t *testing.T) {
			dir := setupTestProject(t)
			writeFile(t, pullTableFile(dir, "t"), "name: t\nschema: silver\ncolumns:\n  - name: c\n    type: "+typ+"\n")

			stdout, stderr, err := executeCmdSplit("validate")
			require.Error(t, err, stdout+stderr)
			assert.Contains(t, strings.ToLower(stdout+stderr+err.Error()), strings.ToLower(typ))
		})
	}
}

// --- tblproperties ---

func pullPropsFixture() []*backend.LiveTable {
	return []*backend.LiveTable{pullLive("orders",
		[]config.Column{{Name: "id", Type: typBIGINT}},
		map[string]*string{
			litCustomTeam:                strPtr("data"),
			"custom.team2":               strPtr("x"),
			litCustomNote:                strPtr("hello"),
			propChangeDataFeed:           strPtr("true"),
			litDeltaMinReaderVersion:     strPtr("1"),
			"delta.lastCommitTimestamp":  strPtr("123"),
			litIoUnitycatalogTableID:     strPtr("abc"),
			"delta.feature.rowTracking":  strPtr("supported"),
			litDatabricksInternalSecret:  strPtr("s"),
			litDatabricksInternalAnother: strPtr("s2"),
		})}
}

func pullWithProps(t *testing.T, extra ...string) (props map[string]*string, body, stderr string) {
	t.Helper()
	dir := setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullPropsFixture())
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit(append([]string{cmdPull, "-t", litDev}, extra...)...)
	require.NoError(t, err, stderr)
	return pullLoadTables(t, dir)["orders"].TblProperties, pullReadTable(t, dir, "orders"), stderr
}

func pullKeys(m map[string]*string) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	return k
}

func TestPull_TblProps_DefaultNone(t *testing.T) {
	props, body, stderr := pullWithProps(t)
	assert.Empty(t, props)
	assert.NotContains(t, body, "tblproperties")
	assert.NotContains(t, body, "custom.team")
	assert.NotContains(t, body, "delta.")
	assert.NotContains(t, stderr, "tblproperties")
}

func TestPull_TblProps_Masks(t *testing.T) {
	cases := []struct {
		name string
		mask string
		want []string
	}{
		{"prefix and exact with spaces", "custom.*, delta.enableChangeDataFeed",
			[]string{litCustomTeam, "custom.team2", "custom.note", "delta.enableChangeDataFeed"}},
		{"spaces around every mask", "  custom.note  ,  delta.minReaderVersion ",
			[]string{"custom.note", "delta.minReaderVersion"}},
		{"exact key is not a prefix", litCustomTeam, []string{"custom.team"}},
		{"wide prefix", "databricks.internal.*", []string{"databricks.internal.secret", "databricks.internal.another"}},
		{"no match is not an error", "nothing.*", nil},
		{"exact no match", "custom.tea", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props, body, stderr := pullWithProps(t, "--tblproperties", tc.mask)
			assert.ElementsMatch(t, tc.want, pullKeys(props))
			if tc.want == nil {
				assert.NotContains(t, body, "tblproperties")
			}
			assert.NotContains(t, stderr, "--tblproperties all")
		})
	}
}

func TestPull_TblProps_ExactDoesNotTakeLongerKey(t *testing.T) {
	props, _, _ := pullWithProps(t, "--tblproperties", "custom.team")
	assert.Contains(t, props, "custom.team")
	assert.NotContains(t, props, "custom.team2")
}

func TestPull_TblProps_SystemKeysExcludedByMask(t *testing.T) {
	props, body, _ := pullWithProps(t, "--tblproperties", "custom.*, delta.enableChangeDataFeed")
	for _, k := range []string{"delta.minReaderVersion", "delta.lastCommitTimestamp", "io.unitycatalog.tableId",
		"delta.feature.rowTracking", "databricks.internal.secret", "databricks.internal.another"} {
		assert.NotContains(t, props, k)
		assert.NotContains(t, body, k)
	}
	require.Contains(t, props, "custom.team")
	assert.Equal(t, "data", *props["custom.team"])
	assert.Equal(t, "true", *props["delta.enableChangeDataFeed"])
}

func TestPull_TblProps_AllWithWarning(t *testing.T) {
	props, _, stderr := pullWithProps(t, "--tblproperties", "all")
	assert.Len(t, props, len(pullPropsFixture()[0].Properties))
	assert.Contains(t, props, "io.unitycatalog.tableId")
	assert.Contains(t, props, "delta.minReaderVersion")
	assert.Contains(t, stderr, "warning: --tblproperties all")
}

func TestPull_TblProps_ParseErrorsBeforeBackend(t *testing.T) {
	for _, mask := range []string{"a,,b", "*", "a*b", "all,custom.*", "custom.*,all"} {
		t.Run(mask, func(t *testing.T) {
			dir := setupTestProject(t)
			mock := &backendtest.MockUCBackend{}
			mock.SetFixture(pullDevSilver, pullPropsFixture())
			spy := useSpy(t, newSpy(mock))

			stdout, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", mask)
			require.Error(t, err)
			assert.NotEqual(t, 0, codeOf(err))
			assert.Contains(t, stdout+stderr+err.Error(), "--tblproperties")
			assert.Zero(t, spy.factoryCalls, "must fail before touching the backend")
			assert.NoFileExists(t, pullTableFile(dir, "orders"))
		})
	}
}

// Filter applies only to pull: plan after a filtered pull sees no tblproperties changes.
func TestPull_TblProps_PlanAfterFilteredPullNoChanges(t *testing.T) {
	setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullPropsFixture())
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "custom.*")
	require.NoError(t, err, stderr)

	out, errOut, err := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err, out+errOut)
	assert.Contains(t, out, "No changes")
	assert.NotContains(t, out, "tblproperties")

	out, errOut, err = executeCmdSplit("validate", "-t", "dev")
	require.NoError(t, err, out+errOut)
}

func TestPull_TblProps_DefaultPullPlanNoChanges(t *testing.T) {
	setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullPropsFixture())
	useBackend(t, mock)

	_, _, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	out, errOut, err := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err, out+errOut)
	assert.Contains(t, out, "No changes")
}

// One-way diff is untouched: hand-edited tblproperties are still compared with live.
func TestPull_TblProps_ManualEditStillDiffed(t *testing.T) {
	dir := setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullPropsFixture())
	useBackend(t, mock)

	_, _, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "custom.*")
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(pullReadTable(t, dir, "orders")), &doc))
	props, ok := doc["tblproperties"].(map[string]any)
	require.True(t, ok, doc)
	require.Contains(t, props, "custom.team")
	props["custom.team"] = "other"
	edited, err := yaml.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(pullTableFile(dir, "orders"), edited, 0644))

	out, errOut, err := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err, out+errOut)
	assert.Contains(t, out, "set 1 tblproperties")
	assert.NotContains(t, out, "No changes")
}

func TestPull_TblProps_Deterministic(t *testing.T) {
	dir := setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullPropsFixture())
	useBackend(t, mock)

	args := []string{cmdPull, "-t", litDev, flagForce, flagTblproperties, litAll}
	_, _, err := executeCmdSplit(args...)
	require.NoError(t, err)
	first := pullReadTable(t, dir, "orders")
	for i := 0; i < 3; i++ {
		_, _, err = executeCmdSplit(args...)
		require.NoError(t, err)
		assert.Equal(t, first, pullReadTable(t, dir, "orders"))
	}
}

// --- boundary types (top level only, spaces) ---

func TestPull_Types_Boundaries(t *testing.T) {
	cases := []struct {
		typ     string
		want    string
		warning bool
	}{
		{"Varchar(  3 )", typSTRING, true},
		{"  varchar(4)  ", typSTRING, true},
		{"CHAR( 5 )", typSTRING, true},
		{"char(0)", typSTRING, true},
		// not converted, and validate rejects them: the unsupported-type warning, not the conversion one
		{"varchar(x)", "varchar(x)", true},
		{"varchar()", "varchar()", true},
		{"varchar(1,2)", "varchar(1,2)", true},
		{"decimal(10,2)", "decimal(10,2)", false},
		{typSTRING, "STRING", false},
		{"array<varchar(5)>", "array<varchar(5)>", false},
		{"map<string,varchar(3)>", "map<string,varchar(3)>", false},
		{"struct<a:char(2),b:int>", "struct<a:char(2),b:int>", false},
		{"array<struct<a:varchar(2)>>", "array<struct<a:varchar(2)>>", false},
	}
	var cols []config.Column
	for i, c := range cases {
		cols = append(cols, config.Column{Name: fmt.Sprintf("c%d", i), Type: c.typ})
	}
	dir := setupTestProject(t)
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, []*backend.LiveTable{pullLive("t", cols, nil)})
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err, stderr)

	got := pullYAMLColTypes(t, pullTableFile(dir, "t"))
	for i, c := range cases {
		name := fmt.Sprintf("c%d", i)
		assert.Equal(t, c.want, got[name], "type %q", c.typ)
		hasWarn := strings.Contains(stderr, fmt.Sprintf(`column "%s": `, name))
		assert.Equal(t, c.warning, hasWarn, "warning for type %q\n%s", c.typ, stderr)
	}
	assert.Equal(t, 4, strings.Count(stderr, "pulled as STRING"), stderr)
}

// --- Location in the type warning ---

func TestPull_Types_WarningLocationForceOtherFile(t *testing.T) {
	dir := setupTestProject(t)
	// the table is defined in a file whose name differs from the table name
	writeFile(t, filepath.Join(dir, "tables", "silver", "legacy_defs.yml"),
		"name: t\nschema: silver\ncolumns:\n  - name: v\n    type: INT\n")
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, []*backend.LiveTable{pullLive("t", []config.Column{{Name: "v", Type: "varchar(9)"}}, nil)})
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--force")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, `column "v": varchar(9) pulled as STRING (length limit is dropped) (tables/silver/legacy_defs.yml)`)
	assert.NotContains(t, stderr, "tables/silver/t.yml")
	assert.Equal(t, map[string]string{"v": "STRING"}, pullYAMLColTypes(t, filepath.Join(dir, "tables", "silver", "legacy_defs.yml")))
}

func TestPull_Types_SameTableTwoSchemasDistinctWarnings(t *testing.T) {
	dir := setupProjectWithCatalog(t,
		"name: main\nuc_name: dev\nschemas:\n  silver:\n    uc_name: silver\n  gold:\n    uc_name: gold\n")
	mk := func(schema string) *backend.LiveTable {
		return backendtest.Live("dev", schema, "t", backendtest.Col("v", "varchar(3)"))
	}
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, []*backend.LiveTable{mk("silver")})
	mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litGold}, []*backend.LiveTable{mk("gold")})
	useBackend(t, mock)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err, stderr)
	assert.Equal(t, 1, strings.Count(stderr, "(tables/silver/t.yml)"), stderr)
	assert.Equal(t, 1, strings.Count(stderr, "(tables/gold/t.yml)"), stderr)
	assert.FileExists(t, filepath.Join(dir, "tables", "silver", "t.yml"))
	assert.FileExists(t, filepath.Join(dir, "tables", "gold", "t.yml"))
}

// --- tblproperties: empty value, broad masks, "matched no property" ---

func TestPull_TblProps_EmptyValueIsAbsentFlag(t *testing.T) {
	_, bodyDefault, errDefault := pullWithProps(t)
	props, body, stderr := pullWithProps(t, "--tblproperties", "")
	assert.Empty(t, props)
	assert.Equal(t, bodyDefault, body)
	assert.Equal(t, errDefault, stderr)
	assert.NotContains(t, stderr, "--tblproperties")
}

func TestPull_TblProps_BroadMaskWarning(t *testing.T) {
	cases := []struct {
		mask string
		warn bool
	}{
		{"delta.*", true}, {"delta*", true}, {"d*", true}, {"databricks.*", true},
		{"io.*", true}, {"io.unitycatalog.*", true}, {"i*", true},
		{"delta.feature.*", false}, {"delta.autoOptimize.*", false}, {"custom.*", false},
		{"delta.enableChangeDataFeed", false}, {"custom.team", false}, {"io.unitycatalog.tableId", false},
	}
	for _, tc := range cases {
		t.Run(tc.mask, func(t *testing.T) {
			_, _, stderr := pullWithProps(t, "--tblproperties", tc.mask)
			w := fmt.Sprintf(`warning: --tblproperties mask "%s" can select system properties`, tc.mask)
			if tc.warn {
				assert.Equal(t, 1, strings.Count(stderr, w), stderr)
			} else {
				assert.NotContains(t, stderr, "can select system properties")
			}
		})
	}
}

func TestPull_TblProps_AllOnlyAllWarning(t *testing.T) {
	_, _, stderr := pullWithProps(t, "--tblproperties", "all")
	assert.Equal(t, 1, strings.Count(stderr, "warning: --tblproperties all"), stderr)
	assert.NotContains(t, stderr, "can select system properties")
	assert.NotContains(t, stderr, "matched no property")
}

func TestPull_TblProps_BroadMaskWarningsInMaskOrderOncePerMask(t *testing.T) {
	_, _, stderr := pullWithProps(t, "--tblproperties", "io.*, delta.*, io.*, custom.*, delta.*")
	assert.Equal(t, 1, strings.Count(stderr, `mask "io.*" can select`), stderr)
	assert.Equal(t, 1, strings.Count(stderr, `mask "delta.*" can select`), stderr)
	assert.Less(t, strings.Index(stderr, `mask "io.*"`), strings.Index(stderr, `mask "delta.*"`), stderr)
}

func TestPull_TblProps_MatchedNoProperty(t *testing.T) {
	noMatch := func(m string) string { return fmt.Sprintf(`warning: --tblproperties mask "%s" matched no property`, m) }

	t.Run("one warning per unmatched mask in mask order, matched mask silent", func(t *testing.T) {
		_, _, stderr := pullWithProps(t, "--tblproperties", "zzz, custom.*, nothing.*, custom.team")
		assert.Equal(t, 1, strings.Count(stderr, noMatch("zzz")), stderr)
		assert.Equal(t, 1, strings.Count(stderr, noMatch("nothing.*")), stderr)
		assert.NotContains(t, stderr, noMatch("custom.*"))
		assert.NotContains(t, stderr, noMatch("custom.team"))
		assert.Less(t, strings.Index(stderr, noMatch("zzz")), strings.Index(stderr, noMatch("nothing.*")), stderr)
	})
	t.Run("exact mask does not match a longer key", func(t *testing.T) {
		_, _, stderr := pullWithProps(t, "--tblproperties", "custom.tea")
		assert.Contains(t, stderr, noMatch("custom.tea"))
	})
	t.Run("duplicate masks collapse", func(t *testing.T) {
		_, _, stderr := pullWithProps(t, "--tblproperties", "nothing.*, nothing.*")
		assert.Equal(t, 1, strings.Count(stderr, noMatch("nothing.*")), stderr)
	})
	t.Run("all never reports no match", func(t *testing.T) {
		_, _, stderr := pullWithProps(t, "--tblproperties", "all")
		assert.NotContains(t, stderr, "matched no property")
	})
	t.Run("no flag no warning", func(t *testing.T) {
		_, _, stderr := pullWithProps(t)
		assert.NotContains(t, stderr, "matched no property")
	})
	t.Run("mask matched only in the second schema is silent, unmatched still warns once", func(t *testing.T) {
		setupProjectWithCatalog(t,
			"name: main\nuc_name: dev\nschemas:\n  silver:\n    uc_name: silver\n  gold:\n    uc_name: gold\n")
		mk := func(schema string, props map[string]string) *backend.LiveTable {
			t := backendtest.Live("dev", schema, "t", backendtest.Col("id", "BIGINT"))
			t.Properties = props
			return t
		}
		mock := &backendtest.MockUCBackend{}
		mock.SetFixture(pullDevSilver, []*backend.LiveTable{mk("silver", map[string]string{"a.x": "1"})})
		mock.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litGold},
			[]*backend.LiveTable{mk("gold", map[string]string{"custom.k": "1"})})
		useBackend(t, mock)

		_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "custom.*,none.*")
		require.NoError(t, err, stderr)
		assert.NotContains(t, stderr, noMatch("custom.*"))
		assert.Equal(t, 1, strings.Count(stderr, noMatch("none.*")), stderr)
	})
}

func TestPull_TblProps_MatchedNoProperty_SkippedTablesDoNotCount(t *testing.T) {
	noMatch := `warning: --tblproperties mask "custom.*" matched no property`
	live := func(name string, props map[string]*string) *backend.LiveTable {
		return pullLive(name, []config.Column{{Name: "id", Type: typBIGINT}}, props)
	}

	t.Run("match only in table skipped as already defined", func(t *testing.T) {
		dir := setupTestProject(t)
		writeFile(t, pullTableFile(dir, "old"), "name: old\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")
		mock := &backendtest.MockUCBackend{}
		mock.SetFixture(pullDevSilver, []*backend.LiveTable{
			live("old", map[string]*string{"custom.a": strPtr("1")}),
			live("fresh", map[string]*string{"x": strPtr("1")}),
		})
		useBackend(t, mock)

		_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "custom.*")
		require.NoError(t, err, stderr)
		assert.FileExists(t, pullTableFile(dir, "fresh"))
		assert.Contains(t, stderr, noMatch)
	})
	t.Run("match only in rename_from source", func(t *testing.T) {
		dir := setupTestProject(t)
		writeFile(t, pullTableFile(dir, "new_name"),
			"name: new_name\nschema: silver\nrename_from: src\ncolumns:\n  - name: id\n    type: BIGINT\n")
		mock := &backendtest.MockUCBackend{}
		mock.SetFixture(pullDevSilver, []*backend.LiveTable{
			live("src", map[string]*string{"custom.a": strPtr("1")}),
			live("fresh", map[string]*string{"x": strPtr("1")}),
		})
		useBackend(t, mock)

		_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "custom.*")
		require.NoError(t, err, stderr)
		assert.FileExists(t, pullTableFile(dir, "fresh"))
		assert.NoFileExists(t, pullTableFile(dir, "src"))
		assert.Contains(t, stderr, noMatch)
	})
	t.Run("no table written: no matched-no-property warning", func(t *testing.T) {
		dir := setupTestProject(t)
		writeFile(t, pullTableFile(dir, "old"), "name: old\nschema: silver\ncolumns:\n  - name: id\n    type: BIGINT\n")
		mock := &backendtest.MockUCBackend{}
		mock.SetFixture(pullDevSilver, []*backend.LiveTable{live("old", map[string]*string{"x": strPtr("1")})})
		useBackend(t, mock)

		_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", "custom.*")
		require.NoError(t, err, stderr)
		assert.NotContains(t, stderr, "matched no property")
	})
}

// --- timing: warnings about the masks come after config load and local validation, before the backend ---

func TestPull_TblProps_BrokenConfigNoMaskWarningsNoBackend(t *testing.T) {
	broken := map[string]string{
		"invalid yaml":       "name: t\nschema: silver\ncolumns: [unterminated\n",
		"validate rejection": "name: t\nschema: silver\ncolumns:\n  - name: c\n    type: TIME\n",
	}
	for name, body := range broken {
		for _, mask := range []string{"all", "delta.*"} {
			t.Run(name+"/"+mask, func(t *testing.T) {
				dir := setupTestProject(t)
				writeFile(t, pullTableFile(dir, "t"), body)
				mock := &backendtest.MockUCBackend{}
				mock.SetFixture(pullDevSilver, pullPropsFixture())
				spy := useSpy(t, newSpy(mock))

				stdout, stderr, err := executeCmdSplit("pull", "-t", "dev", "--tblproperties", mask)
				require.Error(t, err, stdout+stderr)
				assert.Zero(t, spy.factoryCalls, "backend must not be used on a broken config")
				all := stdout + stderr
				assert.NotContains(t, all, "warning: --tblproperties")
				assert.NotContains(t, all, "can select system properties")
				assert.NotContains(t, all, "matched no property")
			})
		}
	}
}

func TestPull_TblProps_MaskWarningsBeforeBackendCall(t *testing.T) {
	setupTestProject(t)
	var errBuf bytes.Buffer
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, pullPropsFixture())
	var atBackend string
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		atBackend = errBuf.String()
		return mock, nil
	})
	var outBuf bytes.Buffer
	require.NoError(t, runRootIn(&outBuf, &errBuf, []string{cmdPull, "-t", litDev, "--tblproperties", "delta.*,nothing.*"}))
	assert.Contains(t, atBackend, `mask "delta.*" can select system properties`)
	assert.NotContains(t, atBackend, "matched no property", "matched-no-property is reported at the end")
	assert.Contains(t, errBuf.String(), `mask "nothing.*" matched no property`)
}
