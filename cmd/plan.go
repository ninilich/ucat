package cmd

import (
	"github.com/ninilich/ucat/internal/output"
	"github.com/spf13/cobra"
)

func (a *app) newPlanCmd() *cobra.Command {
	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what changes would be applied",
		Long: `Compare the manifests with the live state of the target and show the changes apply would run.
Nothing is changed. Needs -t <target>; no SQL warehouse is required.

--schema and --table limit the plan to some tables (see "ucat apply --help" for the rules); changes of the other
tables are not planned. Both may be repeated.

Exit code: 0 on success, 1 on error, 130 when interrupted by Ctrl+C or SIGTERM (nothing is changed).
With --detailed-exitcode: 0 = no changes, 2 = changes present.
--json prints one JSON document on stdout (schema_version 1: summary and changes with op, step,
catalog, schema, table, path, description, sql, destructive); warnings go to stderr.`,
		Example: `  ucat plan -t dev
  ucat plan -t prod --show-sql
  ucat plan -t prod --schema silver
  ucat plan -t prod --table silver.orders --table gold.customers
  ucat plan -t prod --json --detailed-exitcode`,
		RunE: a.runPlan,
	}
	a.addTargetFlag(planCmd)
	a.addPlanOutputFlags(planCmd)
	a.addSelectorFlags(planCmd)
	planCmd.Flags().BoolVar(&a.detailedExitCode, "detailed-exitcode", false, "exit 2 when changes present, exit 0 when no changes")
	return planCmd
}

func (a *app) runPlan(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	result, err := a.runPipeline(ctx, cmd)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()

	if len(result.plan) == 0 && !a.jsonOutput {
		output.NoChanges(w)
		return nil
	}

	if a.jsonOutput {
		if err := output.PrintJSON(w, result.plan); err != nil {
			return err
		}
	} else {
		output.PrintPlan(w, result.plan, a.showSQL)
	}

	if a.detailedExitCode && len(result.plan) > 0 {
		return &exitCodeError{code: 2}
	}
	return nil
}
