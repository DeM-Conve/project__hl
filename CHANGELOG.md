# Changelog

All notable changes to hl are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and hl uses
[semantic versioning](https://semver.org/). Before 1.0.0, a minor version (0.x.0) may change
behaviour or `hl.yml`; such changes are listed under **Changed** with what to do.

<!-- Releasing: rename "Unreleased" to "[0.1.0] - YYYY-MM-DD", add a fresh empty "Unreleased"
above it, and update the links at the bottom (RELEASING.md). -->

## [Unreleased]

Everything below becomes 0.1.0, the first release. **Changed** and **Fixed** describe how this
release differs from earlier builds of `main`.

### Added

- `hl init` creates a homelab folder (a short `hl.yml`, `data/`, `.gitignore`) and makes it the
  default; `--home` and `$HL_HOME` pick another. It works out a plan - projects folders (`~/code`,
  `~/projects`, `~/src` when they exist), the proxy (port 8080 when 80 is busy), every web app
  found with its service, port and env, and the folders it skips and why - shows it and asks
  once; `--projects`, `--routing`, `--bind`, `--port`, `--apps`, `--strip-prefix` and `--no-env`
  change the plan and `--yes` applies it without asking. It checks Docker and compose first, and
  on an existing homelab never overwrites `hl.yml` but adds the new apps. The `hl.yml` it writes
  holds only the projects folders, the proxy settings that differ from the defaults, and the
  apps; every other setting is documented in the README.
- `projects:` in `hl.yml`: folders whose subfolders with a compose file are apps; `ignore:`
  patterns, a `.hlignore` file or `scan_depth` control what is picked up.
- `hl sync` keeps `hl.yml` in step with the projects folders: adds new apps, removes ones whose
  folder is gone (data kept), re-detects a pinned service or port that disappeared. It shows the
  plan first; `--dry-run` only shows it, `--yes` applies it without asking. `hl status` says when
  `hl sync` has something to do.
- `hl add` adds any folder with a compose file as an app, finding the service that publishes a
  port; when several services or ports could be the one, it asks on a terminal and otherwise
  names the flag to pass. `hl remove` takes an app out again and keeps its data.
- One Traefik proxy in front of every app, with `path` (`http://localhost/<app>/`), `host`
  (`http://<app>.localhost`) or `none` routing, per homelab or per app.
- Every named volume kept as a plain folder under `data/<app>/<volume>`.
- Apps run unmodified: hl layers a generated compose override on top (`hl show <app> --compose`).
- Shared `env` for every app, per-app `env` and `env_files`, and `${HL_URL}`, `${HL_PREFIX}`,
  `${HL_HOST}`, `${HL_DATA}`, `${HL_APP}` for an app's settings.
- Commands: `status`/`ls`, `up`/`start`, `down`/`stop`, `restart`, `rebuild`, `logs`,
  `open`, `shell`, `show`, `doctor`,
  `version` and shell `completion` (which offers app and service names).
- `hl status` has a HEALTH column: `healthy`, `starting: <services>`, `unhealthy: <services>`,
  or `-` for apps without healthchecks.
- `--wait` and `--wait-timeout` (default 2m) on `hl up` and `hl rebuild`: wait until
  each app's service behind the proxy and the services it depends on (`depends_on`) are running
  and healthy, and for an app that isn't, name those services and show their last log lines. A
  sidecar nothing depends on (say, a backup job with a long healthcheck interval) no longer fails
  `--wait`; for an app with routing `none` it waits for every service.
- Apps get this machine's time zone as `TZ` (from `$TZ`, `/etc/localtime` or `/etc/timezone`)
  unless the shared env, the app's env or its env files set one; `hl show` shows it
  and where it came from. When the zone can't be told, no `TZ` is set.
- `hl logs` with no app shows every app's logs at once, each line prefixed by its app; `-n`/`--tail`
  sets how many lines (default 200).
- `hl rebuild --pull` runs `git pull --ff-only` in each app's folder before rebuilding it, skips
  folders that aren't git repos or have uncommitted changes, and says which it skipped and why;
  `hl rebuild proxy --pull` pulls the proxy's image.
- `hl shell` opens the service behind the proxy by default, also when hl detected it rather than
  `hl.yml` pinning it, and exits with the shell's own status.
- `hl version` shows the Go version, platform and whether the build had local changes;
  `--short` prints only the version.
- `name:` in `hl.yml` runs a second homelab on the same Docker: its apps become `hl-<name>-<app>`
  and its proxy project and network `hl-<name>-proxy`. hl never starts, stops or replaces a
  compose project that another homelab's folder runs (`hl down` leaves it alone, `hl status`
  shows `other homelab`), and refuses a proxy or network another homelab uses, saying how to fix
  it.
