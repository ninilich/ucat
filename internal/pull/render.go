package pull

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
)

// charTypeRe matches a whole column type that is CHAR(n) or VARCHAR(n); it is applied to the trimmed type.
// Only the top level is converted: the validator rejects CHAR/VARCHAR at the top level but accepts them
// inside ARRAY<>, MAP<>, STRUCT<>, and the differ compares the type as a whole, so a nested varchar(n) is
// left as UC returned it (changing it would also need ALTER of nested fields, not of the whole column).
var charTypeRe = regexp.MustCompile(`(?i)^(?:var)?char\s*\(\s*\d+\s*\)$`)

// PulledType returns STRING for a top-level CHAR(n) / VARCHAR(n) column type and the type unchanged otherwise.
func PulledType(typ string) string {
	if charTypeRe.MatchString(strings.TrimSpace(typ)) {
		return "STRING"
	}
	return typ
}

// liveToDesired converts a live table into the desired-state table that pull writes to YAML. It is
// the one place that decides what a pulled file contains:
//   - schema is the logical schema name the file is written for (UC names are not written);
//   - a nullable column carries no nullable key (nullable: true is the default), a NOT NULL column
//     gets nullable: false;
//   - top-level CHAR/VARCHAR column types become STRING, with a warning per column;
//   - a column type that ucat validate does not support (config.ColumnTypeError) is written as UC
//     returned it, with a warning: the file stays, the owner fixes it;
//   - only the tblproperties the filter selects are kept;
//   - a clustering on a nested field is not written, with one warning, because writing a part of
//     the columns would change its meaning.
//
// loc is the YAML file the table is written to (for the warnings). The live table is not modified.
func liveToDesired(t *backend.LiveTable, schema, loc string, filter *Filter) (config.Table, diag.Diagnostics) {
	var diags diag.Diagnostics
	d := config.Table{
		Name:          t.Name,
		Schema:        schema,
		Comment:       t.Comment,
		TblProperties: filter.Select(t.Properties),
	}

	for _, col := range t.Columns {
		typ := PulledType(col.Type)
		if typ != col.Type {
			diags.Append(diag.Diagnostic{
				Severity: diag.Warning,
				Summary:  fmt.Sprintf("table %q, column %q: %s pulled as %s (length limit is dropped)", t.Name, col.Name, col.Type, typ),
				Location: loc,
			})
		}
		if config.ColumnTypeError(typ) != "" {
			diags.Append(diag.Diagnostic{
				Severity: diag.Warning,
				Summary:  fmt.Sprintf("table %q, column %q: type %q is not supported by ucat validate; fix it in the file", t.Name, col.Name, typ),
				Location: loc,
			})
		}
		c := config.Column{Name: col.Name, Type: typ, Comment: col.Comment}
		if !col.Nullable {
			notNull := false
			c.Nullable = &notNull
		}
		d.Columns = append(d.Columns, c)
	}

	switch field := t.NestedClusteringField(); {
	case field != "":
		diags.Append(diag.Diagnostic{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("table %q: clustering on nested field %q is not supported; clustering is not written", t.Name, field),
			Location: loc,
		})
	case t.ClusterAuto:
		d.Clustering = &config.Clustering{Auto: true}
	case len(t.Clustering) > 0:
		d.Clustering = &config.Clustering{Columns: append([]string(nil), t.Clustering...)}
	}

	if len(t.Partitioning) > 0 {
		d.Partitioning = &config.Partitioning{Columns: append([]string(nil), t.Partitioning...)}
	}
	return d, diags
}

// Render returns the YAML of a live table as pull writes it (see liveToDesired). schema is the
// logical schema name; loc is the YAML file the table is written to (for the warnings).
func Render(t *backend.LiveTable, schema, loc string, filter *Filter) ([]byte, diag.Diagnostics, error) {
	desired, diags := liveToDesired(t, schema, loc, filter)
	data, err := yaml.Marshal(desired)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal table %s: %w", t.Name, err)
	}
	return data, diags, nil
}
