package output

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/executor"
)

// SchemaVersion is the version of the --json contract of plan and apply, the value of the
// "schema_version" field. Within one version fields are only added and "op" gains new values: a
// consumer must ignore what it does not know. Removing or renaming a field, or changing its meaning,
// is a new version.
const SchemaVersion = 1

type jsonOutput struct {
	SchemaVersion int          `json:"schema_version"`
	Summary       jsonSummary  `json:"summary"`
	Changes       []jsonChange `json:"changes"`
}

type jsonSummary struct {
	Add     int `json:"add"`
	Change  int `json:"change"`
	Destroy int `json:"destroy"`
}

// jsonChange is one change of the plan (and the failed change of an apply result).
// Step is the 1-based position in the plan. Catalog, Schema and Table are UC names; for a table
// rename Table is the new name and PreviousName the old one, for a column rename Column is the new
// name. Path is catalog.schema.table without SQL quoting.
type jsonChange struct {
	Op           string `json:"op"`
	Step         int    `json:"step"`
	Catalog      string `json:"catalog"`
	Schema       string `json:"schema"`
	Table        string `json:"table"`
	Column       string `json:"column,omitempty"`
	PreviousName string `json:"previous_name,omitempty"`
	Alter        string `json:"alter,omitempty"` // alter_column only: type, comment or nullable
	Path         string `json:"path"`
	Description  string `json:"description"`
	SQL          string `json:"sql"`
	Destructive  bool   `json:"destructive"`
}

// newJSONChange renders ch as the step-th (1-based) change of a plan.
func newJSONChange(step int, ch change.Change) jsonChange {
	d := ch.Detail()
	return jsonChange{
		Op:           ch.Type().Op(),
		Step:         step,
		Catalog:      d.Table.Catalog,
		Schema:       d.Table.Schema,
		Table:        d.Table.Name,
		Column:       d.Column,
		PreviousName: d.PreviousName,
		Alter:        d.Alter,
		Path:         strings.Join([]string{d.Table.Catalog, d.Table.Schema, d.Table.Name}, "."),
		Description:  ch.Describe(),
		SQL:          ch.SQL(),
		Destructive:  ch.IsDestructive(),
	}
}

// PrintJSON writes the plan as JSON (one line) to w.
func PrintJSON(w io.Writer, changes []change.Change) error {
	out := jsonOutput{
		SchemaVersion: SchemaVersion,
		Changes:       make([]jsonChange, 0, len(changes)),
	}

	for i, ch := range changes {
		switch classify(ch) {
		case kindAdd:
			out.Summary.Add++
		case kindDestroy:
			out.Summary.Destroy++
		default:
			out.Summary.Change++
		}
		out.Changes = append(out.Changes, newJSONChange(i+1, ch))
	}

	return json.NewEncoder(w).Encode(out)
}

// applyJSON is the apply result: Applied and Skipped count steps, Failed is the change that
// stopped the apply (null when none did). A declined or unreadable confirmation skips everything, fails
// nothing and sets Aborted ("declined" or "input_error"); an interrupt (Ctrl+C, SIGTERM) sets Aborted
// "interrupted" and skips the steps that were not started; the field is absent in every other result.
type applyJSON struct {
	SchemaVersion int           `json:"schema_version"`
	Total         int           `json:"total"`
	Applied       int           `json:"applied"`
	Failed        *failedChange `json:"failed"`
	Skipped       int           `json:"skipped"`
	Aborted       string        `json:"aborted,omitempty"` // "declined", "input_error" or "interrupted": see above
}

// failedChange is the failed change in the form of the plan plus the reason.
type failedChange struct {
	jsonChange
	Error string `json:"error"`
}

// PrintApplyJSON writes the apply result as JSON (one line) to w.
func PrintApplyJSON(w io.Writer, res executor.Result) error {
	out := applyJSON{
		SchemaVersion: SchemaVersion,
		Total:         res.Total,
		Applied:       res.Applied,
		Skipped:       res.Skipped,
		Aborted:       res.Aborted,
	}
	if f := res.Failed; f != nil {
		out.Failed = &failedChange{jsonChange: newJSONChange(f.Step, f.Change), Error: f.Err.Error()}
	}
	return json.NewEncoder(w).Encode(out)
}
