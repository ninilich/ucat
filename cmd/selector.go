package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ninilich/ucat/internal/config"
	"github.com/spf13/cobra"
)

// addSelectorFlags binds --schema and --table to c (plan, apply). Both are repeatable; a value is
// one name, commas are not separators (a name with a comma is not a valid logical name anyway).
func (a *app) addSelectorFlags(c *cobra.Command) {
	c.Flags().StringArrayVar(&a.selSchemas, "schema", nil, "plan only the tables of this schema (logical name from the YAML, any case); repeat the flag for several schemas")
	c.Flags().StringArrayVar(&a.selTables, "table", nil, "plan only this table: <schema>.<table> or <table> (logical names, any case; a bare name selects it in every schema); repeat the flag for several tables")
}

// selector is the set of tables picked by --schema and --table. The zero value selects everything.
type selector struct {
	schemas []string // logical schema names as given
	tables  []string // "<schema>.<table>" or "<table>" as given
}

// active reports whether any selector was given.
func (s selector) active() bool { return len(s.schemas) > 0 || len(s.tables) > 0 }

// selectTables returns the tables picked by the selector, in their original order: a table is picked
// when its schema is in --schema or it matches a --table (the union of all selectors). Names are the
// logical names of the YAML, compared with config.SameName. The table is chosen by its name in the
// YAML, so a table with drop: true is picked like any other (its DropTableChange is planned), and a
// table with rename_from is picked by the new name. Live tables that the YAML does not mention are
// never planned, so there is nothing else to pick. A selector that matches nothing (or an empty one)
// is an error naming the available schemas and tables; nothing is read from UC for it.
func (s selector) selectTables(tables []config.Table) ([]config.Table, error) {
	if !s.active() {
		return tables, nil
	}
	var bad []string
	for _, name := range s.schemas {
		if !hasSchema(tables, name) {
			bad = append(bad, fmt.Sprintf("--schema %q", name))
		}
	}
	for _, name := range s.tables {
		if !hasTable(tables, name) {
			bad = append(bad, fmt.Sprintf("--table %q", name))
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("unknown selector: %s\n%s", strings.Join(bad, ", "), availableTables(tables))
	}

	var picked []config.Table
	for _, t := range tables {
		if s.matches(t) {
			picked = append(picked, t)
		}
	}
	return picked, nil
}

// matches reports whether the table is picked by any of the selectors.
func (s selector) matches(t config.Table) bool {
	for _, name := range s.schemas {
		if config.SameName(t.Schema, name) {
			return true
		}
	}
	for _, name := range s.tables {
		if tableMatches(t, name) {
			return true
		}
	}
	return false
}

func hasSchema(tables []config.Table, name string) bool {
	for _, t := range tables {
		if config.SameName(t.Schema, name) {
			return true
		}
	}
	return false
}

func hasTable(tables []config.Table, name string) bool {
	for _, t := range tables {
		if tableMatches(t, name) {
			return true
		}
	}
	return false
}

// tableMatches compares a table with "<schema>.<table>" or a bare "<table>". Logical names cannot
// contain a dot, so the first dot separates the schema.
func tableMatches(t config.Table, sel string) bool {
	if schema, name, ok := strings.Cut(sel, "."); ok {
		return config.SameName(t.Schema, schema) && config.SameName(t.Name, name)
	}
	return config.SameName(t.Name, sel)
}

// availableTables lists the schemas and the "<schema>.<table>" names a selector can name, sorted.
func availableTables(tables []config.Table) string {
	if len(tables) == 0 {
		return "the project has no tables"
	}
	schemaSet := map[string]bool{}
	var names []string
	for _, t := range tables {
		schemaSet[t.Schema] = true
		names = append(names, t.Schema+"."+t.Name)
	}
	schemas := make([]string, 0, len(schemaSet))
	for s := range schemaSet {
		schemas = append(schemas, s)
	}
	sort.Strings(schemas)
	sort.Strings(names)
	return fmt.Sprintf("available schemas: %s\navailable tables: %s", strings.Join(schemas, ", "), strings.Join(names, ", "))
}
