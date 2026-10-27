# Architecture

How hl works inside. Read this before changing code in `internal/`. What the user sees (commands,
routing) is described in [README.md](../README.md), every setting and default in
[reference.md](reference.md); neither is repeated here.

## Packages

```
cmd/hl/main.go          calls cli.Execute()
internal/cli            the cobra commands; project.go builds compose projects, output.go colours
internal/config         hl.yml: types, defaults, validation, derived values, comment-keeping edits,
                        finding the homelab folder
internal/compose        runs `docker compose`; reads the resolved model (`config --format json`);
                        Exec keeps the tail of a failed command's stderr for the error (compose.Error)
internal/defaults       every default and every name hl gives files, labels and Docker objects
                        (one file; docs/reference.md's "Built-in values" lists them)
internal/fsutil         atomic file writes (temp file + rename, mode and symlinks kept)
internal/interrupt      Ctrl-C/SIGTERM while docker runs (Notify, Run, exit code 128+signal)
internal/gen            writes the per-app override, the proxy's compose file and its Traefik
                        static config, and checks a user-supplied one (pure functions)
internal/scan           finds apps in the `projects` folders and plans `hl sync` (no writes, no Docker)
internal/version        version, commit, build date (ldflags, else Go build info)
```

Dependencies point one way: `cli` -> `gen` -> `compose`, `config` -> `defaults`. `gen` and `config`
do no Docker calls; `cli` is tested against a fake `docker` (below). Third-party deps are `cobra`, `yaml.v3`
and `golang.org/x/term` (is stdin a terminal).

## What every command does first

1. **Find the homelab folder** (`config.Home`): `--home`, then `$HL_HOME`, then the nearest folder
   at or above the working directory that has an `hl.yml`, then the default recorded by `hl init`
   in `<os.UserConfigDir()>/hl/home`.
2. **Load `hl.yml`** (`config.Load`): check it strictly (`config.check`, below: every problem with
   file:line:column), unmarshal, fill defaults (`bind 127.0.0.1`, `port 80`, `domain localhost`,
   `routing path`, `image` the tested Traefik; the proxy's `Network`/`Project`, not settings, are `hl-proxy` or `hl-<name>-proxy` -
   all named in `internal/defaults`). A missing hl.yml is a `config.NotFoundError` naming `hl init`.
   hl.yml is re-read on every command; there is no daemon and no cached state besides the
   generated files.
3. **Build the app's compose project** (`cli.project`): the app folder (`path`, `~` and relative
   paths resolved against the homelab folder), its compose files (`compose:` or the first of
   `compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`), its env file
   (the app's `.env`, passed as `--env-file` when it exists),
   and extra process env (`config.AppEnv`).
4. **Prepare it** (`cli.prepared`): run `docker compose config --format json` to get the resolved
   model, pick service and port if `hl.yml` doesn't pin them, write
   `<homelab>/.hl/<app>/override.yml` with `gen.Override`, create the `data/<app>/<volume>` folders
   and every folder inside `data/<app>/` an env value points at (`envDataDirs`; left to Docker, a
   missing bind-mount source is created owned by root), and append the override as the last `--file`.
5. **Run** `docker compose --project-name hl-<id> --project-directory <app folder> --file ...
   --env-file ... <args>` attached to the terminal (`compose.Project.Run`; arguments are passed
   as they are, never through a shell, so paths with spaces or quotes are safe).

Commands that start or stop apps first take one inventory of every compose container
(`cli.containers`: one `docker ps --all` with the project, service, state, status and
`config_files` labels). It answers "is the proxy running", the HEALTH column, and who owns
a project: a project whose containers were started from another homelab's `.hl/` folder
(`inventory.owner`) is never started, stopped or replaced - `up`/`restart`/`rebuild`/
`remove` refuse with the fix, `down` leaves it out with a warning. If Docker can't be
reached, that first call fails with docker's own message and a hint.

hl never parses compose files itself: everything it knows about an app comes from compose's own
resolved model, so anything compose accepts (includes, extends, profiles, interpolation) works.

## Names

- App names match `^[a-z0-9][a-z0-9._-]*$` (dots allowed, e.g. `my-app.io`). `hl add` derives one
  from the folder name (`config.NameFor`: `My App` -> `my-app`), after `config.Stripped` drops the
  first `strip_prefix` it starts with (`project__blog` -> `blog`); `scan.AppName` does the same for init/sync.
- A homelab's optional top-level `name` prefixes its Docker names: projects `hl-<name>-<id>`,
  default proxy project and network `hl-<name>-proxy` (`Config.ProjectName`, `ProxyProject`).
  Without it they are `hl-<id>` and `hl-proxy` as before, so existing setups keep their names.
- Reserved names: one list in `internal/config/reserved.go` (`IsReserved`, `ReservedAppNames`),
  compared by ID. It holds only `proxy` (`defaults.ReservedAppName`): its project would be the
  proxy's default `hl-proxy`, and commands take `proxy` to mean the proxy. `ValidName` (so
  `Load` and `hl add`) and `scan` (init/sync skip such folders) use it. `Validate` also rejects an app whose
  `hl-<id>` equals the proxy's project.
