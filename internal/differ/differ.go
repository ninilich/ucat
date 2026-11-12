// Package differ compares desired table definitions with the live state and
// produces the list of change.Change values that closes the gap.
package differ

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
)

// pathOf builds a change.TablePath from a resolved config.Table.
func pathOf(t config.Table) change.TablePath {
	return change.TablePath{Catalog: t.ResolvedCatalog, Schema: t.ResolvedSchema, Name: t.Name}
}

func foldNames(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = config.FoldName(n)
	}
	return out
}

// clusteringEqual reports whether the desired clustering block describes the live clustering.
// No block equals a table without clustering; the columns are compared as sets, ignoring case.
func clusteringEqual(desired *config.Clustering, live *backend.LiveTable) bool {
	if desired == nil || !live.HasClustering() {
		return desired == nil && !live.HasClustering()
	}
	if desired.Auto || live.ClusterAuto {
		return desired.Auto == live.ClusterAuto
	}
	// Compare column sets order-insensitively.
	dc := foldNames(desired.Columns)
	lc := foldNames(live.Clustering)
	sort.Strings(dc)
	sort.Strings(lc)
	return slices.Equal(dc, lc)
}

// partitioningEqual reports whether the desired partitioning block describes the live partition
// columns, in order. No block equals a table without partitioning.
func partitioningEqual(desired *config.Partitioning, live []string) bool {
	if desired == nil || len(live) == 0 {
		return desired == nil && len(live) == 0
	}
	return slices.EqualFunc(desired.Columns, live, config.SameName)
}

// Diff compares desired table state vs live UC state.
// live == nil means the table does not exist in UC → change.CreateTableChange.
// Returns changes ordered: renames, column adds/alters, column drops, tblprops, clustering.
// Also returns diagnostics (e.g. Warning for partitioning change attempts).
func Diff(desired config.Table, live *backend.LiveTable) ([]change.Change, diag.Diagnostics) {
	var changes []change.Change
	var diags diag.Diagnostics

	path := pathOf(desired)

	// drop: true → immediate change.DropTableChange, no further diff.
	// idempotent - if table already gone, just warn instead of erroring.
	if desired.Drop {
		if live == nil {
			diags.Append(diag.Diagnostic{
				Severity: diag.Warning,
				Summary:  "drop: true but table not in UC - file can be removed",
				Location: desired.SourceFile,
			})
			return nil, diags
		}
		return []change.Change{&change.DropTableChange{Base: change.Base{Table: path}}}, diags
	}

	// Table doesn't exist in UC → change.CreateTableChange.
	if live == nil {
		return []change.Change{&change.CreateTableChange{Base: change.Base{Table: path}, Def: desired}}, diags
	}

	if ch := diffTableRename(desired, live, path); ch != nil {
		changes = append(changes, ch)
	}

	colChanges, colDiags, ok := diffColumns(desired, live, path)
	diags = append(diags, colDiags...)
	if !ok {
		return nil, diags
	}
	changes = append(changes, colChanges...)

	changes = append(changes, diffTblProperties(desired, live, path)...)

	// --- Clustering diff ---
	// Live clustering on a nested field is not expressible in YAML (pull does not write it), so
	// a table without a clustering block keeps it instead of getting CLUSTER BY NONE.
	keepNested := desired.Clustering == nil && live.NestedClusteringField() != ""
	if !keepNested && !clusteringEqual(desired.Clustering, live) {
		changes = append(changes, &change.AlterClusteringChange{Base: change.Base{Table: path}, Clustering: desired.Clustering})
	}
	// Unity Catalog may assign automatic clustering to a new managed table; a file that never
	// mentioned clustering would then drop it on the next apply, so say so.
	if live.ClusterAuto && desired.Clustering == nil && desired.Partitioning == nil {
		diags.Append(diag.Diagnostic{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("%s: Unity Catalog assigned automatic clustering and the file has no clustering block, so apply will drop it; add \"clustering: {auto: true}\" to keep it", path.Subject()),
			Location: desired.SourceFile,
		})
	}

	// --- Table comment diff ---
	if desired.Comment != live.Comment {
		changes = append(changes, &change.AlterTableCommentChange{Base: change.Base{Table: path}, Comment: desired.Comment})
	}

	// --- Partitioning diff (warning only — cannot change after creation) ---
	if !partitioningEqual(desired.Partitioning, live.Partitioning) {
		diags.Append(diag.Diagnostic{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("%s: partitioning cannot be changed after creation; ignoring", path.Subject()),
			Location: desired.SourceFile,
		})
	}

	return changes, diags
}

