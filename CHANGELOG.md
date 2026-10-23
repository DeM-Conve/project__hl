# Changelog

All notable changes to hl are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and hl uses
[semantic versioning](https://semver.org/). Before 1.0.0, a minor version (0.x.0) may change
behaviour or `hl.yml`; such changes are listed under **Changed** with what to do.

<!-- Releasing: rename "Unreleased" to "[0.1.0] - YYYY-MM-DD", add a fresh empty "Unreleased"
above it, and update the links at the bottom (RELEASING.md). -->

## [Unreleased]

Everything below becomes 0.1.0, the first release.

### Added

- `hl init` creates a homelab folder (`hl.yml`, `data/`, `.hl/`) and makes it the default;
  `--home` and `$HL_HOME` pick another. On a terminal it asks a few questions (projects folders,
  routing, this machine or the network, another port if the default one is busy) and offers the
  apps it finds as a checklist; `--projects`, `--routing`, `--bind`, `--port`, `--apps` and
  `--yes` answer them in scripts. It checks Docker and compose first, and on an existing homelab
  never overwrites `hl.yml` but offers the new apps.
- `projects:` in `hl.yml`: folders whose subfolders with a compose file are apps; `ignore:`
  patterns, a `.hlignore` file or `scan_depth` control what is picked up.
- `hl sync` keeps `hl.yml` in step with the projects folders: adds new apps, removes ones whose
  folder is gone (data kept), re-detects a pinned service or port that disappeared. It shows the
  plan first; `--dry-run` only shows it, `--yes` applies it without asking.
- `hl status` says when `hl sync` has something to do.
- `hl add` adds any folder with a compose file as an app, finding the service that publishes a
  port; `hl remove` takes it out again and keeps its data.
- One Traefik proxy in front of every app, with `path` (`http://localhost/<app>/`), `host`
  (`http://<app>.localhost`) or `none` routing, per homelab or per app.
- Every named volume kept as a plain folder under `data/<app>/<volume>`.
- Apps run unmodified: hl layers a generated compose override on top (`hl config <app>`).
- Shared `env` for every app, per-app `env` and `env_files`, and `${HL_URL}`, `${HL_PREFIX}`,
  `${HL_HOST}`, `${HL_DATA}`, `${HL_APP}` for an app's settings.
- Commands: `status`/`ls`, `up`/`start`, `down`/`stop`, `restart`, `rebuild`, `update`, `logs`,
  `ps`, `open`, `shell`, `config`, `path`, `proxy up|down|logs`, `doctor`, `version` and shell
  `completion`.
- Release archives for Linux and macOS (amd64, arm64) with `checksums.txt` and an SPDX SBOM per
  archive; `install.sh` verifies the checksum, takes `--version` and `--dir`, and installs to
  `~/.local/bin` (or `/usr/local/bin` as root).
- The proxy runs with one readable Traefik static config file, `.hl/proxy/traefik.yml`, generated
  from `hl.yml`; `hl config proxy` (or `hl proxy config`) prints it. `proxy.config` uses your own
  file instead, and hl says what it is missing for hl's apps.
- `proxy.dynamic` for a Traefik dynamic config file (extra routes and middlewares), kept in a
  subfolder of the homelab.
- `proxy.access_log`, `proxy.log_level` and `proxy.project`; a healthcheck for the proxy;
  `hl proxy status`.
- The Traefik image is pinned to the tested version (`traefik:v3.6.25`); `hl doctor` warns when
  `proxy.image` is older.
- hl refuses to use a proxy or Docker network that another homelab runs, and says how to run both
  side by side. The app name `proxy` is reserved, since its compose project would be the proxy's,
  and so are `check` and `defaults` (the `hl config` subcommands), so `hl config <app>` always
  shows the app. `hl add`, `hl.yml`, `hl init`, `hl sync` and the schema all refuse them.
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
- `hl add` asks which service or port to route when there are several; flags still win, and
  without a terminal it names the flag to pass instead of guessing.
- `hl config defaults` shows every default, the value in effect and where it comes from.
- Example apps in `examples/` and guides in `docs/guides/`: base paths, moving an existing compose
  app into hl, reaching apps from your network, backups, putting hl behind HTTPS.

<!-- Placeholder: entries from the feat/* branches merged before 0.1.0 go in the list above (in
a first release everything is "Added"), e.g. the Traefik dashboard being opt-in with
`proxy.dashboard` / `proxy.dashboard_public`, and `hl restart` starting the proxy first. -->

[Unreleased]: https://github.com/DeM-Conve/project__hl/commits/main
<!-- After v0.1.0 is tagged:
[Unreleased]: https://github.com/DeM-Conve/project__hl/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/DeM-Conve/project__hl/releases/tag/v0.1.0
-->
