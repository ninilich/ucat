package config

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/ninilich/ucat/internal/diag"
)

// validExactTypes is the set of canonical UC types without parameters (case-insensitive).
var validExactTypes = map[string]struct{}{
	"TINYINT": {}, "SMALLINT": {}, "INT": {}, "BIGINT": {},
	"FLOAT": {}, "DOUBLE": {}, "BOOLEAN": {}, "STRING": {}, "BINARY": {},
	"DATE": {}, "TIMESTAMP": {}, "TIMESTAMP_NTZ": {}, "VARIANT": {},
}

// paramTypesHelp lists the parameterized types for error messages.
const paramTypesHelp = "ARRAY<...>, DECIMAL[(p,s)], GEOGRAPHY(...), GEOMETRY(...), INTERVAL ..., MAP<...>, STRUCT<...>"

// supportedTypesList returns the supported types for error messages.
func supportedTypesList() string {
	names := make([]string, 0, len(validExactTypes))
	for t := range validExactTypes {
		names = append(names, t)
	}
	sort.Strings(names)
	return strings.Join(names, ", ") + ", " + paramTypesHelp
}

// typeProblem classifies why a column type is rejected.
type typeProblem int

const (
	typeOK               typeProblem = iota // supported
	typeUnsupported                         // not a supported type (the message lists the supported ones)
	typeWhitespaceInWord                    // whitespace inside a word, e.g. "BIG INT"
	typeStructModifier                      // NOT NULL / COMMENT inside STRUCT
	typeTooDeep                             // ARRAY/MAP/STRUCT nested deeper than maxTypeDepth
)

// reason is the suffix of the error for the problems that have their own text.
func (p typeProblem) reason() string {
	switch p {
	case typeWhitespaceInWord:
		return "whitespace inside a word"
	case typeStructModifier:
		return "NOT NULL and COMMENT are not supported inside STRUCT in v1"
	}
	return ""
}

var (
	structOpenRe     = regexp.MustCompile(`\bSTRUCT\s*<`)
	structModifierRe = regexp.MustCompile(`[\w>)]\s+(?:NOT\s+NULL|COMMENT)\b`)
)

// hasStructModifier reports whether the type has a STRUCT field modifier (STRUCT<a:INT NOT NULL>,
// STRUCT<a:INT COMMENT x>). A field only named "comment" does not count: it is followed by ":".
func hasStructModifier(colType string) bool {
	upper := strings.ToUpper(colType)
	return structOpenRe.MatchString(upper) && structModifierRe.MatchString(upper)
}

// ColumnTypeError returns why a column type is not supported, or "" for a supported one.
// It is the single type check: validate and pull both use it.
func ColumnTypeError(colType string) string {
	problem, nested := columnTypeProblem(colType)
	switch problem {
	case typeOK:
		return ""
	case typeTooDeep:
		return fmt.Sprintf("type %s is not supported; nesting is deeper than %d levels", quoteTypePrefix(colType), maxTypeDepth)
	case typeUnsupported:
		msg := fmt.Sprintf("type %q is not supported; supported types: %s", colType, supportedTypesList())
		if nested != "" {
			msg += fmt.Sprintf("; unsupported nested type: %q", nested)
		}
		return msg
	}
	return fmt.Sprintf("type %q is not supported: %s", colType, problem.reason())
}

// columnTypeProblem classifies a column type: typeOK for a supported one, typeWhitespaceInWord
// when it has whitespace inside a word (and would be well-formed without it, e.g. "BIG INT"),
// typeStructModifier for a STRUCT field modifier (it would otherwise look like whitespace
// inside a word), and typeUnsupported otherwise. For typeUnsupported, nested is the first
// unknown type word inside ARRAY/MAP/STRUCT (empty when the problem is not such a word).
func columnTypeProblem(colType string) (problem typeProblem, nested string) {
	if hasWhitespaceInWord(colType) {
		ok, _, tooDeep := parseColumnType(strings.Join(strings.Fields(colType), ""), true)
		if tooDeep {
			return typeTooDeep, ""
		}
		if ok {
			if hasStructModifier(colType) {
				return typeStructModifier, ""
			}
			return typeWhitespaceInWord, ""
		}
		return typeUnsupported, ""
	}
	ok, bad, tooDeep := parseColumnType(colType, false)
	if tooDeep {
		return typeTooDeep, ""
	}
	if !ok {
		return typeUnsupported, bad
	}
	return typeOK, ""
}

