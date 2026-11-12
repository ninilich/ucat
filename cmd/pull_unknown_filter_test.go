package cmd

import (
	"testing"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countBackendCreations(t *testing.T) *int {
	t.Helper()
	n := 0
	useBackendFactory(t, func(_, _ string) (backend.UCBackend, error) {
		n++
		return &backendtest.MockUCBackend{}, nil
	})
	return &n
}

func TestPull_UnknownCatalogFilter(t *testing.T) {
	setupTestProject(t)
	created := countBackendCreations(t)

	_, err := executeCmd("pull", "-t", "dev", "--catalog", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown catalog "nope"`)
	assert.Zero(t, *created, "backend must not be created")
}

func TestPull_UnknownSchemaFilter(t *testing.T) {
	setupTestProject(t)
	created := countBackendCreations(t)

	_, err := executeCmd("pull", "-t", "dev", "--catalog", "main", "--schema", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown schema "nope" in catalog "main"`)
	assert.Zero(t, *created, "backend must not be created")
}

func TestPull_KnownFilterStillConnects(t *testing.T) {
	setupTestProject(t)
	created := countBackendCreations(t)

	_, err := executeCmd("pull", "-t", "dev", "--catalog", "main", "--schema", "silver")
	require.NoError(t, err)
	assert.Equal(t, 1, *created)
}
