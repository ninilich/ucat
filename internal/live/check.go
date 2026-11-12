package live

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
)

// Result is the outcome of one live check: a one-line description and the diagnostics
// the check produced (for a schema: those of the diff against the live tables).
type Result struct {
	OK    bool
	Name  string // what was checked, e.g. "schema prod.silver" or "warehouse wh1": the final error names it
	Line  string // e.g. "schema prod.silver exists"; on failure it carries the reason
	Diags diag.Diagnostics
}

// Failed reports whether the check failed: its own failure or an error diagnostic.
func (r Result) Failed() bool { return !r.OK || r.Diags.HasError() }

// CheckLive runs the live checks of "validate -t" in order: every schema of the project
// must be listable (so its catalog and schema exist) and its live tables must diff without
// errors; then, when a warehouse is configured, it must be reachable. A failed check does
// not stop the next ones. Without warehouse_id there is no warehouse result. The schemas are
// checked concurrently (at most MaxParallel at a time); the results keep the order of
// resolved.Schemas, the warehouse result comes last.
func CheckLive(ctx context.Context, b backend.UCBackend, resolved config.ResolvedConfig) []Result {
	desired, _ := GroupBySchema(resolved.Tables)
	results := make([]Result, len(resolved.Schemas))

	var g errgroup.Group // no WithContext: one failed schema must not stop the other checks
	g.SetLimit(MaxParallel)
	for i, rs := range resolved.Schemas {
		g.Go(func() error {
			results[i] = checkSchema(ctx, b, desired, backend.SchemaRef{UCCatalog: rs.UCCatalog, UCSchema: rs.UCSchema})
			return nil
		})
	}
	_ = g.Wait() // the functions never fail: a failed check is a Result

	if id := resolved.WarehouseID; id != "" {
		if err := b.CheckWarehouse(ctx); err != nil {
			results = append(results, Result{Name: "warehouse " + id, Line: fmt.Sprintf("warehouse %s: %v", id, err)})
		} else {
			results = append(results, Result{OK: true, Name: "warehouse " + id, Line: fmt.Sprintf("warehouse %s reachable", id)})
		}
	}
	return results
}

// checkSchema lists the live tables of one schema and diffs them with the desired ones.
func checkSchema(ctx context.Context, b backend.UCBackend, desired map[backend.SchemaRef][]config.Table, ref backend.SchemaRef) Result {
	name := fmt.Sprintf("schema %s.%s", ref.UCCatalog, ref.UCSchema)
	liveTables, err := b.ListTables(ctx, ref)
	if err != nil {
		return Result{Name: name, Line: fmt.Sprintf("schema %s.%s: %v", ref.UCCatalog, ref.UCSchema, err)}
	}
	// Only diagnostics are used (rename_from, drop warnings, conflicts); changes are discarded.
	_, diags := differ.DiffSchema(desired[ref], liveTables)
	return Result{OK: true, Name: name, Line: name + " exists", Diags: diags}
}