// forbiddenInType reports characters and sequences a type must never contain (quotes, comments,
// statement separators): they would let a type break out of the generated SQL.
func forbiddenInType(colType string) bool {
	return strings.ContainsAny(colType, ";\\'\"`\n\r") || strings.Contains(colType, "--") || strings.Contains(colType, "/*")
}

// parseColumnType checks a whole type recursively: every word inside ARRAY<...>, MAP<..., ...>,
// STRUCT<name: ...> must be a supported type (STRUCT field names are not). It reads the text once,
// so the cost is linear. bad is the first unknown word inside a wrapper.
// lenient is for the type with its whitespace removed, which is only used to classify a
// whitespace problem: only the structure is checked there, nested words and words after
// a nested type (STRUCT<a:INT NOT NULL> glued) are accepted.
func parseColumnType(colType string, lenient bool) (ok bool, bad string, tooDeep bool) {
	if forbiddenInType(colType) {
		return false, "", false
	}
	p := &typeParser{s: []rune(colType), lenient: lenient}
	if !p.parseType(false) {
		return false, p.bad, p.tooDeep
	}
	p.skipSpace()
	return p.pos == len(p.s), p.bad, false
}

// maxTypeDepth is how deep ARRAY<, MAP< and STRUCT< may be nested. Real types are rarely deeper
// than 10; without a limit the recursive reader overflows the stack (not recoverable in Go) on a
// type of millions of levels.
const maxTypeDepth = 100

// maxTypeQuoteRunes is how much of a too deep type the error message shows.
const maxTypeQuoteRunes = 80

// quoteTypePrefix quotes the start of a long type for a message: at most maxTypeQuoteRunes
// characters, then "..." inside the quotes.
func quoteTypePrefix(colType string) string {
	r := []rune(colType)
	if len(r) <= maxTypeQuoteRunes {
		return strconv.Quote(colType)
	}
	q := strconv.Quote(string(r[:maxTypeQuoteRunes]))
	return q[:len(q)-1] + `..."`
}

// typeParser is a recursive-descent reader of a column type.
type typeParser struct {
	s       []rune
	pos     int
	lenient bool
	bad     string
	depth   int  // ARRAY/MAP/STRUCT wrappers open at the current position
	tooDeep bool // depth went over maxTypeDepth: the reader stops at once
}

func (p *typeParser) skipSpace() {
	for p.pos < len(p.s) && unicode.IsSpace(p.s[p.pos]) {
		p.pos++
	}
}

// word reads a run of identifier characters (after whitespace); "" when there is none.
func (p *typeParser) word() string {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.s) && isIdentRune(p.s[p.pos]) {
		p.pos++
	}
	return string(p.s[start:p.pos])
}

// peek reports whether the next non-space character is r.
func (p *typeParser) peek(r rune) bool {
	p.skipSpace()
	return p.pos < len(p.s) && p.s[p.pos] == r
}

// eat consumes the next non-space character when it is r.
func (p *typeParser) eat(r rune) bool {
	if !p.peek(r) {
		return false
	}
	p.pos++
	return true
}

// parseType reads one type. nested is true inside a wrapper.
func (p *typeParser) parseType(nested bool) bool {
	w := p.word()
	if _, ok := validExactTypes[strings.ToUpper(w)]; ok {
		return true
	}
	if ok, known := p.parseCompound(strings.ToUpper(w), nested); known {
		return ok
	}
	// an unknown word: inside a wrapper it is named in the error
	if w != "" && nested {
		if p.lenient {
			return true
		}
		if p.bad == "" {
			p.bad = w
		}
	}
	return false
}