// diffTableRename returns the table rename change, or nil. rename_from equal to the name
// (validator warns "no rename will occur") is not a rename: the table is diffed like one without it.
func diffTableRename(desired config.Table, live *backend.LiveTable, path change.TablePath) change.Change {
	if desired.RenameFrom == "" || config.FoldName(desired.RenameFrom) == config.FoldName(desired.Name) || live.Name != desired.RenameFrom {
		return nil
	}
	oldPath := change.TablePath{Catalog: live.Catalog, Schema: path.Schema, Name: desired.RenameFrom}
	return &change.RenameTableChange{
		OldPath: oldPath,
		NewName: desired.Name,
	}
}

// columnDiff is the state shared by the column steps of one table diff.
type columnDiff struct {
	desired config.Table
	live    *backend.LiveTable
	path    change.TablePath

	liveByName     map[string]backend.LiveColumn
	desiredByName  map[string]bool
	consumedLive   map[string]bool   // live column names consumed by renames
	renamedDesired map[string]string // desired name → old live name

	renames    []change.Change
	addsAlters []change.Change
	drops      []change.Change
	diags      diag.Diagnostics
}

// diffColumns runs steps A (renames), B (add/alter) and C (drops). Returns the changes in the
// order renames, adds/alters, drops; ok == false when a step failed and the plan must not go on.
func diffColumns(desired config.Table, live *backend.LiveTable, path change.TablePath) (changes []change.Change, diags diag.Diagnostics, ok bool) {
	d := &columnDiff{
		desired:        desired,
		live:           live,
		path:           path,
		liveByName:     make(map[string]backend.LiveColumn, len(live.Columns)),
		desiredByName:  make(map[string]bool, len(desired.Columns)),
		consumedLive:   make(map[string]bool),
		renamedDesired: make(map[string]string),
	}
	for _, col := range live.Columns {
		d.liveByName[config.FoldName(col.Name)] = col
	}
	for _, col := range desired.Columns {
		d.desiredByName[config.FoldName(col.Name)] = true
	}

	if d.stepRenames() {
		return nil, d.diags, false
	}
	if d.stepAddAlter() {
		return nil, d.diags, false
	}
	d.stepDrops()

	changes = append(changes, d.renames...)
	changes = append(changes, d.addsAlters...)
	changes = append(changes, d.drops...)
	return changes, d.diags, true
}

// stepRenames is step A: the rename pass. Reports whether it failed.
func (d *columnDiff) stepRenames() (failed bool) {
	for _, col := range d.desired.Columns {
		// A case-only "rename" is a no-op: UC resolves column names case-insensitively.
		if col.RenameFrom == "" || config.FoldName(col.RenameFrom) == config.FoldName(col.Name) {
			continue
		}
		if !d.renameColumn(col) {
			failed = true
		}
	}
	return failed
}

