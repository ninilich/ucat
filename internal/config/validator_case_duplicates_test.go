package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLocal_caseOnlyDuplicateCatalog(t *testing.T) {
	cfg := schemaCfg(
		rs("Main", "silver", "a", "silver", "catalogs/a.yml"),
		rs("Main", "gold", "a", "gold", "catalogs/a.yml"),
		rs("main", "bronze", "b", "bronze", "catalogs/b.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "one diag per group: %v", diags)
	for _, frag := range []string{`"Main"`, `"main"`, pathCatalogsAYml, pathCatalogsBYml, "case"} {
		assert.True(t, hasErrorContaining(diags, frag), "missing %q in %v", frag, diags)
	}
}

func TestValidateLocal_caseOnlyDuplicateSchema(t *testing.T) {
	cfg := schemaCfg(
		rs("a", "Silver", "a", "silver_x", "catalogs/a.yml"),
		rs("b", "silver", "b", "silver_y", "catalogs/b.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	for _, frag := range []string{`"Silver"`, `"silver"`, pathCatalogsAYml, "catalogs/b.yml", "case"} {
		assert.True(t, hasErrorContaining(diags, frag), "missing %q in %v", frag, diags)
	}
}

func TestValidateLocal_caseOnlyDuplicateNoDoubleReportWithExact(t *testing.T) {
	// exact duplicate "silver" in a and b stays check 21 only; no case-only message
	cfg := schemaCfg(
		rs("a", "silver", "a", "silver", "catalogs/a.yml"),
		rs("b", "silver", "b", "silver", "catalogs/b.yml"),
	)
	diags := config.ValidateLocal(cfg)
	assert.Equal(t, 1, errorCount(diags), "diags: %v", diags)
	assert.False(t, hasErrorContaining(diags, "differ only in case"), "diags: %v", diags)
}

func TestValidateLocal_caseOnlyDuplicateSchemaWithExactDuplicate(t *testing.T) {
	// Silver + two exact "silver": exact pair is check 21, plus one case-only diag
	cfg := schemaCfg(
		rs("a", "Silver", "a", "s1", "catalogs/a.yml"),
		rs("b", "silver", "b", "s2", "catalogs/b.yml"),
		rs("c", "silver", "c", "s3", "catalogs/c.yml"),
	)
	diags := config.ValidateLocal(cfg)
	caseOnly := 0
	for _, d := range diags {
		if containsStr(d.Summary, "differ only in case") {
			caseOnly++
		}
	}
	assert.Equal(t, 1, caseOnly, "diags: %v", diags)
}

func TestValidateLocal_caseOnlyDuplicateDeterministicOrder(t *testing.T) {
	cfg := schemaCfg(
		rs("Zeta", "s1", "z1", "s1", "catalogs/z1.yml"),
		rs("zeta", "s2", "z2", "s2", "catalogs/z2.yml"),
		rs("Alpha", "s3", "a1", "s3", "catalogs/a1.yml"),
		rs("alpha", "s4", "a2", "s4", "catalogs/a2.yml"),
	)
	first := config.ValidateLocal(cfg)
	require.Equal(t, 2, errorCount(first), "diags: %v", first)
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, config.ValidateLocal(cfg))
	}
	// groups are sorted: the alpha group is reported before the zeta group
	var summaries []string
	for _, d := range first {
		summaries = append(summaries, d.Summary)
	}
	assert.Contains(t, summaries[0], "lpha")
	assert.Contains(t, summaries[1], "eta")
}
