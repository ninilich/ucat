package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/diag"
	"github.com/ninilich/ucat/internal/term"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a new ucat project",
		Long: `Ask for the project name, one logical catalog and schema, the targets and, per target, the
workspace host, the Unity Catalog catalog name and the SQL warehouse ID; then create ucat.yml,
catalogs/<catalog>.yml, targets/<target>.yml and the tables directory in the current directory.
Every answer is checked before a file is written. An empty answer takes the default shown in brackets.`,
		Example: "  ucat init",
		RunE:    runInit,
	}
}

// dialog is the input and the output of the init questions.
type dialog struct {
	out     io.Writer
	scanner *bufio.Scanner
	endLine bool // stdin is not a terminal: nobody types Enter, so ucat ends the prompt line itself
}

func prompt(d *dialog, message, defaultVal string) (string, error) {
	scanner := d.scanner
	_, _ = fmt.Fprint(d.out, message)
	scanned := scanner.Scan()
	if d.endLine { // also after EOF: the next prompt or output must not stick to this one
		_, _ = fmt.Fprintln(d.out)
	}
	if !scanned {
		if err := scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				return "", fmt.Errorf("answer to %q is too long (max 64 KiB)", strings.TrimRight(strings.TrimSpace(message), ": "))
			}
			return "", fmt.Errorf("failed to read answer: %s", diag.ErrText(err))
		}
		return defaultVal, nil // EOF: the default value
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return defaultVal, nil
	}
	return input, nil
}

// validateLogicalName checks a logical catalog or schema name or a target name entered in init with the
// rule of the loader (config.IsFileSafeName): these names become file names.
func validateLogicalName(label, name string) error {
	if name == "" {
		return fmt.Errorf("%s is required", label)
	}
	if !config.IsFileSafeName(name) {
		return fmt.Errorf("%s %q %s", label, name, config.FileSafeNameRule)
	}
	return nil
}

// validateUCName checks a physical UC catalog or schema name entered in init with the rule of validate
// (config.UCNameError) and requires lowercase (Unity Catalog names are lowercase).
func validateUCName(label, name string) error {
	if name == "" {
		return fmt.Errorf("%s is required", label)
	}
	if rule := config.UCNameError(name); rule != "" {
		return fmt.Errorf("%s %q %s", label, name, rule)
	}
	if !config.IsLowerName(name) {
		return fmt.Errorf("%s %q must be lowercase", label, name)
	}
	return nil
}

// initTarget is a target entered in init.
type initTarget struct {
	name        string
	host        string
	ucCatalog   string // physical UC catalog of the logical catalog in this target
	warehouseID string
}

// initAnswers are the answers collected by the init dialog.
type initAnswers struct {
	projectName        string
	logicalCatalogName string
	logicalSchemaName  string
	ucSchemaName       string
	targets            []initTarget
}

func runInit(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	// ucat.yml is looked for in the current directory only, not in its parents (unlike the other
	// commands): init creates a project right here, and a project further up is not a reason to refuse.
	if _, err := os.Stat(config.ProjectFile); err == nil {
		return fmt.Errorf("%s already exists in current directory", config.ProjectFile)
	}

	in := cmd.InOrStdin()
	d := &dialog{out: out, scanner: bufio.NewScanner(in), endLine: !term.IsTerminal(in)}

	a, err := askInit(d)
	if err != nil {
		return err
	}

	// Check pre-existence of the files to create
	if err := checkInitFilesFree(a); err != nil {
		return err
	}

	if err := writeProject(cmd, a); err != nil {
		return err
	}

	// Print next steps
	firstTarget := a.targets[0].name
	_, _ = fmt.Fprintf(out, "\nNext steps:\n  ucat pull -t %s    # import existing tables\n  ucat plan -t %s    # preview changes\n",
		firstTarget, firstTarget)

	return nil
}

