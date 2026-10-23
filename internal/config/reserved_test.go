package config

import (
	"strings"
	"testing"
)

func TestReserveAppName(t *testing.T) {
	ReserveAppName("my-cmd", "`hl config my-cmd` is a command")
	ReserveAppName("my-cmd", "a second reason is ignored")
	for _, n := range []string{"my-cmd", "my_cmd"} {
		err := ValidName(n)
		if err == nil || !strings.Contains(err.Error(), "is reserved: `hl config my-cmd` is a command") {
			t.Errorf("ValidName(%s) = %v", n, err)
		}
	}
	got := strings.Join(ReservedAppNames(), " ")
	if !strings.Contains(got, "my-cmd") || !strings.Contains(got, ReservedAppName) {
		t.Errorf("ReservedAppNames() = %s", got)
	}
	ps := Check([]byte("apps:\n  my-cmd: {path: x}\n"))
	if len(ps) != 1 || ps[0].Line != 2 || !strings.Contains(ps[0].Message, "reserved") {
		t.Errorf("Check: %v", ps)
	}
}
