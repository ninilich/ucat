package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
)

// --- drop+rename_from, clustering/partitioning columns, empty catalog uc_name, physical schema uniqueness ---

func TestValidateLocal_dropWithRenameFrom(t *testing.T) {
	tests := []struct {
		name    string
		drop    bool
		rename  string
		wantErr bool
	}{
		{"drop and rename_from", true, litOld, true},
		{"drop only", true, "", false},
		{"rename_from only", false, litOld, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := resolvedTable("t")
			tbl.Drop = tt.drop
			tbl.RenameFrom = tt.rename
			tbl.SourceFile = "tables/t.yml"
			diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
			got := hasErrorContaining(diags, "drop: true cannot be combined with rename_from")
			assert.Equal(t, tt.wantErr, got, "diags: %v", diags)
			if tt.wantErr {
				assert.True(t, hasErrorContaining(diags, `table "t"`), "diags: %v", diags)
				assert.True(t, hasErrorContaining(diags, "remove rename_from"), "diags: %v", diags)
				assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
			}
		})
	}
}

func TestValidateLocal_clusteringPartitioningColumnsMustExist(t *testing.T) {
	tests := []struct {
		name       string
		drop       bool
		clustering []string
		partition  []string
		wantFrags  []string
	}{
		{name: "clustering ok", clustering: []string{"id"}},
		{name: "clustering case-insensitive", clustering: []string{"ID"}},
		{name: "partitioning ok", partition: []string{"id"}},
		{name: "partitioning case-insensitive", partition: []string{"Id"}},
		{name: "clustering missing", clustering: []string{litNope}, wantFrags: []string{`clustering column "nope" is not defined in columns`}},
		{name: "partitioning missing", partition: []string{litNope}, wantFrags: []string{`partitioning column "nope" is not defined in columns`}},
		{name: "one of two missing", clustering: []string{"id", "zzz"}, wantFrags: []string{`clustering column "zzz"`}},
		{name: "drop true skipped", drop: true, clustering: []string{litNope}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := resolvedTable("t")
			tbl.Drop = tt.drop
			if tt.clustering != nil {
				tbl.Clustering = &config.Clustering{Columns: tt.clustering}
			}
			if tt.partition != nil {
				tbl.Partitioning = &config.Partitioning{Columns: tt.partition}
			}
			diags := config.ValidateLocal(resolvedConfig([]config.Table{tbl}))
			assert.Equal(t, len(tt.wantFrags), errorCount(diags), "diags: %v", diags)
			for _, f := range tt.wantFrags {
				assert.True(t, hasErrorContaining(diags, f), "missing %q in %v", f, diags)
				assert.True(t, hasErrorContaining(diags, `table "t"`), "diags: %v", diags)
			}
		})
	}
}

func TestValidateLocal_emptyCatalogUCName(t *testing.T) {
	cfg := schemaCfg(
		rs("main", "silver", "", "silver", "catalogs/main.yml"),
		rs("main", "gold", "", "gold", "catalogs/main.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "reported once per catalog, diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, `catalog "main": uc_name is not set`), "diags: %v", diags)
	for _, d := range diags {
		assert.Contains(t, d.Location, "catalogs/main.yml")
	}
}