- `config.ID` turns `.` and `_` into `-` for Docker and Traefik: compose project `hl-<id>` (two apps
  with the same ID are a config error),
  Traefik router/service/middleware names `<id>`, `<id>-slash`, `<id>-strip`.
- The proxy is compose project `hl-proxy` and its network `hl-proxy` (`hl-<name>-proxy` with a `name`; no setting changes them, so `proxy.network`/`proxy.project` are removed keys since 2026-10-27). Inside each override
  the network is always referred to by the key `hl-proxy`, mapped to the real name as an external network.

## Generated files

All under `<homelab>/.hl/`, regenerated on every command that uses them (written atomically, and
only when the content changed: `cli.writeGenerated`), headed "Generated by hl ... edit hl.yml, not
this file". `hl init` writes a `.gitignore` with `data/` and `.hl/`.

**`.hl/<app>/override.yml`** (`gen.Override`), only for routing `path`/`host`:
- the exposed service gets `ports: !reset []` (no host port; `!reset` is why compose v2.24+ is
  required), its existing networks re-listed plus `hl-proxy` (re-listing keeps the ones it had,
  `default` if none), and Traefik labels: `traefik.enable`, `traefik.docker.network`, entrypoint
  `web`, the load-balancer port, and the rule.
- host routing: ``Host(`<host>`)``.
- path routing: ``Path(`<prefix>`) || PathPrefix(`<prefix>/`)`` with no `Host()` match (the app
  answers on any name or address that reaches the proxy), a `redirectregex` middleware sending
  `<prefix>` to `<prefix>/`, and a `stripprefix` middleware (Traefik then sets
  `X-Forwarded-Prefix`). `$` is doubled so compose doesn't interpolate it.
- for every routing (including `none`): each named volume that is not `external`, has no non-local
  `driver` and no `driver_opts` becomes a local bind volume with `device: <homelab>/data/<app>/<volume>`.
  hl creates those folders (mode 0755) before compose runs; containers may later chown them.

**`.hl/<app>/builds.json`** (`cli.recordBuilds`, `internal/cli/build.go`): not a compose file
but state - for each service with a `build:` section, a hash of that section as compose resolved
it (`compose.Model.BuildHashes`: context, dockerfile, args with every variable filled in, keys
sorted). `runApp` writes it after every successful `up` (with or without `--build`) and, on an
`up` without `--build`, first runs `docker compose build <services>` for the services whose hash
differs from the recorded one (`staleBuilds`), because compose's `up` reuses an existing image
whatever its build args. No file (the first `up`, or an app added before this existed) means
nothing is rebuilt: compose builds the missing images, as before. A new service with a build
section is not stale either.

