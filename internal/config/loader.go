package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/ninilich/ucat/internal/diag"
	"gopkg.in/yaml.v3"
)

// FindProjectRoot walks up from startDir, looking for ucat.yml.
// Returns the directory containing ucat.yml, or an error if not found.
func FindProjectRoot(startDir string) (string, error) {
	current := startDir
	for {
		if _, err := os.Stat(filepath.Join(current, ProjectFile)); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("%s not found: searched from %s to filesystem root", ProjectFile, startDir)
		}
		current = parent
	}
}

var (
	typeErrLineRe  = regexp.MustCompile(`^line (\d+): (.*)$`)
	unknownFieldRe = regexp.MustCompile(`^field (.+) not found in type (\S+)$`)
	syntaxErrRe    = regexp.MustCompile(`(?s)^yaml: line (\d+): (.*)$`)
)

// relPath returns path relative to the project root, the form every diagnostic shows;
// it falls back to path itself when no relative form exists.
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return SlashPath(path)
	}
	return SlashPath(rel)
}

// SlashPath returns p with every backslash replaced by a forward slash, on any OS.
// Paths in diagnostics, Location, SourceFile and pull output always use "/" (as in the docs),
// so the same project gives the same text on Windows, Linux and macOS, and keys built from
// such paths compare equal. filepath.ToSlash is not enough: it does nothing on Linux/macOS.
func SlashPath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// syntaxDiag turns a YAML syntax error (not a *yaml.TypeError) into a diagnostic.
// When yaml.v3 reports "yaml: line N: <reason>", the line moves to Location as <path>:<N>
// and the reason alone is the summary, like every other load error. yaml.v3 omits the line
// for some errors (e.g. on line 1, control characters); then the summary names the file and
// Location is the file without a line.
func syntaxDiag(path string, err error) diag.Diagnostic {
	text := strings.ReplaceAll(err.Error(), "\r", "")
	if m := syntaxErrRe.FindStringSubmatch(text); m != nil {
		return diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "failed to parse: " + m[2],
			Location: path + ":" + m[1],
		}
	}
	return diag.Diagnostic{
		Severity: diag.Error,
		Summary:  fmt.Sprintf("failed to parse %s: %s", path, text),
		Location: path,
	}
}

// ExtensionKeyPrefix starts the keys that ucat ignores instead of rejecting as unknown fields: "x-"
// keys are free-form notes and YAML anchors (x-defaults: &d ...) of the user, kept out of the format.
const ExtensionKeyPrefix = "x-"

// UnknownFieldHint is appended to the error about an unknown field: the usual cause besides a typo
// is a file written for a newer ucat.
const UnknownFieldHint = "if the file was written for a newer ucat, upgrade ucat"

// extensionTypes are the types whose mappings accept x- keys: the top level of ucat.yml, a catalog
// (in catalogs/ and in a target), a target, a table and a column. The names are those yaml.v3 prints.
var extensionTypes = map[string]bool{
	reflect.TypeFor[ProjectConfig]().String(): true,
	reflect.TypeFor[CatalogConfig]().String(): true,
	reflect.TypeFor[TargetConfig]().String():  true,
	reflect.TypeFor[Table]().String():         true,
	reflect.TypeFor[Column]().String():        true,
}

// isExtensionKey reports whether an unknown key of a mapping of the Go type typeName is an x- key
// that is allowed there.
func isExtensionKey(key, typeName string) bool {
	return strings.HasPrefix(key, ExtensionKeyPrefix) && extensionTypes[typeName]
}

// decodeStrict decodes YAML into v, rejecting unknown fields (except x- keys, see ExtensionKeyPrefix).
// path is the display path (relative to the project root); each yaml type error becomes one
// Error diagnostic located at <path>:<line>. A second document in the file ("---") is not read:
// it is reported as a warning.
func decodeStrict(path string, data []byte, v any) diag.Diagnostics {
	var diags diag.Diagnostics

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(v)
	if err == nil || errors.Is(err, io.EOF) {
		if err == nil {
			diags = append(diags, extraDocumentWarning(path, data, dec)...)
		}
		return diags
	}

	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		diags.Append(syntaxDiag(path, err))
		return diags
	}

	for _, msg := range typeErr.Errors {
		m := typeErrLineRe.FindStringSubmatch(msg)
		if m == nil {
			diags.Append(diag.Diagnostic{Severity: diag.Error, Summary: msg, Location: path})
			continue
		}
		summary := m[2]
		if f := unknownFieldRe.FindStringSubmatch(summary); f != nil {
			if isExtensionKey(f[1], f[2]) {
				continue
			}
			summary = fmt.Sprintf("field %q is not supported; %s", f[1], UnknownFieldHint)
		}
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  summary,
			Location: path + ":" + m[1],
		})
	}
	if !diags.HasError() {
		diags = append(diags, extraDocumentWarning(path, data, dec)...)
	}
	return diags
}

