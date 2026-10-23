# Architecture

How hl works inside. Read this before changing code in `internal/`. What the user sees (commands,
`hl.yml` fields, routing) is described in [README.md](../README.md) and not repeated here.

## Packages

```
cmd/hl/main.go          calls cli.Execute()
internal/cli            the cobra commands; project.go builds compose projects, output.go colours
internal/config         hl.yml: types, defaults, validation, derived values, comment-keeping edits,
                        finding the homelab folder
internal/compose        runs `docker compose`; reads the resolved model (`config --format json`);
                        Exec keeps the tail of a failed command's stderr for the error (compose.Error)
internal/defaults       every default and every name hl gives files, labels and Docker objects
                        (one file; `hl config defaults` lists them)
internal/fsutil         atomic file writes (temp file + rename, mode and symlinks kept)
internal/interrupt      Ctrl-C/SIGTERM while docker runs (Notify, Run, exit code 128+signal)
internal/gen            writes the per-app override, the proxy's compose file and its Traefik
                        static config, and checks a user-supplied one (pure functions)
internal/scan           finds apps in the `projects` folders and plans `hl sync` (no writes, no Docker)
internal/version        version, commit, build date (ldflags, else Go build info)
```

Dependencies point one way: `cli` -> `gen` -> `compose`, `config` -> `defaults`. `gen` and `config`
do no Docker calls; `cli` is tested against a fake `docker` (below). Third-party deps are `cobra`, `yaml.v3`
and `golang.org/x/term` (is stdin a terminal); tests also use `santhosh-tekuri/jsonschema`.

## What every command does first

1. **Find the homelab folder** (`config.Home`): `--home`, then `$HL_HOME`, then the nearest folder
   at or above the working directory that has an `hl.yml`, then the default recorded by `hl init`
   in `<os.UserConfigDir()>/hl/home`.
2. **Load `hl.yml`** (`config.Load`): check it strictly (`config.check`, below: every problem with
   file:line:column), unmarshal, fill defaults (`bind 127.0.0.1`, `port 80`, `domain localhost`,
   `routing path`, `network`/`project` `hl-proxy` or `hl-<name>-proxy`, `image` the tested Traefik -
   all named in `internal/defaults`). A missing hl.yml is a `config.NotFoundError` naming `hl init`.
   hl.yml is re-read on every command; there is no daemon and no cached state besides the
   generated files.
3. **Build the app's compose project** (`cli.project`): the app folder (`path`, `~` and relative
   paths resolved against the homelab folder), its compose files (`compose:` or the first of
   `compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`), its env files
   (the app's `.env` first, because passing any `--env-file` stops compose reading `.env` itself;
   then `env_files`), and extra process env (`config.AppEnv`).
4. **Prepare it** (`cli.prepared`): run `docker compose config --format json` to get the resolved
   model, pick service and port if `hl.yml` doesn't pin them, write
   `<homelab>/.hl/<app>/override.yml` with `gen.Override`, create the `data/<app>/<volume>` folders,
   and append the override as the last `--file`.
5. **Run** `docker compose --project-name hl-<id> --project-directory <app folder> --file ...
   --env-file ... <args>` attached to the terminal (`compose.Project.Run`; arguments are passed
   as they are, never through a shell, so paths with spaces or quotes are safe).

Commands that start or stop apps first take one inventory of every compose container
(`cli.containers`: one `docker ps --all` with the project, service, state, status and
`config_files` labels). It answers "is the proxy running", the HEALTH column, `ps`, and who owns
a project: a project whose containers were started from another homelab's `.hl/` folder
(`inventory.owner`) is never started, stopped or replaced - `up`/`restart`/`rebuild`/`update`/
`remove` refuse with the fix, `down` and `ps` leave it out with a warning. If Docker can't be
reached, that first call fails with docker's own message and a hint.

hl never parses compose files itself: everything it knows about an app comes from compose's own
resolved model, so anything compose accepts (includes, extends, profiles, interpolation) works.

## Names

