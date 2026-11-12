// Package testenv isolates the tests from the developer's environment: the variables the Databricks
// SDK and ucat read (DATABRICKS_*, ARM_*, AZURE_*) and the profile file in the home directory must not
// change a test result. It is called from the TestMain of every package whose tests build a
// backend or load a project (cmd, internal/config, internal/uc); a test that needs a variable sets it
// itself with t.Setenv.
package testenv

import (
	"os"
	"path/filepath"
	"strings"
)

// prefixes of the environment variables that are removed.
var prefixes = []string{"DATABRICKS_", "ARM_", "AZURE_"}

// Clear unsets every DATABRICKS_*, ARM_* and AZURE_* variable and points DATABRICKS_CONFIG_FILE to a
// file that does not exist, so ~/.databrickscfg is not read. Call it before the tests start.
func Clear() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		for _, p := range prefixes {
			if strings.HasPrefix(strings.ToUpper(name), p) {
				_ = os.Unsetenv(name)
			}
		}
	}
	_ = os.Setenv("DATABRICKS_CONFIG_FILE", filepath.Join(os.TempDir(), "ucat-tests-no-databrickscfg"))
}