// askInit runs the dialog: asks the questions in order and validates each answer right away.
func askInit(d *dialog) (initAnswers, error) {
	var a initAnswers

	// Collect inputs
	projectName, err := prompt(d, "Project name: ", "")
	if err != nil {
		return a, err
	}
	if projectName == "" {
		return a, fmt.Errorf("project name is required")
	}
	a.projectName = projectName

	logicalCatalogName, err := prompt(d, "Logical catalog name [main]: ", "main")
	if err != nil {
		return a, err
	}

	if err = validateLogicalName("logical catalog name", logicalCatalogName); err != nil {
		return a, err
	}
	a.logicalCatalogName = logicalCatalogName

	// logical schema name is required, no default
	logicalSchemaName, err := prompt(d, "Logical schema name: ", "")
	if err != nil {
		return a, err
	}
	if err = validateLogicalName("logical schema name", logicalSchemaName); err != nil {
		return a, err
	}
	a.logicalSchemaName = logicalSchemaName

	// UC schema name defaults to logical schema name
	ucSchemaName, err := prompt(d, fmt.Sprintf("UC schema name [%s]: ", logicalSchemaName), logicalSchemaName)
	if err != nil {
		return a, err
	}

	if err = validateUCName("UC schema name", ucSchemaName); err != nil {
		return a, err
	}
	a.ucSchemaName = ucSchemaName

	targetNames, err := askTargetNames(d)
	if err != nil {
		return a, err
	}
	a.targets, err = askTargetDetails(d, targetNames, a.logicalCatalogName)
	if err != nil {
		return a, err
	}
	return a, nil
}

// askTargetNames asks for the comma-separated target names and validates them.
func askTargetNames(d *dialog) ([]string, error) {
	// default targets changed from "dev,prod" to "dev"
	targetNamesRaw, err := prompt(d, "Target names (comma-separated) [dev]: ", "dev")
	if err != nil {
		return nil, err
	}

	// Parse target names
	var targetNames []string
	seenTargets := make(map[string]bool)
	for _, name := range strings.Split(targetNamesRaw, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			if err := validateLogicalName("target name", name); err != nil {
				return nil, err
			}
			// a target is the file targets/<name>.yml; file systems on macOS and Windows ignore case
			key := config.FoldName(name)
			if seenTargets[key] {
				return nil, fmt.Errorf("duplicate target name %q", name)
			}
			seenTargets[key] = true
			targetNames = append(targetNames, name)
		}
	}

	if len(targetNames) == 0 {
		return nil, fmt.Errorf("target names: at least one name is required")
	}
	return targetNames, nil
}

// defaultUCCatalog is the default answer of "UC catalog name for target": the logical catalog name when
// there is one target, <logical>_<target> when there are several (so that the targets do not share a
// catalog by default), in lower case in both cases (UC names are lower case; the logical name may not be).
func defaultUCCatalog(logicalCatalog, target string, targets int) string {
	if targets == 1 {
		return strings.ToLower(logicalCatalog)
	}
	return strings.ToLower(logicalCatalog + "_" + target)
}

// askTargetDetails collects host, UC catalog and warehouse per target. The UC catalog is the physical
// catalog the logical catalog maps to in this target (default: see defaultUCCatalog); each answer is
// checked right away, before any file is written.
func askTargetDetails(d *dialog, targetNames []string, logicalCatalog string) ([]initTarget, error) {
	targets := make([]initTarget, 0, len(targetNames))
	for _, name := range targetNames {
		host, err := prompt(d, fmt.Sprintf("Workspace host for '%s': ", name), "")
		if err != nil {
			return nil, err
		}
		def := defaultUCCatalog(logicalCatalog, name, len(targetNames))
		ucCatalog, err := prompt(d, fmt.Sprintf("UC catalog name for target '%s' [%s]: ", name, def), def)
		if err != nil {
			return nil, err
		}
		if err = validateUCName(fmt.Sprintf("UC catalog name for target '%s'", name), ucCatalog); err != nil {
			return nil, err
		}
		wh, err := prompt(d, fmt.Sprintf("SQL warehouse ID for target '%s' (from Databricks UI > SQL Warehouses, optional): ", name), "")
		if err != nil {
			return nil, err
		}
		targets = append(targets, initTarget{name: name, host: host, ucCatalog: ucCatalog, warehouseID: wh})
	}
	return targets, nil
}