// parseCompound reads the rest of a type that starts with the keyword (upper case); known is false
// when the word is not such a keyword.
func (p *typeParser) parseCompound(keyword string, nested bool) (ok, known bool) {
	if isWrapper(keyword) {
		if !p.enter() {
			return false, true
		}
		defer p.leave()
	}
	return p.parseKeyword(keyword, nested)
}

// parseKeyword is parseCompound after the depth check.
func (p *typeParser) parseKeyword(keyword string, nested bool) (ok, known bool) {
	switch keyword {
	case "DECIMAL":
		return !p.peek('(') || p.parens(), true
	case "GEOGRAPHY", "GEOMETRY":
		return p.peek('(') && p.parens(), true
	case "VARCHAR", "CHAR":
		// UC returns a nested varchar(n)/char(n) in type_text as is; at the top level they are not supported
		return nested && p.peek('(') && p.parens(), true
	case "ARRAY":
		return p.arrayArgs(), true
	case "MAP":
		return p.mapArgs(), true
	case "STRUCT":
		return p.eat('<') && p.structFields() && p.eat('>'), true
	case "INTERVAL":
		return p.intervalQualifier(), true
	}
	return false, false
}

// enter opens one level of nesting; false (and tooDeep) when it is deeper than maxTypeDepth.
func (p *typeParser) enter() bool {
	p.depth++
	p.tooDeep = p.depth > maxTypeDepth
	return !p.tooDeep
}

// leave closes the level opened by enter.
func (p *typeParser) leave() { p.depth-- }

// isWrapper reports whether the keyword opens a nested type (ARRAY<, MAP<, STRUCT<).
func isWrapper(keyword string) bool {
	return keyword == "ARRAY" || keyword == "MAP" || keyword == "STRUCT"
}

// arrayArgs reads "<type>".
func (p *typeParser) arrayArgs() bool {
	return p.eat('<') && p.nestedType() && p.eat('>')
}

// mapArgs reads "<key, value>".
func (p *typeParser) mapArgs() bool {
	return p.eat('<') && p.nestedType() && p.eat(',') && p.nestedType() && p.eat('>')
}

// skipWords reads the identifier words that follow.
func (p *typeParser) skipWords() {
	for p.word() != "" {
		continue
	}
}

// nestedType reads a type inside a wrapper.
func (p *typeParser) nestedType() bool {
	if !p.parseType(true) {
		return false
	}
	if p.lenient {
		p.skipWords()
	}
	return true
}

// structFields reads "name: type" pairs separated by commas.
func (p *typeParser) structFields() bool {
	for {
		if p.word() == "" || !p.eat(':') || !p.nestedType() {
			return false
		}
		if !p.eat(',') {
			return true
		}
	}
}

// parens reads a non-empty "(...)" argument list (DECIMAL(p,s), GEOMETRY(4326)); it is not parsed further.
func (p *typeParser) parens() bool {
	if !p.eat('(') {
		return false
	}
	start := p.pos
	for p.pos < len(p.s) && !strings.ContainsRune("()<>", p.s[p.pos]) {
		p.pos++
	}
	if strings.TrimSpace(string(p.s[start:p.pos])) == "" {
		return false
	}
	return p.eat(')')
}

// intervalQualifier reads the words after INTERVAL ("DAY TO SECOND"); the qualifier is not parsed until v2.
func (p *typeParser) intervalQualifier() bool {
	if p.word() == "" {
		return false
	}
	p.skipWords()
	return true
}