- App names match `^[a-z0-9][a-z0-9._-]*$` (dots allowed, e.g. `my-app.io`). `hl add` derives one
  from the folder name (`config.NameFor`: `My App` -> `my-app`).
- A homelab's optional top-level `name` prefixes its Docker names: projects `hl-<name>-<id>`,
  default proxy project and network `hl-<name>-proxy` (`Config.ProjectName`, `ProxyProject`).
  Without it they are `hl-<id>` and `hl-proxy` as before, so existing setups keep their names.
- Reserved names: one list in `internal/config/reserved.go` (`IsReserved`, `ReservedAppNames`),
  compared by ID. It starts with `proxy` (`config.ReservedAppName`: its project would be the
  proxy's default `hl-proxy`); `internal/cli/reserved.go` adds, at start-up, every subcommand
  name and alias of `hl config` plus `proxy` (read from the registered command, not a second
  list), so `hl config <app>` is never shadowed. `ValidName` (so `Load`, `hl add`, `config
  check`) and `scan` (init/sync skip such folders) use it; a cli test checks the schema's
  `apps.propertyNames.not.enum` lists the same names. `Validate` also rejects an app whose
  `hl-<id>` equals `proxy.project`.
- `config.ID` turns `.` and `_` into `-` for Docker and Traefik: compose project `hl-<id>` (two apps
  with the same ID are a config error),
  Traefik router/service/middleware names `<id>`, `<id>-slash`, `<id>-strip`.
- The proxy is compose project `proxy.project` (default `hl-proxy`); its network defaults to `hl-proxy` (inside each override
  it is always referred to by the key `hl-proxy`, mapped to `proxy.network` as an external network).

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

**`.hl/proxy/traefik.yml`** (`gen.Traefik`): Traefik's static config, readable and commented, with
no inline literals (every name, path and value is a constant in `internal/defaults` or an `hl.yml`
setting): entrypoint `web` on `:80` (every app router uses it), entrypoint `traefik`
on `:8080` for `ping` only (not published), the Docker provider with `exposedByDefault: false` on
`proxy.network`, the file provider reading `proxy.dynamic` when set (`watch: true`), `api.dashboard`
when the dashboard is on (`gen.Dashboard`, i.e. `proxy.dashboard`, off by default; the single
switch for the `api` section and the dashboard router labels; `Validate` refuses it on a
non-loopback `proxy.bind` unless `proxy.dashboard_public` is set), `log.level` (`proxy.log_level`, default `ERROR`, Traefik's own default),
and `accessLog` (format `common`) when `proxy.access_log` is on. With `proxy.config` set, hl writes
no `traefik.yml` (and removes a stale one); it reads the user's file and `gen.CheckTraefik` reports
problems (no `entryPoints.web` on port 80, no `providers.docker`, `providers.docker.network` not
`proxy.network`, `proxy.dynamic` set but no `providers.file` reading it) that stop every proxy
command, and warnings (`exposedByDefault` not false, no `api.dashboard` while the dashboard is on,
no `ping`, so no healthcheck) shown by `hl proxy status`, `hl config proxy` and `hl doctor`. Keys
are matched case-insensitively, like Traefik does. `proxy.access_log` and `proxy.log_level` are
config errors together with `proxy.config`.

**`.hl/proxy/compose.yml`** (`gen.Proxy`): compose project `proxy.project` (default `hl-proxy`),
one `traefik` service, `restart: unless-stopped`, no `command:` - Traefik reads its static config
from `/etc/traefik/traefik.yml`, where the generated file or `proxy.config` is mounted read-only.
`web` is published at `<bind>:<port>` (an IPv6 bind in brackets). The Docker socket is mounted read-only; with
`proxy.dynamic`, the folder holding that file is mounted read-only at `/etc/traefik/dynamic` (a
folder, not the file, so Traefik's watch survives editors that replace the file). Because the whole folder is
mounted, `Validate` requires `proxy.dynamic` in a subfolder of the homelab that is not the homelab
folder itself, `data/` or `.hl/`. When the static
config has `ping`, the service gets a healthcheck (`traefik healthcheck --ping`). A label
`hl.static-config: <hash of the static config>` makes compose recreate the container on
`hl proxy up` when only the file changed (Traefik doesn't reload static config). The dashboard
router labels (``Host(`traefik.<domain>`)``, `api@internal`) are there when the dashboard is on.
The `networks:` section (name, and `ipam` subnet when `proxy.subnet` is set) is byte-for-byte what
earlier versions wrote, so upgrading keeps the existing network and its subnet
(`internal/gen/proxy_migration_test.go` compares the old flag-based setup with the new one). There
is no TLS entrypoint.

**Upgrading from the flag-based proxy**: a running proxy keeps running until the next `hl proxy up`
(or `hl down` and `hl up`), which recreates only the container; `hl proxy status` says when the
running proxy predates the current config (compose's `config-hash` label vs `docker compose config
--hash traefik`).

## Service and port detection

`compose.Model.Exposed`: the only service that publishes a port; among several, the first of
`frontend`, `web`, `app`, `nginx`, `proxy`, `ui` that exists; otherwise an error asking for
`--service`. `TargetPort` is the container (`target`) port of that service's first published port.
`hl add` (`cli.pickServicePort`) asks instead when there is more than one candidate service, or
the service publishes several distinct container ports: a numbered list on a terminal (the
shared `prompter` from `prompt.go`), with `Exposed`'s pick (or the first port) as the default;
without a terminal (`stdinTerminal`, `golang.org/x/term`) it fails naming `--service`/`--port`. Flags skip the question.
`hl add` writes the picked `service` and `port` into `hl.yml` so later changes to the app's compose
file don't silently move the route; `prepared` only detects them when `hl.yml` leaves them empty.
Routing `none` needs neither.

## Environment layering

Compose interpolation sees, lowest to highest priority: the app's `.env`, `env_files` in order,
the shell environment, then what hl adds to the process env: `HL_APP`, `HL_URL`, `HL_PREFIX`,
`HL_HOST`, `HL_DATA`, the shared `env`, the app's `env`. Values in `env`/shared `env` are expanded
by hl (`os.Expand`) against the `HL_*` values first, then the shell environment; they can't refer
to each other. `HL_URL` has no trailing slash; `HL_PREFIX` is empty unless routing is `path`;
`HL_HOST` is the domain unless routing is `host`.

These are only interpolation variables: an app sees them in its containers only if its own compose
file passes them through (e.g. `environment: BASE_PATH: ${MY_APP_BASE_PATH:-}`).

## Command notes

- Every command that touches the proxy (`proxy()` -> `proxyProject` in `internal/cli/proxy_cmds.go`)
  first checks that the proxy's compose project isn't run from another homelab's folder (the
  `com.docker.compose.project.working_dir` label of its containers) and that `proxy.network`, if it
  exists, belongs to `proxy.project`; otherwise it stops with the fix (another `proxy.project`,
  `proxy.network` and `proxy.port`), so a second homelab never stops or reconfigures the first one's
  proxy. Without Docker the check is skipped.
- `proxy status`: state and health of the proxy container, image (and the configured one if they
  differ), listen address, static config file and its source, dynamic file, dashboard URL, access
  log, the config warnings, a clash, an image older than `config.TestedTraefikVersion`, and whether
  the running container predates the config. `proxy config` and `config proxy` print the static
  config file with its path.
- `doctor` also checks the proxy config (as above), the project/network ownership, and warns (not a
  failure) when `proxy.image` is older than the tested Traefik or its version can't be told.
- `up`/`start`, `rebuild`, `update` and `restart` start the proxy first if none of its containers
  runs (`ensureProxy`), because each override joins the proxy network as `external`. Before
  starting it (`startProxy`, also `hl proxy up`): `checkPort` - something else on `bind:port`?
  (`net.Listen`; when hl may not open a low port, a connect instead), named by `docker ps --filter
  publish=<port>`, else `lsof`, else `ss`, with a free port suggested - and `checkNetwork` - with
  `proxy.subnet` set, the proxy network existing with another subnet, or the subnet overlapping
  another Docker network or one of the machine's own, each with the fix (a free `172.x.0.0/16`).
- Multi-app commands (`eachApp`) run every app even when one fails, print each failure as it
  happens and end with "n of m apps failed: ..." (exit 1). An interruption stops the loop.
- `--wait` (`up`, `rebuild`, `update`) passes `--wait --wait-timeout <s>` to compose; when an app
  doesn't come up, hl lists its services that aren't running or healthy and prints their last
  `defaults.FailLogTail` log lines.
- `down`/`stop` with no app stops every app that has containers, then the proxy; apps and a proxy
  that aren't there are skipped, so it is safe to repeat. An app hl can no longer prepare (folder
  gone) is stopped by project name alone (`downByName`). `remove` does the same for the app,
  deletes it from `hl.yml` and `.hl/<app>/`, and keeps `data/<app>/`.
- `update` per app: skip if `git status --porcelain` fails (not a repo) or shows changes; otherwise
  `git pull --ff-only` (a failed pull is a failure for that app; the folder is left as it was),
  then `up --detach --build`. It ends with the updated and the skipped apps and why.
- `status`: STATE `down`, `up n/m` or `partial n/m`; HEALTH from the containers' status:
  `healthy` (every healthcheck passes), `starting: <services>`, `unhealthy: <services>` (also
  restarting or exited ones), `-` without healthchecks. Without Docker it still prints hl.yml's
  apps, states `?`, and a warning with the hint.
- `logs` with no app runs `docker compose logs` for every app at once, each line prefixed by its
  app; `--tail` (default `defaults.LogTail`) and `-f` apply to all.
- `ps` with no app lists the containers of this homelab's projects (the proxy and each app).
- `open` prints the URL and only launches `xdg-open`/`open` on macOS or when `DISPLAY` or
  `WAYLAND_DISPLAY` is set.
- `shell` runs `bash` if the container has it, else `sh` (`defaults.Shell`, a fixed string; the
  service name is validated so it can't pass as a flag). Without a service it uses the pinned or
  detected one. It exits with the shell's own status.
- `doctor` prints hl's version, Go version and platform, then checks docker on PATH (and its
  version), the daemon (`docker info`: version, OS, architecture), compose >= 2.24, that `hl.yml`
  loads (every problem with its line), the proxy (config, ownership, image), a non-loopback bind
  or public dashboard (warnings), the port and subnet when the proxy isn't running, projects owned
  by another homelab, and that every app prepares. It ends "all good" or "n problem(s) found"
  (exit 1) with a note to paste it into bug reports.
- Completion: commands taking one app complete only the first argument; multi-app commands leave
  out apps already typed; `logs` and `shell` complete the app's services after the app name.
- Output: colours only on a terminal (`golang.org/x/term`; `/dev/null` is not one) and never with
  `NO_COLOR` set. Commands write through `cli.stdout`/`stderr`, set from cobra's writers.
- Errors (`cli.report`): `error: <what failed>`, then `hint: <what to do>` when known - from a
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
`internal/config/edit.go`), so comments and key order survive (yaml.v3 drops blank lines and
normalises indentation to two spaces).

- `hl add`/`hl remove` (`config.AddApp`, `RemoveApp`) touch only the `apps:` mapping (`apps: {}`
  from the template is turned into a block mapping).
- `hl set`/`hl unset` (`config.EditApp`, `internal/config/set.go`) change keys of one app; the
  keys and their types come from the `App` struct by reflection (ints parsed, lists split on
  commas, map fields as `env.NAME`). A replaced value keeps the old one's comments. The new
  content is run through `config.Check` and written only if it has no problems.
- `hl edit` copies `hl.yml` to `.hl/edit/hl.yml`, runs `$VISUAL`/`$EDITOR`/`vi` on it (through
  `sh -c`, so the variable may carry arguments), and `config.Replace`s `hl.yml` only when the copy
  checks out. It and `hl config check` find the homelab with `config.Home`, not `Load`, so they
  work on a file that doesn't load.

**Checking** (`config.Check`, `internal/config/validate.go`): walks the node tree against the
`Config` type by reflection (yaml tags give the known keys, so a new field needs no checker
change), reporting syntax errors, unknown keys with a Levenshtein "did you mean", keys set twice,
type mismatches, required keys, and per-key rules (`rules`, keyed by path with app names as `*`:
routing values, ports, prefix, app names, `proxy.project`, `proxy.log_level`, version). If the walk
finds nothing it decodes and runs `Validate`, which collects every value problem and clash (bind
is an IP, domain/host names, network, subnet, image, env names, the dashboard on a public bind,
two apps with the same Docker project, URL prefix or host) as `Problem`s keyed by path; `check`
gives them the line of their key. `Load` calls it, so every command reports unknown keys and bad
values with their lines.

**Versions** (`defaults.ConfigVersion`): `version: 1`; no line means
the format before versions (0), which loads as before. `config.Upgrade` adds the line (at the top,
after any header comment) and the CLI prints a note; it runs before `add`/`remove` and after
`set`/`unset`. `Validate` refuses a version above `defaults.ConfigVersion`.

**Schema**: `schema/hl.schema.json` (draft-07), referenced from new files by
`config.SchemaComment`. `internal/config/schema_test.go` keeps it in step with the code: same keys
as the Go types, same enums, patterns and defaults, and it accepts the README's yaml examples.

**Defaults listing** (`hl config defaults`, `config.Settings`): the homelab-wide fields of `Config`
by reflection, the default taken by running `defaults()` on an empty config, the source `hl.yml` when
the raw file sets it; the CLI fills in `scan_depth`/`ignore` from `internal/defaults`, adds the
homelab folder, version and editor with their sources, and the named values hl.yml can't set from
`cli.builtinDefaults` (all read from `internal/defaults`).

## Version

`internal/version` holds `Version`/`Commit`/`Date`, set by GoReleaser's `-ldflags -X`. Without them
`Get` fills in from `runtime/debug` build info: the module version for `go install ...@vX`, and
``vcs.revision`/`vcs.time` for builds from a checkout (`-dirty` with `vcs.modified`). `String`
omits commit and date when unknown; `Full` (what `hl version` and `--version` print) adds the Go
version and OS/architecture for bug reports; `hl version --short` prints only the version.

## Projects folders: `hl init` and `hl sync`

hl.yml's `projects`, `ignore` and `scan_depth` (a separate block in `config.Config`; edits in
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
  error). A kept app with a pinned `service` is checked against its model: service gone -> detect
  both again; pinned port not among the service's published targets -> pin its first one. Apps
  with nothing pinned are detected on every run anyway and are left alone.
- **Applying**: adds go through `config.AddApp` with `path` = the projects entry as written joined
  with the subfolder (so `~/code/blog`, not an absolute path); removes stop the app by compose
  project name if it still runs (its files are gone), then `config.RemoveApp` and delete
  `.hl/<app>/` - `data/<app>/` is never touched; updates use `config.PinApp`. All edits keep
  hl.yml's comments.
- **`hl status`** calls `syncHint`, which runs only `scan.Compare` (no Docker), and prints one line
  when there are new folders or gone apps.
- **`hl init`**: questions only when stdin is a terminal and `--yes` isn't given (`prompt.go`,
  plain bufio, Enter = default, end of input = default). Pre-flight runs the doctor's docker checks;
  without a working docker it still writes hl.yml but leaves app detection to `hl sync`. Port check:
  `net.Listen` on bind:port; only "address in use" counts as busy (a normal user may not open
  port 80, Docker can). A new hl.yml is the `Template` with the answers set through the yaml node
  tree (`config.WriteNew`, `O_EXCL`, so it never overwrites); projects folders are stored relative
  inside the homelab, with `~` inside the user's home, else absolute. On an existing hl.yml init only
  saves `projects` if there are none and offers the new apps (`scan.PlanAdds`).

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
