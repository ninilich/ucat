package pull_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/pull"
)

func TestWindowsNameProblem_Full(t *testing.T) {
	reserved := []string{"con", "prn", "aux", "nul"}
	for i := '1'; i <= '9'; i++ {
		reserved = append(reserved, "com"+string(i), "lpt"+string(i))
	}
	require.Len(t, reserved, 22)
	for _, r := range reserved {
		for _, form := range []string{r, strings.ToUpper(r), strings.ToUpper(r[:1]) + r[1:], r + ".yml", r + ".txt", strings.ToUpper(r) + ".tar.gz"} {
			assert.Contains(t, pull.WindowsNameProblem(form), "reserved file name on Windows", form)
			assert.Contains(t, pull.WindowsNameProblem("silver", form), "reserved file name on Windows", form)
		}
	}
	for _, n := range []string{"con.yml", "COM1", "Nul.txt"} {
		assert.NotEmpty(t, pull.WindowsNameProblem(n), n)
	}

	for _, ok := range []string{"console", "com10", "com0", "lpt0", "lpt10", "aux1", "auxiliary", "nulls", "prn1", "xcon", "con_x", "orders", litAB, "my_table-1", "Ünï"} {
		assert.Empty(t, pull.WindowsNameProblem(ok), ok)
		assert.Empty(t, pull.WindowsNameProblem("silver", ok), ok)
	}

	for _, c := range []string{"<", ">", ":", `"`, "/", `\`, "|", "?", "*"} {
		for _, n := range []string{"a" + c + "b", c + "ab", "ab" + c} {
			assert.Contains(t, pull.WindowsNameProblem(n), "not allowed", n)
		}
	}
	assert.Contains(t, pull.WindowsNameProblem("name."), "dot or space")
	assert.Contains(t, pull.WindowsNameProblem("name "), "dot or space")
	assert.Contains(t, pull.WindowsNameProblem("name..."), "dot or space")

	assert.Empty(t, pull.WindowsNameProblem(""), "empty name has no problem by itself")
	assert.Empty(t, pull.WindowsNameProblem())
	// the problem is found in any of the arguments
	assert.NotEmpty(t, pull.WindowsNameProblem("silver", "con"))
	assert.NotEmpty(t, pull.WindowsNameProblem("con", "orders"))
	assert.Empty(t, pull.WindowsNameProblem("silver", "orders"))
}

// The reserved-name check applies only when pull creates a new file named after the table.
func TestNeedsWindowsNameCheck(t *testing.T) {
	assert.True(t, pull.NeedsWindowsNameCheck(false, "windows"), "new file on windows is checked")
	assert.False(t, pull.NeedsWindowsNameCheck(true, "windows"), "overwrite in place (--force) is not checked")
	assert.False(t, pull.NeedsWindowsNameCheck(false, "linux"))
	assert.False(t, pull.NeedsWindowsNameCheck(false, "darwin"))
	assert.False(t, pull.NeedsWindowsNameCheck(true, "linux"))
}
