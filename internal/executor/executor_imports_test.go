package executor

import (
	"go/build"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Invariant: the production code of internal/executor must not import internal/config
// directly (it is a lower layer; helpers it needs live in internal/diag).
func TestExecutorPackage_doesNotImportConfig(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	require.NoError(t, err)
	for _, imp := range pkg.Imports {
		assert.NotContains(t, imp, "internal/config", "production import of config in internal/executor")
	}
}
