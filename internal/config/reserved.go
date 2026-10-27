package config

import (
	"fmt"
	"sort"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// reservedNames are the names an app can't have, each with the reason: the one list that
// hl.yml, hl add, hl init and hl sync check against.
var reservedNames = map[string]string{
	defaults.ReservedAppName: fmt.Sprintf("its compose project would be %s, the proxy's, and `hl up %s` and the like mean the proxy",
		ProjectName(defaults.ReservedAppName), defaults.ReservedAppName),
}

// ReservedAppNames are the names an app can't have, sorted.
func ReservedAppNames() []string {
	out := make([]string, 0, len(reservedNames))
	for n := range reservedNames {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// IsReserved reports whether name (or a name with the same ID, like pro_xy) is reserved, and why.
func IsReserved(name string) (why string, ok bool) {
	why, ok = reservedNames[ID(name)]
	return why, ok
}
