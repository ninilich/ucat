// Package diag defines diagnostics (errors and warnings, with an optional file
// location) produced while loading, validating and diffing a project.
package diag

import "fmt"

// Severity of a diagnostic message.
type Severity int

// The severities: an Error fails the command, a Warning is only shown.
const (
	Error   Severity = iota
	Warning Severity = iota
)

// Diagnostic is a single issue found during config loading or validation.
type Diagnostic struct {
	Severity Severity
	Summary  string
	Location string // e.g. "tables/silver/orders.yml" or "tables/silver/orders.yml:12"
}

// String is the line commands print for a diagnostic: "error: <summary> (<location>)" or
// "warning: ...", without the parenthesised part when there is no location.
func (d Diagnostic) String() string {
	prefix := "error"
	if d.Severity == Warning {
		prefix = "warning"
	}
	if d.Location != "" {
		return fmt.Sprintf("%s: %s (%s)", prefix, d.Summary, d.Location)
	}
	return fmt.Sprintf("%s: %s", prefix, d.Summary)
}

// Diagnostics is an ordered list of diagnostics.
type Diagnostics []Diagnostic

// HasError returns true if any diagnostic has Error severity.
func (d Diagnostics) HasError() bool {
	for _, diag := range d {
		if diag.Severity == Error {
			return true
		}
	}
	return false
}

// Append adds a diagnostic to the list.
func (d *Diagnostics) Append(diag Diagnostic) {
	*d = append(*d, diag)
}
