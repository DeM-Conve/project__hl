package interrupt

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestNotifyCancelsOnSignal(t *testing.T) {
	ctx, stop := Notify(context.Background())
	defer stop()
	if Err(ctx) != nil || Signal(ctx) != nil {
		t.Fatal("not interrupted yet")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("not cancelled")
	}
	if Signal(ctx) != syscall.SIGTERM || !errors.Is(Err(ctx), ErrInterrupted) || ExitCode(Err(ctx)) != 143 {
		t.Errorf("signal %v, err %v, code %d", Signal(ctx), Err(ctx), ExitCode(Err(ctx)))
	}
	// A second signal is the hard stop.
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hardChan(ctx):
	case <-time.After(5 * time.Second):
		t.Fatal("no hard stop")
	}
}

func TestRunPassesOnTermAndWaits(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cmd := exec.Command("sleep", "30")
	done := make(chan error)
	go func() { done <- Run(ctx, cmd) }()
	time.Sleep(100 * time.Millisecond)
	cancel(&Error{Signal: syscall.SIGTERM})
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the command was not stopped")
	}
	// Already interrupted: nothing starts.
	if err := Run(ctx, exec.Command("sleep", "30")); !errors.Is(err, ErrInterrupted) {
		t.Errorf("err = %v", err)
	}
	// Not interrupted: the command's own result.
	if err := Run(context.Background(), exec.Command("sh", "-c", "exit 3")); err == nil {
		t.Error("want the exit error")
	}
	if ExitCode(errors.New("x")) != 130 {
		t.Error("default exit code")
	}
}