// separatorLine is the 1-based line of the "---" that starts the second document: the first "---" line
// after the content of the first document (a "---" before any content opens the first document itself).
// 0 when there is none.
func separatorLine(data []byte) int {
	content := false
	for i, l := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		if l == "---" || strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "---\t") {
			if content {
				return i + 1
			}
			continue
		}
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			content = true
		}
	}
	return 0
}

// extraDocumentWarning is the warning for a second YAML document ("---") after the one dec has
// decoded: only the first document of a file is read. An empty trailing document (a lone "---") is
// nothing to warn about.
func extraDocumentWarning(path string, data []byte, dec *yaml.Decoder) diag.Diagnostics {
	var next yaml.Node
	err := dec.Decode(&next)
	if errors.Is(err, io.EOF) {
		return nil
	}
	loc := path
	line := next.Line
	if err != nil {
		line = separatorLine(data) // no node for a document that does not parse
	}
	if line > 0 {
		loc = fmt.Sprintf("%s:%d", path, line)
	}
	if len(next.Content) == 1 && next.Content[0].Kind == yaml.ScalarNode && next.Content[0].Tag == "!!null" && next.Content[0].Value == "" {
		return nil
	}
	return diag.Diagnostics{{Severity: diag.Warning, Summary: "the file has a second YAML document (after ---): it is ignored, only the first document is read", Location: loc}}
}

// readYAML reads path and decodes it strictly into v. rel is the display path (relative to the
// project root, "/"-separated); data is the raw file for callers that need it after decoding.
// A read error and every decode error come back as Error diagnostics: the read error as
// "failed to read <rel>: <reason>", so every YAML file reports an unreadable file the same way.
func readYAML(root, path string, v any) (rel string, data []byte, diags diag.Diagnostics) {
	rel = relPath(root, path)
	data, err := os.ReadFile(path)
	if err != nil {
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("failed to read %s: %s", rel, diag.ErrText(err)),
			Location: rel,
		})
		return rel, nil, diags
	}
	return rel, data, decodeStrict(rel, data, v)
}

var driveLetterRe = regexp.MustCompile(`^[A-Za-z]:`)

// isAbsolutePath reports whether p is an absolute path in any OS spelling, independent of the
// OS ucat runs on: a leading "/" or "\\" (rooted and UNC paths), a drive letter ("C:\x", "C:/x", "C:x"),
// or whatever filepath.IsAbs accepts here.
func isAbsolutePath(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") ||
		driveLetterRe.MatchString(p) || filepath.IsAbs(p)
}

// keyLine returns the line of a key in YAML data, or 0 when it is not found. The path is the keys
// from the top-level mapping down: keyLine(data, "catalogs", "main", "schemas") is the line of "schemas".
func keyLine(data []byte, path ...string) int {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return 0
	}
	node := doc.Content[0]
	line := 0
	for _, key := range path {
		if node.Kind != yaml.MappingNode {
			return 0
		}
		next := (*yaml.Node)(nil)
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				line, next = node.Content[i].Line, node.Content[i+1]
				break
			}
		}
		if next == nil {
			return 0
		}
		node = next
	}
	return line
}

// nameDiag is the Error for a logical or target name that is not a file-safe name (see IsFileSafeName),
// located at the line of its key in the file; no diagnostic (ok false) for a valid name.
// The empty name is reported only when emptyIsError (an empty catalog name is check 20 of the validator).
func nameDiag(kind, name, rel string, line int, emptyIsError bool) (d diag.Diagnostic, ok bool) {
	if IsFileSafeName(name) || (name == "" && !emptyIsError) {
		return d, false
	}
	loc := rel
	if line > 0 {
		loc = fmt.Sprintf("%s:%d", rel, line)
	}
	summary := fmt.Sprintf("%s name is empty", kind)
	if name != "" {
		summary = fmt.Sprintf("%s name %q %s", kind, name, FileSafeNameRule)
	}
	return diag.Diagnostic{Severity: diag.Error, Summary: summary, Location: loc}, true
}

