package change_test

// SQL escaping at every site where user data meets SQL:
//   - backslash and apostrophe in string literals (comments, tblproperties keys and values);
//   - hostile catalog, schema, table and column names stay inside their backtick identifiers
//     (checked structurally: the SQL is scanned like the Databricks lexer, and the identifiers found
//     must be exactly the names that were put in, in order);
//   - reserved words as column names are quoted.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
)

var escTable = change.TablePath{Catalog: "c", Schema: "s", Name: "t"}

func TestSQL_BackslashEscaping_AddColumnComment(t *testing.T) {
	ch := &change.AddColumnChange{
		Base:   change.Base{Table: escTable},
		Column: config.Column{Name: "a", Type: typSTRING, Comment: `a\nb it's x\`},
	}
	assert.Contains(t, ch.SQL(), `COMMENT 'a\\nb it\'s x\\'`)
}

func TestSQL_BackslashEscaping_AlterColumnComment(t *testing.T) {
	ch := &change.AlterColumnChange{
		Base:       change.Base{Table: escTable},
		ColumnName: "a",
		Kind:       change.AlterComment,
		NewValue:   `a\nb x\`,
	}
	assert.Contains(t, ch.SQL(), `COMMENT 'a\\nb x\\'`)
}

func TestSQL_BackslashEscaping_AlterTableComment(t *testing.T) {
	ch := &change.AlterTableCommentChange{Base: change.Base{Table: escTable}, Comment: `it's \'x`}
	assert.Contains(t, ch.SQL(), `IS 'it\'s \\\'x'`)
}

func TestSQL_BackslashEscaping_SetTBLProperties(t *testing.T) {
	ch := &change.SetTBLPropertiesChange{
		Base:       change.Base{Table: escTable},
		Properties: map[string]string{`k\1`: `a\b'c`},
	}
	assert.Contains(t, ch.SQL(), `'k\\1'='a\\b\'c'`)
}

func TestSQL_BackslashEscaping_UnsetTBLProperties(t *testing.T) {
	ch := &change.UnsetTBLPropertiesChange{Base: change.Base{Table: escTable}, Keys: []string{`k\1'`}}
	assert.Contains(t, ch.SQL(), `'k\\1\''`)
}

func TestSQL_BackslashEscaping_CreateTable(t *testing.T) {
	v := `v\x`
	ch := &change.CreateTableChange{
		Base: change.Base{Table: escTable},
		Def: config.Table{
			Name:          "t",
			Comment:       `path C:\tmp`,
			Columns:       []config.Column{{Name: "a", Type: typSTRING, Comment: `col\n'`}},
			TblProperties: map[string]*string{`k\1`: &v},
		},
	}
	sql := ch.SQL()
	assert.Contains(t, sql, `COMMENT 'col\\n\''`)
	assert.Contains(t, sql, `COMMENT 'path C:\\tmp'`)
	assert.Contains(t, sql, `'k\\1'='v\\x'`)
}

// scanIdents returns the decoded backtick-quoted identifiers of sql (outside single-quoted literals, where a
// backslash escapes the next character) and the SQL with identifiers and literals removed.
func scanIdents(t *testing.T, sql string) (idents []string, rest string) {
	t.Helper()
	var restB strings.Builder
	r := []rune(sql)
	for i := 0; i < len(r); i++ {
		switch r[i] {
		case '`':
			ident, next, closed := scanQuotedIdent(r, i)
			require.True(t, closed, "unterminated identifier in %q", sql)
			idents = append(idents, ident)
			restB.WriteString("ID")
			i = next
		case '\'':
			next, closed := scanLiteral(r, i)
			require.True(t, closed, "unterminated string literal in %q", sql)
			restB.WriteString("STR")
			i = next
		default:
			restB.WriteRune(r[i])
		}
	}
	return idents, restB.String()
}

// scanQuotedIdent decodes the identifier that opens with the backtick at r[start] (a doubled backtick
// is one literal backtick). It returns the identifier, the index of the closing backtick and whether
// there was one.
func scanQuotedIdent(r []rune, start int) (ident string, end int, closed bool) {
	var cur strings.Builder
	for i := start + 1; i < len(r); i++ {
		if r[i] != '`' {
			cur.WriteRune(r[i])
			continue
		}
		if i+1 < len(r) && r[i+1] == '`' {
			cur.WriteRune('`')
			i++
			continue
		}
		return cur.String(), i, true
	}
	return cur.String(), len(r), false
}

// scanLiteral skips the single-quoted literal that opens at r[start] (a backslash escapes the next
// character). It returns the index of the closing quote and whether there was one.
func scanLiteral(r []rune, start int) (end int, closed bool) {
	for i := start + 1; i < len(r); i++ {
		switch r[i] {
		case '\\':
			i++
		case '\'':
			return i, true
		}
	}
	return len(r), false
}

func TestChangeSQL_HostileNames_StayInsideIdentifiers(t *testing.T) {
	// role -> hostile name
	nameSets := map[string]func(role string) string{
		"backtick":  func(role string) string { return role + "`x" },
		"two ticks": func(role string) string { return role + "``x`" },
		"semicolon": func(role string) string { return role + "; DROP TABLE z" },
		"comment":   func(role string) string { return role + " -- x" },
		"mixed":     func(role string) string { return "`" + role + "`;--`" },
		"reserved":  func(_ string) string { return litOrder },
	}
	for setName, nm := range nameSets {
		t.Run(setName, func(t *testing.T) {
			cat, sch, tbl, newTbl := nm("cat"), nm("sch"), nm("tbl"), nm("newtbl")
			c1, c2, c3 := nm("col1"), nm("col2"), nm("col3")
			p := change.TablePath{Catalog: cat, Schema: sch, Name: tbl}
			path := []string{cat, sch, tbl}
			with := func(extra ...string) []string { return append(append([]string{}, path...), extra...) }
			no := false
			v := "v"

			cases := []struct {
				name string
				ch   interface{ SQL() string }
				want []string
			}{
				{"CreateTable", &change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{
					Columns:       []config.Column{{Name: c1, Type: typBIGINT, Nullable: &no, Comment: litItS}, {Name: c2, Type: typSTRING}},
					Clustering:    &config.Clustering{Columns: []string{c1, c2}},
					Partitioning:  &config.Partitioning{Columns: []string{c3}},
					TblProperties: map[string]*string{"k": &v},
				}}, with(c1, c2, c1, c2, c3)},
				{"AddColumn", &change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: c1, Type: typSTRING, Comment: "c"}}, with(c1)},
				{"AlterColumn comment", &change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: c1, Kind: change.AlterComment, NewValue: "c"}, with(c1)},
				{"AlterColumn type", &change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: c1, Kind: change.AlterType, NewValue: typBIGINT}, with(c1)},
				{"AlterColumn set not null", &change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: c1, Kind: change.AlterNullable, NewNullable: false}, with(c1)},
				{"AlterColumn drop not null", &change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: c1, Kind: change.AlterNullable, OldNullable: false, NewNullable: true}, with(c1)},
				{"DropColumn", &change.DropColumnChange{Base: change.Base{Table: p}, ColumnName: c1}, with(c1)},
				{"RenameColumn", &change.RenameColumnChange{Base: change.Base{Table: p}, OldName: c1, NewName: c2}, with(c1, c2)},
				{"RenameTable", &change.RenameTableChange{OldPath: p, NewName: newTbl}, []string{cat, sch, tbl, cat, sch, newTbl}},
				{"DropTable", &change.DropTableChange{Base: change.Base{Table: p}}, path},
				{"SetTBLProperties", &change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"k": "v"}}, path},
				{"UnsetTBLProperties", &change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}, Keys: []string{"k"}}, path},
				{"AlterClustering", &change.AlterClusteringChange{Base: change.Base{Table: p}, Clustering: &config.Clustering{Columns: []string{c1, c2}}}, with(c1, c2)},
				{"AlterClustering none", &change.AlterClusteringChange{Base: change.Base{Table: p}}, path},
				{"AlterTableComment", &change.AlterTableCommentChange{Base: change.Base{Table: p}, Comment: "c"}, path},
				{"AlterTableComment unset", &change.AlterTableCommentChange{Base: change.Base{Table: p}}, path},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					sql := tc.ch.SQL()
					idents, rest := scanIdents(t, sql)
					assert.Equal(t, tc.want, idents, "identifiers decoded from: %s", sql)
					// Nothing hostile may be left outside identifiers and literals.
					assert.NotContains(t, rest, ";", "sql: %s", sql)
					assert.NotContains(t, rest, "--", "sql: %s", sql)
					assert.NotContains(t, rest, "`", "sql: %s", sql)
					if strings.Contains(nm("x"), "`") {
						// A single unescaped backtick form of the table name must not occur anywhere.
						assert.NotContains(t, sql, fmt.Sprintf("`%s`", tbl), "unescaped identifier in: %s", sql)
					}
				})
			}
		})
	}
}

