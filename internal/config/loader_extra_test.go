package config_test

// Cases that mutation testing showed were not pinned.

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/config"
)

// A second document that is an empty string (not an empty document) is still a second document.
func TestSecondDocument_EmptyStringScalarIsReported(t *testing.T) {
	for name, tail := range map[string]string{"double quoted": "---\n\"\"\n", "single quoted": "---\n''\n"} {
		t.Run(name, func(t *testing.T) {
			_, d := loadTablesFrom(t, "name: orders\nschema: silver\n"+tail)
			assert.Len(t, warnSummaries(d), 1, d)
		})
	}
}

// A second document that is not valid YAML is reported too (the first document is read).
func TestSecondDocument_SyntaxErrorInSecondIsStillWarned(t *testing.T) {
	tables, d := loadTablesFrom(t, "name: orders\nschema: silver\n---\n[: bad\n")
	require.Len(t, tables, 1)
	assert.Equal(t, []string{secondDocWarning + " @tables/silver/orders.yml:3"}, warnSummaries(d))
	assert.False(t, d.HasError())
}

// The line of the --- is found past comments and blank lines, and a leading --- is not the second document.
func TestSecondDocument_SyntaxErrorLine(t *testing.T) {
	cases := map[string]struct {
		content string
		line    int
	}{
		"single separator":       {"name: orders\nschema: silver\n---\n[: bad\n", 3},
		"after comments":         {"# head\nname: orders\n\n# c\nschema: silver\n---\n[: bad\n", 6},
		"leading separator":      {"---\nname: orders\nschema: silver\n---\n[: bad\n", 4},
		"separator with comment": {"name: orders\nschema: silver\n--- # two\n[: bad\n", 3},
		"crlf":                   {"name: orders\r\nschema: silver\r\n---\r\n[: bad\r\n", 3},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, d := loadTablesFrom(t, tc.content)
			assert.Equal(t, []string{secondDocWarning + " @tables/silver/orders.yml:" + strconv.Itoa(tc.line)}, warnSummaries(d))
		})
	}
}

// Duplicated path: the tables directory overlaps catalogs/, so one *.yaml file is reached twice and counted once.
func TestIgnoredYAMLWarning_FileReachedTwiceIsCountedOnce(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "catalogs/a.yaml", "x")
	d := config.IgnoredYAMLWarning(root, config.ProjectConfig{TablesDir: "catalogs"}, true)
	require.Len(t, d, 1)
	assert.Equal(t, "1 file(s) are ignored (ucat reads only files named *.yml with a lower-case extension, not *.yaml or *.YML): catalogs/a.yaml; rename them to .yml", d[0].Summary)
}

// targets/ is flat, like catalogs/: a *.yaml file in a subdirectory is not reported.
func TestIgnoredYAMLWarning_TargetsDirIsFlat(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "targets/sub/nested.yaml", "x")
	assert.Empty(t, config.IgnoredYAMLWarning(root, config.ProjectConfig{TablesDir: litTables}, true))
}

// Only targets/*.yml are targets: a stray non-YAML file (even one that parses as an empty target) is not counted.
func TestSharedCatalogWarning_NonYAMLFileIsNotATarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "targets/dev.yml", litEmptyWorkspace)
	writeFile(t, root, "targets/README.md", "")
	writeFile(t, root, "targets/notes.txt", "# comment only\n")
	d := config.SharedCatalogWarning(root, []config.CatalogConfig{{Name: litMain, UCName: "base"}})
	assert.Empty(t, d)
}

const secondDocWarning = "the file has a second YAML document (after ---): it is ignored, only the first document is read"
