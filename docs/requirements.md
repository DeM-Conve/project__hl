# Requirements and design decisions

What hl must do and the decisions behind it. Read this before proposing a feature or changing
behaviour, so a change doesn't undo a choice that was made on purpose. Each entry says what and why;
where the why was never written down it says so instead of guessing.

## Purpose

Run several self-hosted apps on one machine behind one reverse proxy, with one set of commands,
where each app is an ordinary project folder with its own `docker compose` file that also runs on
its own without hl.

hl grew out of the maintainer's own homelab: a bash script plus hand-written Traefik overrides for
a handful of web apps, replaced by this standalone tool. Its real-world test bed is that homelab
(kept in a separate, private repo) running five apps, all path-routed, each exposing a `frontend`
service on container port 80.

## Hard rules

1. **Generic.** Nothing in hl may be specific to one person's homelab or one app. Homelab-specific
   settings belong in that homelab's `hl.yml`, never in hl's code or defaults.
2. **Apps are not edited.** hl never writes into an app's folder. It layers a generated compose
   override on top at run time. Why: the apps are standalone open-source projects that other
   people run without hl.
3. **hl doesn't parse compose files.** It asks `docker compose config --format json` for the
   resolved model, so it supports whatever compose supports and never drifts from it.
4. **All state is in the homelab folder.** `hl.yml` (hand-editable, comments kept), `data/` (app
   data) and `.hl/` (generated, disposable). Besides that, only the default-homelab pointer in the
   user config dir. No daemon, no database.
5. **Data is plain folders.** Every named volume is bind-mounted at `data/<app>/<volume>`, so a
   backup is one folder copy and removing an app never deletes its data.
6. **Safe by default.** The proxy listens on `127.0.0.1` unless `proxy.bind` says otherwise;
   exposing to the network is an explicit choice (see SECURITY.md).

## Decisions

| Decision | Why |
|---|---|
| Traefik as the proxy, configured by Docker labels | Labels can be added from an override without touching the app; the Docker provider picks apps up as they start. |
| Path routing (`http://localhost/<app>/`) is the default; host routing and `none` are per app | Google sign-in accepts plain-`http` redirect URIs only for exactly `localhost`, so `<app>.localhost` breaks OAuth. Host routing stays for apps that can't take a base path. |
| Path routing strips the prefix and sends `X-Forwarded-Prefix`; `/<app>` redirects to `/<app>/` | Apps then serve from `/`; they only need a base-path setting for the links they build, fed from `${HL_PREFIX}`. |
| Apps are configured through their own env vars, fed from `hl.yml` (`env`, `${HL_URL}`, `${HL_PREFIX}`, ...) | Keeps app-specific wiring in config, not code. The maintainer's apps follow the Immich/Paperless/linuxserver conventions (`<APP>_URL`, `<APP>_BASE_PATH`, `*_LOCATION`, `TZ`). |
| Shared top-level `env` for every app; an app's own `env` wins | One place for things like `TZ`. |
| Optional fixed `proxy.subnet` | So an app can trust the proxy's `X-Forwarded-For` by address (e.g. per-IP rate limits). |
| Each app is its own compose project `hl-<id>` | Apps can be started, stopped and rebuilt independently; container names are predictable. |
| `hl add` pins the detected `service` and `port` in `hl.yml` | A later change to the app's compose file doesn't silently move the route. |
| `hl update` pulls fast-forward only and skips folders with uncommitted changes | Never merges or overwrites local work in an app checkout. |
| Docker compose v2.24+ | The override uses `!reset` to drop the exposed service's host ports. |
| Default image `traefik:v3.6` | v3.6.1+ is needed for Docker Engine 29's API. |
| Linux and macOS, amd64 and arm64; Go 1.21 | Release matrix in `.goreleaser.yaml`; `go.mod` sets the Go version. Windows was never targeted (reason not recorded). |
| Releases are built locally with GoReleaser; no GitHub Actions | The CI and release workflows were added and then removed on purpose ("Releases are cut locally with goreleaser"). Checks run locally. |
| Homebrew tap: prepared but off | Waits for a `homebrew-tap` repo; steps are in the comment at the end of `.goreleaser.yaml`. |
| `install.sh` verifies the archive against `checksums.txt` and uses sudo only when the target folder needs it | Safe `curl | sh` install. |
| MIT licence | |

## Not goals (as built today)

These are absent, not necessarily rejected; see [status.md](status.md) for open questions about them.

- TLS/HTTPS (the proxy has only an `:80` entrypoint).
- Authentication in front of apps or the Traefik dashboard.
- Running apps on more than one machine.
- Managing app secrets: they stay in each app's own `.env`, which hl reads but never writes.
- Backups beyond keeping data in plain folders.