// schemaNameDiags checks the logical schema names (keys of schemas:) of one catalog; names sorted.
// path is the keys down to the catalog mapping in data.
func schemaNameDiags(rel string, data []byte, schemas map[string]SchemaConfig, path ...string) diag.Diagnostics {
	var diags diag.Diagnostics
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		line := keyLine(data, append(append([]string(nil), path...), "schemas", name)...)
		if d, bad := nameDiag("logical schema", name, rel, line, true); bad {
			diags.Append(d)
		}
	}
	return diags
}

// LoadProject loads and unmarshals ucat.yml from the given root directory.
func LoadProject(root string) (ProjectConfig, diag.Diagnostics) {
	var cfg ProjectConfig

	rel, data, diags := readYAML(root, filepath.Join(root, ProjectFile), &cfg)
	if diags.HasError() {
		return cfg, diags
	}

	if isAbsolutePath(cfg.TablesDir) {
		loc := rel
		if line := keyLine(data, "tables_dir"); line > 0 {
			loc = fmt.Sprintf("%s:%d", rel, line)
		}
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("tables_dir must be relative to the project root (got \"%s\")", cfg.TablesDir),
			Location: loc,
		})
		return cfg, diags
	}
	if cfg.TablesDir == "" {
		cfg.TablesDir = DefaultTablesDir
	}
	// "tables\prod" and "tables/prod" are the same directory (Windows); keep one spelling.
	cfg.TablesDir = SlashPath(cfg.TablesDir)

	return cfg, diags
}

// LoadCatalogs scans catalogs/*.yml under root and returns all CatalogConfig values.
// The name: field inside each file is authoritative (not the filename).
func LoadCatalogs(root string) ([]CatalogConfig, diag.Diagnostics) {
	var diags diag.Diagnostics
	var catalogs []CatalogConfig
	seen := make(map[string]string) // catalog name -> source file

	catDir := filepath.Join(root, CatalogsDir)
	entries, err := os.ReadDir(catDir) // sorted by filename
	if err != nil {
		if os.IsNotExist(err) {
			return catalogs, diags
		}
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("failed to read %s/: %s", CatalogsDir, diag.ErrText(err)),
			Location: relPath(root, catDir),
		})
		return catalogs, diags
	}

	var matches []string
	for _, e := range entries {
		if e.IsDir() || !hasYAMLExt(e.Name()) {
			continue
		}
		matches = append(matches, filepath.Join(catDir, e.Name()))
	}

	for _, path := range matches {
		var cat CatalogConfig
		rel, data, d := readYAML(root, path, &cat)
		diags = append(diags, d...) // errors and warnings (a second document)
		if d.HasError() {
			continue
		}

		// Warn if filename base doesn't match the name: field
		base := strings.TrimSuffix(filepath.Base(path), YAMLExt)
		if cat.Name != base {
			diags.Append(diag.Diagnostic{
				Severity: diag.Warning,
				Summary:  fmt.Sprintf("catalog name %q does not match filename %q; using name field", cat.Name, base),
				Location: rel,
			})
		}

		cat.SourceFile = rel

		nameDiags := schemaNameDiags(rel, data, cat.Schemas)
		if d, bad := nameDiag("logical catalog", cat.Name, rel, keyLine(data, "name"), false); bad {
			nameDiags = append(diag.Diagnostics{d}, nameDiags...)
		}
		if nameDiags.HasError() {
			diags = append(diags, nameDiags...)
			continue
		}

		if existing, ok := seen[cat.Name]; ok {
			diags.Append(diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("catalog %q is defined in both %s and %s", cat.Name, existing, rel),
				Location: rel,
			})
			continue
		}
		seen[cat.Name] = rel

		catalogs = append(catalogs, cat)
	}

	return catalogs, diags
}

// LoadTarget loads and unmarshals targets/<targetName>.yml from the given root directory.
// The target name and the logical catalog and schema names declared in the file must be
// file-safe names (see IsFileSafeName); a bad target name is reported before any file is read.
func LoadTarget(root, targetName string) (TargetConfig, diag.Diagnostics) {
	var cfg TargetConfig
	if d, bad := nameDiag("target", targetName, "", 0, true); bad {
		return cfg, diag.Diagnostics{d}
	}

	rel, data, diags := readYAML(root, filepath.Join(root, filepath.FromSlash(TargetFile(targetName))), &cfg)
	if diags.HasError() {
		return cfg, diags
	}

	catalogs := make([]string, 0, len(cfg.Catalogs))
	for name := range cfg.Catalogs {
		catalogs = append(catalogs, name)
	}
	sort.Strings(catalogs)
	for _, name := range catalogs {
		if d, bad := nameDiag("logical catalog", name, rel, keyLine(data, "catalogs", name), true); bad {
			diags.Append(d)
		}
		diags = append(diags, schemaNameDiags(rel, data, cfg.Catalogs[name].Schemas, "catalogs", name)...)
	}
	return cfg, diags
}

