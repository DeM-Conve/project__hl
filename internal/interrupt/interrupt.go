// Package interrupt handles Ctrl-C and SIGTERM while hl waits for docker.
//
// The first signal cancels the context: hl stops starting new work and waits
// for the command that is running to stop by itself. A Ctrl-C on a terminal
// reaches that command too (it is in the same process group), so it is not
// sent again; any other signal is passed on. A second signal kills the
// command, so hl never hangs.
package interrupt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// Error is the cause of a context cancelled by a signal.
type Error struct{ Signal os.Signal }

func (e *Error) Error() string { return "interrupted (" + e.Signal.String() + ")" }

// ErrInterrupted matches any *Error with errors.Is.
var ErrInterrupted = errors.New("interrupted")

func (e *Error) Is(target error) bool { return target == ErrInterrupted }

type hardKey struct{}

// Notify returns a context cancelled by the first SIGINT or SIGTERM. stop
// releases the signal handler.
func Notify(parent context.Context) (ctx context.Context, stop func()) {
	ctx, cancel := context.WithCancelCause(parent)
	hard := make(chan struct{})
	ctx = context.WithValue(ctx, hardKey{}, (<-chan struct{})(hard))
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		n := 0
		for {
			select {
			case s := <-sigs:
				n++
				if n == 1 {
					cancel(&Error{Signal: s})
				} else {
					close(hard)
					return
				}
			case <-done:
				return
			}
		}
	}()
	return ctx, func() {
		signal.Stop(sigs)
		close(done)
		cancel(context.Canceled)
	}
}

// Signal is the signal that cancelled ctx, or nil.
func Signal(ctx context.Context) os.Signal {
	var e *Error
	if errors.As(context.Cause(ctx), &e) {
		return e.Signal
	}
	return nil
}

// Err is the interruption error for ctx, or nil when it wasn't interrupted.
func Err(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	var e *Error
	if errors.As(context.Cause(ctx), &e) {
		return e
	}
	return ctx.Err()
}

func hardChan(ctx context.Context) <-chan struct{} {
	if c, ok := ctx.Value(hardKey{}).(<-chan struct{}); ok {
		return c
	}
	return nil
}

// Run starts cmd and waits for it. When ctx is cancelled first, the command
// gets the chance to stop by itself (see the package comment) and Run returns
// the interruption, whatever the command's own result.
func Run(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return Err(ctx)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return Wait(ctx, cmd)
}

// Wait waits for a started cmd the way Run does.
func Wait(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	if s := Signal(ctx); s != nil && s != os.Interrupt {
		_ = cmd.Process.Signal(s)
	} else if s == nil {
		// Cancelled without a signal: ask it to stop.
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	var waitErr error // the command's own result no longer matters
	select {
	case waitErr = <-done:
	case <-hardChan(ctx):
		_ = cmd.Process.Kill()
		waitErr = <-done
	}
	_ = waitErr
	return Err(ctx)
}

// ExitCode is the conventional shell exit code for an interruption: 128+signal.
func ExitCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		if s, ok := e.Signal.(syscall.Signal); ok {
			return 128 + int(s)
		}
	}
	return 130
}

// String describes an interruption for a message.
func String(err error) string { return fmt.Sprint(err) }
