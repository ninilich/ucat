// Package live reads the current Unity Catalog state for the schemas of a project and
// checks it against the local definitions: grouping of the desired tables by UC schema,
// fetching the live tables, and the live part of "validate -t".
package live

import (
	"context"
	"fmt"
	"sort"

	"golang.org/x/sync/errgroup"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
)

// MaxParallel is the most requests to UC that run at the same time when schemas are read in
// parallel (FetchTables, CheckLive). A project can have hundreds of schemas: unlimited goroutines would
// open that many connections at once and run into the rate limits of the workspace. 8 keeps a project
// with a few schemas fully parallel and does not change the result, only the number in flight.
const MaxParallel = 8

// GroupBySchema groups the tables by the UC schema they resolve to and returns the groups
// with their schemas in sorted order (catalog, then schema). A fixed order keeps the plan
// deterministic regardless of the order the live tables are fetched in.
func GroupBySchema(tables []config.Table) (map[backend.SchemaRef][]config.Table, []backend.SchemaRef) {
	groups := make(map[backend.SchemaRef][]config.Table)
	for _, t := range tables {
		ref := backend.SchemaRef{UCCatalog: t.ResolvedCatalog, UCSchema: t.ResolvedSchema}
		groups[ref] = append(groups[ref], t)
	}
	refs := make([]backend.SchemaRef, 0, len(groups))
	for ref := range groups {
		refs = append(refs, ref)
	}
	SortRefs(refs)
	return groups, refs
}

// SortRefs sorts the schemas by UC catalog, then by UC schema.
func SortRefs(refs []backend.SchemaRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].UCCatalog != refs[j].UCCatalog {
			return refs[i].UCCatalog < refs[j].UCCatalog
		}
		return refs[i].UCSchema < refs[j].UCSchema
	})
}

// FetchTables lists the live tables of every schema; result[i] belongs to refs[i]. With
// parallel the schemas are listed concurrently (at most MaxParallel at a time), otherwise one after
// another; either way the first failure is returned and names the schema, and result keeps the order of refs. Listing a schema also proves that its
// catalog and schema exist.
func FetchTables(ctx context.Context, b backend.UCBackend, refs []backend.SchemaRef, parallel bool) ([][]*backend.LiveTable, error) {
	result := make([][]*backend.LiveTable, len(refs))
	list := func(ctx context.Context, i int) error {
		tables, err := b.ListTables(ctx, refs[i])
		if err != nil {
			return fmt.Errorf("list tables in %s.%s: %w", refs[i].UCCatalog, refs[i].UCSchema, err)
		}
		result[i] = tables
		return nil
	}
	if !parallel {
		for i := range refs {
			if err := list(ctx, i); err != nil {
				return nil, err
			}
		}
		return result, nil
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(MaxParallel)
	for i := range refs {
		g.Go(func() error { return list(gctx, i) })
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}