// renameColumn handles one column with rename_from. Reports false on a planning error.
func (d *columnDiff) renameColumn(col config.Column) bool {
	desired := d.desired
	liveCol, ok := d.liveByName[config.FoldName(col.RenameFrom)]
	if !ok {
		if _, exists := d.liveByName[config.FoldName(col.Name)]; !exists {
			// neither source nor target exists in live - most likely a typo in rename_from.
			d.diags.Append(diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("%s: column %q: rename_from %q not found in the UC table and %q does not exist yet; fix the name or remove rename_from to add %q as a new column", d.path.Subject(), col.Name, col.RenameFrom, col.Name, col.Name),
				Location: desired.SourceFile,
			})
			return false
		}
		// source column not in live - rename already applied.
		d.diags.Append(diag.Diagnostic{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("%s: column %q: rename_from %q can be removed after apply", d.path.Subject(), col.Name, col.RenameFrom),
			Location: desired.SourceFile,
		})
		return true
	}
	// Conflicts are not resolved automatically: the plan must fail before apply.
	if _, exists := d.liveByName[config.FoldName(col.Name)]; exists {
		d.diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("%s: column %q: cannot rename %q to %q: column %q already exists in the UC table", d.path.Subject(), col.Name, col.RenameFrom, col.Name, col.Name),
			Location: desired.SourceFile,
		})
		return false
	}
	if d.desiredByName[config.FoldName(col.RenameFrom)] {
		d.diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("%s: column %q is both defined and the rename source of column %q in the same plan; use distinct names", d.path.Subject(), col.RenameFrom, col.Name),
			Location: desired.SourceFile,
		})
		return false
	}
	d.renames = append(d.renames, &change.RenameColumnChange{
		Base:    change.Base{Table: d.path},
		OldName: liveCol.Name,
		NewName: col.Name,
	})
	d.consumedLive[config.FoldName(liveCol.Name)] = true
	d.renamedDesired[config.FoldName(col.Name)] = liveCol.Name
	// After rename, check for type/comment/nullable changes.
	return d.appendColumnAlters(col, liveCol)
}

// stepAddAlter is step B: the add/alter pass. Reports whether it failed.
func (d *columnDiff) stepAddAlter() (failed bool) {
	for _, col := range d.desired.Columns {
		if _, wasRenamed := d.renamedDesired[config.FoldName(col.Name)]; wasRenamed {
			continue // already handled above
		}
		liveCol, exists := d.liveByName[config.FoldName(col.Name)]
		if !exists {
			// ADD COLUMN has no NOT NULL in Databricks SQL, so a new NOT NULL column
			// of an existing table is a planning error (CREATE TABLE still allows it).
			if !col.IsNullable() {
				d.diags.Append(diag.Diagnostic{
					Severity: diag.Error,
					Summary:  fmt.Sprintf("%s, column %q: a new NOT NULL column cannot be added to an existing table (not supported in v1): add it as nullable, fill it with data, then set nullable: false", d.path.Subject(), col.Name),
					Location: d.desired.SourceFile,
				})
				failed = true
				continue
			}
			d.addsAlters = append(d.addsAlters, &change.AddColumnChange{Base: change.Base{Table: d.path}, Column: col})
		} else if !d.appendColumnAlters(col, liveCol) {
			failed = true
		}
	}
	return failed
}

// stepDrops is step C: the drop pass - live columns not matched by any desired column.
func (d *columnDiff) stepDrops() {
	for _, liveCol := range d.live.Columns {
		if d.consumedLive[config.FoldName(liveCol.Name)] {
			continue // consumed by a rename
		}
		if !d.desiredByName[config.FoldName(liveCol.Name)] {
			d.drops = append(d.drops, &change.DropColumnChange{Base: change.Base{Table: d.path}, ColumnName: liveCol.Name})
		}
	}
}

// diffTblProperties returns the SET/UNSET tblproperties changes.
func diffTblProperties(desired config.Table, live *backend.LiveTable, path change.TablePath) []change.Change {
	set, unset := splitProps(desired.TblProperties, live.Properties)
	var changes []change.Change
	if len(set) > 0 {
		changes = append(changes, &change.SetTBLPropertiesChange{Base: change.Base{Table: path}, Properties: set})
	}
	if len(unset) > 0 {
		changes = append(changes, &change.UnsetTBLPropertiesChange{Base: change.Base{Table: path}, Keys: unset})
	}
	return changes
}

