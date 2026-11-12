// Package executor applies a plan: it asks for confirmation when needed, runs the
// changes through a backend one by one and reports progress and partial failures.
package executor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/term"
)

// ErrCancelled is returned when the confirmation prompt is declined or has no input.
var ErrCancelled = errors.New("apply cancelled")

// ErrConfirmRead is returned when reading the confirmation fails for a reason other than EOF.
// It is a failure, not a user refusal, so it is not ErrCancelled.
var ErrConfirmRead = errors.New("failed to read confirmation")

// ErrInterrupted is returned when the context was canceled (Ctrl+C, SIGTERM) before the plan was done:
// the step that was running has finished, the later ones were not started. It wraps context.Canceled.
var ErrInterrupted = fmt.Errorf("apply interrupted: %w", context.Canceled)

// ApplyError is the failure of one change: Change is the change the backend rejected, Err the reason
// (backend, SDK or UC error). Its text, `apply "<describe>": <reason>`, is the error field of the
// --json result; commands build their own text from the fields.
type ApplyError struct {
	Change change.Change
	Err    error
}

func (e *ApplyError) Error() string { return fmt.Sprintf("apply %q: %v", e.Change.Describe(), e.Err) }

// Unwrap returns the reason.
func (e *ApplyError) Unwrap() error { return e.Err }

// ExecOpts controls execution behavior.
type ExecOpts struct {
	AutoApprove bool
	// Now is the clock of the step durations; nil means time.Now. Tests inject their own.
	Now func() time.Time
}

// Failure is the change that stopped an apply: Step is its 1-based position in the plan (the i of
// the "[i/N]" lines), Change the change the backend rejected, Err the reason.
type Failure struct {
	Step   int
	Change change.Change
	Err    error
}

// The values of Result.Aborted: why an apply stopped before it ran anything.
const (
	AbortDeclined    = "declined"    // the prompt was answered with anything but yes, or had no input (EOF)
	AbortInputError  = "input_error" // reading the answer failed
	AbortInterrupted = "interrupted" // the context was canceled (Ctrl+C, SIGTERM): the running step finished, the rest did not start
)

// Result is what an apply did, by plan step: steps 1..Applied succeeded, step Failed.Step (if any)
// failed, and the Skipped steps after it (or all of them, when the prompt was declined) never ran.
// Total = Applied + Skipped, plus one when Failed is set.
type Result struct {
	Total   int
	Applied int
	Skipped int
	Failed  *Failure
	// Aborted is AbortDeclined or AbortInputError when the confirmation stopped the apply before any
	// step ran (Skipped = Total, Failed nil), AbortInterrupted when the context was canceled (Applied
	// steps ran, Skipped = Total - Applied, Failed nil); empty otherwise. A cancel that comes when no
	// step is left is not an interruption: the apply is complete.
	Aborted string
	// Elapsed is the time the steps took (from the first step to the last one that ran), by the Now
	// clock; the wait for the confirmation is not included. It is not part of the --json result.
	Elapsed time.Duration
}

