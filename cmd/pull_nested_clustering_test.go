package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- pull does not write clustering on nested fields ---

func nestedLive(name string, cluster ...string) *backend.LiveTable {
	cols := []config.Column{{Name: "a", Type: typSTRING}, {Name: "c", Type: typSTRING}}
	live := pullLive(name, cols, nil)
	live.Clustering = cluster
	return live
}

func installPullFixture(t *testing.T, tables ...*backend.LiveTable) {
	t.Helper()
	mock := &backendtest.MockUCBackend{}
	mock.SetFixture(pullDevSilver, tables)
	useBackend(t, mock)
}

func TestPull_NestedClustering(t *testing.T) {
	cases := []struct {
		name    string
		cols    []string // live clustering columns as the backend parses them (nested path joined by a dot)
		written bool
		field   string
	}{
		{"nested", []string{litAB}, false, litAB},
		{"flat", []string{"a", "c"}, true, ""},
		{"mixed", []string{"a", litBC}, false, litBC},
		{"mixed nested first", []string{"b.c", "a"}, false, "b.c"},
		{"deep nested", []string{"a.b.c"}, false, "a.b.c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupTestProject(t)
			installPullFixture(t, nestedLive("t", tc.cols...))

			_, stderr, err := executeCmdSplit("pull", "-t", "dev")
			require.NoError(t, err)
			body := pullReadTable(t, dir, "t")
			assert.Equal(t, tc.written, strings.Contains(body, "clustering:"), body)
			if tc.field == "" {
				assert.NotContains(t, stderr, "nested field")
				assert.Contains(t, body, "- a\n")
			} else {
				assert.Equal(t, 1, strings.Count(stderr, "clustering on nested field"), stderr)
				assert.Contains(t, stderr, `warning: table "t": clustering on nested field "`+tc.field+`" is not supported; clustering is not written`)
				assert.Contains(t, stderr, "(tables/silver/t.yml)")
			}

			// round trip: validate passes, plan has no clustering change
			_, _, err = executeCmdSplit("validate")
			require.NoError(t, err)
			stdout, _, err := executeCmdSplit("plan", "-t", "dev")
			require.NoError(t, err)
			assert.NotContains(t, strings.ToLower(stdout), "cluster")
		})
	}
}

// pull --force over a file that has an old clustering block: the block is gone, one warning.
func TestPull_Force_NestedClustering_RemovesOldBlock(t *testing.T) {
	dir := setupTestProject(t)
	installPullFixture(t, nestedLive("t", "a.b"))
	require.NoError(t, os.MkdirAll(dir+"/tables/silver", 0755))
	writeFile(t, pullTableFile(dir, "t"), "name: t\nschema: silver\ncolumns:\n  - name: a\n    type: STRING\n  - name: c\n    type: STRING\nclustering:\n  columns:\n    - a\n")

	// without --force the file is kept (and still has the block)
	_, _, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, pullReadTable(t, dir, "t"), "clustering:")

	_, stderr, err := executeCmdSplit("pull", "-t", "dev", "--force")
	require.NoError(t, err)
	body := pullReadTable(t, dir, "t")
	assert.NotContains(t, body, "clustering")
	assert.Equal(t, 1, strings.Count(stderr, "clustering on nested field"), stderr)
	assert.Contains(t, stderr, `"a.b"`)

	// and the plan against UC has no clustering change
	stdout, _, err := executeCmdSplit("plan", "-t", "dev")
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(stdout), "cluster")
}

// Two tables with nested clustering: one warning each, each with its own table and file.
func TestPull_NestedClustering_TwoTables_WarningEach(t *testing.T) {
	dir := setupTestProject(t)
	installPullFixture(t, nestedLive("t1", "a.b"), nestedLive("t2", "x.y"), nestedLive("t3", "a"))

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(stderr, "clustering on nested field"), stderr)
	assert.Contains(t, stderr, `table "t1": clustering on nested field "a.b"`)
	assert.Contains(t, stderr, "(tables/silver/t1.yml)")
	assert.Contains(t, stderr, `table "t2": clustering on nested field "x.y"`)
	assert.Contains(t, stderr, "(tables/silver/t2.yml)")
	assert.NotContains(t, pullReadTable(t, dir, "t1"), "clustering")
	assert.NotContains(t, pullReadTable(t, dir, "t2"), "clustering")
	assert.Contains(t, pullReadTable(t, dir, "t3"), "clustering:") // flat one is untouched
}

// Partitioning and the other fields are written as before when the clustering is dropped.
func TestPull_NestedClustering_KeepsPartitioningAndComment(t *testing.T) {
	dir := setupTestProject(t)
	live := nestedLive("t", "a.b")
	live.Partitioning = []string{"c"}
	live.Comment = "keep me"
	installPullFixture(t, live)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	body := pullReadTable(t, dir, "t")
	assert.NotContains(t, body, "clustering")
	assert.Contains(t, body, "partitioning:")
	assert.Contains(t, body, "keep me")
	assert.Equal(t, 1, strings.Count(stderr, "clustering on nested field"))
}

// Fixed by fact (question in the report): a flat column whose name contains a dot is
// indistinguishable from a nested path, so its clustering is not written either, with the warning.
func TestPull_ClusteringOnColumnWithDotInName_FixedByFact(t *testing.T) {
	dir := setupTestProject(t)
	live := pullLive("t", []config.Column{{Name: "a.b", Type: typSTRING}}, nil)
	live.Clustering = []string{"a.b"}
	installPullFixture(t, live)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.NotContains(t, pullReadTable(t, dir, "t"), "clustering")
	assert.Equal(t, 1, strings.Count(stderr, "clustering on nested field"), stderr)
}

// Automatic clustering has no columns and is still written.
func TestPull_AutoClustering_StillWritten(t *testing.T) {
	dir := setupTestProject(t)
	live := nestedLive("t")
	live.ClusterAuto = true
	installPullFixture(t, live)

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, pullReadTable(t, dir, "t"), "auto: true")
	assert.NotContains(t, stderr, "nested field")
}

// pull writes a column type that validate rejects, with a warning naming table, column, type and file.
func TestPull_UnsupportedType_WarnsAndWritesFile(t *testing.T) {
	dir := setupTestProject(t)
	installPullFixture(t, pullLive("t", []config.Column{
		{Name: "ok", Type: "map<string,int>"},
		{Name: "coll", Type: "string collate UTF8_LCASE"},
	}, nil))

	stdout, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(stderr, "is not supported by ucat validate"), stderr)
	assert.Contains(t, stderr, `warning: table "t", column "coll": type "string collate UTF8_LCASE" is not supported by ucat validate; fix it in the file (tables/silver/t.yml)`)
	assert.Contains(t, stdout, "tables/silver/t.yml")
	assert.Contains(t, pullReadTable(t, dir, "t"), "string collate UTF8_LCASE")
}

func TestPull_SupportedType_NoWarning(t *testing.T) {
	setupTestProject(t)
	installPullFixture(t, pullLive("t", []config.Column{{Name: "a", Type: "struct<a:array<decimal(10,2)>>"}}, nil))

	_, stderr, err := executeCmdSplit("pull", "-t", "dev")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "not supported")
}