// isIdentRune reports whether r can be part of a type word (letter, digit, underscore).
func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// hasWhitespaceInWord reports whether whitespace separates two identifier characters
// ("IN T", "DEC IMAL"). Whitespace next to < > ( ) , : or at the edges is fine.
// Words of an INTERVAL qualifier ("INTERVAL DAY TO SECOND") are one atom: a gap
// between words is allowed until the next punctuation character.
func hasWhitespaceInWord(colType string) bool {
	upper := strings.ToUpper(strings.TrimSpace(colType))
	atomFirst := "" // first word of the current atom
	var word []rune // word being read
	gap := false    // whitespace seen after the previous word, no punctuation since
	flush := func() {
		if len(word) > 0 && atomFirst == "" {
			atomFirst = string(word)
		}
		word = nil
	}
	for _, r := range upper {
		switch {
		case isIdentRune(r):
			if gap && atomFirst != "INTERVAL" {
				return true
			}
			gap = false
			word = append(word, r)
		case unicode.IsSpace(r):
			if len(word) > 0 {
				flush()
				gap = true
			}
		default: // punctuation
			flush()
			atomFirst = ""
			gap = false
		}
	}
	return false
}

// appendNotLower appends an Error with the given summary when name is non-empty and not lowercase.
// Reports whether a diagnostic was appended.
func appendNotLower(diags *diag.Diagnostics, name, summary, loc string) bool {
	if name == "" || IsLowerName(name) {
		return false
	}
	diags.Append(diag.Diagnostic{Severity: diag.Error, Summary: summary, Location: loc})
	return true
}

// tableKey identifies a table name within its resolved catalog and schema.
func tableKey(t Table, name string) string {
	return joinKey(t.ResolvedCatalog, t.ResolvedSchema, name)
}

// tableLoc is the diagnostic location of a table: its source file, or "table:<name>" without one.
func tableLoc(t Table) string {
	if t.SourceFile != "" {
		return t.SourceFile
	}
	return "table:" + t.Name
}

// isRealRename reports whether the column renames something (a case-only rename_from is not a rename).
func isRealRename(col Column) bool {
	return col.RenameFrom != "" && !SameName(col.RenameFrom, col.Name)
}

// appendCaseCollisions reports each group of names that differ only in case.
// spellings: folded name -> spelling -> source. One diagnostic per group, groups and spellings sorted.
func appendCaseCollisions(diags *diag.Diagnostics, kind string, spellings map[string]map[string]string) {
	folded := make([]string, 0, len(spellings))
	for f, byName := range spellings {
		if len(byName) > 1 {
			folded = append(folded, f)
		}
	}
	sort.Strings(folded)
	for _, f := range folded {
		names := make([]string, 0, len(spellings[f]))
		for n := range spellings[f] {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = fmt.Sprintf("%q (%s)", n, spellings[f][n])
		}
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("logical %s names %s differ only in case", kind, joinAnd(parts)),
			Location: kind + ":" + names[0],
		})
	}
}

// validation holds the state shared by the checks of one ValidateLocal run.
type validation struct {
	config ResolvedConfig
	diags  diag.Diagnostics

	resolvedSeen map[string]string // (ResolvedCatalog, ResolvedSchema, Name) -> source (check 2)
	renameSeen   map[string]string // rename_from per resolved schema -> first table name (check 12)
	definedLoc   map[string]string // defined table locations per resolved schema (check 14)
}

// errorf appends an Error diagnostic.
func (v *validation) errorf(loc, format string, args ...any) {
	v.diags.Append(diag.Diagnostic{Severity: diag.Error, Summary: fmt.Sprintf(format, args...), Location: loc})
}

// warnf appends a Warning diagnostic.
func (v *validation) warnf(loc, format string, args ...any) {
	v.diags.Append(diag.Diagnostic{Severity: diag.Warning, Summary: fmt.Sprintf(format, args...), Location: loc})
}

// ValidateLocal runs all local (no-network) validation checks on a resolved config.
// Returns diagnostics for every issue found - does not stop at first error.
func ValidateLocal(config ResolvedConfig) diag.Diagnostics {
	v := &validation{
		config:       config,
		resolvedSeen: make(map[string]string),
		renameSeen:   make(map[string]string),
		definedLoc:   make(map[string]string),
	}

	v.checkEmptyCatalogs()
	v.checkSchemaInManyCatalogs()
	v.checkCaseOnlyCollisions()
	v.checkEmptyUCCatalogs()
	v.checkPhysicalSchemas()
	v.indexDefinedTables()
	v.checkUCNames()

	for _, table := range config.Tables {
		v.checkTable(table)
	}

	return v.diags
}