// Execute applies plan changes to the backend.
// If the plan contains destructive changes and AutoApprove is false,
// it prompts the user for confirmation. Fails fast on first error: the failed change is reported
// on out as "[i/N] failed in <time>: ..." and returned as an *ApplyError (and as Result.Failed), the
// reason is not printed here. Returns the result by step and an error on failure or user decline; a
// declined prompt leaves every step skipped and Result.Failed nil.
//
// A canceled ctx (Ctrl+C) never interrupts a running change: the step is executed on a context that
// is not canceled with ctx, so the DDL is not cut in the middle, and the cancel is looked at between
// steps. The steps that have not started stay skipped, Result.Aborted is AbortInterrupted and the
// error is ErrInterrupted. A cancel while the prompt waits for the answer ends the wait the same way.
func Execute(ctx context.Context, plan []change.Change, b backend.UCBackend, opts ExecOpts, in io.Reader, out io.Writer) (Result, error) {
	total := len(plan)
	res := Result{Total: total}
	if total == 0 {
		return res, nil
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	if ctx.Err() != nil { // canceled before anything started: no prompt, no step
		res.Skipped = total
		res.Aborted = AbortInterrupted
		return res, ErrInterrupted
	}

	if !opts.AutoApprove && hasDestructive(plan) {
		if aborted, err := confirm(ctx, in, out); err != nil {
			res.Skipped = total
			res.Aborted = aborted
			return res, err
		}
	}

	begin := now()
	// The running change must not be cut by the cancel of ctx; it keeps the values of ctx.
	stepCtx := context.WithoutCancel(ctx)
	for i, ch := range plan {
		if ctx.Err() != nil {
			res.Skipped = total - i
			res.Aborted = AbortInterrupted
			res.Elapsed = now().Sub(begin)
			return res, ErrInterrupted
		}
		start := now()
		err := b.ApplyChange(stepCtx, ch)
		took := FormatDuration(now().Sub(start))
		if err != nil {
			_, _ = fmt.Fprintf(out, "  [%d/%d] failed in %s: %s - %s\n", i+1, total, took, ch.TableName(), ch.Describe())
			res.Skipped = total - i - 1
			res.Elapsed = now().Sub(begin)
			res.Failed = &Failure{Step: i + 1, Change: ch, Err: err}
			return res, &ApplyError{Change: ch, Err: err}
		}
		res.Applied++
		_, _ = fmt.Fprintf(out, "  [%d/%d] applied in %s: %s - %s\n", i+1, total, took, ch.TableName(), ch.Describe())
	}

	res.Elapsed = now().Sub(begin)
	return res, nil
}

// confirm asks for the confirmation of a destructive plan. It returns nil when the answer is yes,
// otherwise the Aborted value that says why the apply stops and the error to return.
func confirm(ctx context.Context, in io.Reader, out io.Writer) (aborted string, err error) {
	// the prompt stays on the line of the answer (no trailing \n before input)
	_, _ = fmt.Fprint(out, "\nPlan contains destructive changes. Type 'yes' to confirm: ")
	answer, overflow, err := awaitAnswer(ctx, in)
	if errors.Is(err, ErrInterrupted) {
		_, _ = fmt.Fprintln(out) // the prompt has no newline: end its line before the error is printed
		return AbortInterrupted, err
	}
	if !term.IsTerminal(in) { // no Enter was typed, so nothing ended the prompt line: the next output must not stick to it
		_, _ = fmt.Fprintln(out)
	}
	if err != nil {
		if errors.Is(err, ErrCancelled) { // EOF without data: no answer is a refusal
			return AbortDeclined, err
		}
		return AbortInputError, err
	}
	if overflow || answer != "yes" {
		return AbortDeclined, fmt.Errorf("%w by user (got %s)", ErrCancelled, quoteEcho(answer))
	}
	return "", nil
}

// FormatDuration renders a duration for the progress output: milliseconds under a second ("250ms"),
// otherwise seconds with one decimal ("1.6s", "1m2.3s"). A negative duration (a clock that went
// back) is shown as zero.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if ms := d.Round(time.Millisecond); ms < time.Second {
		return fmt.Sprintf("%dms", ms.Milliseconds())
	}
	return d.Round(100 * time.Millisecond).String()
}

// answer is the result of one readAnswer.
type answer struct {
	text     string
	overflow bool
	err      error
}

// awaitAnswer reads the confirmation like readAnswer, but gives up with ErrInterrupted as soon as ctx
// is canceled (a read from a terminal does not end on Ctrl+C by itself). The read goes on in its
// goroutine until the input ends; the process is leaving by then.
func awaitAnswer(ctx context.Context, in io.Reader) (text string, overflow bool, err error) {
	if ctx.Err() != nil {
		return "", false, ErrInterrupted
	}
	done := make(chan answer, 1)
	go func() {
		t, o, e := readAnswer(in)
		done <- answer{t, o, e}
	}()
	select {
	case <-ctx.Done():
		return "", false, ErrInterrupted
	case a := <-done:
		return a.text, a.overflow, a.err
	}
}

// maxAnswerBytes is the longest confirmation line that is read; the rest is not consumed.
const maxAnswerBytes = 64 * 1024

// maxEchoRunes is the number of runes of a declined answer shown in the error.
const maxEchoRunes = 64

// readAnswer reads one confirmation line (up to maxAnswerBytes) and returns it trimmed.
// EOF without data is ErrCancelled ("no input"); a line without trailing newline is accepted;
// a line over the limit (overflow=true) is returned as read, untrimmed, and must always be declined
// whatever it contains (its tail is unread, so "yes"+spaces is not a full answer);
// a real read error is returned as "failed to read confirmation" and is not ErrCancelled.
func readAnswer(in io.Reader) (answer string, overflow bool, err error) {
	r := bufio.NewReaderSize(in, maxAnswerBytes)
	line, err := r.ReadSlice('\n')
	switch {
	case err == nil:
	case errors.Is(err, bufio.ErrBufferFull):
		return string(line), true, nil
	case errors.Is(err, io.EOF):
		if len(line) == 0 {
			return "", false, fmt.Errorf("%w: no input", ErrCancelled)
		}
	default:
		return "", false, fmt.Errorf("%w: %s", ErrConfirmRead, diag.ErrText(err))
	}
	return strings.TrimSpace(string(line)), false, nil
}

// quoteEcho renders s with %q-style quoting (invisible characters show as escapes),
// keeping the first maxEchoRunes runes. If s was cut, "..." goes inside the quotes before
// the closing one: got "aaaa...".
func quoteEcho(s string) string {
	n, cut := 0, false
	for i := range s {
		if n == maxEchoRunes {
			s, cut = s[:i], true
			break
		}
		n++
	}
	q := strconv.Quote(s)
	if cut {
		q = q[:len(q)-1] + `..."`
	}
	return q
}

// hasDestructive reports whether any change in plan is destructive.
func hasDestructive(plan []change.Change) bool {
	for _, ch := range plan {
		if ch.IsDestructive() {
			return true
		}
	}
	return false
}