**`.hl/proxy/traefik.yml`** (`gen.Traefik`): Traefik's static config, readable and commented, with
no inline literals (every name, path and value is a constant in `internal/defaults` or an `hl.yml`
setting): entrypoint `web` on `:80` (every app router uses it), entrypoint `traefik`
on `:8080` for `ping` only (not published), the Docker provider with `exposedByDefault: false` on
the proxy network, `ping`, `log.level` `ERROR` (Traefik's own default; `INFO` with `proxy.debug`)
and, with `proxy.debug`, `accessLog` (format `common`). There is no `api` section (no dashboard),
no file provider and no way to use a file of one's own: `proxy.config`, `proxy.dynamic`,
`proxy.dashboard`, `proxy.dashboard_public`, `proxy.access_log` and `proxy.log_level` were removed
on 2026-10-27; `config.removedKeys` makes `Check` report each as removed, with the
`hl unset proxy <key>` that drops it (`config.Edit` accepts unsetting them). The same map holds
`proxy.network`, `proxy.project` and the app key `env_files` (as `apps.*.env_files`; `Check`
looks keys up by their pattern, `EditApp` accepts unsetting it).

**`.hl/proxy/compose.yml`** (`gen.Proxy`): compose project `hl-proxy` (`hl-<name>-proxy`),
one `traefik` service, `restart: unless-stopped`, no `command:` - Traefik reads its static config
from `/etc/traefik/traefik.yml`, where the generated file is mounted read-only.
`web` is published at `<bind>:<port>` (an IPv6 bind in brackets). The Docker socket is mounted
read-only. The service gets a healthcheck (`traefik healthcheck --ping`). A label
`hl.static-config: <hash of the static config>` makes compose recreate the container on
`hl up proxy` when only the file changed (Traefik doesn't reload static config).
The `networks:` section (name, and `ipam` subnet when `proxy.subnet` is set) is byte-for-byte what
earlier versions wrote, so upgrading keeps the existing network and its subnet
(`internal/gen/proxy_migration_test.go` compares the old flag-based setup, less its dashboard, with the new one). There
is no TLS entrypoint.

**Upgrading from the flag-based proxy**: a running proxy keeps running until the next `hl up proxy`
(or `hl down` and `hl up`), which recreates only the container; `hl show proxy` says when the
running proxy predates the current config (compose's `config-hash` label vs `docker compose config
--hash traefik`).

## Service and port detection

`compose.Model.Exposed`: the only service that publishes a port; among several, the first of
`frontend`, `web`, `app`, `nginx`, `proxy`, `ui` that exists; otherwise an error asking for
`service=`. `TargetPort` is the container (`target`) port of that service's first published port.
`hl add` (`cli.pickServicePort`) asks instead when there is more than one candidate service, or
the service publishes several distinct container ports: a numbered list on a terminal (the
shared `prompter` from `prompt.go`), with `Exposed`'s pick (or the first port) as the default;
without a terminal (`stdinTerminal`, `golang.org/x/term`) it takes `Exposed`'s pick with a note, and fails naming
`service=` only when no service has a preferred name (or `port=` when the service publishes several). Settings given to
`hl add` skip the question. `hl add <folder> key=value...` takes `hl set <app>`'s keys (applied to the new
`config.App` by `config.ApplyApp`, the same parsing as `hl set`) plus `name`; `env.NAME=` with no value is
left out of the mapped env. A routed service that fails `scan.LooksWeb` (what init/sync skip a
folder for) gets a warning naming `routing=none` instead of the base-path hint, which (one line,
the same in `applyPlan` for init and sync) is shown only with path routing when no value maps
`${HL_PREFIX}`.
`hl add` writes the picked `service` and `port` into `hl.yml` so later changes to the app's compose
file don't silently move the route; `prepared` only detects them when `hl.yml` leaves them empty.
Routing `none` needs neither.

## Environment layering

Compose interpolation sees, lowest to highest priority: the app's `.env`,
the shell environment, then what hl adds to the process env: `HL_APP`, `HL_URL`, `HL_PREFIX`,
`HL_HOST`, `HL_DATA`, the shared `env`, the app's `env`. Values in `env`/shared `env` are expanded
by hl (`os.Expand`) against the `HL_*` values first, then the shell environment; they can't refer
to each other. `HL_URL` has no trailing slash; `HL_PREFIX` is empty unless routing is `path`;
`HL_HOST` is the domain unless routing is `host`.

`TZ` (`config.hostTZ`, `internal/config/timezone.go`): when neither the shared `env`, the app's
`env`, nor a `TZ=`/`export TZ=` line in the app's `.env` sets it (hl reads those
lines because the process env would otherwise win over them), hl adds the host's zone right
after the `HL_*` values: `$TZ` (unless it names a file, `:...`), else the part after `zoneinfo/`
of `/etc/localtime`'s symlink target (Linux and macOS), else `/etc/timezone`. If none tells,
no `TZ` is added. `config.HostTimeZone` is a variable so tests can fix it; the paths are in
`internal/defaults`. `config.EnvVars` reports it with the source (`host ($TZ)`, ...), and
`hl show` (no app) lists it with the shared env.

These are only interpolation variables: an app sees them in its containers only if its own compose
file passes them through (e.g. `environment: BASE_PATH: ${MY_APP_BASE_PATH:-}`).

**Auto-mapped env** (`scan.AutoEnv`, rules in `internal/defaults`: `EnvBasePathSuffix`,
`EnvURLSuffix`, `EnvByName`): `hl add`, and `hl init`/`hl sync` for each new app
(`scan.PlanAdds` with a `VarsFunc`), run `docker compose config --variables --format json`
(`compose.Project.Variables`; only the names are used) and write the matching values into
`apps.<app>.env`, where they are ordinary settings. `<X>_BASE_PATH` -> `${HL_PREFIX}` with path
routing; `<X>_URL` -> `${HL_URL}` only when `<X>_BASE_PATH` exists too and routing isn't `none`;
`DATA_LOCATION`/`BACKUP_LOCATION` by name. A failing `--variables` call (older compose) only
warns. Kept apps are never re-mapped.

## Command notes

- Every command that touches the proxy (`proxy()` -> `proxyProject` in `internal/cli/proxy_cmds.go`)
  first checks that the proxy's compose project isn't run from another homelab's folder (the
  `com.docker.compose.project.working_dir` label of its containers) and that the proxy network, if it
  exists, belongs to the proxy project; otherwise it stops with the fix (`hl set name=<name>`, which
  gives this homelab its own project and network, and `hl set proxy port=`), so a second homelab never stops or reconfigures the first one's
  proxy. Without Docker the check is skipped.
- **The proxy as a pseudo-app**: `proxy` is the reserved app name, so `up`, `down`, `restart`,
  `rebuild`, `logs` and `show` take it among their arguments (`splitProxy`); the proxy runs first,
  then any apps named with it. `up proxy` (`upProxy`) starts it, or runs `up --detach` on a running
  one so compose recreates it only when its files changed. Before that, `moved` reads the
  running container's published address (`docker ps --format {{.Ports}}`); when the port
  differs from `proxy.port` (or the bind does and neither is a wildcard address), `checkPort`
  runs first and a busy address is refused with the running proxy left alone - compose would
  remove it and then fail to bind the new one. `statusAfter` prints a command's error before
  the status table and exits 1. `rebuild proxy` adds
  `--force-recreate`; `restart proxy` restarts its container (starting it when it isn't running);
  `down proxy` is `downProxy`; `logs proxy` runs `docker compose logs` on it. Other commands that
  take an app refuse it with a hint (`targets`). Completion offers `proxy` where it is accepted.
- `show proxy` (`showProxy`): state and health of the proxy container, listen address, every
  `proxy.*` setting with its source (`config.Settings`) and the running image when it differs,
  the static config file, a clash, an image
  older than `config.TestedTraefikVersion`, and whether the running container predates the config
  (compose's `config-hash` label vs `docker compose config --hash traefik`).
- `doctor` also checks the proxy config (as above), the project/network ownership, and warns (not a
  failure) when `proxy.image` is older than the tested Traefik or its version can't be told.
- `up`/`start`, `rebuild` and `restart` start the proxy first if none of its containers
  runs (`ensureProxy`), because each override joins the proxy network as `external`. Before
  starting it (`startProxy`, also `hl up proxy`): `checkPort` - something else on `bind:port`?
  (`net.Listen`; when hl may not open a low port, a connect instead), named by `docker ps --filter
  publish=<port>`, else `lsof`, else `ss`, with a free port suggested - and `checkNetwork` - with
  `proxy.subnet` set, the proxy network existing with another subnet, or the subnet overlapping
  another Docker network or one of the machine's own, each with the fix (a free `172.x.0.0/16`).
- Multi-app commands (`eachApp`) run every app even when one fails, print each failure as it
  happens and end with "n of m apps failed: ..." (exit 1). An interruption stops the loop.
- `--wait` (`up`, `rebuild`) runs the command as usual (every service starts), then
  `up --detach --wait --wait-timeout <s> <services>` for the routed service and its
  `depends_on`, followed through (`compose.Model.WithDeps`, from the model `prepare` already
  read), so a sidecar with a slow healthcheck doesn't fail an app that serves. `--wait` on an
  app with routing `none` (no service matters more) passes `--wait --wait-timeout <s>` to the
  command itself, waiting for everything. When an app doesn't come up, hl lists the
  waited-for services that aren't running or healthy and prints their last
  `defaults.FailLogTail` log lines. Once a routed app's services are healthy, `waitRoute` asks
  the proxy for the app's URL (sent to the address the proxy really publishes, `publishedAddr`,
  with the URL's `Host` header) every `defaults.RouteWaitInterval` until the answer is anything
  but Traefik's own 404 for an unknown route (`defaults.ProxyNotFound`; the app's own 404
  counts as an answer). Traefik loads a new route a moment after the container is up, so a
  request right after `--wait` used to get that 404. Past the `--wait-timeout` (counted from
  the start of the wait) it fails with a hint. Tests replace `routeReady`.
- `down`/`stop` with no app stops every app that has containers, then the proxy; apps and a proxy
  that aren't there are skipped, so it is safe to repeat. An app hl can no longer prepare (folder
  gone) is stopped by project name alone (`downByName`). `remove` does the same for the app
  (and, when the app's folder still exists inside a projects folder - `scan.InProjects` - and
  no pattern ignores it yet, adds `scan.IgnoreEntry(<folder name>)`, the name with pattern
  characters escaped, to `ignore`; `hl add` removes an `ignore` entry that is exactly the
  folder's name),
  deletes it from `hl.yml` and `.hl/<app>/`, and keeps `data/<app>/`.
- `rebuild --pull` per app (`pullAndRebuild`): no pull if `git status --porcelain` fails (not a repo) or shows changes; otherwise
  `git pull --ff-only` (a failed pull is a failure for that app; the folder is left as it was),
  then `up --detach --build` either way. It ends with the pulled apps and the ones rebuilt without a pull, and why.
- `status`: STATE `down`, `up n/m` or `partial n/m`; HEALTH from the containers' status:
  `healthy` (every healthcheck passes), `starting: <services>`, `unhealthy: <services>` (also
  restarting or exited ones), `-` without healthchecks. Without Docker it still prints hl.yml's
  apps, states `?`, and a warning with the hint.
  A running proxy that publishes another address than hl.yml's (`publishedAddr`, from `docker ps
  --format {{.Ports}}`, the same check `hl up proxy` uses before replacing it) is shown with that
  address, the URLs use its port, and a note says how to apply hl.yml's.
- `logs <app>` (and `logs proxy`) first checks the inventory: a project with no containers at
  all gets "isn't running" and exit 0 instead of compose's empty output (`notRunning`; without
  Docker compose runs and says what is wrong). Services named after the app must be in its model
  (else a usage error listing them). `logs proxy` counts what compose printed (`countWriter`):
  nothing, without `-f` and `proxy.debug`, gets a note naming `hl set proxy debug=true`.
- `logs` with no app runs `docker compose logs` for every app that has containers at once, each
  line prefixed by its app; `--tail` (default `defaults.LogTail`) and `-f` apply to all. With
  none, it says nothing is running (exit 0).
- `restart` leaves out apps with no containers (`runningApps`: "`<app>` isn't running - start it
  with: `hl up <app>`", exit 0, like `logs`): compose's restart would do nothing for them, and
  the proxy isn't started when no named app runs.
- `show` (`internal/cli/show.go`) reads `hl.yml` and, only for an app whose `service` or `port`
  isn't pinned, the compose model (`docker compose config`, as `prepared` would; nothing is
  written or started). The env list comes from `config.EnvVars`: `AppEnv` with one entry per
  name, its source (`hl`, the shared `env`, the app's `env`), the value as written, and the
  source it overrides. With no app it lists the homelab folder and its source (`homeSource`),
  the `proxy.*` entries of `config.Settings` that `hl.yml` sets, the shared env with the `TZ`
  apps get (`config.HostTimeZone` when the shared env has none), the projects folders (and which
  are missing), `ignore` with the built-in patterns, `scan_depth`, `strip_prefix`, and the apps.
  `--compose` runs `docker compose config` on the prepared app (or the proxy's project) instead:
  the whole config as hl runs it. Only the key column is aligned (the source follows the value
  after two spaces, so one long path doesn't push every row; values hl works out itself, an
  app's `project` and `url`, have no source), except the homelab's app list, which is in
  columns (name, URL, folder), and paths inside the user's home
  folder print as `~/...` (`tilde`, also in `hl status` and `hl show proxy`).
- `open` prints the URL and only launches `xdg-open`/`open` on macOS or when `DISPLAY` or
  `WAYLAND_DISPLAY` is set.
- `shell` runs `bash` if the container has it, else `sh` (`defaults.Shell`, a fixed string; the
  service name is validated so it can't pass as a flag). Without a service it uses the pinned or
  detected one. It exits with the shell's own status. It checks the inventory first: a service
  with no running container fails with one hl error (and the `hl up` hint) before `docker
  exec` can print its own.
- `doctor` prints hl's version, Go version and platform, compares it with the latest release
  (`checkLatest`: `version.Kind` skips `dev` builds, tells a Go pseudo-version how to install a
  release, and for a release asks `defaults.LatestReleaseURL` with `defaults.UpdateCheckTimeout`;
  any failure is silent; `version.Newer` compares semver, pre-releases included). This is the
  only network request hl makes. Then it checks docker on PATH (and its
  version), the daemon (`docker info`: version, OS, architecture), compose >= 2.24, that `hl.yml`
  loads (every problem with its line), the proxy (config, ownership, image), a non-loopback bind
  (a warning), the port and subnet when the proxy isn't running, projects owned
  by another homelab, and that every app prepares. It ends "all good" or "n problems found"
  (exit 1) with a note to paste it into bug reports.
- Completion: commands taking one app complete only the first argument; multi-app commands leave
  out apps already typed; `logs` and `shell` complete the app's services after the app name.
- Output: colours only on a terminal (`golang.org/x/term`; `/dev/null` is not one) and never with
  `NO_COLOR` set. Commands write through `cli.stdout`/`stderr`, set from cobra's writers.
- Errors (`cli.report`): hints that fix a setting name the `hl set`/`hl unset` command, not the
  `hl.yml` key (`name`'s is `cli.nameHowTo`: `hl down` first). `error: <what failed>`, then `hint: <what to do>` when known - from a
  `hintError` the code attached, or read from docker's own error output (`dockerHint`: daemon
  down, permission, port allocated, old compose, pool overlap, missing network, name conflict,
  pull denied, disk full). Exit codes: 0 ok, 1 failure, 2 usage (cobra's unknown command/flag
  and every `Args` check, wrapped in `usageError`), 128+signal when interrupted.
- Ctrl-C (`internal/interrupt`): `cli.Main` cancels the command's context on the first SIGINT or
  SIGTERM. A running docker/git command (`compose.Exec` -> `interrupt.Run`) is waited for; SIGINT
  from a terminal already reached it, other signals are passed on; a second signal kills it. No
  further app is started, and hl exits 128+signal without an error line.
- Windows: `supported` (the root's PersistentPreRunE) refuses every command but `version`,
  `help` and `completion` with a friendly error suggesting WSL 2.

## Editing hl.yml

Every change hl makes to `hl.yml` goes through the `yaml.Node` tree (`config.edited`/`editFile` in
`internal/config/edit.go`), so comments and key order survive (yaml.v3 normalises indentation
to two spaces). yaml.v3 also drops blank lines; `keepBlankLines` puts them back: it matches the
encoder's lines against the old file's non-blank lines (longest common subsequence), and each
line still there gets the blank lines that stood above it; a changed or removed line hands its
blank lines on to the next line written. If the result doesn't decode to the same YAML as the
encoder's output (a blank line inside a block scalar), the encoder's output is written as it is.

- `hl add`/`hl remove` (`config.AddApp`, `RemoveApp`) touch only the `apps:` mapping (`apps: {}`
  from the template is turned into a block mapping).
- `hl set`/`hl unset` (`config.Edit`, `internal/config/set.go`) change keys of one scope: an
  app (`config.EditApp`), the proxy (`config.ProxyScope`, i.e. `proxy`, the reserved app name,
  so never an app) or the homelab's own settings (`config.HomelabScope`, no first argument:
  `env`, `name`, `projects`, `ignore`, `scan_depth`, `strip_prefix`).
  Keys and their types come from the `App`/`Proxy`/`Config` structs by reflection (ints parsed,
  bools as true/false, lists split on commas, map fields as `env.NAME`). A list takes `key=`
  (replace), `key+=` (add the entries not in it, `config.OpAdd`) or `key-=` (remove them, each
  must be there, `config.OpRemove`); an empty list removes the key, and a new top-level key goes
  before `apps:`. Before editing, the CLI (`homelabChanges`) writes `projects` folders like
  `hl init` does (`storedPath`; each must exist; `-=` matches the written entry or the resolved
  path) and refuses `name` while any of this homelab's projects has containers (`nothingRuns`),
  since it renames every project and network. An app's `path=` is resolved against the current
  folder and written with `storedPath` too (`editScope`). For an app with no `compose` list, `compose+=`/
  `compose-=` (in `hl set` and `hl add`) get a replace with the file `compose.FindFile` picks put
  in front (`seedCompose`, not printed as a change), so the list starts from it. `hl set <app>
  name=<new>` (alone) is a rename (`renameApp`): `ValidName`, no such app, no containers of the
  old or new Docker project in the inventory, no `data/<new>`; then `config.RenameApp` changes
  the key in the node tree (checked like any edit, so a clash of prefix, host or project is
  refused), and `data/<old>` and `.hl/<old>` are renamed (a failed data move puts hl.yml
  back). `ignore` holds folder names, which a rename doesn't change. A missing `proxy:` or
  shared `env:` is added above `apps:`, and a `proxy:` left empty by an unset is removed. A
  replaced value keeps the old one's comments. The new
  content is run through `config.Check` and written only if it has no problems. An unknown key
  or app is a `config.UnknownError`, which the CLI turns into a usage error (exit 2); so is a key
  given without an app that isn't a homelab one (`notHomelabKey` names the scope that has it).
  A value that can't be used (not a number, not in the list for `-=`, or a problem `Check` finds
  in the result while the file before had none) is a `config.ValueError`: the full key
  (`apps.<app>.<key>`, `proxy.<key>`) and the problem's text up to " - ", with the rest as its
  `Hint()`; no file:line, since hl.yml wasn't written. The CLI makes it a usage error (exit 2)
  with that hint. When hl.yml already had problems, the `ProblemsError` with file:line stays.
  A change of an app's `path`, `compose` or `service` is tried first (`checkAppChanges`: the
  `project` of the changed app must resolve, and the service be in its model when that can be
  read). The CLI tells
  the scopes apart without guessing: for `set`, a first argument with `=` can't be an app name,
  so it is the shared scope; for `unset`, `env`/`env.NAME` is the shared scope unless an app has
  that name (`config.HasApp`). It then prints what applies the change (`applyCommand`:
  `hl up <app>`, `hl up`, `hl up proxy`, `hl up proxy` then `hl up` when the proxy's `domain`,
  `routing` or `port` changed (they are in the apps' routes or `HL_URL`; `bind` is not), `hl sync`
  for the projects settings, or `hl down` then `hl up` when the proxy's network, subnet or
  project changed).
- There is no editor command: every key is reachable with `hl set`/`hl unset`. A file broken by
  hand fails `config.Load` with a `*config.ProblemsError` (each problem with file:line:column);
  `cli.load` adds the hint to fix those lines in an editor and re-check with `hl doctor`.

**Checking** (`config.Check`, `internal/config/validate.go`): walks the node tree against the
`Config` type by reflection (yaml tags give the known keys, so a new field needs no checker
change), reporting syntax errors, unknown keys with a Levenshtein "did you mean", keys set twice,
type mismatches, required keys, and per-key rules (`rules`, keyed by path with app names as `*`:
routing values, ports, prefix, app names, version; removed keys). If the walk
finds nothing it decodes and runs `Validate`, which collects every value problem and clash (bind
is an IP, domain/host names, subnet, image, env names,
two apps with the same Docker project, URL prefix or host) as `Problem`s keyed by path; `check`
gives them the line of their key. `Load` calls it, so every command reports unknown keys and bad
values with their lines.

**Versions** (`defaults.ConfigVersion`): `version: 1`; no line means
the format before versions (0), which loads as before. `config.Upgrade` adds the line (at the top,
after any header comment) and the CLI prints a note; it runs before `add`/`remove` and after
`set`/`unset`. `Validate` refuses a version above `defaults.ConfigVersion`.

There is no JSON Schema for hl.yml (removed 2026-10-27): `Check` walks the Go types, and
nobody is meant to open the file.

**Settings listing** (`config.Settings`, used by `hl show` and `hl show proxy`): the
homelab-wide fields of `Config` by reflection, the default taken by running `defaults()` on an
empty config, the source `hl.yml` when the raw file sets it.

## Version

`internal/version` holds `Version`/`Commit`/`Date`, set by GoReleaser's `-ldflags -X`. Without them
`Get` fills in from `runtime/debug` build info: the module version for `go install ...@vX`, and
``vcs.revision`/`vcs.time` for builds from a checkout (`-dirty` with `vcs.modified`). `String`
omits commit and date when unknown; `Full` (what `hl version` prints) adds the Go
version and OS/architecture for bug reports.

## Projects folders: `hl init` and `hl sync`

hl.yml's `projects`, `ignore`, `scan_depth` and `strip_prefix` (a separate block in `config.Config`; edits in
`config/projects.go`) drive both commands. Their defaults (`.hlignore`, the always-ignored name
patterns, scan depth 1, the folders init suggests, init's fallback port and network bind) are in
`internal/defaults`; the compose file names are `defaults.ComposeFiles`.

- **`scan.Compare`** (file system only): lists the subfolders of each projects folder, down to
  `scan_depth`, that have a compose file (`compose.FindFile`, a name check - never parsed). A
  folder with a compose file is an app and is not looked into further. Names matching the default
  patterns or `ignore:` (`filepath.Match` on the folder name) and folders containing `.hlignore`
  are skipped. It returns new folders (no app uses them, compared after resolving symlinks), apps
  inside a projects folder whose folder is gone, the apps still there, and projects folders that
  don't exist. An app under a missing projects folder (e.g. an unmounted drive) is never "gone".
  Apps outside every projects folder are not looked at.
- **`scan.Plan`** adds Docker through a `ModelFunc` (`docker compose config` via `cli.project`):
  a new folder gets the service and port `hl add` would pick (`Exposed`/`TargetPort`), or a skip
  with the reason and the `hl add` command to run instead (name taken, ambiguous services, compose
  error, or not a web app: `scan.LooksWeb` - the service has none of `defaults.ExposedServices`'
  names and its port isn't in `defaults.WebPorts` - which suggests `routing=none` and
  `hl set ignore+=`). A kept app with a pinned `service` is checked against its model: service gone -> detect
  both again; pinned port not among the service's published targets -> pin its first one. Apps
  with nothing pinned are detected on every run anyway and are left alone. Before the adds,
  `planMoves` pairs gone apps with new folders: a gone app with a pinned `service` and `port`
  (routing not `none`) matches a new folder that has its `compose` files, isn't named like
  another app, exposes that service on that port, and has a named volume for every folder in
  `data/<app>/`. A pair unambiguous both ways (one folder for the app, one app for the folder)
  becomes a `scan.Move`, and both drop out of the adds and removes. The `ModelFunc` is cached per
  folder and compose list, so a folder's model is read once.
- **Showing**: `printPlan` lists each change once (an add with its env); a folder under a
  projects folder written as an absolute path shows as `<its name>/<rest>` (`shortPath`), and
  skip reasons name the folder once, as in hl.yml when that starts with `~/` (`scan.shellPath`).
  Applying then says only "added <app>".
- **Applying**: adds go through `config.AddApp` with `path` = the projects entry as written joined
  with the subfolder (so `~/code/blog`, not an absolute path); removes stop the app by compose
  project name if it still runs (its files are gone), then `config.RemoveApp` and delete
  `.hl/<app>/` - `data/<app>/` is never touched; updates use `config.PinApp`; a move is
  `config.EditApp` with the new `path` (name, settings, `data/<app>/` and `.hl/<app>/` stay). All edits keep
  hl.yml's comments.
- **`hl status`** calls `syncHint`: `scan.Compare`, then, for new folders only, `scan.PlanAdds`
  (one `docker compose config` each, as sync does), and prints one line when sync would add an
  app or remove a gone one. Folders sync only skips are left out, so the hint doesn't stay
  forever. When another homelab runs the proxy project, the header says so instead of this
  homelab's address.
- **`hl init`** sets up the folder given, else the one `config.Home` finds (so a second run adds
  new apps to it), else `~/homelab` (`defaults.HomelabDir`) - never the current folder by itself
  (`initDir`). After recording the default it runs `config.Home` again and warns (`otherHome`)
  when `--home`, `$HL_HOME` or an hl.yml above the working folder would still win; the plan has a
  `default` line when another homelab was the default (`config.DefaultHome`). It plans first and writes nothing until it is confirmed: the settings come from `--projects`,
  `--strip-prefix` and defaults only (the proxy has no flags: path routing on `127.0.0.1`;
  `--projects`, else `defaults.CandidateProjects` that exist in the home folder;
  the default port, or the first of `defaults.AltPorts` that is free and unclaimed when that is
  busy or claimed, shown as "port 80 is in use" (or "is the proxy port of the homelab in <dir>")
  in the plan. Claimed ports (`otherHomelabPorts`) are the `proxy.port` of every other homelab hl
  can find: the recorded default and, with docker working, the folder in the `config_files`
  label of any container (running or stopped) started from a homelab's `.hl/`; with docker working, `initName` gives a new homelab a `name`
  when `proxyNamesTaken` (docker.go) finds the default proxy project run from another homelab's
  folder or the proxy network already there - the folder's name as an ID, else
  `defaults.HomelabDir`, then `-2` ... up to `defaults.MaxNameTries` - and `WriteNew` writes it
  with a comment saying why), `InitSettings.Config` makes
  the homelab in memory (with no `--strip-prefix`, `strip_prefix` is `scan.SharedPrefixes` of the
  new folders' names; `none` turns that off), `initPlan` runs `scan.Compare`/`scan.PlanAdds` on it (service, port,
  env, skips) and prints the plan with `printPlan`. Then, only when there is something to write and `--yes`
  isn't given: without a terminal it stops ("pass --yes", like `hl sync`), else one `confirm`
  (`prompt.go`, plain bufio, Enter = yes); "no" writes nothing. Pre-flight runs the
  doctor's docker checks; without a working docker it still writes hl.yml but leaves app
  detection to `hl sync`. Port check: `net.Listen` on bind:port; only "address in use" counts
  as busy (a normal user may not open port 80, Docker can). A new hl.yml is the short `Template`
  (header comment, `version`, `apps: {}`) plus, through the yaml node tree
  (`config.WriteNew`, `O_EXCL`, so it never overwrites), `projects`, `strip_prefix` and a
  `proxy` block with the port when it isn't the default; projects
  folders are stored relative inside the homelab, with `~` inside the user's home, else
  absolute. On an existing hl.yml init only saves `projects`/`strip_prefix` when the flag is
  given and hl.yml has none (`initWhy.saveProjects`/`saveStrip`, a `+ ... (hl.yml updated)` plan
  line) and adds the new apps; the usual projects folders (`candidateProjects`) are only named in
  the plan, and the `.gitignore` is written for a new homelab only. The plan's first line says
  `existing homelab <dir>`; a change of the default homelab is asked about like a write.

## Tests of the CLI

`internal/cli/fake_test.go`: `TestMain` turns the test binary into a fake `docker` (and `git`,
`lsof`, `xdg-open`) when it runs under that name. `newFake` links it into a folder that is the
whole PATH, makes a fresh home folder with spaces in its path (`HOME`, `XDG_CONFIG_HOME`) and no hl
state, and replaces the port and host-network probes. Each fake call appends its arguments, working
folder and `HL_*` environment to a JSON-lines file; what it prints and its exit code come from
rules (`f.on`), and by default `compose ... config --format json` prints the app folder's
`model.json`, `ps --all` lists nothing and `network inspect` finds nothing. `f.hl(...)` runs
`runCLI` with buffers and returns the exit code and both outputs. Interrupts are tested by
cancelling the context with an `interrupt.Error` while a fake waits for a signal.
