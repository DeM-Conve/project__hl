// Package defaults is the one place for every value hl uses when hl.yml, a
// flag or the environment doesn't say otherwise. Code reads these names
// instead of repeating literals, so a default changes in exactly one place.
//
// Values a user can override say where (hl.yml key, flag or env var); the
// others are names hl owns (generated files, labels) and are listed here so
// they are not scattered either.
package defaults

import "time"

// hl.yml: proxy.*
const (
	// Bind is proxy.bind: listen on this machine only.
	Bind = "127.0.0.1"
	// Port is proxy.port: the host port the proxy listens on.
	Port = 80
	// Domain is proxy.domain: path routing serves <domain>/<app>/, host routing <app>.<domain>.
	Domain = "localhost"
	// Routing is proxy.routing, the routing of apps that don't set their own.
	Routing = "path"
	// Dashboard is proxy.dashboard: the Traefik dashboard is off.
	Dashboard = false
)

// Names hl gives Docker objects.
const (
	// Prefix starts every compose project hl runs: hl-<app>, or hl-<name>-<app>
	// for a homelab with a `name`.
	Prefix = "hl-"
	// ProxyProjectSuffix and NetworkSuffix end the default proxy project and
	// network of a named homelab: hl-<name>-proxy.
	ProxyProjectSuffix = "proxy"
	NetworkSuffix      = "proxy"
	// OverrideProxyNetworkKey is the key the proxy network has inside each
	// generated app override, mapped to the real network name; unlikely to
	// clash with a network of the app's own.
	OverrideProxyNetworkKey = "hl-proxy"
	// TraefikLabel prefixes the Traefik labels hl writes.
	TraefikLabel = "traefik"
	// Labels docker compose sets on every container; hl reads them to find
	// its projects and which homelab owns them.
	ComposeProjectLabel     = "com.docker.compose.project"
	ComposeConfigFilesLabel = "com.docker.compose.project.config_files"
	ComposeServiceLabel     = "com.docker.compose.service"
	ComposeWorkingDirLabel  = "com.docker.compose.project.working_dir"
	ComposeConfigHashLabel  = "com.docker.compose.config-hash"
)

// The proxy (folded in from the proxy-file work). Settings in hl.yml
// (proxy.*) override the ones marked "hl.yml:".
const (
	// TestedTraefikVersion is the Traefik release hl is tested with; `hl doctor`
	// warns when proxy.image is older. v3.6.1+ is needed for Docker Engine 29's API.
	TestedTraefikVersion = "3.6.25"
	// DefaultProxyImage - hl.yml: proxy.image.
	DefaultProxyImage = "traefik:v" + TestedTraefikVersion
	// DefaultProxyProject is the proxy's compose project - hl.yml: proxy.project.
	// A homelab with a `name` defaults to hl-<name>-proxy instead.
	DefaultProxyProject = "hl-proxy"
	// DefaultProxyNetwork is the network the proxy shares with apps - hl.yml: proxy.network.
	// A homelab with a `name` defaults to hl-<name>-proxy instead.
	DefaultProxyNetwork = "hl-proxy"
	// DefaultProxyLogLevel is Traefik's own default - hl.yml: proxy.log_level.
	DefaultProxyLogLevel = "ERROR"

	// ProxyStateDir is the proxy's folder under the homelab's generated folder.
	ProxyStateDir = "proxy"
	// ProxyComposeFile and TraefikStaticFile are the files hl writes there.
	ProxyComposeFile  = "compose.yml"
	TraefikStaticFile = "traefik.yml"

	// ProxyService is the proxy's compose service; ProxyNetworkKey its network's key in that file.
	ProxyService    = "traefik"
	ProxyNetworkKey = "proxy"
	// ProxyRestart is the proxy container's restart policy.
	ProxyRestart = "unless-stopped"
	// ProxyContainerPort is the port the web entrypoint listens on inside the
	// container; proxy.bind:proxy.port is published to it.
	ProxyContainerPort = 80
	// EntryPointWeb is the entrypoint every app's router uses.
	EntryPointWeb = "web"
	// EntryPointPing and PingPort serve /ping for the healthcheck, inside the
	// container only (Traefik's own default names).
	EntryPointPing = "traefik"
	PingPort       = 8080

	// Paths inside the proxy container.
	DockerSocket      = "/var/run/docker.sock"
	TraefikStaticPath = "/etc/traefik/traefik.yml" // Traefik reads it without flags
	TraefikDynamicDir = "/etc/traefik/dynamic"     // the folder of proxy.dynamic, read-only

	// Healthcheck of the proxy container (traefik healthcheck --ping).
	ProxyHealthInterval    = "10s"
	ProxyHealthTimeout     = "5s"
	ProxyHealthRetries     = 3
	ProxyHealthStartPeriod = "5s"

	// AccessLogFormat is Traefik's access log format when proxy.access_log is on.
	AccessLogFormat = "common"

	// The dashboard: router name and host name prefix (traefik.<domain>).
	DashboardRouter     = "hl-dashboard"
	DashboardHostPrefix = "traefik."

	// ReservedAppName can't be an app's name (nor a name whose ID becomes it):
	// its compose project would be the proxy's default one.
	ReservedAppName = "proxy"
	// ExampleDynamicFile is where error messages suggest putting proxy.dynamic.
	ExampleDynamicFile = "proxy/dynamic.yml"

	// StaticConfigLabel holds a hash of the static config on the proxy
	// container, so a changed file recreates the container on `hl proxy up`.
	StaticConfigLabel = "hl.static-config"
)

