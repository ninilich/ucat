package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestValidateLocal_caseOnlyCatalogAndSchemaNames(t *testing.T) {
	cfg := config.ResolvedConfig{Schemas: []config.ResolvedSchema{
		{LogicalCatalog: "Main", LogicalSchema: litSilver, UCCatalog: "a", UCSchema: "s1", SourceFile: pathCatalogsAYml},
		{LogicalCatalog: litMain, LogicalSchema: litSilver2, UCCatalog: "b", UCSchema: "s2", SourceFile: pathCatalogsBYml},
	}}
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, countErrorsWithSummary(diags, `logical catalog names "Main" (catalogs/a.yml) and "main" (catalogs/b.yml) differ only in case`), "diags: %v", diags)
	assert.Equal(t, 1, countErrorsWithSummary(diags, `logical schema names "Silver" (catalogs/a.yml) and "silver" (catalogs/b.yml) differ only in case`), "diags: %v", diags)
}

func TestValidateLocal_exactDuplicateSchemaNotCaseCollision(t *testing.T) {
	cfg := config.ResolvedConfig{Schemas: []config.ResolvedSchema{
		{LogicalCatalog: "a", LogicalSchema: litSilver2, UCCatalog: "a", UCSchema: litSilver2, SourceFile: "catalogs/a.yml"},
		{LogicalCatalog: "b", LogicalSchema: litSilver2, UCCatalog: "b", UCSchema: litSilver2, SourceFile: pathCatalogsBYml},
	}}
	for _, d := range config.ValidateLocal(cfg) {
		assert.NotContains(t, d.Summary, "differ only in case")
	}
}
