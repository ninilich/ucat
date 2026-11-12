package cmd

import (
	"fmt"

	"github.com/ninilich/ucat/internal/live"
	"github.com/spf13/cobra"
)

func (a *app) newValidateCmd() *cobra.Command {
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate manifest files",
		Long: `Run the local checks on ucat.yml, catalogs, targets and table files. Without -t only the
checks that do not depend on a target run. With -t <target> the config is resolved with the target
and the live checks follow: catalog and schema existence, warehouse connectivity and the
differ's warnings.`,
		Example: `  ucat validate
  ucat validate -t dev`,
		RunE: a.runValidate,
	}
	a.addTargetFlag(validateCmd)
	return validateCmd
}

func (a *app) runValidate(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	ctx := cmd.Context()

	loaded, err := a.loadConfig(cmd, loadTables)
	if err != nil {
		return err
	}
	resolved := loaded.resolved

	// Same local checks as plan, on the config resolved with the target (if any).
	if err = validateLocal(cmd, resolved); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "✓ all local checks passed\n")

	if a.target == "" {
		return nil
	}

	// Live mode
	b, err := a.connect(resolved)
	if err != nil {
		return err
	}

	results := live.CheckLive(ctx, b, resolved)
	if ctx.Err() != nil { // the failures of canceled requests are not results: print none of them
		return &interruptedError{msg: "interrupted during live checks"}
	}
	var failedName string // the first failed check, named in the error like plan names the schema of a failed diff
	for _, r := range results {
		mark := "✓"
		if !r.OK {
			mark = "✗"
		}
		_, _ = fmt.Fprintf(out, "%s %s\n", mark, r.Line)
		printDiags(errOut, r.Diags)
		if failedName == "" && r.Failed() {
			failedName = r.Name
		}
	}
	if resolved.WarehouseID == "" {
		printDiags(errOut, warehouseWarning("warehouse connectivity not checked"))
	}

	if failedName != "" {
		return fmt.Errorf("live validation failed for %s", failedName)
	}
	return nil
}