// Check 20: a catalog with an empty name (reported once per catalog; Schemas is already ordered)
func (v *validation) checkEmptyCatalogs() {
	emptyCatSeen := make(map[string]bool)
	for _, rs := range v.config.Schemas {
		if rs.LogicalCatalog != "" || emptyCatSeen[rs.SourceFile] {
			continue
		}
		emptyCatSeen[rs.SourceFile] = true
		v.errorf(rs.SourceFile, "catalog name is empty")
	}
}

// Check 21: one logical schema name must not be declared in several catalogs
// (tables reference a schema by logical name only). One diagnostic per name, names sorted.
func (v *validation) checkSchemaInManyCatalogs() {
	schemaCatalogs := make(map[string][]string) // logical schema -> "catalog (file)" in Schemas order
	for _, rs := range v.config.Schemas {
		schemaCatalogs[rs.LogicalSchema] = append(schemaCatalogs[rs.LogicalSchema], fmt.Sprintf("catalog %q (%s)", rs.LogicalCatalog, rs.Source()))
	}
	dupNames := make([]string, 0)
	for name, owners := range schemaCatalogs {
		if len(owners) > 1 {
			dupNames = append(dupNames, name)
		}
	}
	sort.Strings(dupNames)
	for _, name := range dupNames {
		owners := schemaCatalogs[name]
		v.errorf("schema:"+name, "logical schema %q is defined in %s %s; a table references a schema by logical name only", name, both(len(owners), "all of"), joinAnd(owners))
	}
}

// Check 26: logical catalog (or schema) names that differ only in case (exact duplicates are check 21).
func (v *validation) checkCaseOnlyCollisions() {
	catSpellings := make(map[string]map[string]string)
	schemaSpellings := make(map[string]map[string]string)
	addSpelling := func(m map[string]map[string]string, name, src string) {
		if name == "" {
			return
		}
		f := FoldName(name)
		if m[f] == nil {
			m[f] = make(map[string]string)
		}
		if _, ok := m[f][name]; !ok {
			m[f][name] = src
		}
	}
	for _, rs := range v.config.Schemas {
		addSpelling(catSpellings, rs.LogicalCatalog, rs.Source())
		addSpelling(schemaSpellings, rs.LogicalSchema, rs.Source())
	}
	appendCaseCollisions(&v.diags, "catalog", catSpellings)
	appendCaseCollisions(&v.diags, "schema", schemaSpellings)
}

// Check 22: a catalog must resolve to a non-empty UC name (reported once per logical catalog).
// Limit: a catalog without schemas yields no ResolvedSchema, so only schema-bearing catalogs are checked.
// Skipped without a target: uc_name may be set only there.
func (v *validation) checkEmptyUCCatalogs() {
	emptyUCCatSeen := make(map[string]bool)
	for _, rs := range v.config.Schemas {
		if v.config.NoTarget || rs.UCCatalog != "" || rs.LogicalCatalog == "" || emptyUCCatSeen[rs.LogicalCatalog] {
			continue
		}
		emptyUCCatSeen[rs.LogicalCatalog] = true
		v.errorf(rs.Source(), "catalog %q: uc_name is not set (set it in the catalog file or override it in the target)", rs.LogicalCatalog)
	}
}

// physKey is a physical (UC) catalog and schema pair.
type physKey struct{ catalog, schema string }

// Check 23: each logical schema must map to its own physical schema.
// The same logical name in several catalogs is already reported by check 21.
func (v *validation) checkPhysicalSchemas() {
	physOwners := ownersByPhys(v.config.Schemas)
	for _, k := range sharedPhysKeys(physOwners) {
		owners := physOwners[k]
		parts := make([]string, len(owners))
		for i, o := range owners {
			parts[i] = fmt.Sprintf("%q (%s)", o.LogicalSchema, o.Source())
		}
		v.errorf("schema:"+owners[0].LogicalSchema,
			"schemas %s %s resolve to physical schema %s.%s; each logical schema must map to its own physical schema",
			joinAnd(parts), both(len(owners), "all"), k.catalog, k.schema)
	}
}

