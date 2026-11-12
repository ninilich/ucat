package diag

import (
	"errors"
	"io/fs"
	"os"
)

// ErrText returns the text of err without any OS path: for *fs.PathError and *os.LinkError
// it is "<op>: <OS message>", otherwise err.Error(). The path reaches the user through
// relPath/Location, so diagnostics never show an absolute path.
func ErrText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Op + ": " + le.Err.Error()
	}
	return err.Error()
}