func TestValidateLocal_emptyCatalogUCNameTwoCatalogs(t *testing.T) {
	cfg := schemaCfg(
		rs("a", "s1", "", "s1", "catalogs/a.yml"),
		rs("b", "s2", "", "s2", "catalogs/b.yml"),
		rs("c", "s3", "c", "s3", "catalogs/c.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 2, errorCount(diags), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, `catalog "a"`))
	assert.True(t, hasErrorContaining(diags, `catalog "b"`))
	assert.False(t, hasErrorContaining(diags, `catalog "c"`))
}

func TestValidateLocal_emptySchemaUCNameAllowed(t *testing.T) {
	cfg := schemaCfg(rs("main", "silver", "prod", "", "catalogs/main.yml"))
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 0, errorCount(diags), "diags: %v", diags)
}

func TestValidateLocal_physicalSchemaCollision(t *testing.T) {
	cfg := schemaCfg(
		rs("x", "a", "main", "silver", "catalogs/x.yml"),
		rs("y", "b", "main", "silver", "catalogs/y.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	for _, frag := range []string{`"a"`, `"b"`, "catalogs/x.yml", "catalogs/y.yml", "main.silver", "both resolve to physical schema"} {
		assert.True(t, hasErrorContaining(diags, frag), "missing %q in %v", frag, diags)
	}
}

func TestValidateLocal_physicalSchemaCollisionThreeOwners(t *testing.T) {
	cfg := schemaCfg(
		rs("x", "a", "main", "silver", "catalogs/x.yml"),
		rs("y", "b", "main", "silver", "catalogs/y.yml"),
		rs("z", "c", "main", "silver", "catalogs/z.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	for _, frag := range []string{`"a"`, `"b"`, `"c"`, "catalogs/z.yml", "main.silver"} {
		assert.True(t, hasErrorContaining(diags, frag), "missing %q in %v", frag, diags)
	}
}

func TestValidateLocal_physicalSchemaCollisionDeterministic(t *testing.T) {
	cfg := schemaCfg(
		rs("x", "a", "zcat", "s", "catalogs/x.yml"),
		rs("y", "b", "zcat", "s", "catalogs/y.yml"),
		rs("p", "c", "acat", "s", "catalogs/p.yml"),
		rs("q", "d", "acat", "s", "catalogs/q.yml"),
	)
	first := config.ValidateLocal(cfg)
	assert.Equal(t, 2, errorCount(first), "diags: %v", first)
	for i := 0; i < 5; i++ {
		assert.Equal(t, first, config.ValidateLocal(cfg))
	}
	// pairs are sorted: acat.s before zcat.s
	var errs []string
	for _, d := range first {
		errs = append(errs, d.Summary)
	}
	if assert.Len(t, errs, 2) {
		assert.Contains(t, errs[0], "acat.s")
		assert.Contains(t, errs[1], "zcat.s")
	}
}

func TestValidateLocal_sameSchemaNameInDifferentPhysicalCatalogsAllowed(t *testing.T) {
	// Same uc schema name in different physical catalogs; logical names differ.
	cfg := schemaCfg(
		rs("main", "silver", "main", "silver", "catalogs/main.yml"),
		rs("common", "silver_c", "common", "silver", "catalogs/common.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 0, errorCount(diags), "diags: %v", diags)
}

func TestValidateLocal_targetCollapsingCatalogs(t *testing.T) {
	// A target maps two logical catalogs to the same physical catalog; schemas keep their uc names.
	cfg := schemaCfg(
		rs("bronze", "raw", "dev", "data", "catalogs/bronze.yml"),
		rs("silver", "clean", "dev", "data", "catalogs/silver.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	assert.True(t, hasErrorContaining(diags, "dev.data"), "diags: %v", diags)
}

func TestValidateLocal_physicalCollisionNotDoubleReportedWithDuplicateLogical(t *testing.T) {
	// Same logical schema name in two catalogs, same physical pair: reported only by the logical-duplicate check.
	cfg := schemaCfg(
		rs("x", "silver", "main", "silver", "catalogs/x.yml"),
		rs("y", "silver", "main", "silver", "catalogs/y.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	assert.False(t, hasErrorContaining(diags, "physical schema"), "diags: %v", diags)
}

func TestValidateLocal_physicalCollisionSkipsEmptyUCCatalog(t *testing.T) {
	cfg := schemaCfg(
		rs("x", "a", "", "silver", "catalogs/x.yml"),
		rs("y", "b", "", "silver", "catalogs/y.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.False(t, hasErrorContaining(diags, "physical schema"), "diags: %v", diags)
}

func TestValidateLocal_emptyCatalogUCNameSkippedWithoutTarget(t *testing.T) {
	cfg := schemaCfg(rs("main", "silver", "", "silver", "catalogs/main.yml"))
	cfg.NoTarget = true
	assert.Zero(t, errorCount(config.ValidateLocal(cfg)))
}
