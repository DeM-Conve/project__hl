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

// The time zone hl gives apps (TZ) when neither the shared env, the app's env nor its env
// files set one: the host's, from $TZ, else the zoneinfo file /etc/localtime links to, else
// /etc/timezone. When none of these tells, hl sets no TZ.
const (
	TZEnv         = "TZ"
	LocalTimeFile = "/etc/localtime"
	ZoneInfoDir   = "zoneinfo"
	TimeZoneFile  = "/etc/timezone"
)

// Releases: `hl doctor` asks GitHub for the latest one (its only network call; skipped for
// builds that aren't releases, and silently when it fails or takes longer than the timeout).
const (
	LatestReleaseURL   = "https://api.github.com/repos/DeM-Conve/project__hl/releases/latest"
	ReleasesPage       = "https://github.com/DeM-Conve/project__hl/releases"
	UpdateCheckTimeout = 3 * time.Second
	// InstallCommand installs (or updates to) the latest release.
	InstallCommand = "curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh"
)

// Env auto-mapping: `hl add`, `hl init` and `hl sync` read the variables an app's compose
// file uses (`docker compose config --variables`) and fill in apps.<app>.env for the ones that
// follow the common self-hosting conventions (Immich, Paperless, linuxserver). --no-env turns it off.
const (
	// EnvBasePathSuffix: <X>_BASE_PATH gets EnvBasePathValue, with path routing only.
	EnvBasePathSuffix = "_BASE_PATH"
	EnvBasePathValue  = "${HL_PREFIX}"
	// EnvURLSuffix: <X>_URL gets EnvURLValue, only when the same <X>_BASE_PATH exists (so
	// URLs of other services, like an upstream API, are left alone).
	EnvURLSuffix = "_URL"
	EnvURLValue  = "${HL_URL}"
)

// EnvByName maps variables by their exact name (any routing). Data the app keeps in bind
// mounts goes under the app's data folder; named volumes are already there.
var EnvByName = map[string]string{
	"DATA_LOCATION":   "${HL_DATA}",
	"BACKUP_LOCATION": "${HL_DATA}/backups",
}

// WebPorts are container ports `hl init` and `hl sync` take as a sign of a web app: a new folder
// whose service has none of the ExposedServices names and whose port isn't one of these is
// listed with the `hl add` command to use instead of being added (it may be a database or a
// cache, which the HTTP proxy can't serve).
var WebPorts = []int{80, 443, 3000, 3001, 4000, 4200, 5000, 5173, 8000, 8008, 8080, 8081, 8088, 8443, 8888}

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
	// HomelabDir is the folder, under the user's home, `hl init` sets up when it is given none
	// and finds no homelab (--home, $HL_HOME, an hl.yml at or above the current folder, the default).
	HomelabDir = "homelab"
)

// AltPorts are the proxy ports hl tries, in order, when the configured one is in use: `hl init`
// takes the first free one, and errors about a busy port suggest it.
var AltPorts = []int{AltPort, 8000, 8081, 8888, 8090}

// PrefixEnds are the characters a folder-name prefix `hl init` offers to strip ends with
// (project__blog, my-blog, my_blog): a prefix up to one of them that at least two folders, and
// at least half of the folders found, share.
const PrefixEnds = "-_"

// MaxNameTries is how many names `hl init` tries (<folder>, <folder>-2, ...) when another
// homelab on this Docker already uses the default proxy project or network.
const MaxNameTries = 9

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
