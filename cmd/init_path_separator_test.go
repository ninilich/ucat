package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A name that becomes a file name (catalog, target) or a path part must not contain "/" or "\"
// on any OS; nothing is created before the error.
func TestInit_PathSeparatorInNames_RejectedNoFiles(t *testing.T) {
	// prompts: project, logical catalog, uc catalog, logical schema, uc schema, targets
	cases := map[string]string{
		"catalog slash":     "proj\na/b\n",
		"catalog backslash": "proj\na\\b\n",
		"uc catalog slash":  "proj\nmain\nsilver\nsilver\ndev\nh\na/b\n",
		"schema slash":      "proj\nmain\na/b\n",
		"schema backslash":  "proj\nmain\na\\b\n",
		"uc schema slash":   "proj\nmain\nsilver\na/b\n",
		"target slash":      initHead + "a/b\n",
		"target backslash":  initHead + "..\\x\n",
		"target dotdot":     initHead + "../x\n",
	}
	for name, stdin := range cases {
		t.Run(name, func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			_, err = executeInitCmd(t, stdin+"good\ngood\ngood\ngood\ngood\ngood\n")
			require.Error(t, err)
			if strings.HasPrefix(name, "uc ") { // a physical name has its own rule
				assert.Contains(t, err.Error(), nameRuleUC)
			} else {
				assert.Contains(t, err.Error(), nameRuleText)
			}
			entries, _ := os.ReadDir(dir)
			assert.Empty(t, entries, "no file may be created")
		})
	}
}
