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

- `hl init` creates a homelab folder (`hl.yml`, `data/`, `.gitignore`) and makes it the default;
  `--home` and `$HL_HOME` pick another. On a terminal it asks a few questions (projects folders,
  routing, this machine or the network, another port if the default one is busy) and offers the
  apps it finds as a checklist; `--projects`, `--routing`, `--bind`, `--port`, `--apps` and
  `--yes` answer them in scripts. It checks Docker and compose first, and on an existing homelab
  never overwrites `hl.yml` but offers the new apps.
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
- Apps run unmodified: hl layers a generated compose override on top (`hl config <app>`).
- Shared `env` for every app, per-app `env` and `env_files`, and `${HL_URL}`, `${HL_PREFIX}`,
  `${HL_HOST}`, `${HL_DATA}`, `${HL_APP}` for an app's settings.
- Commands: `status`/`ls`, `up`/`start`, `down`/`stop`, `restart`, `rebuild`, `update`, `logs`,
  `ps`, `open`, `shell`, `config`, `path`, `proxy up|down|logs|status|config`, `doctor`,
  `version` and shell `completion` (which offers app and service names).
- `hl status` has a HEALTH column: `healthy`, `starting: <services>`, `unhealthy: <services>`,
  or `-` for apps without healthchecks.
- `--wait` and `--wait-timeout` (default 2m) on `hl up`, `hl rebuild` and `hl update`: wait until
  every app is running and healthy, and for one that isn't, name its services and show their
  last log lines.
- `hl logs` with no app shows every app's logs at once, each line prefixed by its app; `-n`/`--tail`
  sets how many lines (default 200).
- `hl update` says which apps it skipped (not a git repo, uncommitted changes) and why.
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
  from `hl.yml`; `hl config proxy` (or `hl proxy config`) prints it. `proxy.config` uses your own
  file instead, and hl says what it is missing for hl's apps.
- `proxy.dynamic` for a Traefik dynamic config file (extra routes and middlewares), kept in a
  subfolder of the homelab.
- `proxy.access_log`, `proxy.log_level` and `proxy.project`; a healthcheck for the proxy;
  `hl proxy status`.
- `proxy.dashboard` turns on the Traefik dashboard at `http://traefik.<domain>`; on a
  `proxy.bind` that isn't loopback it also needs `proxy.dashboard_public: true`, and `hl doctor`
  warns while it is reachable from the network.
- The Traefik image is pinned to the tested version (`traefik:v3.6.25`); `hl doctor` warns when
  `proxy.image` is older.
- Reserved app names: `proxy` (its compose project would be the proxy's), and `check` and
  `defaults` (the `hl config` subcommands), so `hl config <app>` always shows the app. `hl add`,
  `hl.yml`, `hl init`, `hl sync` and the schema all refuse them.
- `hl.yml` is versioned (`version: 1`). Older files without it keep working and get the line,
  with a message, the first time hl edits them; a newer file asks you to update hl.
- A JSON Schema for `hl.yml` (`schema/hl.schema.json`); `hl init` writes the
  `yaml-language-server` line so editors autocomplete and flag mistakes.
- `hl config check` lists every problem in `hl.yml` with its line number and a fix, including
  unknown keys with a "did you mean".
- `hl set <app> key=value` and `hl unset <app> key` change an app's settings, keeping `hl.yml`'s
  comments and writing only a valid result.
- `hl edit` opens `hl.yml` in your editor, checks it on save and reopens it on a problem;
  `hl.yml` is never left broken.
- `hl config defaults` shows every default, the value in effect and where it comes from, and the
  built-in values (log tail, wait timeout, Docker names, generated files).
- Windows gets a short "not supported" error suggesting WSL 2; `hl version` and `hl completion`
  still work there.
- Release archives for Linux and macOS (amd64, arm64) with `checksums.txt` and an SPDX SBOM per
  archive; `install.sh` verifies the checksum, takes `--version` and `--dir`, and installs to
  `~/.local/bin` (or `/usr/local/bin` as root).
- Example apps in `examples/` and guides in `docs/guides/`: base paths, moving an existing compose
  app into hl, reaching apps from your network, backups, putting hl behind HTTPS.

### Changed

- `hl.yml` is checked strictly on every command, not only by `hl config check`: unknown keys,
  wrong types, bad values and clashes between apps (same Docker project, URL prefix or host) stop
  the command, all listed at once with their lines. Run `hl config check` before upgrading and
  fix what it lists.
- The Traefik dashboard is off unless `proxy.dashboard: true` is set.
- `hl restart` starts the proxy first, like `hl up`, `hl rebuild` and `hl update`.

### Fixed

- Colours are used only on a real terminal: output sent to `/dev/null` had them.

[Unreleased]: https://github.com/DeM-Conve/project__hl/commits/main
<!-- After v0.1.0 is tagged:
[Unreleased]: https://github.com/DeM-Conve/project__hl/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/DeM-Conve/project__hl/releases/tag/v0.1.0
-->
