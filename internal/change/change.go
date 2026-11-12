// Package change is the model of a planned modification to a Unity Catalog table: the Kind
// kinds, the Change interface and one implementation per kind. Every implementation renders its own
// Databricks SQL and plan text. The package depends only on config; differ creates the changes,
// planner orders them, preflight checks them, backend and executor run them, output prints them.
package change

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ninilich/ucat/internal/config"
)

// sqlStrReplacer escapes backslash and single quote in one pass.
var sqlStrReplacer = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

// sqlStr escapes a string value for use inside single-quoted SQL literals.
// Databricks SQL treats backslash as an escape character; the legacy
// spark.sql.parser.escapedStringLiterals=true changes this, the risk is accepted.
func sqlStr(s string) string {
	return sqlStrReplacer.Replace(s)
}

// sqlIdent wraps an identifier in backticks, escaping any embedded backticks.
func sqlIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// sqlIdentList quotes every column name with sqlIdent and joins them with ", "
// (the body of CLUSTER BY (...) and PARTITIONED BY (...)). An empty list gives "".
func sqlIdentList(cols []string) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = sqlIdent(col)
	}
	return strings.Join(parts, ", ")
}

// sqlPropList renders properties as 'k'='v' pairs sorted by key and joined with ", "
// (the body of TBLPROPERTIES (...)); keys and values go through sqlStr. An empty map gives "".
func sqlPropList(props map[string]string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("'%s'='%s'", sqlStr(k), sqlStr(props[k]))
	}
	return strings.Join(parts, ", ")
}

// sqlKeyList renders property keys as 'k' literals sorted and joined with ", "
// (the body of UNSET TBLPROPERTIES (...)). The input slice is not modified.
func sqlKeyList(keys []string) string {
	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)
	parts := make([]string, len(sorted))
	for i, k := range sorted {
		parts[i] = fmt.Sprintf("'%s'", sqlStr(k))
	}
	return strings.Join(parts, ", ")
}

// Kind identifies the kind of change a Change object represents.
type Kind int

// The kinds of change, one per Change implementation.
const (
	AddColumn Kind = iota
	DropColumn
	AlterColumn
	RenameColumn
	RenameTable
	SetTBLProperties
	UnsetTBLProperties
	AlterClustering
	DropTable
	CreateTable
	AlterTableComment // set or unset a table-level comment

	// KindCount is the number of Kind values. Keep it last: add new
	// Kinds before it. Tests iterate up to it to pin the per-type data: Op and the
	// sample table here, planner.orderGroup, the destructive flag (Base default plus overrides).
	KindCount
)

// opNames are the machine-readable operation names of the JSON output, one per Kind.
var opNames = map[Kind]string{
	AddColumn:          "add_column",
	DropColumn:         "drop_column",
	AlterColumn:        "alter_column",
	RenameColumn:       "rename_column",
	RenameTable:        "rename_table",
	SetTBLProperties:   "set_tbl_properties",
	UnsetTBLProperties: "unset_tbl_properties",
	AlterClustering:    "alter_clustering",
	AlterTableComment:  "alter_table_comment",
	DropTable:          "drop_table",
	CreateTable:        "create_table",
}

// Op returns the operation name used in the JSON output ("add_column", "drop_table", ...).
// It panics for a value without a name: a new Kind must be added to opNames.
func (t Kind) Op() string {
	op, ok := opNames[t]
	if !ok {
		panic(fmt.Sprintf("change: no op name for change type %d: add it to opNames", int(t)))
	}
	return op
}

// Change represents a single planned modification to a Unity Catalog object.
type Change interface {
	Type() Kind
	Describe() string // human-readable, used for plan output
	SQL() string      // exact DDL statement; empty string for REST-only changes
	IsDestructive() bool
	// TableName is the SQL-quoted full name of the table the change applies to, e.g.
	// "`catalog`.`schema`.`table`". For RenameTableChange it is the old (current) name.
	TableName() string
	// GroupKey is the table a change is grouped under in the plan output: TableName(), except
	// that a table rename is keyed by the new name so it shares a header with later changes
	// of the same table.
	GroupKey() string
	// Detail says what the change is about, for the machine-readable output.
	Detail() Detail
}

