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