// LoadTables recursively discovers all *.yml files under the tables directory
// and unmarshals each as a Table.
func LoadTables(root string, project ProjectConfig) ([]Table, diag.Diagnostics) {
	var diags diag.Diagnostics
	var tables []Table

	tablesDir := filepath.Join(root, project.TablesDir)

	// Track logical duplicates: (schema, name) -> source file
	seen := make(map[string]string)

	err := filepath.WalkDir(tablesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Directory doesn't exist - not an error
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}
			diags.Append(diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("error walking tables dir: %s", diag.ErrText(err)),
				Location: relPath(root, tablesDir),
			})
			return nil
		}

		if d.IsDir() || !hasYAMLExt(path) {
			return nil
		}

		var table Table
		rel, _, dd := readYAML(root, path, &table)
		diags = append(diags, dd...) // errors and warnings (a second document)
		if dd.HasError() {
			return nil
		}

		// Set SourceFile as relative path from project root
		table.SourceFile = rel

		// Check for logical duplicate (schema, name)
		key := joinKey(table.Schema, table.Name)
		if existing, ok := seen[key]; ok {
			diags.Append(diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("duplicate table %q in schema %q: also defined in %s", table.Name, table.Schema, existing),
				Location: rel,
			})
			return nil
		}
		seen[key] = rel

		tables = append(tables, table)
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		diags.Append(diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("failed to walk tables directory: %s", diag.ErrText(err)),
			Location: relPath(root, tablesDir),
		})
	}

	return tables, diags
}

// EmptyTablesWarning explains why no tables were defined: a missing tables directory, or a
// directory without *.yml files (with a hint when *.yaml files, which are ignored, exist).
// The caller invokes it only when LoadTables returned no tables and no errors.
func EmptyTablesWarning(root string, project ProjectConfig) diag.Diagnostics {
	tablesDir := filepath.Join(root, project.TablesDir)
	if _, err := os.Stat(tablesDir); err != nil && os.IsNotExist(err) {
		return diag.Diagnostics{{Severity: diag.Warning,
			Summary: fmt.Sprintf("tables_dir %q not found (no tables are defined)", project.TablesDir)}}
	}

	yamlFiles := 0
	_ = filepath.WalkDir(tablesDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && isIgnoredYAMLName(d.Name()) {
			yamlFiles++
		}
		return nil
	})

	summary := fmt.Sprintf("no table files (*.yml) found in %q", project.TablesDir)
	if yamlFiles > 0 {
		summary += fmt.Sprintf("; found %d file(s) named *.yaml or with an upper-case extension (*.YML), which are ignored: rename them to .yml", yamlFiles)
	}
	return diag.Diagnostics{{Severity: diag.Warning, Summary: summary}}
}

// maxListedIgnored is the number of paths IgnoredYAMLWarning names; the rest is counted.
const maxListedIgnored = 5

// isIgnoredYAMLName reports whether a file name looks like a ucat YAML file that ucat does not read:
// the extension is .yaml, or .yml/.yaml in any other case (.YML, .Yaml). Only a lower-case ".yml" is read.
func isIgnoredYAMLName(name string) bool {
	ext := filepath.Ext(name)
	lower := strings.ToLower(ext)
	return (lower == ".yaml" || lower == ".yml") && ext != YAMLExt
}