// Detail is the structured subject of a change (the --json fields catalog, schema, table, column,
// previous_name and alter). Table is the table the change belongs to: its new name for a table
// rename, so all changes of one table agree on it.
type Detail struct {
	Table        TablePath
	Column       string // column the change is about (the new name for a column rename)
	PreviousName string // the old name of a renamed table or column
	Alter        string // AlterKind.Name of an alter_column change
}

// TablePath holds the fully-qualified UC path for a table.
type TablePath struct {
	Catalog string
	Schema  string
	Name    string
}

// FullName returns "`catalog`.`schema`.`name`" with proper SQL identifier escaping.
func (p TablePath) FullName() string {
	return sqlIdent(p.Catalog) + "." + sqlIdent(p.Schema) + "." + sqlIdent(p.Name)
}

// Label names the table in messages: "<schema>.<table>". The schema is the UC schema, the
// table is its (new) name from YAML. The catalog is not included by decision: the same
// schema.table in different catalogs in one plan is indistinguishable.
func (p TablePath) Label() string {
	return p.Schema + "." + p.Name
}

// Subject is the table reference at the start of a diagnostic: `table "<schema>.<table>"`, built from
// Label. Differ and preflight use it for every message about a table, so one table reads the same
// everywhere. (The validator names the table by its YAML name inside the file Location, and drop prints
// the full catalog.schema.table because the operation is destructive.)
func (p TablePath) Subject() string {
	return fmt.Sprintf("table %q", p.Label())
}

// Base is embedded by every change that targets one table. It provides TableName, GroupKey and
// IsDestructive (false); a type overrides only what differs: DropTableChange and DropColumnChange are
// destructive, RenameTableChange names two tables and so does not embed Base.
type Base struct {
	Table TablePath
}

// TableName is the SQL-quoted full name of the table.
func (b Base) TableName() string { return b.Table.FullName() }

// GroupKey is the table the change is grouped under in the plan: the same as TableName.
func (b Base) GroupKey() string { return b.TableName() }

// Detail is the table of the change; a type with a column or a previous name overrides it.
func (b Base) Detail() Detail { return Detail{Table: b.Table} }

// IsDestructive is false by default. A new destructive type must override it (pinned per type by
// the tests over KindCount in this package and in output).
func (b Base) IsDestructive() bool { return false }

// AlterKind discriminates the subtype of an AlterColumnChange.
type AlterKind int

// The subtypes of an AlterColumnChange.
const (
	AlterType     AlterKind = iota // column type changed
	AlterComment                   // column comment changed
	AlterNullable                  // column nullable changed
)

// Name is the machine-readable name of the subtype: "type", "comment" or "nullable".
// It panics for an unknown value, like Kind.Op.
func (k AlterKind) Name() string {
	switch k {
	case AlterType:
		return "type"
	case AlterComment:
		return "comment"
	case AlterNullable:
		return "nullable"
	default:
		panic(fmt.Sprintf("change: no name for alter kind %d", int(k)))
	}
}

// AddColumnChange adds a new column to an existing table.
type AddColumnChange struct {
	Base
	Column config.Column
}

// Type is the Kind of the change.
func (c *AddColumnChange) Type() Kind { return AddColumn }

// Detail is the table and the column of the change.
func (c *AddColumnChange) Detail() Detail {
	return Detail{Table: c.Table, Column: c.Column.Name}
}

// Describe renders the change for the plan output.
func (c *AddColumnChange) Describe() string {
	return fmt.Sprintf("add column %q %s", c.Column.Name, c.Column.Type)
}

// SQL is the DDL statement that applies the change.
func (c *AddColumnChange) SQL() string {
	s := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.Table.FullName(), sqlIdent(c.Column.Name), c.Column.Type)
	if c.Column.Comment != "" {
		s += fmt.Sprintf(" COMMENT '%s'", sqlStr(c.Column.Comment))
	}
	return s
}

// DropColumnChange removes a column from an existing table.
type DropColumnChange struct {
	Base
	ColumnName string
}

