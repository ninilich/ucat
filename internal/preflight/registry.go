package preflight

import "github.com/ninilich/ucat/internal/change"

// DefaultCheckers returns the default set of preflight checkers.
func DefaultCheckers(plan []change.Change) []Checker {
	return []Checker{
		&ColumnMappingChecker{plan: plan},
	}
}
