package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nameRuleText = "must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name"

// badLogicalNames are names the white list rejects (none is empty: an empty answer in init means the default).
func badLogicalNames() []string {
	return []string{
		strings.Repeat("a", 101), "con", "CON", "Nul", "COM1", "lpt9", "C:evil", "a*b", litAB, "a b", pathAB, bsAB, "..",
		"схема", "a\x01b", "a\x00b", "a\tb",
	}
}

// init validates every name and its length before it writes any file.
func TestInit_NameWhiteList_RejectedNoFiles(t *testing.T) {
	for _, name := range badLogicalNames() {
		for kind, stdin := range map[string]string{
			"catalog":    "proj\n" + name + "\n",
			litSchema:    "proj\nmain\n" + name + "\n",
			litTarget:    initHead + name + "\n",
			"2nd target": initHead + "dev," + name + "\n",
		} {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			_, err = executeInitCmd(t, stdin+"h\nc\nw\nh\nc\nw\n")
			require.Error(t, err, "%s %q", kind, name)
			assert.Contains(t, err.Error(), nameRuleText, "%s %q", kind, name)
			entries, _ := os.ReadDir(dir)
			assert.Empty(t, entries, "%s %q: no file may be created", kind, name)
		}
	}
}

// The physical UC names have their own, wider rule (config.UCNameError, the one validate uses).
const nameRuleUC = "must not contain whitespace, dots, slashes or control characters and must be at most 255 characters"

func TestInit_UCNames_RejectedNoFiles(t *testing.T) {
	for _, name := range []string{"a b", litAB, pathAB, bsAB, "a\x01b", "a\x00b", "a\tb", "a\u00a0b", strings.Repeat("a", 256)} {
		for kind, stdin := range map[string]string{
			"uc catalog": "proj\nmain\nsilver\nsilver\ndev\nh\n" + name + "\nw\n",
			"uc schema":  "proj\nmain\nsilver\n" + name + "\ndev\nh\nc\nw\n",
		} {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			_, err = executeInitCmd(t, stdin)
			require.Error(t, err, "%s %q", kind, name)
			assert.Contains(t, err.Error(), nameRuleUC, "%s %q", kind, name)
			entries, _ := os.ReadDir(dir)
			assert.Empty(t, entries, "%s %q: no file may be created", kind, name)
		}
	}
}

// A UC name may be unicode (the logical name may not).
func TestInit_UCNames_UnicodeAccepted(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, "proj\nmain\nsilver\nсхема-2\ndev\nh\nкаталог_1\nw\n")
	require.NoError(t, err)
	assert.FileExists(t, "catalogs/main.yml")
	target, readErr := os.ReadFile("targets/dev.yml")
	require.NoError(t, readErr)
	assert.Contains(t, string(target), "uc_name: каталог_1")
	emptyDir(t)
	_, err = executeInitCmd(t, "proj\nмайн\n")
	assert.Error(t, err, "a logical name with unicode is still rejected")
}

func TestInit_NameWhiteList_AcceptedNames(t *testing.T) {
	emptyDir(t)
	long := strings.Repeat("a", 100)
	_, err := executeInitCmd(t, "proj\nMain_1\n"+long+"\nsilver\na-b\nh\nmain\nw\n")
	require.NoError(t, err)
	assert.FileExists(t, "catalogs/Main_1.yml")
	assert.FileExists(t, "targets/a-b.yml")
}

func TestPlan_TargetNameWhiteList_Rejected(t *testing.T) {
	newProject(t, projectOpts{})
	for _, name := range badLogicalNames() {
		if strings.Contains(name, "\x00") {
			continue // an argument cannot contain NUL
		}
		_, stderr, err := executeCmdSplit(cmdPlan, "-t", name)
		require.Error(t, err, "%q", name)
		assert.Contains(t, stderr, "target name "+fmt.Sprintf("%q", name)+" "+nameRuleText)
	}
}
