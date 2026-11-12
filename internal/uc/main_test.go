package uc

import (
	"os"
	"strings"
	"testing"

	"github.com/ninilich/ucat/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain isolates the tests from DATABRICKS_*, ARM_* and AZURE_* of the environment (see testenv).
func TestMain(m *testing.M) {
	testenv.Clear()
	os.Exit(m.Run())
}

// The environment the tests run in has no DATABRICKS_*, ARM_* and AZURE_* variable and no readable
// profile file: TestMain called testenv.Clear.
func TestEnvironmentIsIsolated(t *testing.T) {
	cfgFile := os.Getenv("DATABRICKS_CONFIG_FILE")
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		if upper == "DATABRICKS_CONFIG_FILE" {
			continue
		}
		for _, prefix := range []string{"DATABRICKS_", "ARM_", "AZURE_"} {
			assert.False(t, strings.HasPrefix(upper, prefix), "%s is not cleared by testenv.Clear", name)
		}
	}
	require.NotEmpty(t, cfgFile)
	assert.NoFileExists(t, cfgFile)
}
