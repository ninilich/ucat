package preflight

import (
	"context"
	"fmt"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/diag"
)

// columnMappingOps names the column changes that need delta.columnMapping.mode=name and the word
// that blocks them in the error: "column rename ...", "column drop ...".
var columnMappingOps = map[change.Kind]string{
	change.RenameColumn: "rename",
	change.DropColumn:   "drop",
}

// ColumnMappingChecker verifies that column renames and drops are only attempted when
// delta.columnMapping.mode is set to "name" on the target table.
type ColumnMappingChecker struct {
	plan []change.Change
}

// Applies is true for column renames and column drops.
func (c *ColumnMappingChecker) Applies(ch change.Change) bool {
	_, ok := columnMappingOps[ch.Type()]
	return ok
}

// Check reports an error when the table has no column mapping. A change that Applies reports but
// is not one of the column change types is a programming error: a safety check must not skip it
// silently, so it panics.
func (c *ColumnMappingChecker) Check(ctx context.Context, ch change.Change, b backend.UCBackend) diag.Diagnostics {
	op, ok := columnMappingOps[ch.Type()]
	if ok {
		switch p := ch.(type) {
		case *change.RenameColumnChange:
			return requireColumnMapping(ctx, c.plan, p.Table, b, op, p.OldName)
		case *change.DropColumnChange:
			return requireColumnMapping(ctx, c.plan, p.Table, b, op, p.ColumnName)
		}
	}
	panic(fmt.Sprintf("preflight: ColumnMappingChecker cannot check %T (type %d): add it to columnMappingOps and Check", ch, int(ch.Type())))
}

// requireColumnMapping checks that delta.columnMapping.mode=name is set on table, either
// by a SetTBLPropertiesChange earlier in the same plan or in live UC. op ("rename" or "drop") and
// column name the blocked change in the error. Tables are named by change.TablePath.Label.
func requireColumnMapping(ctx context.Context, plan []change.Change, table change.TablePath, b backend.UCBackend, op, column string) diag.Diagnostics {
	livePath := table
	for _, planned := range plan {
		switch p := planned.(type) {
		case *change.SetTBLPropertiesChange:
			if p.Table.FullName() == table.FullName() && p.Properties["delta.columnMapping.mode"] == "name" {
				return nil
			}
		case *change.RenameTableChange:
			// Preflight runs before apply: a table renamed in this plan still lives under its old name.
			if p.OldPath.Catalog == table.Catalog && p.OldPath.Schema == table.Schema && p.NewName == table.Name {
				livePath = p.OldPath
			}
		}
	}

	var diags diag.Diagnostics
	props, err := b.GetTableProperties(ctx, livePath.Catalog, livePath.Schema, livePath.Name)
	if err != nil {
		// A renamed table is read under its old name: say so, the label stays the new one.
		name := table.Subject()
		if livePath != table {
			name += fmt.Sprintf(" (read as %q)", livePath.Label())
		}
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("%s: failed to get table properties: %s", name, err.Error()),
		})
		return diags
	}
	if props["delta.columnMapping.mode"] != "name" {
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary: fmt.Sprintf("%s: column %s %q requires delta.columnMapping.mode: name in tblproperties (irreversible protocol upgrade)",
				table.Subject(), op, column),
		})
	}
	return diags
}