// ownersByPhys groups the schemas by the physical schema they resolve to: the distinct logical
// names per physical schema, in Schemas order. A schema without a UC catalog has no physical schema.
func ownersByPhys(schemas []ResolvedSchema) map[physKey][]ResolvedSchema {
	owners := make(map[physKey][]ResolvedSchema)
	for _, rs := range schemas {
		if rs.UCCatalog == "" {
			continue
		}
		k := physKey{rs.UCCatalog, rs.UCSchema}
		if !slices.ContainsFunc(owners[k], func(o ResolvedSchema) bool { return o.LogicalSchema == rs.LogicalSchema }) {
			owners[k] = append(owners[k], rs)
		}
	}
	return owners
}

// sharedPhysKeys returns, sorted by catalog then schema, the physical schemas that more than one
// logical schema resolves to.
func sharedPhysKeys(owners map[physKey][]ResolvedSchema) []physKey {
	keys := make([]physKey, 0, len(owners))
	for k, o := range owners {
		if len(o) > 1 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].catalog != keys[j].catalog {
			return keys[i].catalog < keys[j].catalog
		}
		return keys[i].schema < keys[j].schema
	})
	return keys
}

// indexDefinedTables fills definedLoc for check 14.
func (v *validation) indexDefinedTables() {
	for _, t := range v.config.Tables {
		if t.Name == "" || t.ResolvedCatalog == "" || t.ResolvedSchema == "" {
			continue
		}
		key := tableKey(t, t.Name)
		if _, ok := v.definedLoc[key]; !ok {
			v.definedLoc[key] = tableLoc(t)
		}
	}
}

// Check 17: resolved UC catalog/schema names must be a valid UC name
// (UCNameError) and lowercase; each name is reported once. Both the schema list and the tables' resolved
// names are checked. An empty name is left to check 22.
func (v *validation) checkUCNames() {
	namesSeen := make(map[string]bool)
	check := func(kind, name, loc string) {
		if name == "" || namesSeen[joinKey(kind, name)] {
			return
		}
		if rule := UCNameError(name); rule != "" {
			namesSeen[joinKey(kind, name)] = true
			v.errorf(loc, "%s %q %s", kind, name, rule)
			return
		}
		summary := fmt.Sprintf("%s %q must be lowercase: Unity Catalog stores %s names in lowercase", kind, name, kind)
		if appendNotLower(&v.diags, name, summary, loc) {
			namesSeen[joinKey(kind, name)] = true
		}
	}
	for _, rs := range v.config.Schemas {
		check("catalog", rs.UCCatalog, fmt.Sprintf("catalog:%s", rs.LogicalCatalog))
		check("schema", rs.UCSchema, fmt.Sprintf("schema:%s.%s", rs.LogicalCatalog, rs.LogicalSchema))
	}
	for _, t := range v.config.Tables {
		loc := tableLoc(t)
		check("catalog", t.ResolvedCatalog, loc)
		check("schema", t.ResolvedSchema, loc)
	}
}

// checkTable runs the per-table checks, then the per-column checks.
func (v *validation) checkTable(table Table) {
	loc := tableLoc(table)

	v.checkTableNames(table, loc)
	v.checkSchemaRef(table, loc)
	v.checkTableDuplicates(table, loc)
	v.checkTableRenameTargets(table, loc)

	// Check 11: dots/whitespace in table name
	if table.Name != "" && HasDotOrSpace(table.Name) {
		v.warnf(loc, "table %q: name contains a dot or whitespace", table.Name)
	}

	// Columns are required: an empty list would drop every column of an existing table
	if !table.Drop && len(table.Columns) == 0 {
		v.errorf(loc, "table %q: columns is required", table.Name)
	}

	v.checkClustering(table, loc)

	// Check 24: drop: true cannot be combined with rename_from
	if table.Drop && table.RenameFrom != "" {
		v.errorf(loc, "table %q: drop: true cannot be combined with rename_from (remove rename_from)", table.Name)
	}

	v.checkLayoutColumns(table, loc)
	v.checkColumns(table, loc)
}

