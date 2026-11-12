package change_test

// Byte-for-byte golden SQL for the Change types whose SQL() code was touched by the lint cleanup (QF1012):
// CreateTableChange and neighbours. The expectations were verified equal on HEAD's types.go
// (git show HEAD:internal/differ/types.go) and on the current code.

import (
	"testing"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestChangeSQL_goldenCreateAndAlterVariants(t *testing.T) {
	p := change.TablePath{Catalog: litCat, Schema: litSch, Name: "tbl"}
	no := false
	yes := true
	v1, v2 := "1", litItS
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"create minimal", (&change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{
			Columns: []config.Column{{Name: "id", Type: typBIGINT}}}}).SQL(),
			"CREATE TABLE `cat`.`sch`.`tbl` (\n  `id` BIGINT\n)"},
		{"create full", (&change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{
			Comment: "it's a table",
			Columns: []config.Column{
				{Name: "id", Type: typBIGINT, Nullable: &no, Comment: "pk"},
				{Name: litOrder, Type: typSTRING, Nullable: &yes},
				{Name: "d", Type: "DATE", Comment: litItS},
			},
			Clustering:    &config.Clustering{Columns: []string{"id", "order"}},
			Partitioning:  &config.Partitioning{Columns: []string{"d"}},
			TblProperties: map[string]*string{"b": &v2, "a": &v1, "skipped": nil},
		}}).SQL(),
			"CREATE TABLE `cat`.`sch`.`tbl` (\n  `id` BIGINT NOT NULL COMMENT 'pk',\n  `order` STRING,\n  `d` DATE COMMENT 'it\\'s'\n)" +
				"\nCOMMENT 'it\\'s a table'\nCLUSTER BY (`id`, `order`)\nPARTITIONED BY (`d`)\nTBLPROPERTIES ('a'='1', 'b'='it\\'s')"},
		{"create auto clustering", (&change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{
			Columns:    []config.Column{{Name: "id", Type: typINT}},
			Clustering: &config.Clustering{Auto: true}}}).SQL(),
			"CREATE TABLE `cat`.`sch`.`tbl` (\n  `id` INT\n)\nCLUSTER BY AUTO"},
		{"create only nil props", (&change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{
			Columns:       []config.Column{{Name: "id", Type: "INT"}},
			TblProperties: map[string]*string{"x": nil}}}).SQL(),
			"CREATE TABLE `cat`.`sch`.`tbl` (\n  `id` INT\n)"},
		{"alter clustering columns", (&change.AlterClusteringChange{Base: change.Base{Table: p}, Clustering: &config.Clustering{Columns: []string{"a", "b"}}}).SQL(),
			"ALTER TABLE `cat`.`sch`.`tbl` CLUSTER BY (`a`, `b`)"},
		{"alter clustering auto", (&change.AlterClusteringChange{Base: change.Base{Table: p}, Clustering: &config.Clustering{Auto: true}}).SQL(),
			"ALTER TABLE `cat`.`sch`.`tbl` CLUSTER BY AUTO"},
		{"alter clustering none", (&change.AlterClusteringChange{Base: change.Base{Table: p}}).SQL(),
			"ALTER TABLE `cat`.`sch`.`tbl` CLUSTER BY NONE"},
		{"add column with comment", (&change.AddColumnChange{Base: change.Base{Table: p}, Column: config.Column{Name: "c", Type: "INT", Comment: litItS}}).SQL(),
			"ALTER TABLE `cat`.`sch`.`tbl` ADD COLUMN `c` INT COMMENT 'it\\'s'"},
		{"set tblproperties", (&change.SetTBLPropertiesChange{Base: change.Base{Table: p}, Properties: map[string]string{"b": litItS, "a": "1"}}).SQL(),
			"ALTER TABLE `cat`.`sch`.`tbl` SET TBLPROPERTIES ('a'='1', 'b'='it\\'s')"},
		{"unset tblproperties", (&change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}, Keys: []string{"b", "a"}}).SQL(),
			"ALTER TABLE `cat`.`sch`.`tbl` UNSET TBLPROPERTIES ('a', 'b')"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, tc.sql) })
	}
}

// Empty blocks and nil values add no clause to CREATE TABLE (no "CLUSTER BY ()", no
// "PARTITIONED BY ()", no "TBLPROPERTIES ()"), and CLUSTER BY AUTO has its own form.
func TestCreateTableSQL_emptyClausesAreOmitted(t *testing.T) {
	p := change.TablePath{Catalog: "c", Schema: "s", Name: "t"}
	base := "CREATE TABLE `c`.`s`.`t` (\n  `id` BIGINT\n)"
	cols := []config.Column{{Name: "id", Type: typBIGINT}}
	create := func(d config.Table) string {
		d.Columns = cols
		return (&change.CreateTableChange{Base: change.Base{Table: p}, Def: d}).SQL()
	}
	assert.Equal(t, base, create(config.Table{Clustering: &config.Clustering{}}))
	assert.Equal(t, base, create(config.Table{Clustering: &config.Clustering{Columns: []string{}}}))
	assert.Equal(t, base, create(config.Table{Partitioning: &config.Partitioning{}}))
	assert.Equal(t, base, create(config.Table{TblProperties: map[string]*string{"gone": nil}}))
	assert.Equal(t, base+"\nCLUSTER BY AUTO", create(config.Table{Clustering: &config.Clustering{Auto: true}}))
}
