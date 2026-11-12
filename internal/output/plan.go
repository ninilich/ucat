// Package output renders plans and apply results for the terminal and as JSON.
package output

import (
	"fmt"
	"io"

	"github.com/fatih/color"

	"github.com/ninilich/ucat/internal/change"
)

var (
	green  = color.New(color.FgGreen).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
)

// NoChanges writes the line that stands for an empty plan. Commands print it instead of
// PrintPlan, which expects a plan with at least one change.
func NoChanges(w io.Writer) {
	_, _ = fmt.Fprintln(w, "No changes. Infrastructure is up-to-date.")
}

// PrintPlan writes a Terraform-style plan to w, with consecutive changes of one table grouped under a header,
// and, when showSQL is set, the SQL of every change after it.
func PrintPlan(w io.Writer, changes []change.Change, showSQL bool) {
	var add, modify, destroy int

	// Print in plan (execution) order; a header is emitted whenever the table changes.
	prev := ""
	for i, ch := range changes {
		if key := planKey(ch); i == 0 || key != prev {
			if i > 0 {
				_, _ = fmt.Fprintln(w)
			}
			_, _ = fmt.Fprintf(w, "  %s\n", key)
			prev = key
		}
		var sym string
		switch classify(ch) {
		case kindAdd:
			sym = green("+")
			add++
		case kindDestroy:
			sym = red("-")
			destroy++
		default:
			sym = yellow("~")
			modify++
		}
		_, _ = fmt.Fprintf(w, "    %s %s\n", sym, ch.Describe())
	}
	_, _ = fmt.Fprintln(w)

	_, _ = fmt.Fprintf(w, "Plan: %d to add, %d to change, %d to destroy.\n", add, modify, destroy)

	if showSQL {
		PrintShowSQL(w, changes)
	}
}

type changeKind int

const (
	kindChange changeKind = iota
	kindAdd
	kindDestroy
)

// classify sorts a change into the add/change/destroy groups of the plan summary:
// a table create is "add", a destructive change is "destroy", everything else is "change".
func classify(ch change.Change) changeKind {
	switch {
	case ch.Type() == change.CreateTable:
		return kindAdd
	case ch.IsDestructive():
		return kindDestroy
	default:
		return kindChange
	}
}

// planKey returns the table a change belongs to for grouping (a rename is keyed by the new name).
func planKey(ch change.Change) string { return ch.GroupKey() }
