package change_test

import (
	"go/build"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The change model is the bottom layer: it may use config, but never the packages that create,
// order, check, run or print changes.
func TestChangePackage_doesNotImportItsConsumers(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	require.NoError(t, err)
	for _, imp := range pkg.Imports {
		for _, consumer := range []string{"backend", "differ", "planner", "preflight", "executor", "output", "uc", "cmd"} {
			assert.NotContains(t, imp, "internal/"+consumer, "internal/change must not import %s", consumer)
		}
	}
}
