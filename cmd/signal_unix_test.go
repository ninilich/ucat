//go:build !windows

package cmd

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The first SIGINT or SIGTERM cancels the context (the real signal, sent to this process). A second
// signal would end the process, so it is not sent here.
func TestSignalContext_CanceledBySignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, stop := signalContext(context.Background())
			defer stop()
			require.NoError(t, ctx.Err())

			require.NoError(t, syscall.Kill(os.Getpid(), sig))
			select {
			case <-ctx.Done():
				assert.ErrorIs(t, ctx.Err(), context.Canceled)
			case <-time.After(10 * time.Second):
				t.Fatal("the signal did not cancel the context")
			}
		})
	}
}

// Without a signal the context lives until stop; the parent still cancels it.
func TestSignalContext_ParentAndStop(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, stop := signalContext(parent)
	defer stop()
	assert.NoError(t, ctx.Err())
	cancel()
	<-ctx.Done()
	assert.ErrorIs(t, ctx.Err(), context.Canceled)

	ctx2, stop2 := signalContext(context.Background())
	stop2()
	<-ctx2.Done()
	assert.ErrorIs(t, ctx2.Err(), context.Canceled)
}
