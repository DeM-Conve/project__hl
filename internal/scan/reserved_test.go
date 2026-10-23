package scan

import (
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// sync and init skip every name config reserves, not only the proxy's.
func TestReservedFollowsConfigList(t *testing.T) {
	if !reserved(defaults.ReservedAppName) {
		t.Errorf("%s should be reserved", defaults.ReservedAppName)
	}
	if reserved("my-tool") {
		t.Fatal("my-tool reserved before anyone reserved it")
	}
	config.ReserveAppName("my-tool", "a test reserves it")
	for _, n := range []string{"my-tool", "my_tool", "my.tool"} {
		if !reserved(n) {
			t.Errorf("%s should be reserved (same ID as my-tool)", n)
		}
	}
}
