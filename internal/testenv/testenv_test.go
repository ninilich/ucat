package testenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cleared = []string{
	"DATABRICKS_WAREHOUSE_ID", "DATABRICKS_HOST", "DATABRICKS_TOKEN", "DATABRICKS_CONFIG_PROFILE",
	"DATABRICKS_CLIENT_ID", "ARM_CLIENT_ID", "AZURE_TENANT_ID",
}

func TestClear(t *testing.T) {
	for _, name := range cleared {
		t.Setenv(name, "x")
	}
	userCfg := filepath.Join(t.TempDir(), "databrickscfg")
	require.NoError(t, os.WriteFile(userCfg, []byte("[DEFAULT]\n"), 0o600))
	t.Setenv("DATABRICKS_CONFIG_FILE", userCfg)
	t.Setenv("UCAT_TESTENV_KEEP", "1")

	Clear()

	for _, name := range cleared {
		_, ok := os.LookupEnv(name)
		assert.False(t, ok, name)
	}
	assert.Equal(t, "1", os.Getenv("UCAT_TESTENV_KEEP"))
	cfgFile := os.Getenv("DATABRICKS_CONFIG_FILE")
	assert.NotEmpty(t, cfgFile)
	assert.NotEqual(t, userCfg, cfgFile)
	assert.NoFileExists(t, cfgFile)
}
