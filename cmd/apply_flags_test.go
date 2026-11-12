package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApply_RemovedFlags_UnknownFlag verifies apply does not accept flags that
// belong to other commands or were removed.
func TestApply_RemovedFlags_UnknownFlag(t *testing.T) {
	cases := map[string][]string{
		"dry-run":  {cmdApply, "-t", litDev, "--dry-run"},
		"replace":  {cmdApply, "-t", litDev, "--replace"},
		litCatalog: {cmdApply, "-t", litDev, flagCatalog, "x"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := executeCmd(args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown flag")
		})
	}
}