- Clear errors: an `error:` line, then a `hint:` with what to do. Docker's own error output is
  read to explain the common failures (daemon down, no permission, port taken, compose too old,
  subnet overlap, image pull, disk full).
- Exit codes: 0 ok, 1 failure, 2 usage (unknown command, argument or flag), 128 + signal when
  interrupted (130 for Ctrl-C); `hl shell` exits with the shell's status.
- Ctrl-C while docker runs lets the docker command stop by itself and starts nothing more; a
  second Ctrl-C stops at once.
- Commands that act on several apps carry on past one that fails and end with
  "n of m apps failed: ..."; `hl up` and `hl down` are safe to repeat.
- The proxy's port is checked before the proxy starts, naming the program or container that
  holds it; a `proxy.subnet` that overlaps another Docker network or one of the machine's is
  found, with a free subnet suggested.
- `hl.yml`, the default-homelab note and every generated file are written atomically (an existing
  file keeps its mode, a symlink stays a symlink).
- The proxy runs with one readable Traefik static config file, `.hl/proxy/traefik.yml`, generated
  from `hl.yml`; `hl show proxy` names it. `proxy.config` uses your own
  file instead, and hl says what it is missing for hl's apps.
- `proxy.dynamic` for a Traefik dynamic config file (extra routes and middlewares), kept in a
  subfolder of the homelab.
- `proxy.access_log`, `proxy.log_level` and `proxy.project`; a healthcheck for the proxy.
- The proxy works like an app: `hl up proxy`, `hl down proxy`, `hl restart proxy`,
  `hl rebuild proxy` (recreates it), `hl logs proxy [-f]`, and `hl show proxy` for its state,
  health, listen address, every setting, and warnings (a running proxy behind `hl.yml`, an old
  image).
- `proxy.dashboard` turns on the Traefik dashboard at `http://traefik.<domain>`; on a
  `proxy.bind` that isn't loopback it also needs `proxy.dashboard_public: true`, and `hl doctor`
  warns while it is reachable from the network.
- The Traefik image is pinned to the tested version (`traefik:v3.6.25`); `hl doctor` warns when
  `proxy.image` is older.
- Reserved app name: `proxy` (its compose project would be the proxy's, and `hl up proxy` and
  the like mean the proxy). `hl add`, `hl.yml`, `hl init`, `hl sync` and the schema refuse it.
- `hl.yml` is versioned (`version: 1`). Older files without it keep working and get the line,
  with a message, the first time hl edits them; a newer file asks you to update hl.
- A JSON Schema for `hl.yml` (`schema/hl.schema.json`); `hl init` writes the
  `yaml-language-server` line so editors autocomplete and flag mistakes.
- `hl set <app> key=value` and `hl unset <app> key` change an app's settings, keeping `hl.yml`'s
  comments and writing only a valid result. `hl set proxy port=8080 bind=0.0.0.0` changes the
  proxy and `hl set env.TZ=UTC` (no app) the env every app gets; `hl unset proxy port` and
  `hl unset env.TZ` remove them. hl says which command applies the change, and shell completion
  offers the keys.
- `hl show [app]` shows what hl stores and works out for an app (folder, service and port, routing,
  URL, data folder, and each env variable with where its value comes from), or with no app the
  homelab: its folder, the proxy's settings, the shared env and the apps.
- `hl doctor` warns when hl is older than the latest release on GitHub (a 3-second request,
  silently skipped offline and for development builds) and tells a `go install` build of a
  commit how to install a release. It is the only network request hl makes.
- `hl edit` opens `hl.yml` in your editor, checks it on save and reopens it on a problem;
  `hl.yml` is never left broken.
- Windows gets a short "not supported" error suggesting WSL 2; `hl version` and `hl completion`
  still work there.
- Release archives for Linux and macOS (amd64, arm64) with `checksums.txt` and an SPDX SBOM per
  archive; `install.sh` verifies the checksum, takes `--version` and `--dir`, and installs to
  `~/.local/bin` (or `/usr/local/bin` as root).
- Example apps in `examples/` and guides in `docs/guides/`: base paths, moving an existing compose
  app into hl, reaching apps from your network, backups, putting hl behind HTTPS.
