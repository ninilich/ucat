package diag_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
)

func TestHasError_withErrors(t *testing.T) {
	var d diag.Diagnostics
	d.Append(diag.Diagnostic{Severity: diag.Error, Summary: "something failed"})
	assert.True(t, d.HasError())
}

func TestHasError_warningsOnly(t *testing.T) {
	var d diag.Diagnostics
	d.Append(diag.Diagnostic{Severity: diag.Warning, Summary: "a warning"})
	assert.False(t, d.HasError())
}

func TestHasError_empty(t *testing.T) {
	var d diag.Diagnostics
	assert.False(t, d.HasError())
}

func TestAppend(t *testing.T) {
	var d diag.Diagnostics
	assert.Len(t, d, 0)
	d.Append(diag.Diagnostic{Severity: diag.Error, Summary: "first"})
	assert.Len(t, d, 1)
	d.Append(diag.Diagnostic{Severity: diag.Warning, Summary: "second"})
	assert.Len(t, d, 2)
}

func TestDiagnosticString(t *testing.T) {
	tests := []struct {
		d    diag.Diagnostic
		want string
	}{
		{diag.Diagnostic{Severity: diag.Error, Summary: "boom"}, "error: boom"},
		{diag.Diagnostic{Severity: diag.Error, Summary: "boom", Location: "a.yml:3"}, "error: boom (a.yml:3)"},
		{diag.Diagnostic{Severity: diag.Warning, Summary: "careful"}, "warning: careful"},
		{diag.Diagnostic{Severity: diag.Warning, Summary: "careful", Location: "tables/s/t.yml"}, "warning: careful (tables/s/t.yml)"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.d.String())
	}
}