// ProxyLogLevels are the values proxy.log_level accepts.
var ProxyLogLevels = []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL", "PANIC"}

// Files and folders.
const (
	// ConfigFile is the config file at the root of a homelab folder.
	ConfigFile = "hl.yml"
	// DataDir holds the apps' data: <homelab>/data/<app>/<volume>.
	DataDir = "data"
	// StateDir holds the files hl generates: <homelab>/.hl/.
	StateDir = ".hl"
	// OverrideFile is the generated override in <homelab>/.hl/<app>/.
	OverrideFile = "override.yml"
	// UserConfigDir is hl's folder in the OS user config dir; HomeFile there
	// records the default homelab (`hl init`).
	UserConfigDir = "hl"
	HomeFile      = "home"
	// AppEnvFile is the app's own env file, read first.
	AppEnvFile = ".env"
	// DataDirMode and FileMode are the permissions of folders and files hl creates.
	DataDirMode = 0o755
	FileMode    = 0o644
)

// ComposeFiles are the names `docker compose` looks for, in its order.
var ComposeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// ExposedServices are the service names hl prefers, in order, when several publish a port.
var ExposedServices = []string{"frontend", "web", "app", "nginx", "proxy", "ui"}

// Environment variables.
const (
	// HomeEnv is the homelab folder, after --home.
	HomeEnv = "HL_HOME"
	// NoColorEnv turns colours off (https://no-color.org).
	NoColorEnv = "NO_COLOR"
)

// Commands and their defaults.
const (
	// LogTail is how many lines `hl logs` shows (--tail).
	LogTail = 200
	// FailLogTail is how many log lines `hl up --wait` shows for an app that didn't become healthy.
	FailLogTail = 20
	// WaitTimeout is how long `--wait` waits for an app (--wait-timeout).
	WaitTimeout = 2 * time.Minute
	// ComposeMinMajor and ComposeMinMinor: the override uses `!reset`, new in compose v2.24.
	ComposeMinMajor = 2
	ComposeMinMinor = 24
	// Shell is what `hl shell` runs in the container: bash when it has it, else sh.
	Shell = "command -v bash >/dev/null && exec bash || exec sh"
	// OpenerLinux and OpenerMac open a URL in the browser (`hl open`).
	OpenerLinux = "xdg-open"
	OpenerMac   = "open"
	// StderrTail is how much of a failed command's error output hl keeps to explain the failure.
	StderrTail = 8 << 10
)

// `hl init` and `hl sync` (folded in from internal/scan). The compose file
// names searched are ComposeFiles, shared with `hl add`.
const (
	// IgnoreFile: a folder containing a file with this name is never picked up as an app.
	IgnoreFile = ".hlignore"
	// ScanDepth is hl.yml's scan_depth: 1 = only direct subfolders of a projects folder are apps.
	ScanDepth = 1
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

// The config commands (version, schema, edit; folded in from internal/config).
const (
	// ConfigVersion is the hl.yml format this hl writes and reads. Files without a `version:` line
	// are the older, unversioned format (0) and are upgraded the next time hl edits them.
	ConfigVersion = 1

	// SchemaURL is the JSON Schema for hl.yml that editors use through the
	// yaml-language-server comment hl writes at the top of a new hl.yml.
	SchemaURL = "https://raw.githubusercontent.com/DeM-Conve/project__hl/main/schema/hl.schema.json"

	// DefaultEditor is what `hl edit` runs when neither $VISUAL nor $EDITOR is set.
	DefaultEditor = "vi"

	// EditDraft is the copy of hl.yml that `hl edit` opens, relative to the homelab folder.
	// hl.yml itself is only replaced once the draft is valid.
	EditDraft = StateDir + "/edit/" + ConfigFile
)

// SchemaComment is the first line of a new hl.yml: it points editors at the schema.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL
