package pull

import (
	"fmt"
	"strings"

	"github.com/ninilich/ucat/internal/config"
)

// WindowsNameProblem reports why a schema or table name cannot be a file name on Windows
// (reserved device name, forbidden character, trailing dot or space), or "" when it can.
// Pure function: pull calls it on Windows only, tests call it on any OS.
func WindowsNameProblem(names ...string) string {
	for _, n := range names {
		if n == "" {
			continue
		}
		if strings.ContainsAny(n, "<>:\"/\\|?*") {
			return fmt.Sprintf("%q contains a character not allowed in Windows file names", n)
		}
		if strings.HasSuffix(n, ".") || strings.HasSuffix(n, " ") {
			return fmt.Sprintf("%q ends with a dot or space, not allowed in Windows file names", n)
		}
		stem := strings.ToUpper(strings.SplitN(n, ".", 2)[0])
		stem = strings.TrimRight(stem, " ")
		if config.IsReservedWindowsName(stem) {
			return "reserved file name on Windows"
		}
	}
	return ""
}

// NeedsWindowsNameCheck reports whether pull must check the table name against reserved Windows
// file names: only when it creates a new file whose name is derived from the table name
// (tables/<schema>/<table>.yml). An existing file overwritten in place (--force) keeps its own name.
func NeedsWindowsNameCheck(overwriteInPlace bool, goos string) bool {
	return goos == "windows" && !overwriteInPlace
}
