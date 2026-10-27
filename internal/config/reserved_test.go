package config

import (
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

func TestReservedNames(t *testing.T) {
	for _, n := range []string{defaults.ReservedAppName} {
		if err := ValidName(n); err == nil || !strings.Contains(err.Error(), "is reserved") {
			t.Errorf("ValidName(%s) = %v", n, err)
		}
	}
	if got := strings.Join(ReservedAppNames(), " "); got != defaults.ReservedAppName {
		t.Errorf("ReservedAppNames() = %s", got)
	}
	ps := Check([]byte("apps:\n  proxy: {path: x}\n"))
	if len(ps) != 1 || ps[0].Line != 2 || !strings.Contains(ps[0].Message, "reserved") {
		t.Errorf("Check: %v", ps)
	}
}
