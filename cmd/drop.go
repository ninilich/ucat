package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ninilich/ucat/internal/change"
)

const dropInterruptedMsg = "interrupted before the drop; nothing was dropped"

func (a *app) newDropCmd() *cobra.Command {
	dropCmd := &cobra.Command{
		Use:   "drop",
		Short: "Drop a table directly in Unity Catalog",
		Long: `Drop one managed table in the target without looking for its YAML file, for pipelines that
detect deleted manifests with git diff. --schema is the logical schema name; the table must exist
(a missing table is an error, exit code 1). Requires -t <target> and --auto-approve.

Ctrl+C or SIGTERM: before the DROP statement is sent the command stops with exit code 130 and nothing is
dropped; once it is sent it is awaited to its end (like a step of apply) and reported as usual. A second
signal ends the process at once; then the outcome is unknown: check with "ucat plan" or "ucat pull".`,
		Example: `  ucat drop -t dev --schema silver --table orders --auto-approve`,
		RunE:    a.runDrop,
	}
	a.addTargetFlag(dropCmd)
	a.addAutoApproveFlag(dropCmd)
	dropCmd.Flags().StringVar(&a.dropSchema, "schema", "", "logical schema name (required)")
	dropCmd.Flags().StringVar(&a.dropTable, "table", "", "table name (required)")
	_ = dropCmd.MarkFlagRequired("schema")
	_ = dropCmd.MarkFlagRequired("table")
	return dropCmd
}

func (a *app) runDrop(cmd *cobra.Command, _ []string) error {
	if !a.autoApprove {
		return fmt.Errorf("drop requires --auto-approve")
	}
	if err := a.requireTarget(cmd); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	ctx := cmd.Context()

	cfg, err := a.loadConfig(cmd, skipTables)
	if err != nil {
		return err
	}
	resolved := cfg.resolved

	if err = validateLocal(cmd, resolved); err != nil {
		return err
	}

	foundRS, found := resolved.SchemaByLogical(a.dropSchema)
	if !found {
		return fmt.Errorf("unknown schema %q: not defined in any catalog config", a.dropSchema)
	}

	if err = a.requireWarehouse("drop", resolved); err != nil {
		return err
	}

	drop := &change.DropTableChange{
		Base: change.Base{Table: change.TablePath{
			Catalog: foundRS.UCCatalog,
			Schema:  foundRS.UCSchema,
			Name:    a.dropTable,
		}},
	}

	if ctx.Err() != nil { // nothing was sent yet
		return &interruptedError{msg: dropInterruptedMsg}
	}
	b, err := a.connect(resolved)
	if err != nil {
		return err
	}
	if ctx.Err() != nil { // a signal during connect: the client construction is not cancellable
		return &interruptedError{msg: dropInterruptedMsg}
	}

	// Like a step of apply, the DROP is not cut by Ctrl+C: it is awaited to its end and reported.
	if err := b.ApplyChange(context.WithoutCancel(ctx), drop); err != nil {
		return fmt.Errorf("drop %s: %w", drop.Table.FullName(), err)
	}

	_, _ = fmt.Fprintf(out, "✓ dropped %s\n", drop.Table.FullName())
	return nil
}
