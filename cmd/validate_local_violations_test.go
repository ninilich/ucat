package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- new local checks fail validate and plan before any backend call ---

const idCols = "columns:\n  - name: id\n    type: BIGINT\n"

func TestLocalViolations_FailValidateAndPlanBeforeBackend(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, tmpDir string)
		want  []string
		// planOnly skips plain validate: it does not apply target overrides until
		// validate -t reaches parity with plan.
		planOnly bool
	}{
		{
			name: "duplicate logical schema across catalogs",
			setup: func(t *testing.T, tmpDir string) {
				writeFile(t, filepath.Join(tmpDir, "catalogs/common.yml"), "name: common\nuc_name: common\nschemas:\n  silver:\n    uc_name: silver\n")
				addTableFile(t, tmpDir, "silver", "orders", "name: orders\nschema: silver\n"+idCols)
			},
			want: []string{litSilver, pathCatalogMain, "catalogs/common.yml"},
		},
		{
			name: "duplicate logical schema from target-only catalog",
			setup: func(t *testing.T, tmpDir string) {
				writeFile(t, filepath.Join(tmpDir, "targets/dev.yml"),
					"workspace:\n  host: https://dev.example.com\ncatalogs:\n  extra:\n    uc_name: extra\n    schemas:\n      silver:\n        uc_name: silver\n")
				addTableFile(t, tmpDir, "silver", "orders", "name: orders\nschema: silver\n"+idCols)
			},
			want:     []string{litSilver, litExtra, litMain},
			planOnly: true,
		},
		{
			name: "empty catalog name",
			setup: func(t *testing.T, tmpDir string) {
				writeFile(t, filepath.Join(tmpDir, "catalogs/noname.yml"), "name: \"\"\nuc_name: noname\nschemas:\n  gold:\n    uc_name: gold\n")
			},
			want: []string{"catalog name is empty"},
		},
		{
			name: "duplicate column rename_from",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders",
					"name: orders\nschema: silver\ncolumns:\n  - name: x\n    type: INT\n    rename_from: a\n  - name: y\n    type: INT\n    rename_from: a\n")
			},
			want: []string{litRenameFrom, `"x"`, `"y"`},
		},
		{
			name: "column rename swap",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders",
					"name: orders\nschema: silver\ncolumns:\n  - name: a\n    type: INT\n    rename_from: b\n  - name: b\n    type: INT\n    rename_from: a\n")
			},
			want: []string{"rename_from"},
		},
		{
			name: "column rename chain",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders",
					"name: orders\nschema: silver\ncolumns:\n  - name: a\n    type: INT\n    rename_from: b\n  - name: b\n    type: INT\n    rename_from: c\n")
			},
			want: []string{"rename_from"},
		},
		{
			name: "empty clustering",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders", "name: orders\nschema: silver\nclustering: {}\n"+idCols)
			},
			want: []string{"clustering"},
		},
		{
			name: litUnknownSchemaRef,
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "nonexistent", "orders", "name: orders\nschema: nonexistent\n"+idCols)
			},
			want: []string{"unknown schema ref"},
		},
		{
			name: "duplicate table name",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders_a", "name: orders\nschema: silver\n"+idCols)
				addTableFile(t, tmpDir, "silver", "orders_b", "name: orders\nschema: silver\n"+idCols)
			},
			want: []string{litDuplicateTable},
		},
		{
			name: "clustering plus partitioning",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders",
					"name: orders\nschema: silver\n"+idCols+"clustering:\n  columns: [id]\npartitioning:\n  columns: [id]\n")
			},
			want: []string{"mutually exclusive"},
		},
		{
			name: "unsupported column type",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders",
					"name: orders\nschema: silver\ncolumns:\n  - name: id\n    type: BADTYPE\n")
			},
			want: []string{"is not supported"},
		},
		{
			name: "columns missing",
			setup: func(t *testing.T, tmpDir string) {
				addTableFile(t, tmpDir, "silver", "orders", "name: orders\nschema: silver\n")
			},
			want: []string{"columns"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := setupTestProject(t)
			tc.setup(t, tmpDir)

			if !tc.planOnly {
				out, err := executeCmd("validate")
				require.Error(t, err, "validate out: %s", out)
				for _, w := range tc.want {
					assert.Contains(t, out+err.Error(), w)
				}
			}

			mock := newSpy(nil).forbid(t)
			useBackend(t, mock)

			out, err := executeCmd("plan", "-t", "dev")
			require.Error(t, err, "plan out: %s", out)
			for _, w := range tc.want {
				assert.Contains(t, out+err.Error(), w)
			}
		})
	}
}

func TestLocalViolations_ValidFormsPass(t *testing.T) {
	tmpDir := setupTestProject(t)
	addTableFile(t, tmpDir, "silver", "clustered", "name: clustered\nschema: silver\nclustering:\n  columns: [id]\n"+idCols)
	addTableFile(t, tmpDir, "silver", "auto", "name: auto\nschema: silver\nclustering:\n  auto: true\n"+idCols)
	addTableFile(t, tmpDir, "silver", "legacy", "name: legacy\nschema: silver\ndrop: true\n")
	addTableFile(t, tmpDir, "silver", "renamed",
		"name: renamed\nschema: silver\ncolumns:\n  - name: A\n    type: INT\n    rename_from: a\n  - name: b\n    type: INT\n    rename_from: x\n")

	out, err := executeCmd("validate")
	require.NoError(t, err, "out: %s", out)
	assert.Contains(t, out, "all local checks passed")
}
