// Package pull holds the logic of "ucat pull" that does not touch the terminal or the
// backend: the --tblproperties filter, the shape of a pulled table, the decision where a
// table file goes (or why it is skipped) and the Windows file name rules.
package pull

import (
	"fmt"
	"strings"

	"github.com/ninilich/ucat/internal/diag"
)

// reservedTblPropRoots are the system tblproperties namespaces; a prefix mask that can reach them is risky.
var reservedTblPropRoots = []string{"delta.", "databricks.", "io.unitycatalog."}

// Filter selects the tblproperties keys that pull writes to YAML.
type Filter struct {
	all     bool
	masks   []string // distinct masks in the order given (empty for "all")
	exact   map[string]bool
	prefix  []string
	matched map[string]bool // masks that selected at least one key (see Select)
}

// ParseFilter parses the --tblproperties value. Empty means no filter (nothing is written).
func ParseFilter(spec string) (*Filter, error) {
	f := &Filter{exact: map[string]bool{}, matched: map[string]bool{}}
	if spec == "" {
		return f, nil
	}
	seen := map[string]bool{}
	parts := strings.Split(spec, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
			return nil, fmt.Errorf("--tblproperties: empty mask in %q", spec)
		case p == "*":
			return nil, fmt.Errorf("--tblproperties: a lone * is not allowed, use \"all\" to write every key")
		case p == "all":
			if len(parts) > 1 {
				return nil, fmt.Errorf("--tblproperties: \"all\" cannot be combined with other masks")
			}
			f.all = true
			continue
		case strings.Contains(p, "*") && strings.Index(p, "*") != len(p)-1:
			return nil, fmt.Errorf("--tblproperties: * is only allowed at the end of a mask, got %q", p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		f.masks = append(f.masks, p)
		if strings.HasSuffix(p, "*") {
			f.prefix = append(f.prefix, strings.TrimSuffix(p, "*"))
		} else {
			f.exact[p] = true
		}
	}
	return f, nil
}

// maskMatches reports whether a single mask selects the key.
func maskMatches(mask, key string) bool {
	if strings.HasSuffix(mask, "*") {
		return strings.HasPrefix(key, strings.TrimSuffix(mask, "*"))
	}
	return mask == key
}

// Select returns the properties selected by the filter and remembers which masks matched a key.
// It returns nil when no key is selected.
func (f *Filter) Select(props map[string]string) map[string]*string {
	var res map[string]*string
	for k, v := range props {
		if !f.all && !f.matchKey(k) {
			continue
		}
		if res == nil {
			res = map[string]*string{}
		}
		res[k] = &v
	}
	return res
}

// matchKey reports whether any mask selects the key and records every mask that does.
func (f *Filter) matchKey(key string) bool {
	found := false
	for _, m := range f.masks {
		if maskMatches(m, key) {
			f.matched[m] = true
			found = true
		}
	}
	return found
}

// Warnings are the warnings known before any table is read: "all", and prefix masks that can select
// system properties (their literal prefix is a prefix of a reserved root).
func (f *Filter) Warnings() diag.Diagnostics {
	var diags diag.Diagnostics
	if f.all {
		diags.Append(diag.Diagnostic{Severity: diag.Warning, Summary: "--tblproperties all writes system properties (delta.feature.*, delta.minReaderVersion, io.unitycatalog.tableId, ...) to YAML; applying such a file can fail or irreversibly upgrade the table protocol"})
	}
	for _, m := range f.masks {
		if !strings.HasSuffix(m, "*") {
			continue
		}
		lit := strings.TrimSuffix(m, "*")
		for _, root := range reservedTblPropRoots {
			if strings.HasPrefix(root, lit) {
				diags.Append(diag.Diagnostic{Severity: diag.Warning, Summary: fmt.Sprintf("--tblproperties mask %q can select system properties (delta.feature.*, delta.lastCommitTimestamp, io.unitycatalog.tableId, ...); applying such a file may fail or irreversibly upgrade the table protocol", m)})
				break
			}
		}
	}
	return diags
}

// UnmatchedWarnings are the warnings for masks that selected no key of any written table, in mask order.
func (f *Filter) UnmatchedWarnings() diag.Diagnostics {
	var diags diag.Diagnostics
	for _, m := range f.masks {
		if !f.matched[m] {
			diags.Append(diag.Diagnostic{Severity: diag.Warning, Summary: fmt.Sprintf("--tblproperties mask %q matched no property", m)})
		}
	}
	return diags
}
