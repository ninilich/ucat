//go:build !windows

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const signalHelperEnv = "UCAT_TEST_SIGNAL_HELPER"

// TestSignalHelperProcess is not a test: it is the body of the child process of
// TestSignalContext_SecondSignalKillsProcess. It installs signalContext, reports, and then sits in a
// "hanging statement" for a minute.
func TestSignalHelperProcess(t *testing.T) {
	if os.Getenv(signalHelperEnv) != "1" {
		t.Skip("helper process of TestSignalContext_SecondSignalKillsProcess")
	}
	ctx, stop := signalContext(context.Background())
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
	fmt.Println("canceled")
	time.Sleep(time.Minute) // the "hanging step": only a second signal can end this process
	fmt.Println("survived")
}

// The first Ctrl+C cancels the context; the handler is gone after it, so the second one ends the process
// at once with the default action (the way out of a statement that hangs). Kills the mutant that
// drops the "stop after the first signal" goroutine of signalContext.
func TestSignalContext_SecondSignalKillsProcess(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSignalHelperProcess$", "-test.v")
			cmd.Env = append(os.Environ(), signalHelperEnv+"=1")
			out, err := cmd.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, cmd.Start())
			defer func() { _ = cmd.Process.Kill() }()

			lines := make(chan string, 16)
			go func() {
				sc := bufio.NewScanner(out)
				for sc.Scan() {
					lines <- sc.Text()
				}
				close(lines)
			}()
			waitFor := func(want string) { waitForLine(t, lines, want) }
			waitFor("ready")
			require.NoError(t, cmd.Process.Signal(sig))
			waitFor("canceled")

			require.NoError(t, cmd.Process.Signal(sig))
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				var ee *exec.ExitError
				require.True(t, errors.As(err, &ee), "the helper must end with a signal, got %v", err)
				ws, ok := ee.Sys().(syscall.WaitStatus)
				require.True(t, ok)
				assert.True(t, ws.Signaled(), "ended by the second %s, not by its own exit", sig)
				assert.Equal(t, sig, ws.Signal())
			case <-time.After(10 * time.Second):
				t.Fatal("the second signal did not end the process")
			}
		})
	}
}

// waitForLine reads lines until one equals want; it fails the test if the stream ends or 20 seconds pass.
func waitForLine(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	timeout := time.After(20 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("the helper ended before it printed %q", want)
			}
			if l == want {
				return
			}
		case <-timeout:
			t.Fatalf("no %q from the helper", want)
		}
	}
}