// splitProps compares the desired tblproperties with the live ones: set holds the properties to
// SET (absent or different live), unset the sorted keys to UNSET (a nil desired value that is
// live). System keys are skipped.
func splitProps(desired map[string]*string, live map[string]string) (set map[string]string, unset []string) {
	set = make(map[string]string)
	for k, v := range desired {
		if backend.IsSystemTblPropKey(k) {
			continue
		}
		liveVal, isLive := live[k]
		switch {
		case v == nil: // nil means explicitly unset this property
			if isLive {
				unset = append(unset, k)
			}
		case !isLive || liveVal != *v:
			set[k] = *v
		}
	}
	sort.Strings(unset)
	return set, unset
}

// normalizeType returns the form of a column type used only for comparison: lower case with all
// whitespace removed, except inside backtick-quoted identifiers (kept as is, apart from case),
// and with the Spark DECIMAL defaults spelled out (see expandDecimalDefaults).
func normalizeType(t string) string {
	var b strings.Builder
	inQuote := false
	for _, r := range strings.ToLower(t) {
		if r == '`' {
			inQuote = !inQuote
		}
		if !inQuote && unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return expandDecimalDefaults(b.String())
}

// expandDecimalDefaults spells out the Spark defaults of DECIMAL in an already lower-cased,
// whitespace-free type, at any nesting depth: bare "decimal" becomes "decimal(10,0)" and
// "decimal(p)" becomes "decimal(p,0)", which is what UC returns in type_text. Backtick-quoted
// identifiers are skipped, a whole word is matched (not "decimals"), and a word followed by ':'
// is a STRUCT field name, not a type, so it is left alone.
func expandDecimalDefaults(t string) string {
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(t); {
		c := t[i]
		if c == '`' {
			inQuote = !inQuote
		}
		if inQuote || !isTypeWordChar(c) {
			b.WriteByte(c)
			i++
			continue
		}
		j := i
		for j < len(t) && isTypeWordChar(t[j]) {
			j++
		}
		word, rest := t[i:j], t[j:]
		i = j
		switch {
		case word != "decimal" || strings.HasPrefix(rest, ":"):
			b.WriteString(word)
		case !strings.HasPrefix(rest, "("):
			b.WriteString("decimal(10,0)")
		default:
			b.WriteString(word)
			if n := precisionOnlyLen(rest); n > 0 {
				b.WriteString(rest[:n-1] + ",0)")
				i += n
			}
		}
	}
	return b.String()
}

// precisionOnlyLen returns the length of a leading "(digits)" in s (at least one digit), or 0.
func precisionOnlyLen(s string) int {
	k := 1
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	if k > 1 && k < len(s) && s[k] == ')' {
		return k + 1
	}
	return 0
}

func isTypeWordChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 0x80
}

// stringType is the normalized form of the only supported target of a type change.
const stringType = "string"

// liveStringLikeType matches a normalized live varchar(n)/char(n) type (whole string).
var liveStringLikeType = regexp.MustCompile(`^(?:varchar|char)\(\d+\)$`)

// typeChangeSupported reports whether changing a column type from live to desired is supported
// in v1: only varchar(n)/char(n) to STRING. Both types are normalized; the check is strict on the
// whole string (no type grammar), so nested types and everything else are not supported.
func typeChangeSupported(desired, live string) bool {
	return normalizeType(desired) == stringType && liveStringLikeType.MatchString(normalizeType(live))
}

// appendColumnAlters appends the alters of one existing column to d.addsAlters. Reports false
// (with an error diagnostic) when the type change is not supported in v1.
func (d *columnDiff) appendColumnAlters(desired config.Column, live backend.LiveColumn) bool {
	if normalizeType(desired.Type) != normalizeType(live.Type) && !typeChangeSupported(desired.Type, live.Type) {
		d.diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("%s, column %q: changing the type from %s to %s is not supported in v1 (only varchar(n)/char(n) to STRING); type widening is planned for v2", d.path.Subject(), desired.Name, live.Type, desired.Type),
			Location: d.desired.SourceFile,
		})
		return false
	}
	d.addsAlters = append(d.addsAlters, columnAlters(d.path, desired, live)...)
	return true
}