// --- Adversarial inputs - SQL escaping ---

// TestAddColumnChange_SQL_ApostropheInComment verifies that an apostrophe in a column
// comment is properly escaped in the resulting SQL.
// Unescaped: COMMENT 'O'Neil's data' → broken SQL.
// Acceptable: COMMENT 'O”Neil”s data' or COMMENT "O'Neil's data".
func TestAddColumnChange_SQL_ApostropheInComment(t *testing.T) {
	ch := &change.AddColumnChange{
		Base:   change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		Column: config.Column{Name: "col", Type: "STRING", Comment: "O'Neil's data"},
	}
	sql := ch.SQL()
	// The raw apostrophe sequence 'O'Neil would break SQL parsing.
	// After escaping, it should appear as ''O''Neil'' or be wrapped differently.
	assert.NotContains(t, sql, "COMMENT 'O'Neil",
		"apostrophe in column comment must be escaped; SQL would be invalid: %s", sql)
}

// TestSetTBLPropertiesChange_SQL_ApostropheInKey verifies that an apostrophe in a
// tblproperties key is properly escaped.
func TestSetTBLPropertiesChange_SQL_ApostropheInKey(t *testing.T) {
	ch := &change.SetTBLPropertiesChange{
		Base:       change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		Properties: map[string]string{"key'with'apostrophe": "value"},
	}
	sql := ch.SQL()
	// 'key'with'apostrophe' is broken SQL; must be escaped.
	assert.NotContains(t, sql, "'key'with'",
		"apostrophe in tblproperties key must be escaped; SQL would be invalid: %s", sql)
}

