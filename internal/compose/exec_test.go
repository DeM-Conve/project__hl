package compose

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestExecKeepsStderrForTheError(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo out; echo 'Cannot connect to the Docker daemon' >&2; exit 4")
	err := Exec(context.Background(), cmd)
	var e *Error
	if !errors.As(err, &e) || e.Code != 4 || !strings.Contains(e.Stderr, "Cannot connect") {
		t.Fatalf("err = %#v", err)
	}
	if !strings.HasPrefix(e.Error(), "sh ") || !strings.HasSuffix(e.Error(), "failed (exit status 4)") {
		t.Errorf("message: %s", e)
	}
	err = Exec(context.Background(), exec.Command("no-such-command-hl-test"))
	if !errors.As(err, &e) || e.Error() != "no-such-command-hl-test not found on PATH" {
		t.Errorf("not found: %v", err)
	}
}

func TestDescribe(t *testing.T) {
	p := &Project{Name: "hl-a", Dir: "/x y", Files: []string{"/x y/compose.yml"}, EnvFiles: []string{"/x y/.env"}}
	if got := describe(append([]string{"docker"}, p.Args("up", "--detach", "--wait")...)); got != "docker compose up" {
		t.Errorf("describe = %q", got)
	}
	if got := describe([]string{"docker", "ps", "--format", "{{.Names}}"}); got != "docker ps" {
		t.Errorf("describe = %q", got)
	}
}

func TestTailBuffer(t *testing.T) {
	b := &tailBuffer{max: 5}
	_, _ = b.Write([]byte("abc"))
	_, _ = b.Write([]byte("defgh"))
	if b.String() != "defgh" {
		t.Errorf("tail = %q", b.String())
	}
}

func TestTargetPort(t *testing.T) {
	m := &Model{Services: map[string]Service{"web": {Ports: []Port{{Target: 8080}}}, "db": {}}}
	if p, err := m.TargetPort("web"); err != nil || p != 8080 {
		t.Errorf("web: %d %v", p, err)
	}
	if _, err := m.TargetPort("db"); err == nil {
		t.Error("db publishes nothing")
	}
	if _, err := m.TargetPort("x"); err == nil {
		t.Error("no such service")
	}
}