- `hl add`, `hl init` and `hl sync` fill in an app's `env` from the variables its compose file
  uses (`docker compose config --variables`): `<X>_BASE_PATH` gets `${HL_PREFIX}` (path
  routing), the matching `<X>_URL` gets `${HL_URL}`, `DATA_LOCATION` gets `${HL_DATA}` and
  `BACKUP_LOCATION` `${HL_DATA}/backups`. They print what they mapped; `hl init` and `hl sync`
  take `--no-env` to map nothing, `hl add <folder> env.NAME=` leaves one variable out.
- `hl add <folder> key=value...` takes the settings `hl set <app>` takes, plus `name`:
  `hl add ~/code/wiki routing=host service=web port=8080 env.TZ=UTC`. `env.NAME=value` wins over
  the mapped values.
- `strip_prefix:` in `hl.yml` (and `hl init --strip-prefix`): folder-name prefixes left out of
  app names, so folder `project__blog` becomes app `blog` in `hl init`, `hl sync` and `hl add`.
- `hl set` changes the homelab's own settings too: `projects`, `ignore`, `scan_depth`,
  `strip_prefix` and `name`. A list is replaced with `=`, and `+=` / `-=` add and remove entries
  (`hl set projects+=~/src ignore-=old`, also for an app's `compose` and `env_files`);
  `hl unset ignore` drops one. `name` is refused while any of the homelab's apps or its proxy
  runs. Nothing needs `hl edit` any more; it stays as a fallback.

### Changed

- **Breaking:** `hl add` takes `key=value` settings instead of flags: `--name x` is `name=x`,
  `--routing`, `--prefix`, `--host`, `--service`, `--port` and `--compose` are `routing=`,
  `prefix=`, `host=`, `service=`, `port=` and `compose=`, `--env K=V` is `env.K=V`, and
  `--no-env` is gone (`env.K=` leaves one mapped variable out; `hl unset <app> env` drops them all).
- **Breaking:** `hl projects`, `hl ignore` and `hl strip-prefix` are gone: `hl set projects+=<folder>`,
  `hl set ignore-=<pattern>`, `hl set strip_prefix=<prefix>` and `hl unset <key>` do what their
  `add`/`remove` did, and `hl show` lists them.
- **Breaking:** `hl update` is `hl rebuild --pull` (same skips: folders that aren't git repos or
  have uncommitted changes are neither pulled nor rebuilt).
- **Breaking:** `hl config`, `hl ps`, `hl path` and `--wait-all` are gone. `hl config <app>` is
  `hl show <app> --compose`; `hl config check` is what every command (and `hl doctor`) does
  already; what `hl config defaults` showed that matters is in `hl show` (the `TZ` apps get, the
  projects folders, `ignore`, `scan_depth`) and `hl show proxy`, the rest in the README's
  "Built-in values". Use `hl status` or `docker ps` for containers and `hl show` for folders.
  `check` and `defaults` are no longer reserved app names.
- **Breaking:** the `hl proxy` command group is gone; the proxy is a pseudo-app instead.
  `hl proxy up|down|logs` are `hl up|down|logs proxy`, `hl proxy status` is `hl show proxy`, and
  `hl proxy config` is gone (`hl show proxy` names the file).

- `hl.yml` is checked strictly on every command: unknown keys,
  wrong types, bad values and clashes between apps (same Docker project, URL prefix or host) stop
  the command, all listed at once with their lines (`hl doctor` lists them too).
- The Traefik dashboard is off unless `proxy.dashboard: true` is set.
- `hl init` and `hl sync` add only folders that look like web apps (a preferred service name or a
  common HTTP port); others, like a database on its own port, are listed with the `hl add`
  command to use (`routing=none` to run it without the proxy).
- `hl add` without a terminal no longer stops when several services publish ports and one has a
  usual web name (`frontend`, `web`, `app`, `nginx`, `proxy`, `ui`): it routes that one and says
  so. It still asks for `service=` when none has such a name.
- `hl restart` starts the proxy first, like `hl up` and `hl rebuild`.
- Error hints name the command that fixes a setting (`hl set proxy port=8000`, `hl unset proxy subnet`,
  `hl set my-app service=...`) instead of the `hl.yml` key to edit; `hl edit` stays as the fallback.

### Fixed

- Folders inside `data/<app>/` that the app's env points at (like an auto-mapped
  `BACKUP_LOCATION`) are created by hl before the app starts; Docker created them owned by root,
  so an app running as you couldn't write its backups.
- Colours are used only on a real terminal: output sent to `/dev/null` had them.

[Unreleased]: https://github.com/DeM-Conve/project__hl/commits/main
<!-- After v0.1.0 is tagged:
[Unreleased]: https://github.com/DeM-Conve/project__hl/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/DeM-Conve/project__hl/releases/tag/v0.1.0
-->