// TestSetTBLPropertiesChange_SQL_ApostropheInValue verifies that an apostrophe in a
// tblproperties value is properly escaped.
func TestSetTBLPropertiesChange_SQL_ApostropheInValue(t *testing.T) {
	ch := &change.SetTBLPropertiesChange{
		Base:       change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		Properties: map[string]string{"myKey": "val'ue"},
	}
	sql := ch.SQL()
	// 'val'ue' is broken SQL; must be escaped.
	assert.NotContains(t, sql, "'val'ue'",
		"apostrophe in tblproperties value must be escaped; SQL would be invalid: %s", sql)
}

// TestAddColumnChange_SQL_ReservedWordColumnName verifies that a reserved SQL keyword
// used as a column name is properly backtick-quoted in the SQL output.
func TestAddColumnChange_SQL_ReservedWordColumnName(t *testing.T) {
	ch := &change.AddColumnChange{
		Base:   change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		Column: config.Column{Name: litOrder, Type: typBIGINT},
	}
	sql := ch.SQL()
	assert.Contains(t, sql, "`order`",
		"reserved word column name 'order' must be backtick-quoted in SQL; got: %s", sql)
}

// TestCreateTableChange_SQL_ReservedWordColumnName verifies that reserved word column
// names are backtick-quoted inside CREATE TABLE as well.
func TestCreateTableChange_SQL_ReservedWordColumnName(t *testing.T) {
	ch := &change.CreateTableChange{
		Base: change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: "events"}},
		Def: config.Table{
			Name:   "events",
			Schema: litSilver,
			Columns: []config.Column{
				{Name: "date", Type: "DATE"},
				{Name: "table", Type: "STRING"},
			},
		},
	}
	sql := ch.SQL()
	assert.Contains(t, sql, "`date`",
		"reserved word column 'date' must be backtick-quoted in CREATE TABLE; got: %s", sql)
	assert.Contains(t, sql, "`table`",
		"reserved word column 'table' must be backtick-quoted in CREATE TABLE; got: %s", sql)
}

// TestAlterColumnChange_SQL_ApostropheInComment verifies escaping for ALTER COLUMN COMMENT.
func TestAlterColumnChange_SQL_ApostropheInComment(t *testing.T) {
	ch := &change.AlterColumnChange{
		Base:       change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		ColumnName: "name",
		Kind:       change.AlterComment,
		OldValue:   litOld,
		NewValue:   "it's a test",
	}
	sql := ch.SQL()
	// COMMENT 'it's a test' is broken SQL.
	assert.NotContains(t, sql, "COMMENT 'it's",
		"apostrophe in column comment must be escaped in ALTER COLUMN COMMENT; SQL: %s", sql)
}

// TestCreateTableChange_SQL_ApostropheInComment verifies escaping inside CREATE TABLE.
func TestCreateTableChange_SQL_ApostropheInComment(t *testing.T) {
	ch := &change.CreateTableChange{
		Base: change.Base{Table: change.TablePath{Catalog: "prod", Schema: litSilver, Name: litOrders}},
		Def: config.Table{
			Name:   litOrders,
			Schema: litSilver,
			Columns: []config.Column{
				{Name: "id", Type: "BIGINT", Comment: "user's id"},
			},
		},
	}
	sql := ch.SQL()
	// COMMENT 'user's id' is broken SQL.
	assert.NotContains(t, sql, "COMMENT 'user's",
		"apostrophe in column comment must be escaped inside CREATE TABLE; SQL: %s", sql)
}

// TestCreateTableChange_SQL_ApostropheInTableComment verifies escaping in table-level comment.
func TestCreateTableChange_SQL_ApostropheInTableComment(t *testing.T) {
	ch := &change.CreateTableChange{
		Base: change.Base{Table: change.TablePath{Catalog: "prod", Schema: "silver", Name: "orders"}},
		Def: config.Table{
			Name:    "orders",
			Schema:  "silver",
			Comment: "O'Brien's orders",
		},
	}
	sql := ch.SQL()
	// COMMENT 'O'Brien's orders' is broken SQL.
	assert.NotContains(t, sql, "COMMENT 'O'Brien",
		"apostrophe in table comment must be escaped; SQL: %s", sql)
}