// Type is the Kind of the change.
func (c *DropColumnChange) Type() Kind { return DropColumn }

// Detail is the table and the column of the change.
func (c *DropColumnChange) Detail() Detail {
	return Detail{Table: c.Table, Column: c.ColumnName}
}

// IsDestructive reports whether the change loses data.
func (c *DropColumnChange) IsDestructive() bool { return true }

// Describe renders the change for the plan output.
func (c *DropColumnChange) Describe() string {
	return fmt.Sprintf("drop column %q", c.ColumnName)
}

// SQL is the DDL statement that applies the change.
func (c *DropColumnChange) SQL() string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", c.Table.FullName(), sqlIdent(c.ColumnName))
}

// AlterColumnChange modifies an existing column's type, comment, or nullability.
type AlterColumnChange struct {
	Base
	ColumnName string
	Kind       AlterKind
	// OldValue and NewValue hold the column type (AlterType) or comment (AlterComment);
	// they are unused for AlterNullable.
	OldValue string
	NewValue string
	// OldNullable and NewNullable are used by AlterNullable only.
	OldNullable bool
	NewNullable bool
}

// Type is the Kind of the change.
func (c *AlterColumnChange) Type() Kind { return AlterColumn }

// Detail is the table and the column of the change.
func (c *AlterColumnChange) Detail() Detail {
	return Detail{Table: c.Table, Column: c.ColumnName, Alter: c.Kind.Name()}
}

// Describe renders the change for the plan output.
func (c *AlterColumnChange) Describe() string {
	switch c.Kind {
	case AlterType:
		return fmt.Sprintf("alter column %q type %s → %s", c.ColumnName, c.OldValue, c.NewValue)
	case AlterComment:
		return fmt.Sprintf("alter column %q comment → %q", c.ColumnName, c.NewValue)
	case AlterNullable:
		return fmt.Sprintf("alter column %q nullable %t → %t", c.ColumnName, c.OldNullable, c.NewNullable)
	default:
		panic(fmt.Sprintf("change: no description for alter kind %d", int(c.Kind)))
	}
}

// SQL is the DDL statement that applies the change.
func (c *AlterColumnChange) SQL() string {
	switch c.Kind {
	case AlterType:
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", c.Table.FullName(), sqlIdent(c.ColumnName), c.NewValue)
	case AlterComment:
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s COMMENT '%s'", c.Table.FullName(), sqlIdent(c.ColumnName), sqlStr(c.NewValue))
	case AlterNullable:
		if !c.NewNullable {
			return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", c.Table.FullName(), sqlIdent(c.ColumnName))
		}
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", c.Table.FullName(), sqlIdent(c.ColumnName))
	default:
		panic(fmt.Sprintf("change: no SQL for alter kind %d", int(c.Kind)))
	}
}

// RenameColumnChange renames a column in place.
type RenameColumnChange struct {
	Base
	OldName string
	NewName string
}

// Type is the Kind of the change.
func (c *RenameColumnChange) Type() Kind { return RenameColumn }

// Detail is the table and the column of the change.
func (c *RenameColumnChange) Detail() Detail {
	return Detail{Table: c.Table, Column: c.NewName, PreviousName: c.OldName}
}

// Describe renders the change for the plan output.
func (c *RenameColumnChange) Describe() string {
	return fmt.Sprintf("rename column %q → %q", c.OldName, c.NewName)
}

// SQL is the DDL statement that applies the change.
func (c *RenameColumnChange) SQL() string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", c.Table.FullName(), sqlIdent(c.OldName), sqlIdent(c.NewName))
}

// RenameTableChange renames a table within the same schema.
type RenameTableChange struct {
	OldPath TablePath
	NewName string
}

// Type is the Kind of the change.
func (c *RenameTableChange) Type() Kind { return RenameTable }

// Detail is the table under its new name and the old name as the previous one.
func (c *RenameTableChange) Detail() Detail {
	return Detail{Table: TablePath{Catalog: c.OldPath.Catalog, Schema: c.OldPath.Schema, Name: c.NewName}, PreviousName: c.OldPath.Name}
}

