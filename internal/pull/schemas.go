package pull

import (
	"fmt"

	"github.com/ninilich/ucat/internal/config"
)

// FilterSchemas keeps the schemas of the logical catalog (and schema, if set); empty catalog keeps all.
// A catalog or schema that matches nothing is an error.
func FilterSchemas(all []config.ResolvedSchema, catalog, schema string) ([]config.ResolvedSchema, error) {
	if catalog == "" {
		return all, nil
	}
	var catalogFound bool
	var filtered []config.ResolvedSchema
	for _, rs := range all {
		if rs.LogicalCatalog != catalog {
			continue
		}
		catalogFound = true
		if schema == "" || rs.LogicalSchema == schema {
			filtered = append(filtered, rs)
		}
	}
	if !catalogFound {
		return nil, fmt.Errorf("unknown catalog %q", catalog)
	}
	if schema != "" && len(filtered) == 0 {
		return nil, fmt.Errorf("unknown schema %q in catalog %q", schema, catalog)
	}
	return filtered, nil
}
