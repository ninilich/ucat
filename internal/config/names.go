package config

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Name rules. Every comparison of Unity Catalog names and every check of how a name is
// spelled goes through this file, so the validator, the differ and init cannot disagree.

// FoldName is the case-insensitive form of a name: Unity Catalog resolves table, column and
// schema names case-insensitively and stores them in lowercase. Two names are "the same" when
// their folded forms are equal (see SameName). It is strings.ToLower, not Unicode case folding:
// "ſ" (long s) is not "s" here, in the validator and in the differ alike.
func FoldName(name string) string { return strings.ToLower(name) }

// SameName reports whether two names are equal after FoldName.
func SameName(a, b string) bool { return FoldName(a) == FoldName(b) }

// IsLowerName reports whether name is already in its folded (lowercase) form.
func IsLowerName(name string) bool { return FoldName(name) == name }

// HasDotOrSpace reports whether name contains a dot or any whitespace character
// (such a name needs quoting in SQL and breaks dotted references).
func HasDotOrSpace(name string) bool {
	return strings.IndexFunc(name, func(r rune) bool { return r == '.' || unicode.IsSpace(r) }) >= 0
}

// FileSafeNameRule is the rule IsFileSafeName checks, as the end of an error message ("<what> <name> <rule>").
const FileSafeNameRule = "must contain only letters, digits, underscores and hyphens (at most 100 characters) and must not be a reserved Windows name"

// maxFileSafeNameLen is the longest logical name; a file name of 255 bytes is the limit of the common file
// systems, and the name gets an extension and a directory around it.
const maxFileSafeNameLen = 100

// UCNameRule is what UCNameError reports, as the end of an error message ("<what> <name> <rule>").
const UCNameRule = "must not contain whitespace, dots, slashes or control characters and must be at most 255 characters"

// maxUCNameLen is the longest UC catalog or schema name, in characters (the Unity Catalog limit).
const maxUCNameLen = 255

// UCNameError returns UCNameRule when name cannot be a physical (UC) catalog or schema name, or "" when it can.
// Unity Catalog owns these names, so unlike a logical name (IsFileSafeName) they may be any text without
// whitespace, dots, "/", "\\" and control characters (those break SQL and paths). It is the one check: validate
// (every resolved uc_name) and init (the "UC catalog name" and "UC schema name" answers) both use it.
func UCNameError(name string) string {
	bad := name == "" || utf8.RuneCountInString(name) > maxUCNameLen || HasDotOrSpace(name) ||
		strings.ContainsAny(name, `/\`) || strings.IndexFunc(name, unicode.IsControl) >= 0
	if bad {
		return UCNameRule
	}
	return ""
}

// IsFileSafeName reports whether name can be a logical catalog or schema name or a target name:
// these become path parts (catalogs/<name>.yml, targets/<name>.yml, tables/<schema>/...), so it is a
// white list: not empty, at most 100 characters of A-Z a-z 0-9 _ -, and not a reserved Windows device name.
func IsFileSafeName(name string) bool {
	if name == "" || len(name) > maxFileSafeNameLen || IsReservedWindowsName(name) {
		return false
	}
	for _, r := range name {
		if !isNameRune(r) {
			return false
		}
	}
	return true
}

// isNameRune reports whether r is one of A-Z a-z 0-9 _ -.
func isNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return r == '_' || r == '-'
}

// IsReservedWindowsName reports whether name (any case) is a device name Windows reserves for files:
// CON, PRN, AUX, NUL, COM1-COM9, LPT1-LPT9. It is the only list of them: the logical name rule and
// pull's check of file names on Windows both use it.
func IsReservedWindowsName(name string) bool {
	switch strings.ToUpper(name) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// joinKey builds one map key from several name parts. The parts are joined with NUL, which
// cannot occur in a name read from YAML, so ("a", "bc") and ("ab", "c") never collide.
func joinKey(parts ...string) string { return strings.Join(parts, "\x00") }

// joinAnd lists items as "a", "a and b", "a, b and c" (no Oxford comma).
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// both is the quantifier for a list of several owners: "both" for two, many ("all of", "all") for more.
func both(n int, many string) string {
	if n > 2 {
		return many
	}
	return "both"
}
