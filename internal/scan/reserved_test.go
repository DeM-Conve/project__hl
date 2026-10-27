package scan

import (
	"testing"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// sync and init skip every name config reserves, by its ID.
func TestReservedFollowsConfigList(t *testing.T) {
	for _, n := range []string{defaults.ReservedAppName} {
		if !reserved(n) {
			t.Errorf("%s should be reserved", n)
		}
	}
	if reserved("my-tool") {
		t.Error("my-tool is not reserved")
	}
}