// IsDestructive reports whether the change loses data.
func (c *RenameTableChange) IsDestructive() bool { return false }

// TableName is the SQL-quoted name of the table the change applies to.
func (c *RenameTableChange) TableName() string { return c.OldPath.FullName() }

// GroupKey is the table the change is grouped under in the plan.
func (c *RenameTableChange) GroupKey() string {
	return TablePath{Catalog: c.OldPath.Catalog, Schema: c.OldPath.Schema, Name: c.NewName}.FullName()
}

// Describe renders the change for the plan output.
func (c *RenameTableChange) Describe() string {
	return fmt.Sprintf("rename table %s → %s", c.OldPath.Name, c.NewName)
}

// SQL is the DDL statement that applies the change.
func (c *RenameTableChange) SQL() string {
	newFullName := sqlIdent(c.OldPath.Catalog) + "." + sqlIdent(c.OldPath.Schema) + "." + sqlIdent(c.NewName)
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", c.OldPath.FullName(), newFullName)
}

// SetTBLPropertiesChange sets one or more TBLPROPERTIES on a table.
type SetTBLPropertiesChange struct {
	Base
	Properties map[string]string
}

// Type is the Kind of the change.
func (c *SetTBLPropertiesChange) Type() Kind { return SetTBLProperties }

// Describe renders the change for the plan output.
func (c *SetTBLPropertiesChange) Describe() string {
	return fmt.Sprintf("set %d tblproperties", len(c.Properties))
}

// SQL is the DDL statement that applies the change.
func (c *SetTBLPropertiesChange) SQL() string {
	return fmt.Sprintf("ALTER TABLE %s SET TBLPROPERTIES (%s)", c.Table.FullName(), sqlPropList(c.Properties))
}

// UnsetTBLPropertiesChange removes one or more TBLPROPERTIES from a table.
type UnsetTBLPropertiesChange struct {
	Base
	Keys []string
}

// Type is the Kind of the change.
func (c *UnsetTBLPropertiesChange) Type() Kind { return UnsetTBLProperties }

// Describe renders the change for the plan output.
func (c *UnsetTBLPropertiesChange) Describe() string {
	return fmt.Sprintf("unset %d tblproperties", len(c.Keys))
}

// SQL is the DDL statement that applies the change.
func (c *UnsetTBLPropertiesChange) SQL() string {
	return fmt.Sprintf("ALTER TABLE %s UNSET TBLPROPERTIES (%s)", c.Table.FullName(), sqlKeyList(c.Keys))
}

// AlterClusteringChange changes the clustering configuration of a table.
// Clustering == nil means drop clustering (CLUSTER BY NONE).
type AlterClusteringChange struct {
	Base
	Clustering *config.Clustering
}

// Type is the Kind of the change.
func (c *AlterClusteringChange) Type() Kind { return AlterClustering }

// Describe renders the change for the plan output.
func (c *AlterClusteringChange) Describe() string {
	if c.Clustering == nil {
		return "drop clustering"
	}
	if c.Clustering.Auto {
		return "set clustering auto"
	}
	return fmt.Sprintf("set clustering by (%s)", strings.Join(c.Clustering.Columns, ", "))
}

// SQL is the DDL statement that applies the change.
func (c *AlterClusteringChange) SQL() string {
	if c.Clustering == nil {
		return fmt.Sprintf("ALTER TABLE %s CLUSTER BY NONE", c.Table.FullName())
	}
	if c.Clustering.Auto {
		return fmt.Sprintf("ALTER TABLE %s CLUSTER BY AUTO", c.Table.FullName())
	}
	return fmt.Sprintf("ALTER TABLE %s CLUSTER BY (%s)", c.Table.FullName(), sqlIdentList(c.Clustering.Columns))
}

// AlterTableCommentChange sets or unsets a table-level comment.
// Comment == "" means unset (NULL).
type AlterTableCommentChange struct {
	Base
	Comment string
}

// Type is the Kind of the change.
func (c *AlterTableCommentChange) Type() Kind { return AlterTableComment }

// Describe renders the change for the plan output.
func (c *AlterTableCommentChange) Describe() string {
	if c.Comment == "" {
		return "unset comment"
	}
	return fmt.Sprintf("set comment %q", c.Comment)
}

