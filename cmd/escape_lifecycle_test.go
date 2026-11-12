package cmd

// escape_lifecycle_test.go covers the lifecycle: a backslash in comments and
// tblproperties is escaped in the SQL handed to the backend, and a re-plan against a
// live state equal to desired produces no changes.

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const escapeTableYAML = "name: orders\nschema: silver\ncomment: 'path C:\\tmp'\n" +
	"columns:\n  - name: id\n    type: BIGINT\n    comment: 'col\\n it''s'\n" +
	"tblproperties:\n  my.path: 'a\\b'\n"

func TestApply_BackslashEscapedInSQL(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders", escapeTableYAML)

	b := newSpy(nil)
	b.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{})
	useSpy(t, b)

	_, err := executeCmd("apply", "-t", "dev", "--auto-approve")
	require.NoError(t, err)

	require.Len(t, b.sqls, 1)
	sql := b.sqls[0]
	assert.Contains(t, sql, `COMMENT 'path C:\\tmp'`, "table comment must double the backslash")
	assert.Contains(t, sql, `COMMENT 'col\\n it\'s'`, "column comment must double the backslash and escape the quote")
	assert.Contains(t, sql, `'my.path'='a\\b'`, "tblproperty value must double the backslash")
}

func TestPlan_BackslashValues_LiveEqualsDesired_NoChanges(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "orders", escapeTableYAML)

	v := `a\b`
	b := newSpy(nil)
	b.SetFixture(backend.SchemaRef{UCCatalog: litDev, UCSchema: litSilver}, []*backend.LiveTable{backendtest.LiveOf(config.Table{
		Name:            litOrders,
		Schema:          litSilver,
		ResolvedCatalog: litDev,
		ResolvedSchema:  litSilver,
		Comment:         `path C:\tmp`,
		Columns:         []config.Column{{Name: "id", Type: typBIGINT, Comment: `col\n it's`}},
		TblProperties:   map[string]*string{"my.path": &v},
	})})
	useSpy(t, b)

	out, err := executeCmd("plan", "-t", "dev")
	require.NoError(t, err)
	assert.Contains(t, out, "No changes", "live equal to desired must yield an empty plan")
	assert.Empty(t, b.sqls)
}
