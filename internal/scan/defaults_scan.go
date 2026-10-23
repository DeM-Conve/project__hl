package scan

// ===== Defaults for `hl init` and `hl sync` =====
// Every value init and sync use when hl.yml or a flag doesn't say otherwise,
// in one block, to be folded into internal/defaults. The compose file names
// searched are compose.DefaultFiles (one place, shared with `hl add`).

const (
	// IgnoreFile: a folder containing a file with this name is never picked up as an app.
	IgnoreFile = ".hlignore"
	// DefaultDepth is hl.yml's scan_depth: 1 = only direct subfolders of a projects folder are apps.
	DefaultDepth = 1
	// AltPort is the proxy port `hl init` suggests when the default one is in use.
	AltPort = 8080
	// BindNetwork is the proxy.bind `hl init` writes for "reachable from your network".
	BindNetwork = "0.0.0.0"
)

// DefaultIgnore are folder-name patterns (filepath.Match) sync always skips;
// hl.yml's `ignore:` adds to them.
var DefaultIgnore = []string{".*", "node_modules", "vendor", "__pycache__", "venv"}

// CandidateProjects are the projects folders `hl init` suggests, relative to
// the user's home, when they exist. Only a suggestion: the user is asked.
var CandidateProjects = []string{"code", "projects", "src"}

// ===== end defaults =====
