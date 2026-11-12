package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/differ"
	"github.com/ninilich/ucat/internal/live"
	"github.com/ninilich/ucat/internal/planner"
	"github.com/ninilich/ucat/internal/preflight"
	"github.com/spf13/cobra"
)

// pipelineResult holds the output of the standard Load→Resolve→Diff→Plan pipeline.
type pipelineResult struct {
	resolved config.ResolvedConfig
	plan     []change.Change
	backend  backend.UCBackend
}

// loadedConfig holds the result of loadConfig.
type loadedConfig struct {
	root     string
	project  config.ProjectConfig
	resolved config.ResolvedConfig
}

// tablesMode says whether loadConfig loads the tables and whether an empty result is a warning.
type tablesMode int

const (
	skipTables      tablesMode = iota // tables are not loaded (drop)
	loadTables                        // tables are loaded; none defined is a warning (plan, apply, validate)
	loadTablesQuiet                   // tables are loaded; none defined is fine (pull starts from an empty project)
)

// requireTarget fails when -t is missing; the message names the command.
func (a *app) requireTarget(cmd *cobra.Command) error {
	if a.target == "" {
		return fmt.Errorf("%s requires -t <target>", cmd.Name())
	}
	return nil
}

// loadConfig runs FindProjectRoot -> LoadProject -> LoadCatalogs -> LoadTarget (if -t) ->
// LoadTables (per mode) -> Resolve. Every stage prints its diagnostics (warnings too)
// to stderr via printDiags and fails with the same generic message for every command.
func (a *app) loadConfig(cmd *cobra.Command, mode tablesMode) (*loadedConfig, error) {
	errOut := cmd.ErrOrStderr()

	wd, err := a.getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get the working directory: %w", err)
	}
	root, err := config.FindProjectRoot(wd)
	if err != nil {
		return nil, err
	}

	project, diags := config.LoadProject(root)
	printDiags(errOut, diags)
	if diags.HasError() {
		return nil, fmt.Errorf("failed to load project config")
	}

	catalogs, diags := config.LoadCatalogs(root)
	printDiags(errOut, diags)
	if diags.HasError() {
		return nil, fmt.Errorf("failed to load catalog configs")
	}

	var targetCfg config.TargetConfig
	if a.target != "" {
		targetCfg, diags = config.LoadTarget(root, a.target)
		printDiags(errOut, diags)
		if diags.HasError() {
			return nil, fmt.Errorf("failed to load target %q", a.target)
		}
	}

	var tables []config.Table
	emptyWarned := false // EmptyTablesWarning already names the *.yaml files of the tables directory
	if mode != skipTables {
		tables, diags = config.LoadTables(root, project)
		printDiags(errOut, diags)
		if diags.HasError() {
			return nil, fmt.Errorf("failed to load tables")
		}
		if len(tables) == 0 && mode == loadTables {
			printDiags(errOut, config.EmptyTablesWarning(root, project))
			emptyWarned = true
		}
	}
	printDiags(errOut, config.IgnoredYAMLWarning(root, project, mode != skipTables && !emptyWarned))

	opts := config.ResolveOptions{
		WarehouseID: os.Getenv("DATABRICKS_WAREHOUSE_ID"),
		Host:        os.Getenv("DATABRICKS_HOST"),
		TargetName:  a.target,
		HasTarget:   a.target != "",
	}
	printDiags(errOut, config.EnvOverrideWarnings(project, targetCfg, opts))
	if mode == loadTables { // plan, apply, validate
		printDiags(errOut, config.SharedCatalogWarning(root, catalogs))
	}
	resolved := config.Resolve(project, catalogs, targetCfg, tables, opts)

	return &loadedConfig{root: root, project: project, resolved: resolved}, nil
}

// validateLocal runs config.ValidateLocal, prints its diagnostics to stderr
// and fails when any of them is an error.
func validateLocal(cmd *cobra.Command, resolved config.ResolvedConfig) error {
	diags := config.ValidateLocal(resolved)
	printDiags(cmd.ErrOrStderr(), diags)
	if diags.HasError() {
		return fmt.Errorf("local validation failed")
	}
	return nil
}

// connect creates the backend for the resolved target; the error text is the same for every command.
func (a *app) connect(resolved config.ResolvedConfig) (backend.UCBackend, error) {
	b, err := a.newBackend(resolved.WorkspaceHost, resolved.WarehouseID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Databricks: %w", err)
	}
	return b, nil
}

// warehouseWarning is the warning for a target without warehouse_id; reason says what is affected.
func warehouseWarning(reason string) diag.Diagnostics {
	return diag.Diagnostics{{Severity: diag.Warning, Summary: "warehouse_id not set: " + reason +
		"; add warehouse_id under workspace: in " + config.TargetFile("<name>") + " or set DATABRICKS_WAREHOUSE_ID env var"}}
}

