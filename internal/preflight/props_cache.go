package preflight

import (
	"context"
	"maps"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
)

// propsResult is one remembered GetTableProperties answer, an error included.
type propsResult struct {
	props map[string]string
	err   error
}

// propsCache is the backend a preflight Run hands to its checkers: it answers GetTableProperties
// from the live tables the plan was made from, then from memory after the first read of a table;
// everything else goes to the wrapped backend. Preflight runs right after the live state was read,
// so the properties cannot change during a Run.
type propsCache struct {
	backend.UCBackend
	known map[string]map[string]string // properties of the live tables, by tableKey
	cache map[string]propsResult
}

// newPropsCache builds the cache over b with the properties of the live tables.
func newPropsCache(b backend.UCBackend, live [][]*backend.LiveTable) *propsCache {
	c := &propsCache{UCBackend: b, known: map[string]map[string]string{}, cache: map[string]propsResult{}}
	for _, tables := range live {
		for _, t := range tables {
			c.known[tableKey(t.Catalog, t.Schema, t.Name)] = t.Properties
		}
	}
	return c
}

// tableKey identifies a table by its three names. UC compares names without case, so the key does too.
func tableKey(catalog, schema, table string) string {
	return config.FoldName(catalog) + "\x00" + config.FoldName(schema) + "\x00" + config.FoldName(table)
}

// GetTableProperties returns the properties of a live table from the live state (a copy: the caller
// may change it, the live table must not). A table that is not there is read once; repeated calls
// for the same table return the same result.
func (c *propsCache) GetTableProperties(ctx context.Context, catalog, schema, table string) (map[string]string, error) {
	key := tableKey(catalog, schema, table)
	if props, ok := c.known[key]; ok {
		return maps.Clone(props), nil
	}
	if r, ok := c.cache[key]; ok {
		return maps.Clone(r.props), r.err
	}
	props, err := c.UCBackend.GetTableProperties(ctx, catalog, schema, table)
	c.cache[key] = propsResult{props: maps.Clone(props), err: err}
	return props, err
}
