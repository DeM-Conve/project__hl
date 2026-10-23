package config

import (
	"fmt"
	"sort"
	"sync"
)

// Names an app can't have, each with the reason. ReservedAppName is always one; the CLI adds
// the names of its `hl config` subcommands (so `hl config <app>` is never shadowed) by calling
// ReserveAppName at start-up, so this stays the one list everything checks against.
var (
	reservedMu    sync.RWMutex
	reservedNames = map[string]string{
		ReservedAppName: fmt.Sprintf("its compose project would be %s, the proxy's", ProjectName(ReservedAppName)),
	}
)

// ReserveAppName adds name to the names an app can't have; why completes "app name x is
// reserved: ...". A name reserved already keeps its first reason.
func ReserveAppName(name, why string) {
	reservedMu.Lock()
	defer reservedMu.Unlock()
	if _, ok := reservedNames[ID(name)]; !ok {
		reservedNames[ID(name)] = why
	}
}

// ReservedAppNames are the names an app can't have, sorted.
func ReservedAppNames() []string {
	reservedMu.RLock()
	defer reservedMu.RUnlock()
	out := make([]string, 0, len(reservedNames))
	for n := range reservedNames {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// IsReserved reports whether name (or a name with the same ID, like pro_xy) is reserved, and why.
func IsReserved(name string) (why string, ok bool) {
	reservedMu.RLock()
	defer reservedMu.RUnlock()
	why, ok = reservedNames[ID(name)]
	return why, ok
}
