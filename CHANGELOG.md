# Changelog

All notable changes to hl are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and hl uses
[semantic versioning](https://semver.org/).

## [Unreleased]

### Added

- `hl init` creates a homelab folder (`hl.yml`, `data/`, `.hl/`) and makes it the default;
  `--home` and `$HL_HOME` pick another.
- `hl add` adds any folder with a compose file as an app, finding the service that publishes a
  port; `hl remove` takes it out again and keeps its data.
- One Traefik proxy in front of every app, with `path` (`http://localhost/<app>/`), `host`
  (`http://<app>.localhost`) or `none` routing, per homelab or per app.
- Every named volume kept as a plain folder under `data/<app>/<volume>`.
- Apps run unmodified: hl layers a generated compose override on top (`hl config <app>`).
- Shared `env` for every app, per-app `env` and `env_files`, and `${HL_URL}`, `${HL_PREFIX}`,
  `${HL_HOST}`, `${HL_DATA}`, `${HL_APP}` for an app's settings.
- Commands: `status`/`ls`, `up`, `down`, `restart`, `rebuild`, `update`, `logs`, `ps`, `open`,
  `shell`, `config`, `path`, `proxy up|down|logs`, `doctor`, `version` and shell `completion`.
- Release archives for Linux and macOS (amd64, arm64) with checksums, and `install.sh`.

[Unreleased]: https://github.com/DeM-Conve/project__hl/commits/main
