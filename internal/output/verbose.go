package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/ninilich/ucat/internal/change"
)

// PrintShowSQL writes SQL for each change to w, indented under its description.
func PrintShowSQL(w io.Writer, changes []change.Change) {
	for _, ch := range changes {
		_, _ = fmt.Fprintln(w, ch.Describe())
		sql := ch.SQL()
		if sql != "" {
			for _, line := range strings.Split(sql, "\n") {
				_, _ = fmt.Fprintf(w, "    %s\n", line)
			}
		}
		_, _ = fmt.Fprintln(w) // blank separator
	}
}
