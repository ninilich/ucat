package change_test

// The Describe() and SQL() contracts of the Change types:
//   - RenameTableChange.SQL() uses the full qualified destination path;
//   - Describe() wraps column names in double quotes.
// SQL escaping is in sql_escaping_test.go.

import (
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
)

// --- RenameTableChange.SQL() full path ---

// TestRenameTableChange_SQL_FullQualifiedPath verifies that RENAME TO includes the
// full catalog.schema.name path, not just the bare table name.
// Databricks SQL syntax: ALTER TABLE cat.sch.old RENAME TO cat.sch.new
func TestRenameTableChange_SQL_FullQualifiedPath(t *testing.T) {
	ch := &change.RenameTableChange{
		OldPath: change.TablePath{Catalog: litCat, Schema: litSch, Name: "old"},
		NewName: "new",
	}
	sql := ch.SQL()
	assert.Contains(t, sql, "RENAME TO `cat`.`sch`.`new`",
		"RENAME TO must use full qualified path cat.sch.new, got: %s", sql)
}

// TestRenameTableChange_SQL_FullPath_DifferentNames verifies the correct full path with
// a realistic table name.
func TestRenameTableChange_SQL_FullPath_DifferentNames(t *testing.T) {
	ch := &change.RenameTableChange{
		OldPath: change.TablePath{Catalog: litProd, Schema: litSilver, Name: "orders_old"},
		NewName: "orders_v2",
	}
	sql := ch.SQL()
	assert.Contains(t, sql, "ALTER TABLE `prod`.`silver`.`orders_old`",
		"ALTER TABLE must reference the old fully-qualified path")
	assert.Contains(t, sql, "RENAME TO `prod`.`silver`.`orders_v2`",
		"RENAME TO must contain full qualified destination path")
}

// TestRenameTableChange_SQL_NotBareNewName verifies that the result does NOT contain
// a bare (unqualified) "RENAME TO <name>" pattern that omits catalog and schema.
func TestRenameTableChange_SQL_NotBareNewName(t *testing.T) {
	ch := &change.RenameTableChange{
		OldPath: change.TablePath{Catalog: "cat", Schema: "sch", Name: "old"},
		NewName: "new",
	}
	sql := ch.SQL()
	// The string "RENAME TO new\n" or "RENAME TO new" at end-of-string would indicate
	// a bare name without catalog/schema.
	// "RENAME TO cat.sch.new" is correct and will also contain "new", so we check
	// that the path is fully qualified.
	assert.Contains(t, sql, "`cat`.`sch`.`new`",
		"destination must be fully qualified; got: %s", sql)
}

// --- Describe() column name quoting ---

// TestAddColumnChange_Describe_ColumnNameInQuotes verifies that the column name appears
// in double quotes in the Describe() output.
func TestAddColumnChange_Describe_ColumnNameInQuotes(t *testing.T) {
	ch := &change.AddColumnChange{
		Base:   change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		Column: config.Column{Name: "my_col", Type: typSTRING},
	}
	desc := ch.Describe()
	assert.Contains(t, desc, `"my_col"`,
		"AddColumnChange.Describe() must wrap column name in double quotes; got: %q", desc)
}

// TestDropColumnChange_Describe_ColumnNameInQuotes verifies the same for DropColumnChange.
func TestDropColumnChange_Describe_ColumnNameInQuotes(t *testing.T) {
	ch := &change.DropColumnChange{
		Base:       change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		ColumnName: "stale_col",
	}
	desc := ch.Describe()
	assert.Contains(t, desc, `"stale_col"`,
		"DropColumnChange.Describe() must wrap column name in double quotes; got: %q", desc)
}

// TestRenameColumnChange_Describe_BothNamesInQuotes verifies that both the old and new
// column names appear in double quotes in the Describe() output.
func TestRenameColumnChange_Describe_BothNamesInQuotes(t *testing.T) {
	ch := &change.RenameColumnChange{
		Base:    change.Base{Table: change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}},
		OldName: "user_name",
		NewName: "full_name",
	}
	desc := ch.Describe()
	assert.Contains(t, desc, `"user_name"`,
		"RenameColumnChange.Describe() must wrap old name in double quotes; got: %q", desc)
	assert.Contains(t, desc, `"full_name"`,
		"RenameColumnChange.Describe() must wrap new name in double quotes; got: %q", desc)
}

// --- compile-time checks ---

var _ interface{} = (*change.RenameTableChange)(nil)

// TestRenameTableChange_SQL_ContainsAlterTable is a basic sanity check that SQL output
// contains the required ALTER TABLE keyword.
func TestRenameTableChange_SQL_ContainsAlterTable(t *testing.T) {
	ch := &change.RenameTableChange{
		OldPath: change.TablePath{Catalog: "a", Schema: "b", Name: "c"},
		NewName: "d",
	}
	sql := ch.SQL()
	assert.True(t,
		strings.Contains(sql, "ALTER TABLE") && strings.Contains(sql, "RENAME TO"),
		"SQL must contain ALTER TABLE...RENAME TO; got: %s", sql)
}

// column count in CreateTableChange.Describe() is pluralised.
func TestCreateTableChange_Describe_ColumnPlural(t *testing.T) {
	path := change.TablePath{Catalog: litProd, Schema: litSilver, Name: "t"}
	for n, want := range map[int]string{0: "(0 columns)", 1: "(1 column)", 2: "(2 columns)", 17: "(17 columns)"} {
		t.Run(want, func(t *testing.T) {
			cols := make([]config.Column, n)
			for i := range cols {
				cols[i] = config.Column{Name: "c", Type: typINT}
			}
			c := &change.CreateTableChange{Base: change.Base{Table: path}, Def: config.Table{Columns: cols}}
			assert.Equal(t, "create table `prod`.`silver`.`t` "+want, c.Describe())
		})
	}
}

// AlterClusteringChange: table name and the three plan texts (columns, auto, none).
func TestAlterClusteringChange_TableNameAndDescribe(t *testing.T) {
	tbl := change.TablePath{Catalog: litProd, Schema: litSilver, Name: litOrders}
	cases := []struct {
		name string
		c    *config.Clustering
		want string
	}{
		{"columns", &config.Clustering{Columns: []string{"a", "b"}}, "set clustering by (a, b)"},
		{"auto", &config.Clustering{Auto: true}, "set clustering auto"},
		{"none", nil, "drop clustering"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := &change.AlterClusteringChange{Base: change.Base{Table: tbl}, Clustering: tc.c}
			assert.Equal(t, "`prod`.`silver`.`orders`", ch.TableName())
			assert.Equal(t, tc.want, ch.Describe())
			assert.False(t, ch.IsDestructive())
		})
	}
}

// AlterNullable carries typed flags: OldValue/NewValue are not used for it.
func TestAlterColumnChange_nullable(t *testing.T) {
	p := change.TablePath{Catalog: "c", Schema: "s", Name: "t"}
	set := &change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: "a", Kind: change.AlterNullable, OldNullable: true, NewNullable: false}
	assert.Equal(t, `alter column "a" nullable true → false`, set.Describe())
	assert.Equal(t, "ALTER TABLE `c`.`s`.`t` ALTER COLUMN `a` SET NOT NULL", set.SQL())
	drop := &change.AlterColumnChange{Base: change.Base{Table: p}, ColumnName: "a", Kind: change.AlterNullable, OldNullable: false, NewNullable: true}
	assert.Equal(t, `alter column "a" nullable false → true`, drop.Describe())
	assert.Equal(t, "ALTER TABLE `c`.`s`.`t` ALTER COLUMN `a` DROP NOT NULL", drop.SQL())
}