// Checks 5, 9, 8, 16: table name and rename_from basics.
func (v *validation) checkTableNames(table Table, loc string) {
	// Check 5: table.name required
	if table.Name == "" {
		v.errorf(loc, "table name is required")
	}

	// Check 9: rename_from with empty name
	if table.RenameFrom != "" && table.Name == "" {
		v.errorf(loc, "rename_from set but table name is empty")
	}

	// Check 8: rename_from == name
	if table.RenameFrom != "" && table.RenameFrom == table.Name {
		v.warnf(loc, "table rename_from %q is same as name; no rename will occur", table.RenameFrom)
	}

	// Check 16: UC stores table names in lowercase
	appendNotLower(&v.diags, table.Name,
		fmt.Sprintf("table name %q must be lowercase: Unity Catalog stores table names in lowercase", table.Name), loc)
	appendNotLower(&v.diags, table.RenameFrom,
		fmt.Sprintf("table %q: rename_from %q must be lowercase: Unity Catalog stores table names in lowercase", table.Name, table.RenameFrom), loc)
}

// Check 1: unknown schema ref
func (v *validation) checkSchemaRef(table Table, loc string) {
	if _, ok := v.config.SchemaByLogical(table.Schema); !ok {
		v.errorf(loc, "unknown schema ref %q: not defined in any catalog config", table.Schema)
	}
}

// Checks 2 and 12: duplicate resolved tables and duplicate rename_from.
func (v *validation) checkTableDuplicates(table Table, loc string) {
	// Check 2: duplicate resolved table (only for resolvable tables)
	if table.ResolvedCatalog != "" && table.ResolvedSchema != "" {
		key := tableKey(table, table.Name)
		if existing, ok := v.resolvedSeen[key]; ok {
			v.errorf(loc, "duplicate table %q in %s.%s: also defined in %s", table.Name, table.ResolvedCatalog, table.ResolvedSchema, existing)
		} else {
			v.resolvedSeen[key] = loc
		}
	}

	// Check 12: duplicate rename_from within one resolved schema
	if table.RenameFrom != "" && table.Name != "" && table.ResolvedCatalog != "" && table.ResolvedSchema != "" {
		key := tableKey(table, table.RenameFrom)
		if first, ok := v.renameSeen[key]; ok {
			v.errorf(loc, "rename_from %q is used by both table %q and table %q", table.RenameFrom, first, table.Name)
		} else {
			v.renameSeen[key] = table.Name
		}
	}
}

// Check 14: rename_from refers to a table that is also defined (swap, chain)
func (v *validation) checkTableRenameTargets(table Table, loc string) {
	if table.RenameFrom != "" && table.RenameFrom != table.Name && table.Name != "" && table.ResolvedCatalog != "" && table.ResolvedSchema != "" {
		key := tableKey(table, table.RenameFrom)
		if other, ok := v.definedLoc[key]; ok {
			v.errorf(loc, "table %q: rename_from %q refers to a table that is also defined in %s; remove one of them", table.Name, table.RenameFrom, other)
		}
	}
}

// Checks 3, 4, 18: clustering and partitioning blocks.
func (v *validation) checkClustering(table Table, loc string) {
	// Check 3: clustering XOR partitioning
	if table.Clustering != nil && table.Partitioning != nil {
		v.errorf(loc, "table %q: clustering and partitioning are mutually exclusive", table.Name)
	}

	// Check 4: clustering.auto XOR clustering.columns
	if table.Clustering != nil && table.Clustering.Auto && len(table.Clustering.Columns) > 0 {
		v.errorf(loc, "table %q: clustering.auto and clustering.columns are mutually exclusive", table.Name)
	}

	// Check 18: empty clustering block
	if table.Clustering != nil && !table.Clustering.Auto && len(table.Clustering.Columns) == 0 {
		v.errorf(loc, "table %q: clustering must set columns or auto: true (to disable clustering, remove the block)", table.Name)
	}
}