// columnAlters compares a desired column against its live counterpart and returns AlterColumnChanges.
// A type change is built as is: appendColumnAlters has already checked that it is supported.
func columnAlters(path change.TablePath, desired config.Column, live backend.LiveColumn) []change.Change {
	var changes []change.Change

	// Type comparison: case and whitespace insensitive, DECIMAL defaults expanded (UC returns "decimal(10,2)"
	// for "DECIMAL(10, 2)" and "decimal(10,0)" for "DECIMAL").
	// ALTER keeps the original desired type from YAML.
	if normalizeType(desired.Type) != normalizeType(live.Type) {
		changes = append(changes, &change.AlterColumnChange{
			Base:       change.Base{Table: path},
			ColumnName: desired.Name,
			Kind:       change.AlterType,
			OldValue:   live.Type,
			NewValue:   desired.Type,
		})
	}

	// Comment comparison.
	if desired.Comment != live.Comment {
		changes = append(changes, &change.AlterColumnChange{
			Base:       change.Base{Table: path},
			ColumnName: desired.Name,
			Kind:       change.AlterComment,
			OldValue:   live.Comment,
			NewValue:   desired.Comment,
		})
	}

	// Nullable comparison.
	if desired.IsNullable() != live.Nullable {
		changes = append(changes, &change.AlterColumnChange{
			Base:        change.Base{Table: path},
			ColumnName:  desired.Name,
			Kind:        change.AlterNullable,
			OldNullable: live.Nullable,
			NewNullable: desired.IsNullable(),
		})
	}

	return changes
}

// DiffSchema diffs all desired tables in a schema against live UC tables.
// Tables in live but not in desired are silently ignored (unmanaged).
func DiffSchema(desired []config.Table, live []*backend.LiveTable) ([]change.Change, diag.Diagnostics) {
	var allChanges []change.Change
	var allDiags diag.Diagnostics

	liveByName := make(map[string]*backend.LiveTable, len(live))
	for _, t := range live {
		liveByName[t.Name] = t
	}

	for _, d := range desired {
		if w := warnStaleRenameFrom(d, liveByName); w != nil {
			allDiags.Append(*w)
		}
		changes, diags := Diff(d, matchLive(d, liveByName))
		allChanges = append(allChanges, changes...)
		allDiags = append(allDiags, diags...)
	}

	return allChanges, allDiags
}

// matchLive returns the live table of a desired one: found under its name, else, with
// rename_from, under the old name; nil when the table does not exist in UC.
func matchLive(d config.Table, liveByName map[string]*backend.LiveTable) *backend.LiveTable {
	if t := liveByName[d.Name]; t != nil {
		return t
	}
	if d.RenameFrom != "" {
		return liveByName[d.RenameFrom]
	}
	return nil
}

// warnStaleRenameFrom returns the warning for a rename_from that is stale, unresolved or
// ambiguous, or nil when the rename is pending or there is no rename.
func warnStaleRenameFrom(d config.Table, liveByName map[string]*backend.LiveTable) *diag.Diagnostic {
	if d.RenameFrom == "" || d.Drop || config.FoldName(d.RenameFrom) == config.FoldName(d.Name) {
		return nil
	}
	oldLive, newLive := liveByName[d.RenameFrom], liveByName[d.Name]
	subject := pathOf(d).Subject()
	var msg string
	switch {
	case oldLive == nil && newLive != nil:
		msg = fmt.Sprintf("%s: rename_from %q can be removed after apply", subject, d.RenameFrom)
	case oldLive == nil && newLive == nil:
		msg = fmt.Sprintf("%s: rename_from %q not found in UC schema - a new table will be created", subject, d.RenameFrom)
	case oldLive != nil && newLive != nil:
		msg = fmt.Sprintf("%s: rename_from %q ignored: both %q and %q exist in UC", subject, d.RenameFrom, d.RenameFrom, d.Name)
	default:
		return nil
	}
	return &diag.Diagnostic{Severity: diag.Warning, Summary: msg, Location: d.SourceFile}
}
