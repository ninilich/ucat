// Package term holds the terminal check shared by the commands that prompt the user.
package term

import (
	"io"
	"os"
)

// IsTerminal reports whether in is a terminal (a character device): there the Enter of the user
// ends the prompt line, from a pipe or a file it does not.
func IsTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
