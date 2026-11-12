package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/live"
	"github.com/ninilich/ucat/internal/pull"
	"github.com/spf13/cobra"
)

func (a *app) newPullCmd() *cobra.Command {
	pullCmd := &cobra.Command{
		Use:   "pull",
		Short: "Import current UC state into YAML files",
		Long: `Read the managed tables of the target's schemas from Unity Catalog and write them as table
YAML files under tables_dir. Existing files are kept unless --force is given. Needs -t <target>.
Filter with --catalog <logical> or --catalog <logical> --schema <logical> (logical names from
catalogs/*.yml). tblproperties are not written unless --tblproperties selects them.`,
		Example: `  ucat pull -t dev
  ucat pull -t dev --catalog main --schema silver
  ucat pull -t dev --force --tblproperties 'delta.*'`,
		RunE: a.runPull,
	}
	a.addTargetFlag(pullCmd)
	pullCmd.Flags().BoolVar(&a.pullForce, "force", false, "overwrite existing files")
	pullCmd.Flags().StringVar(&a.pullCatalog, "catalog", "", "filter by logical catalog name")
	pullCmd.Flags().StringVar(&a.pullSchema, "schema", "", "filter by logical schema name (requires --catalog)")
	pullCmd.Flags().StringVar(&a.pullTblProperties, "tblproperties", "", "tblproperties to write to YAML: comma-separated keys or prefixes ending in * (default none); \"all\" writes every key")
	return pullCmd
}

func (a *app) runPull(cmd *cobra.Command, _ []string) error {
	if a.pullSchema != "" && a.pullCatalog == "" {
		return fmt.Errorf("--schema requires --catalog")
	}
	filter, err := pull.ParseFilter(a.pullTblProperties)
	if err != nil {
		return err
	}
	if err = a.requireTarget(cmd); err != nil {
		return err
	}

	cfg, err := a.loadConfig(cmd, loadTablesQuiet)
	if err != nil {
		return err
	}
	resolved := cfg.resolved

	if err = validateLocal(cmd, resolved); err != nil {
		return err
	}
	errOut := cmd.ErrOrStderr()
	printDiags(errOut, filter.Warnings())

	schemas, err := pull.FilterSchemas(resolved.Schemas, a.pullCatalog, a.pullSchema)
	if err != nil {
		return err
	}

	b, err := a.connect(resolved)
	if err != nil {
		return err
	}

	p := &puller{
		app:    a,
		out:    cmd.OutOrStdout(),
		errOut: errOut,
		root:   cfg.root,
		dir:    cfg.project.TablesDir,
		filter: filter,
		index:  pull.NewIndex(resolved.Tables),
	}
	written := 0
	for _, rs := range schemas {
		n, err := p.pullSchema(cmd.Context(), b, rs)
		if err != nil {
			return err
		}
		written += n
	}
	if written > 0 {
		printDiags(errOut, filter.UnmatchedWarnings())
	}
	return nil
}

// puller holds what writing the tables of one project needs.
type puller struct {
	app         *app
	out, errOut io.Writer
	root, dir   string // project root, tables_dir
	filter      *pull.Filter
	index       *pull.Index
}

// pullSchema writes the live tables of one schema (in name order) and returns how many files it wrote.
func (p *puller) pullSchema(ctx context.Context, b backend.UCBackend, rs config.ResolvedSchema) (int, error) {
	fetched, err := live.FetchTables(ctx, b, []backend.SchemaRef{{UCCatalog: rs.UCCatalog, UCSchema: rs.UCSchema}}, false)
	if err != nil {
		return 0, err
	}
	tables := fetched[0]
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })

	written := 0
	for _, t := range tables {
		ok, err := p.pullTable(rs.LogicalSchema, t)
		if err != nil {
			return written, err
		}
		if ok {
			written++
		}
	}
	return written, nil
}

// pullTable writes one live table and reports whether it did; a skipped table is reported on stdout.
func (p *puller) pullTable(schema string, t *backend.LiveTable) (bool, error) {
	target := p.index.Decide(p.root, p.dir, schema, t.Name, p.app.pullForce, fileExists)
	if target.SkipReason != "" {
		_, _ = fmt.Fprintf(p.out, "~ skipped  %s\n", target.SkipReason)
		return false, nil
	}

	data, diags, err := pull.Render(t, schema, target.RelPath, p.filter)
	if err != nil {
		return false, err
	}

	if pull.NeedsWindowsNameCheck(target.InPlace, p.app.goos) {
		if reason := pull.WindowsNameProblem(t.Name); reason != "" {
			return false, fmt.Errorf("table %q: cannot create %s: %s", t.Name, target.RelPath, reason)
		}
	}

	if err := writeTableFile(target, data); err != nil {
		return false, err
	}

	printDiags(p.errOut, diags)
	_, _ = fmt.Fprintf(p.out, "✓ %s  %s\n", target.Verb, target.RelPath)
	return true, nil
}

// writeTableFile creates the directory of the target and writes the file.
func writeTableFile(target pull.Target, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target.FilePath), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %s", path.Dir(target.RelPath), diag.ErrText(err))
	}
	if err := os.WriteFile(target.FilePath, data, 0644); err != nil {
		return fmt.Errorf("write %s: %s", target.RelPath, diag.ErrText(err))
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
