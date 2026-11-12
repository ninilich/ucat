package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- init with a target list that has no names ---

const initHead = "proj\nmain\nsilver\nsilver\n"

func TestInit_EmptyTargetList_ErrorNoFiles(t *testing.T) {
	for _, answer := range []string{",", " , ", ",,", ", ,\t, ", ",\n"} {
		t.Run(answer, func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			out, err := executeInitCmd(t, initHead+answer+"\n")
			require.Error(t, err)
			assert.Equal(t, "target names: at least one name is required", err.Error())
			assert.NotContains(t, out, "Created")
			entries, _ := os.ReadDir(dir)
			assert.Empty(t, entries, "no file may be created")
		})
	}
}

// Exit code of the real command is 1.
func TestInit_EmptyTargetList_ExitCode1(t *testing.T) {
	emptyDir(t)
	useStdin(t, initHead+",\n")
	_, _, err := executeCmdSplit("init")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
}

// Empty input and whitespace-only input (the prompt trims it) fall back to the default "dev".
func TestInit_EmptyOrBlankAnswer_DefaultsToDev(t *testing.T) {
	for _, answer := range []string{"", "   ", "\t"} {
		t.Run("["+answer+"]", func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			_, err = executeInitCmd(t, initHead+answer+"\nhttps://h.example.com\nmain\nwh1\n")
			require.NoError(t, err)
			assert.FileExists(t, filepath.Join(dir, "targets", "dev.yml"))
		})
	}
}

// One real name with empty entries around it: the empty entries are skipped.
func TestInit_EmptyEntriesAroundName_Skipped(t *testing.T) {
	for _, answer := range []string{"dev,", ",dev", " , dev , ", "dev,,prod"} {
		t.Run(answer, func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			_, err = executeInitCmd(t, initHead+answer+"\nh1\nc1\nw1\nh2\nc2\nw2\n")
			require.NoError(t, err)
			assert.FileExists(t, filepath.Join(dir, "targets", "dev.yml"))
			entries, err := os.ReadDir(filepath.Join(dir, "targets"))
			require.NoError(t, err)
			for _, e := range entries {
				assert.NotEqual(t, ".yml", e.Name(), "no target with an empty name")
			}
		})
	}
}

// Duplicate names (case-insensitive, after trimming) are an error before any prompt for a host
// and before any file is created.
func TestInit_DuplicateTargetNames_ErrorNoFiles(t *testing.T) {
	cases := []struct{ answer, dup string }{
		{"dev,dev", litDev},
		{"dev,prod,Dev", "Dev"},
		{"Dev,dev", litDev},
		{" dev , dev ", litDev},
	}
	for _, tc := range cases {
		t.Run(tc.answer, func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			out, err := executeInitCmd(t, initHead+tc.answer+"\nh1\nc1\nw1\nh2\nc2\nw2\n")
			require.Error(t, err)
			assert.Equal(t, `duplicate target name "`+tc.dup+`"`, err.Error())
			assert.NotContains(t, out, "Workspace host")
			entries, _ := os.ReadDir(dir)
			assert.Empty(t, entries, "no file may be created")
		})
	}
}

func TestInit_DuplicateTargetNames_ExitCode1(t *testing.T) {
	emptyDir(t)
	useStdin(t, initHead+"dev,dev\n")
	_, _, err := executeCmdSplit("init")
	require.Error(t, err)
	assert.Equal(t, 1, codeOf(err))
}

// Distinct names are accepted.
func TestInit_DistinctTargetNames_OK(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, initHead+"dev,prod\nh1\nc1\nw1\nh2\nc2\nw2\n")
	require.NoError(t, err)
	assert.FileExists(t, "targets/dev.yml")
	assert.FileExists(t, "targets/prod.yml")
}

// More duplicate shapes (the second occurrence is named as typed); the prompt for a host is not reached.
func TestInit_DuplicateTargetNames_MoreShapes(t *testing.T) {
	cases := []struct{ answer, dup string }{
		{"DEV,dev,prod", litDev},
		{"dev,,dev", litDev},
		{",dev,,DeV,", "DeV"},
		{"dev,prod,staging,PROD", "PROD"},
	}
	for _, tc := range cases {
		t.Run(tc.answer, func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			out, err := executeInitCmd(t, initHead+tc.answer+"\nh\nc\nw\n")
			require.Error(t, err)
			assert.Equal(t, `duplicate target name "`+tc.dup+`"`, err.Error())
			assert.NotContains(t, out, "host")
			assert.NotContains(t, out, "Created")
			entries, _ := os.ReadDir(dir)
			assert.Empty(t, entries)
		})
	}
}

// A name with inner whitespace fails the name validator before duplicates matter (by fact).
func TestInit_TargetNameInnerSpace_ValidatorWins(t *testing.T) {
	emptyDir(t)
	_, err := executeInitCmd(t, initHead+"de v,de v\nh\nc\nw\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name")
}
