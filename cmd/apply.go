package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/ninilich/ucat/internal/executor"
	"github.com/ninilich/ucat/internal/output"
	"github.com/spf13/cobra"
)

func (a *app) newApplyCmd() *cobra.Command {
	applyCmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply changes to Unity Catalog",
		Long: `Plan the changes for the target and execute them one by one as SQL on the target's warehouse;
the first failed change stops the run (exit code 1). A plan with destructive changes (drop table,
drop column) asks for "yes" first; --auto-approve skips the question. Needs -t <target>.
Every step line shows how long the step took: "[3/12] applied in 1.6s: ...".
--json prints the result on stdout (schema_version 1: total, applied, failed, skipped, and "aborted" when
the run stopped before its end for a reason other than a failed change); the plan, the progress and the
prompt go to stderr.

--schema <name> and --table <schema.table | table> limit the run to some tables: only their changes are
planned and applied, the other tables are not looked at in UC. Names are the logical names of the YAML,
without regard to case. Both flags may be repeated and combine as a union; a table with drop: true is
selected like any other, a live table that is not in the YAML is never touched. A name that matches nothing
fails before UC is read and lists the available schemas and tables.

Ctrl+C or SIGTERM: the step that is running is finished (a statement is not cut in the middle), no new step
starts, the line "interrupted after step i/N; k steps not applied" is printed and the exit code is 130. A
second signal ends the process at once. Exit code 130 is also used when plan, apply's checks, validate -t or drop are interrupted.`,
		Example: `  ucat apply -t dev
  ucat apply -t prod --auto-approve
  ucat apply -t prod --schema silver --table gold.customers
  ucat apply -t prod --auto-approve --json`,
		RunE: a.runApply,
	}
	a.addTargetFlag(applyCmd)
	a.addAutoApproveFlag(applyCmd)
	a.addPlanOutputFlags(applyCmd)
	a.addSelectorFlags(applyCmd)
	return applyCmd
}

func (a *app) runApply(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	result, err := a.runPipeline(ctx, cmd)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if a.jsonOutput {
		// stdout carries only the result JSON; the plan stays visible on stderr
		// as context for the confirmation prompt.
		w = cmd.ErrOrStderr()
	}

	if len(result.plan) == 0 {
		return a.applyNothing(cmd, w)
	}

	// Every change is executed as SQL: fail before the prompt and before anything is applied.
	if err = a.requireWarehouse("apply", result.resolved); err != nil {
		return err
	}

	output.PrintPlan(w, result.plan, a.showSQL)

	res, err := executor.Execute(ctx, result.plan, result.backend, executor.ExecOpts{AutoApprove: a.autoApprove, Now: a.now}, cmd.InOrStdin(), w)
	return a.reportApply(cmd, w, res, err)
}

// applyNothing reports an empty plan: the result JSON on stdout, or the "no changes" line on w.
func (a *app) applyNothing(cmd *cobra.Command, w io.Writer) error {
	if a.jsonOutput {
		return output.PrintApplyJSON(cmd.OutOrStdout(), executor.Result{})
	}
	output.NoChanges(w)
	return nil
}

// reportApply turns the result of executor.Execute into the command result: the JSON on stdout
// (--json), the exit code and the closing line.
func (a *app) reportApply(cmd *cobra.Command, w io.Writer, res executor.Result, err error) error {
	if a.jsonOutput {
		if jerr := output.PrintApplyJSON(cmd.OutOrStdout(), res); jerr != nil && err == nil {
			return jerr
		}
	}
	if errors.Is(err, executor.ErrInterrupted) {
		return &interruptedError{msg: interruptedText(res)}
	}
	if err != nil {
		return applyFailure(err, res.Applied, res.Total)
	}

	if !a.jsonOutput {
		_, _ = fmt.Fprintf(w, "\nApply complete in %s.\n", executor.FormatDuration(res.Elapsed))
	}
	return nil
}

// interruptedText says where an interrupted apply stopped: "interrupted after step 3/12; 9 steps not
// applied", or "interrupted before the first step; 12 steps not applied" when nothing ran.
func interruptedText(res executor.Result) string {
	steps := fmt.Sprintf("%d steps", res.Skipped)
	if res.Skipped == 1 {
		steps = "1 step"
	}
	if res.Applied == 0 {
		return fmt.Sprintf("interrupted before the first step; %s not applied", steps)
	}
	return fmt.Sprintf("interrupted after step %d/%d; %s not applied", res.Applied, res.Total, steps)
}

// applyFailure is the error of a failed apply, printed once by run on stderr: a failed change names
// itself, its table and the reason; a declined prompt and a failed read of it are wrapped as
// "apply failed: ...".
func applyFailure(err error, applied, total int) error {
	var applyErr *executor.ApplyError
	if errors.As(err, &applyErr) {
		return fmt.Errorf("apply failed after %d/%d changes: %s - %s: %w", applied, total, applyErr.Change.TableName(), applyErr.Change.Describe(), applyErr.Err)
	}
	return fmt.Errorf("apply failed: %w", err)
}
