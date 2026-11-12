package diag_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"testing"

	"github.com/ninilich/ucat/internal/diag"
	"github.com/stretchr/testify/assert"
)

func TestErrText(t *testing.T) {
	pe := &fs.PathError{Op: "open", Path: "/abs/project/ucat.yml", Err: syscall.EACCES}
	le := &os.LinkError{Op: "rename", Old: "/abs/a", New: "/abs/b", Err: syscall.ENOENT}
	plain := errors.New("plain failure")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"PathError", pe, "open: " + syscall.EACCES.Error()},
		{"LinkError", le, "rename: " + syscall.ENOENT.Error()},
		{"plain", plain, "plain failure"},
		{"wrapped PathError", fmt.Errorf("reading: %w", pe), "open: " + syscall.EACCES.Error()},
		{"wrapped LinkError", fmt.Errorf("moving: %w", le), "rename: " + syscall.ENOENT.Error()},
		{"wrapped plain", fmt.Errorf("ctx: %w", plain), "ctx: plain failure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := diag.ErrText(tc.err)
			assert.Equal(t, tc.want, got)
			assert.NotContains(t, got, "/abs")
		})
	}
}
