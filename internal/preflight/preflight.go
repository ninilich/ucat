// Package preflight runs checks over a plan before apply and reports problems
// the backend would otherwise reject (for example missing column mapping).
package preflight

import (
	"context"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/diag"
)

// Checker is a single pre-execution validation that inspects a planned change.
type Checker interface {
	Applies(ch change.Change) bool
	Check(ctx context.Context, ch change.Change, b backend.UCBackend) diag.Diagnostics
}

// Run executes all applicable checkers against every planned change and returns diagnostics.
// live is the live state the plan was made from (the result of backend ListTables, schema by schema):
// the table properties of its tables are taken from there and cost no backend call. Only a table that
// is not in live is read from the backend (GetTableProperties), once per Run however many changes
// (and checkers) ask for it.
func Run(ctx context.Context, changes []change.Change, b backend.UCBackend, checkers []Checker, live ...[]*backend.LiveTable) diag.Diagnostics {
	if b != nil {
		b = newPropsCache(b, live)
	}
	var diags diag.Diagnostics
	for _, ch := range changes {
		for _, checker := range checkers {
			if checker.Applies(ch) {
				diags = append(diags, checker.Check(ctx, ch, b)...)
			}
		}
	}
	return diags
}