// checkInitFilesFree fails when one of the files init is going to create already exists.
func checkInitFilesFree(a initAnswers) error {
	filesToCreate := []string{config.ProjectFile, config.CatalogFile(a.logicalCatalogName)}
	for _, t := range a.targets {
		filesToCreate = append(filesToCreate, config.TargetFile(t.name))
	}

	for _, path := range filesToCreate {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("file already exists: %s", path)
		}
	}
	return nil
}

// projectWriter creates the init files and remembers what it created (no rollback: on a failure in
// the middle the user is told what already exists).
type projectWriter struct {
	cmd     *cobra.Command
	created []string
}

// fail reports the files created so far (if any) and returns err.
func (w *projectWriter) fail(err error) error {
	if len(w.created) > 0 {
		_, _ = fmt.Fprintf(w.cmd.ErrOrStderr(), "project was created partially: %s; remove them or fix the cause and rerun init in an empty directory\n", strings.Join(w.created, ", "))
	}
	return err
}

// mkdir creates dir (as a "name/" entry in created, unless it already existed before init).
func (w *projectWriter) mkdir(dir string) error {
	_, statErr := os.Stat(dir)
	existed := statErr == nil
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %s", dir, diag.ErrText(err))
	}
	if !existed {
		w.created = append(w.created, dir+"/")
	}
	return nil
}

// writeFile creates the file and prints the "Created" line.
func (w *projectWriter) writeFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %s", path, diag.ErrText(err))
	}
	w.created = append(w.created, path)
	_, _ = fmt.Fprintf(w.cmd.OutOrStdout(), "✓ Created %s\n", path)
	return nil
}

// writeProject creates the project files and directories.
func writeProject(cmd *cobra.Command, a initAnswers) error {
	w := &projectWriter{cmd: cmd}
	steps := []func(initAnswers) error{w.writeProjectFile, w.writeCatalog, w.writeTargets, w.writeTablesDir}
	for _, step := range steps {
		if err := step(a); err != nil {
			return w.fail(err)
		}
	}
	return nil
}

// writeProjectFile writes ucat.yml.
func (w *projectWriter) writeProjectFile(a initAnswers) error {
	content, err := config.ScaffoldProject(a.projectName)
	if err != nil {
		return err
	}
	return w.writeFile(config.ProjectFile, content)
}

// writeCatalog writes catalogs/<name>.yml. Its uc_name is the logical name (in lower case, as UC names are): the schemas do not depend on
// the environment, the UC catalog of each target is set in targets/<target>.yml. The UC schema name
// goes to uc_name of the schema section.
func (w *projectWriter) writeCatalog(a initAnswers) error {
	if err := w.mkdir(config.CatalogsDir); err != nil {
		return err
	}
	content, err := config.ScaffoldCatalog(a.logicalCatalogName, strings.ToLower(a.logicalCatalogName), a.logicalSchemaName, a.ucSchemaName)
	if err != nil {
		return err
	}
	return w.writeFile(config.CatalogFile(a.logicalCatalogName), content)
}

// writeTargets writes targets/<name>.yml for every target.
func (w *projectWriter) writeTargets(a initAnswers) error {
	if err := w.mkdir(config.TargetsDir); err != nil {
		return err
	}
	for _, t := range a.targets {
		content, err := config.ScaffoldTarget(config.WorkspaceConfig{Host: t.host, WarehouseID: t.warehouseID}, a.logicalCatalogName, t.ucCatalog)
		if err != nil {
			return err
		}
		if err := w.writeFile(config.TargetFile(t.name), content); err != nil {
			return err
		}
	}
	return nil
}

// writeTablesDir creates the tables directory.
func (w *projectWriter) writeTablesDir(initAnswers) error {
	if err := w.mkdir(config.DefaultTablesDir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w.cmd.OutOrStdout(), "✓ Created %s/\n", config.DefaultTablesDir)
	return nil
}
