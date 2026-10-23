package config

// ---------------------------------------------------------------------------
// Proxy defaults: every value hl uses for the Traefik proxy, in one place.
// Settings in hl.yml (proxy.*) override the ones marked "hl.yml:". To be
// folded into the shared defaults package.
// ---------------------------------------------------------------------------

const (
	// TestedTraefikVersion is the Traefik release hl is tested with; `hl doctor`
	// warns when proxy.image is older. v3.6.1+ is needed for Docker Engine 29's API.
	TestedTraefikVersion = "3.6.25"
	// DefaultProxyImage - hl.yml: proxy.image.
	DefaultProxyImage = "traefik:v" + TestedTraefikVersion
	// DefaultProxyProject is the proxy's compose project - hl.yml: proxy.project.
	DefaultProxyProject = "hl-proxy"
	// DefaultProxyNetwork is the network the proxy shares with apps - hl.yml: proxy.network.
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

	// StaticConfigLabel holds a hash of the static config on the proxy
	// container, so a changed file recreates the container on `hl proxy up`.
	StaticConfigLabel = "hl.static-config"
)

// ProxyLogLevels are the values proxy.log_level accepts.
var ProxyLogLevels = []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL", "PANIC"}