// ignoredManifestNames lists the files in root that are the project manifest under a name ucat does not
// read (ucat.yaml, UCAT.YML, ...).
func ignoredManifestNames(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !e.IsDir() && strings.EqualFold(base, "ucat") && isIgnoredYAMLName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

// IgnoredYAMLWarning warns about files that ucat does not read because of their extension (only a
// lower-case .yml is read; see isIgnoredYAMLName): ucat.yaml, UCAT.YML and the like next to ucat.yml, and
// such files in catalogs/, targets/ and, when withTables is set, anywhere under the tables directory. It is one warning with the count and the first paths (sorted by location: root, catalogs,
// targets, tables); no warning when there is none. The caller passes withTables=false when the tables
// are not loaded or when EmptyTablesWarning already reports the files of the tables directory.
func IgnoredYAMLWarning(root string, project ProjectConfig, withTables bool) diag.Diagnostics {
	var found []string
	seen := make(map[string]bool)
	add := func(path string) {
		rel := relPath(root, path)
		if !seen[rel] {
			seen[rel] = true
			found = append(found, rel)
		}
	}
	for _, name := range ignoredManifestNames(root) {
		add(filepath.Join(root, name))
	}
	dirs := []string{filepath.Join(root, CatalogsDir), filepath.Join(root, TargetsDir)}
	if withTables {
		dirs = append(dirs, filepath.Join(root, project.TablesDir))
	}
	for i, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return nil // a missing or unreadable directory has no files to report
			case d.IsDir():
				if i < 2 && path != dir {
					return filepath.SkipDir // catalogs/ and targets/ are flat
				}
			case isIgnoredYAMLName(d.Name()):
				add(path)
			}
			return nil
		})
	}
	if len(found) == 0 {
		return nil
	}
	listed := found
	if len(listed) > maxListedIgnored {
		listed = listed[:maxListedIgnored]
	}
	list := strings.Join(listed, ", ")
	if len(found) > len(listed) {
		list += fmt.Sprintf(", ... (%d more)", len(found)-len(listed))
	}
	return diag.Diagnostics{{Severity: diag.Warning,
		Summary: fmt.Sprintf("%d file(s) are ignored (ucat reads only files named *.yml with a lower-case extension, not *.yaml or *.YML): %s; rename them to .yml", len(found), list)}}
}

// SharedCatalogWarning warns when several targets resolve one logical catalog to the same physical UC
// catalog. For every logical catalog of catalogs (the loaded catalogs/*.yml; a key that only a target
// declares does not count) each target's UC catalog is its catalogs.<name>.uc_name, or the catalog file's
// uc_name when it sets none; a value that two or more targets end up with, whether inherited or typed
// in each target, gives one warning naming the value, the catalog and those targets. Values are compared
// with FoldName. A target file that cannot be read or parsed is left out (the loaders report it when it is
// selected), and one target alone cannot share. Targets are in file name order, catalogs sorted by name.
func SharedCatalogWarning(root string, catalogs []CatalogConfig) diag.Diagnostics {
	targets := readTargetsLenient(root)
	if len(targets) < 2 {
		return nil
	}
	sorted := append([]CatalogConfig(nil), catalogs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var diags diag.Diagnostics
	for _, cat := range sorted {
		diags = append(diags, sharedValues(cat, targets)...)
	}
	return diags
}

// namedTarget is a target file read for SharedCatalogWarning.
type namedTarget struct {
	name string
	cfg  TargetConfig
}

// readTargetsLenient reads targets/*.yml in file name order without strict checks; a file that cannot
// be read or parsed is left out.
func readTargetsLenient(root string) []namedTarget {
	entries, err := os.ReadDir(filepath.Join(root, TargetsDir))
	if err != nil {
		return nil
	}
	var targets []namedTarget
	for _, e := range entries {
		if e.IsDir() || !hasYAMLExt(e.Name()) {
			continue
		}
		var cfg TargetConfig
		data, err := os.ReadFile(filepath.Join(root, TargetsDir, e.Name()))
		if err != nil || yaml.Unmarshal(data, &cfg) != nil {
			continue
		}
		targets = append(targets, namedTarget{strings.TrimSuffix(e.Name(), YAMLExt), cfg})
	}
	return targets
}

// sharedValues returns one warning per physical catalog that two or more targets have for cat.
func sharedValues(cat CatalogConfig, targets []namedTarget) diag.Diagnostics {
	var order []string             // folded values in first-seen order
	users := map[string][]string{} // folded value -> target names
	shown := map[string]string{}   // folded value -> the spelling to show
	for _, t := range targets {
		uc := firstNonEmpty(t.cfg.Catalogs[cat.Name].UCName, cat.UCName)
		if uc == "" {
			continue
		}
		key := FoldName(uc)
		if _, seen := users[key]; !seen {
			order = append(order, key)
			shown[key] = uc
		}
		users[key] = append(users[key], t.name)
	}
	var diags diag.Diagnostics
	for _, key := range order {
		if len(users[key]) >= 2 {
			diags.Append(diag.Diagnostic{Severity: diag.Warning, Summary: fmt.Sprintf(
				"targets %s share the UC catalog %q of the logical catalog %q; give each its own catalogs.%s.uc_name in %s",
				strings.Join(users[key], ", "), shown[key], cat.Name, cat.Name, TargetFile("<name>"))})
		}
	}
	return diags
}
