package pull

import (
	"fmt"
	"path/filepath"

	"github.com/ninilich/ucat/internal/config"
)

// Index answers which project definitions a live table collides with.
type Index struct {
	defined      map[[2]string]string // (logical schema, table) -> defining file
	renameSource map[[2]string]string // (logical schema, rename_from source) -> new table name
	tableOfFile  map[string]string    // defining file -> table name
}

// NewIndex indexes the tables already defined in the project, keyed by (logical schema, name):
// they may live in any file. A rename_from source is still live until apply; pulling it
// would create a second definition.
func NewIndex(tables []config.Table) *Index {
	idx := &Index{
		defined:      make(map[[2]string]string, len(tables)),
		renameSource: make(map[[2]string]string),
		tableOfFile:  make(map[string]string, len(tables)),
	}
	for _, t := range tables {
		idx.defined[[2]string{t.Schema, t.Name}] = t.SourceFile
		idx.tableOfFile[t.SourceFile] = t.Name
		if t.RenameFrom != "" && t.RenameFrom != t.Name {
			idx.renameSource[[2]string{t.Schema, t.RenameFrom}] = t.Name
		}
	}
	return idx
}

// Target says where pull writes one live table, or why it does not.
type Target struct {
	// SkipReason is not empty when the table is not written; it is the text after "~ skipped  ".
	SkipReason string
	RelPath    string // display form and the key of the defining file: always "/" (config.SlashPath)
	FilePath   string // OS form of the path
	Verb       string // "created" or "updated"
	InPlace    bool   // an existing defining file is overwritten (--force): its own name is kept
}

// Decide chooses the file for the live table `name` of the logical schema: the canonical
// tables/<schema>/<table>.yml, or, with force, the file that already defines the table.
// A table is skipped when it is the rename_from source of another one, is defined (and force
// is off), when the canonical path holds the definition of another table, or when that file
// exists (and force is off). exists reports whether a path (OS form) exists.
func (i *Index) Decide(root, tablesDir, schema, name string, force bool, exists func(path string) bool) Target {
	relPath := config.TableFile(tablesDir, schema, name)
	t := Target{
		RelPath:  relPath,
		FilePath: filepath.Join(root, filepath.FromSlash(relPath)),
		Verb:     "created",
	}

	if newName, ok := i.renameSource[[2]string{schema, name}]; ok {
		t.SkipReason = fmt.Sprintf("%s.%s (rename_from source of %q, rename not applied yet)", schema, name, newName)
		return t
	}

	if src, ok := i.defined[[2]string{schema, name}]; ok {
		if !force {
			t.SkipReason = fmt.Sprintf("%s (defined in %s)", src, src)
			return t
		}
		// Overwrite the defining file in place. src is SourceFile: relative to the project
		// root with "/", so normally it is joined with root. The IsAbs branch is kept on
		// purpose: config.relPath returns an absolute path when filepath.Rel fails (for
		// example, another volume on Windows), and such a path must not be joined with root.
		t.RelPath = config.SlashPath(src)
		t.FilePath = filepath.FromSlash(t.RelPath)
		if !filepath.IsAbs(t.FilePath) {
			t.FilePath = filepath.Join(root, t.FilePath)
		}
		t.Verb = "updated"
		t.InPlace = true
		return t
	}

	if other, ok := i.tableOfFile[relPath]; ok {
		// The canonical path holds the definition of another table; never overwrite it.
		t.SkipReason = fmt.Sprintf("%s.%s (%s defines table %q)", schema, name, relPath, other)
	} else if !force && exists(t.FilePath) {
		t.SkipReason = fmt.Sprintf("%s (already exists, use --force)", relPath)
	}
	return t
}
