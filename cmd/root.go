// Package cmd defines the ucat command-line interface (cobra commands and the
// shared load, diff, plan pipeline behind them).
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/uc"
	"github.com/spf13/cobra"
)

// backendFactory creates the backend for a workspace host and warehouse id.
type backendFactory func(host, wh string) (backend.UCBackend, error)

// app holds the dependencies of the commands. Tests replace them on their own app,
// production code uses newApp.
type app struct {
	newBackend backendFactory
	goos       string                 // operating system for OS-dependent checks (pull); runtime.GOOS in production
	getwd      func() (string, error) // working directory the project is searched from; os.Getwd in production
	now        func() time.Time       // clock of the apply progress times; time.Now in production
	flags
}

// flags are the command-line flag values; every command tree binds its flags to its own app.
type flags struct {
	target                                     string
	autoApprove, showSQL, jsonOutput           bool
	dropSchema, dropTable                      string
	pullForce                                  bool
	pullCatalog, pullSchema, pullTblProperties string
	detailedExitCode                           bool
	selSchemas, selTables                      []string // --schema and --table of plan and apply
}

// addTargetFlag binds -t/--target to c; the commands that talk to a target (plan, apply, pull, drop,
// validate) have it, init and version do not.
func (a *app) addTargetFlag(c *cobra.Command) {
	c.Flags().StringVarP(&a.target, "target", "t", "", "target environment name (defined in "+config.TargetFile("<name>")+")")
}

// addAutoApproveFlag binds --auto-approve to c (apply, drop).
func (a *app) addAutoApproveFlag(c *cobra.Command) {
	c.Flags().BoolVar(&a.autoApprove, "auto-approve", false, "skip confirmation prompts for destructive operations")
}

// addPlanOutputFlags binds --json and --show-sql to c (plan, apply).
func (a *app) addPlanOutputFlags(c *cobra.Command) {
	c.Flags().BoolVar(&a.showSQL, "show-sql", false, "show SQL statements for each planned change")
	c.Flags().BoolVar(&a.jsonOutput, "json", false, "output in machine-readable JSON format (see schema_version in the output)")
}

// defaultBackend is the production factory: a Databricks backend.
func defaultBackend(host, wh string) (backend.UCBackend, error) {
	return uc.NewDatabricksBackend(host, wh)
}

func newApp() *app {
	return &app{newBackend: defaultBackend, goos: runtime.GOOS, getwd: os.Getwd, now: time.Now}
}

// newRootCmd builds a new command tree on every call. Flags are bound to the fields of a,
// so every tree starts from the defaults.
func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "ucat",
		Short: "Declarative lifecycle management for Databricks Unity Catalog tables",
		Long: `ucat manages Unity Catalog managed tables declaratively using YAML manifests.

A project is a directory with ucat.yml, catalogs/*.yml, targets/*.yml and table files
(tables/<schema>/<table>.yml). "ucat plan" shows what would change in a target, "ucat apply"
applies it; "ucat pull" imports existing tables as YAML.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       versionText(),
	}
	// --version is declared here (not left to cobra) because cobra would also give it the shorthand -v.
	root.Flags().Bool("version", false, "print the version and exit")
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(
		a.newApplyCmd(),
		a.newDropCmd(),
		newInitCmd(),
		a.newPlanCmd(),
		a.newPullCmd(),
		a.newValidateCmd(),
		newVersionCmd(),
	)
	return root
}

// exitInterrupted is the exit code of a run stopped by Ctrl+C or SIGTERM: 128 + SIGINT, the shell
// convention. It differs from 1 (error) and 2 (plan --detailed-exitcode: changes present).
const exitInterrupted = 130

// Execute runs the ucat command line and exits the process with its exit code (see run). The first
// Ctrl+C or SIGTERM cancels the context of the command: reads stop, apply finishes the running
// step and starts no new one. The handler is removed right after, so a second signal ends the
// process at once.
func Execute() {
	ctx, stop := signalContext(context.Background())
	code := runContext(ctx, newRootCmd(newApp()), os.Stderr)
	stop()
	os.Exit(code)
}

// signalContext returns a context that is canceled by the first SIGINT or SIGTERM. The handler is
// removed before the context is canceled, so a second signal (sent after the cancel is seen) has its
// default effect and ends the process. stop releases the handler and the goroutine when the command
// is done.
func signalContext(parent context.Context) (ctx context.Context, stop context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigs:
			signal.Stop(sigs)
			cancel()
		case <-ctx.Done():
			signal.Stop(sigs)
		}
	}()
	return ctx, cancel
}

// exitCodeError signals that the process should exit with the given code.
// Used by --detailed-exitcode to distinguish "changes present" (exit 2) from errors (exit 1).
type exitCodeError struct {
	code int
}

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// interruptedError is the error of a command that stopped because of Ctrl+C or SIGTERM; msg says
// where ("interrupted after step 3/12; 9 steps not applied"). It is context.Canceled for errors.Is.
type interruptedError struct {
	msg string
}

func (e *interruptedError) Error() string { return e.msg }

// Unwrap returns context.Canceled.
func (e *interruptedError) Unwrap() error { return context.Canceled }

// run executes the command tree without a signal handler; see runContext.
func run(root *cobra.Command, stderr io.Writer) int {
	return runContext(context.Background(), root, stderr)
}

// runContext executes the command tree with ctx as the context of the command and returns the
// process exit code: 0 on success, the code of an *exitCodeError (printed nothing), 130 for an
// interrupt, otherwise 1 with "Error: ..." written to stderr.
// A panic is an internal error with code 1: the runtime would exit with 2, which plan
// --detailed-exitcode uses for "changes present".
func runContext(ctx context.Context, root *cobra.Command, stderr io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(stderr, "Error: internal error: %v\n", r)
			code = 1
		}
	}()
	return exitCode(root.ExecuteContext(ctx), stderr)
}

// exitCode maps the error of a command run to the exit code and prints it the way the binary does.
// An interrupt (any error that is context.Canceled) prints "Error: interrupted ..." and is 130.
func exitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var exitErr *exitCodeError
	if errors.As(err, &exitErr) {
		return exitErr.code
	}
	if errors.Is(err, context.Canceled) {
		var ie *interruptedError
		if errors.As(err, &ie) {
			_, _ = fmt.Fprintln(stderr, "Error:", ie.msg)
		} else {
			_, _ = fmt.Fprintln(stderr, "Error: interrupted")
		}
		return exitInterrupted
	}
	_, _ = fmt.Fprintln(stderr, "Error:", err)
	return 1
}
