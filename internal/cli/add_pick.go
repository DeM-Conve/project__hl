package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
)

// pickServicePort fills in the app's service and port for `hl add` from the compose model.
// Flags win. When there is more than one candidate it asks on a terminal (hl's usual pick is
// the default); without a terminal it stops and names the flag to pass, rather than guess.
func pickServicePort(m *compose.Model, a *config.App) error {
	if a.Service == "" {
		var published []string
		for name, s := range m.Services {
			if len(s.Ports) > 0 {
				published = append(published, name)
			}
		}
		sort.Strings(published)
		switch len(published) {
		case 0:
			_, err := m.Exposed() // its message names --service and --port
			return err
		case 1:
			a.Service = published[0]
		default:
			def := 0
			if usual, err := m.Exposed(); err == nil {
				def = indexOf(published, usual)
			}
			if !stdinIsTerminal() {
				return fmt.Errorf("several services publish ports (%s) - pass --service, e.g. --service %s",
					strings.Join(published, ", "), published[def])
			}
			opts := make([]string, len(published))
			for i, s := range published {
				opts[i] = fmt.Sprintf("%s  (container port %s)", s, joinInts(targetPorts(m.Services[s])))
			}
			i, err := choose("Several services publish ports. Which one should the proxy forward to?", opts, def)
			if err != nil {
				return fmt.Errorf("%w - pass --service", err)
			}
			a.Service = published[i]
		}
	}
	if a.Port == 0 {
		s, ok := m.Services[a.Service]
		if !ok {
			names := make([]string, 0, len(m.Services))
			for n := range m.Services {
				names = append(names, n)
			}
			sort.Strings(names)
			return fmt.Errorf("no service %q in the app's compose file (services: %s)", a.Service, strings.Join(names, ", "))
		}
		ports := targetPorts(s)
		switch len(ports) {
		case 0:
			_, err := m.TargetPort(a.Service) // its message names --port
			return err
		case 1:
			a.Port = ports[0]
		default:
			if !stdinIsTerminal() {
				return fmt.Errorf("service %s publishes several ports (%s) - pass --port, e.g. --port %d",
					a.Service, joinInts(ports), ports[0])
			}
			opts := make([]string, len(ports))
			for i, p := range ports {
				opts[i] = strconv.Itoa(p)
			}
			i, err := choose(fmt.Sprintf("%s publishes several ports. Which container port serves the app?", a.Service), opts, 0)
			if err != nil {
				return fmt.Errorf("%w - pass --port", err)
			}
			a.Port = ports[i]
		}
	}
	return nil
}

// targetPorts are the service's published container ports, without repeats, in its order.
func targetPorts(s compose.Service) []int {
	var out []int
	seen := map[int]bool{}
	for _, p := range s.Ports {
		if p.Target != 0 && !seen[p.Target] {
			seen[p.Target] = true
			out = append(out, p.Target)
		}
	}
	return out
}

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return 0
}