// SQL is the DDL statement that applies the change.
func (c *AlterTableCommentChange) SQL() string {
	if c.Comment == "" {
		return fmt.Sprintf("COMMENT ON TABLE %s IS NULL", c.Table.FullName())
	}
	return fmt.Sprintf("COMMENT ON TABLE %s IS '%s'", c.Table.FullName(), sqlStr(c.Comment))
}

// DropTableChange drops a table from UC.
type DropTableChange struct {
	Base
}

// Type is the Kind of the change.
func (c *DropTableChange) Type() Kind { return DropTable }

// IsDestructive reports whether the change loses data.
func (c *DropTableChange) IsDestructive() bool { return true }

// Describe renders the change for the plan output.
func (c *DropTableChange) Describe() string {
	return fmt.Sprintf("drop table %s", c.Table.FullName())
}

// SQL is the DDL statement that applies the change.
func (c *DropTableChange) SQL() string { return fmt.Sprintf("DROP TABLE %s", c.Table.FullName()) }

// CreateTableChange creates a new table in UC.
type CreateTableChange struct {
	Base
	Def config.Table
}

// Type is the Kind of the change.
func (c *CreateTableChange) Type() Kind { return CreateTable }

// Describe renders the change for the plan output.
func (c *CreateTableChange) Describe() string {
	n := len(c.Def.Columns)
	word := "columns"
	if n == 1 {
		word = "column"
	}
	return fmt.Sprintf("create table %s (%d %s)", c.Table.FullName(), n, word)
}

// SQL is the DDL statement that applies the change.
func (c *CreateTableChange) SQL() string {
	defs := make([]string, len(c.Def.Columns))
	for i, col := range c.Def.Columns {
		defs[i] = "  " + columnDef(col)
	}
	body := strings.Join(defs, ",\n")
	if body != "" {
		body += "\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE %s (\n%s)", c.Table.FullName(), body)
	if c.Def.Comment != "" {
		fmt.Fprintf(&sb, "\nCOMMENT '%s'", sqlStr(c.Def.Comment))
	}
	sb.WriteString(clusterClause(c.Def.Clustering))
	sb.WriteString(partitionClause(c.Def.Partitioning))
	sb.WriteString(tblPropsClause(c.Def.TblProperties))
	return sb.String()
}

// columnDef is the definition of one column in CREATE TABLE: name, type, NOT NULL, COMMENT.
func columnDef(col config.Column) string {
	def := fmt.Sprintf("%s %s", sqlIdent(col.Name), col.Type)
	if !col.IsNullable() {
		def += " NOT NULL"
	}
	if col.Comment != "" {
		def += fmt.Sprintf(" COMMENT '%s'", sqlStr(col.Comment))
	}
	return def
}

// clusterClause is the CLUSTER BY line of CREATE TABLE (with its leading newline), or "".
func clusterClause(c *config.Clustering) string {
	switch {
	case c == nil:
		return ""
	case c.Auto:
		return "\nCLUSTER BY AUTO"
	case len(c.Columns) > 0:
		return fmt.Sprintf("\nCLUSTER BY (%s)", sqlIdentList(c.Columns))
	}
	return ""
}

// partitionClause is the PARTITIONED BY line of CREATE TABLE (with its leading newline), or "".
func partitionClause(p *config.Partitioning) string {
	if p == nil || len(p.Columns) == 0 {
		return ""
	}
	return fmt.Sprintf("\nPARTITIONED BY (%s)", sqlIdentList(p.Columns))
}

// tblPropsClause is the TBLPROPERTIES line of CREATE TABLE (with its leading newline), or "".
// A nil value means "unset" and has nothing to set on a new table, so it is skipped.
func tblPropsClause(tblProps map[string]*string) string {
	props := make(map[string]string, len(tblProps))
	for k, v := range tblProps {
		if v != nil {
			props[k] = *v
		}
	}
	if len(props) == 0 {
		return ""
	}
	return fmt.Sprintf("\nTBLPROPERTIES (%s)", sqlPropList(props))
}
