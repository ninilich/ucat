package backend_test

import (
	"go/build"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The backend talks about the live state: it does not import config (the desired state from YAML).
// config still comes in through the change model that ApplyChange takes.
func TestBackendPackage_doesNotImportConfig(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	require.NoError(t, err)
	for _, imp := range pkg.Imports {
		assert.False(t, strings.HasSuffix(imp, "internal/config"), "internal/backend must not import %s", imp)
	}
}