// requireWarehouse fails when no SQL warehouse is configured for the target; cmdName names the
// command in the message. Commands that execute SQL call it before asking for confirmation.
func (a *app) requireWarehouse(cmdName string, resolved config.ResolvedConfig) error {
	if resolved.WarehouseID == "" {
		return fmt.Errorf("%s requires a SQL warehouse: set workspace.warehouse_id in %s (or %s), or DATABRICKS_WAREHOUSE_ID", cmdName, config.TargetFile(a.target), config.ProjectFile)
	}
	return nil
}

// runPipeline executes Load → Resolve → ValidateLocal → Connect → Fetch → Diff → Plan → Preflight.
// It writes warnings to cmd.ErrOrStderr() and returns an error for fatal failures.
func (a *app) runPipeline(ctx context.Context, cmd *cobra.Command) (*pipelineResult, error) {
	if err := a.requireTarget(cmd); err != nil {
		return nil, err
	}

	resolved, b, err := a.prepare(cmd)
	if err != nil {
		return nil, err
	}

	desired, refs := live.GroupBySchema(resolved.Tables)
	liveTables, err := live.FetchTables(ctx, b, refs, true)
	if err != nil {
		return nil, interrupted(ctx, "reading the live state", err)
	}

	errOut := cmd.ErrOrStderr()
	changes, err := diffAll(errOut, refs, desired, liveTables)
	if err != nil {
		return nil, err
	}

	plan, err := planAndPreflight(ctx, errOut, b, changes, liveTables)
	if err != nil {
		return nil, interrupted(ctx, "preflight checks", err)
	}
	return &pipelineResult{resolved: resolved, plan: plan, backend: b}, nil
}

// interrupted turns the failure of a stage into the interrupt error when ctx was canceled (Ctrl+C,
// SIGTERM): the error of a canceled request is not a failure to report, whatever the SDK wrapped it into.
// stage says what was running. Any other error is returned as is.
func interrupted(ctx context.Context, stage string, err error) error {
	if ctx.Err() == nil {
		return err
	}
	return &interruptedError{msg: "interrupted during " + stage}
}

// prepare loads and locally validates the project, applies the --schema/--table selectors, then
// connects to the target. The whole project is validated, the selectors only narrow the tables that
// are diffed; a selector that names nothing fails here, before the backend is created.
func (a *app) prepare(cmd *cobra.Command) (config.ResolvedConfig, backend.UCBackend, error) {
	loaded, err := a.loadConfig(cmd, loadTables)
	if err != nil {
		return config.ResolvedConfig{}, nil, err
	}
	if err = validateLocal(cmd, loaded.resolved); err != nil {
		return config.ResolvedConfig{}, nil, err
	}
	resolved := loaded.resolved
	resolved.Tables, err = selector{schemas: a.selSchemas, tables: a.selTables}.selectTables(resolved.Tables)
	if err != nil {
		return config.ResolvedConfig{}, nil, err
	}
	b, err := a.connect(resolved)
	if err != nil {
		return config.ResolvedConfig{}, nil, err
	}
	return resolved, b, nil
}

// diffAll diffs every schema (liveTables[i] belongs to refs[i]) and returns the changes in schema order.
// All schemas are diffed and all diagnostics printed before failing (every planning
// error in one output); the error names the first failed schema.
func diffAll(errOut io.Writer, refs []backend.SchemaRef, desired map[backend.SchemaRef][]config.Table, liveTables [][]*backend.LiveTable) ([]change.Change, error) {
	var allChanges []change.Change
	var failedRef *backend.SchemaRef
	for i, ref := range refs {
		changes, diags := differ.DiffSchema(desired[ref], liveTables[i])
		printDiags(errOut, diags)
		if diags.HasError() {
			if failedRef == nil {
				failedRef = &refs[i]
			}
			continue
		}
		allChanges = append(allChanges, changes...)
	}
	if failedRef != nil {
		return nil, fmt.Errorf("diff failed for %s.%s", failedRef.UCCatalog, failedRef.UCSchema)
	}
	return allChanges, nil
}

// planAndPreflight orders the changes into a plan and runs the preflight checks on it.
// The live tables are the ones the diff used: preflight reads the tblproperties from them instead of asking
// the backend again for every table.
func planAndPreflight(ctx context.Context, errOut io.Writer, b backend.UCBackend, changes []change.Change, liveTables [][]*backend.LiveTable) ([]change.Change, error) {
	plan := planner.Plan(changes)

	preflightDiags := preflight.Run(ctx, plan, b, preflight.DefaultCheckers(plan), liveTables...)
	if ctx.Err() != nil && preflightDiags.HasError() { // errors of canceled requests are an effect of the cancel: print none
		return nil, interrupted(ctx, "preflight checks", nil)
	}
	printDiags(errOut, preflightDiags)
	if preflightDiags.HasError() {
		return nil, fmt.Errorf("preflight checks failed")
	}
	return plan, nil
}

// printDiags writes all diagnostics (errors and warnings), one line each, to w.
func printDiags(w io.Writer, diags diag.Diagnostics) {
	for _, d := range diags {
		_, _ = fmt.Fprintln(w, d)
	}
}