// Check 25: clustering/partitioning columns must be defined in columns
func (v *validation) checkLayoutColumns(table Table, loc string) {
	if table.Drop {
		return
	}
	defined := make(map[string]bool, len(table.Columns))
	for _, col := range table.Columns {
		defined[FoldName(col.Name)] = true
	}
	var clusterCols, partCols []string
	if table.Clustering != nil {
		clusterCols = table.Clustering.Columns
	}
	if table.Partitioning != nil {
		partCols = table.Partitioning.Columns
	}
	for _, kc := range []struct {
		kind string
		cols []string
	}{{"clustering", clusterCols}, {"partitioning", partCols}} {
		for _, c := range kc.cols {
			if !defined[FoldName(c)] {
				v.errorf(loc, "table %q: %s column %q is not defined in columns", table.Name, kc.kind, c)
			}
		}
	}
}

// columnChecks is the per-table state of the column checks.
type columnChecks struct {
	renamedCols   map[string]bool   // columns that are the target of a real rename (check 19)
	colRenameSeen map[string]string // rename source -> first column (check 19)
	colSeen       map[string]bool   // lowercase names seen (check 15)
}

// checkColumns runs the per-column checks of one table.
func (v *validation) checkColumns(table Table, loc string) {
	// Column renames per table for check 19 (case-only renames are not renames)
	cc := &columnChecks{
		renamedCols:   make(map[string]bool, len(table.Columns)),
		colRenameSeen: make(map[string]string, len(table.Columns)),
		colSeen:       make(map[string]bool, len(table.Columns)),
	}
	for _, col := range table.Columns {
		if isRealRename(col) {
			cc.renamedCols[FoldName(col.Name)] = true
		}
	}
	for _, col := range table.Columns {
		v.checkColumn(table, col, loc, cc)
	}
}

// checkColumn runs checks 6, 13, 15, 7, 10, 19 on one column.
func (v *validation) checkColumn(table Table, col Column, loc string, cc *columnChecks) {
	// Check 6: column.name required
	if col.Name == "" {
		v.errorf(loc, "table %q: column name is required", table.Name)
	}

	// Check 13: dots/whitespace in column name
	if col.Name != "" && HasDotOrSpace(col.Name) {
		v.warnf(loc, "table %q, column %q: name contains a dot or whitespace", table.Name, col.Name)
	}

	// Check 15: duplicate column name (case-insensitive)
	if col.Name != "" {
		lower := FoldName(col.Name)
		if cc.colSeen[lower] {
			v.errorf(loc, "table %q: duplicate column %q (column names are case-insensitive)", table.Name, col.Name)
		}
		cc.colSeen[lower] = true
	}

	v.checkColumnType(table, col, loc)

	// Check 10: column rename_from == column name
	if col.RenameFrom != "" && SameName(col.RenameFrom, col.Name) {
		v.warnf(loc, "table %q, column %q: rename_from is same as name; no rename will occur", table.Name, col.Name)
	}

	v.checkColumnRename(table, col, loc, cc)
}

// Check 7: valid column type
func (v *validation) checkColumnType(table Table, col Column, loc string) {
	if msg := ColumnTypeError(col.Type); msg != "" {
		v.errorf(loc, "table %q, column %q: %s", table.Name, col.Name, msg)
	}
}

// Check 19: column rename_from conflicts (duplicate source, swap, chain)
func (v *validation) checkColumnRename(table Table, col Column, loc string, cc *columnChecks) {
	if !isRealRename(col) {
		return
	}
	from := FoldName(col.RenameFrom)
	if first, ok := cc.colRenameSeen[from]; ok {
		v.errorf(loc, "table %q: rename_from %q is used by both column %q and column %q", table.Name, col.RenameFrom, first, col.Name)
	} else {
		cc.colRenameSeen[from] = col.Name
	}
	if cc.renamedCols[from] {
		v.errorf(loc, "table %q: column %q: rename_from %q refers to a column which is also renamed; chain and swap renames are not supported", table.Name, col.Name, col.RenameFrom)
	}
}
