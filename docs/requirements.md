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
| `hl rebuild --pull` (was `hl update` until 2026-10-27, folded in for a smaller CLI) pulls fast-forward only and skips folders with uncommitted changes (neither pulled nor rebuilt); for the proxy, `--pull` pulls its image | Never merges or overwrites local work in an app checkout. One command for "rebuild", with or without new code. |
| Docker compose v2.24+ | The override uses `!reset` to drop the exposed service's host ports. |
| Default image pinned to the tested Traefik release (`traefik:v3.6.25`); `hl doctor` warns about an older `proxy.image` | v3.6.1+ is needed for Docker Engine 29's API; a pinned tag means every user runs the version hl was tested with. |
| Traefik is configured by one static config file (`.hl/proxy/traefik.yml`), generated from `hl.yml`, not command-line flags | Readable (`hl show proxy` names the file), and replaceable as a whole; a user file is checked for what hl needs instead of being merged. |
| Proxy project and network names stay `hl-proxy` by default; a clash with another homelab is refused with a fix (`proxy.project`, `proxy.network`, `proxy.port`) | Changing the defaults would recreate every existing proxy network; detecting the owner (compose's working-dir and project labels) keeps existing homelabs unchanged. |
| Linux and macOS, amd64 and arm64; Go 1.21 | Release matrix in `.goreleaser.yaml`; `go.mod` sets the Go version. |
| `hl restart` starts the proxy first, like `up` and `rebuild` (2026-10-23, maintainer's call via Claude, revisit if wrong) | Every override joins the proxy network as `external`; restarting an app while the proxy is down left it unreachable (or failing on the missing network). One rule for every command that starts apps. |
| `--wait` waits for the service behind the proxy and its `depends_on` (followed through), not every service (2026-10-27, maintainer's request; `--wait-all` for every service was dropped the same day: an app with routing `none` still waits for all) | A sidecar nothing depends on (a backup job whose healthcheck runs every few minutes) made `hl up --wait` fail while the app already served. Every service is still started; only the waiting is narrower, and `hl status` still shows the sidecar as starting. |
| `TZ` defaults to the host's time zone (`$TZ`, else `/etc/localtime`'s target, else `/etc/timezone`) when neither the shared `env`, the app's `env` nor its env files set it; when the zone can't be told, no `TZ` is set (2026-10-27, maintainer's request; replaces "no `TZ` unless the user sets it" in the product plan) | Containers default to UTC, so logs and schedules were off from the user's clock unless every homelab set `TZ` by hand. The host's zone is the user's own setting, not one baked into hl, so it stays generic; the app's own `.env` and `env_files` are respected because hl's process env would otherwise override them. |
| `hl doctor` is the only command that makes a network request of hl's own: the latest GitHub release, with a short timeout, silent when it fails, skipped for development builds (2026-10-27, maintainer's request) | Users of an old build should learn there is a newer one, without hl phoning home on every command or failing offline. A `go install` of a commit can't be compared, so it is told how to install a release instead. |
| The proxy is a pseudo-app, not a command group: `hl up\|down\|restart\|rebuild\|logs\|show proxy` (2026-10-27, maintainer's request: a minimal CLI) | `proxy` is already the reserved app name, so it can't clash with an app; one set of verbs for apps and the proxy is less to learn than a second `hl proxy ...` group with its own subcommands. |
| No debug or duplicate commands (2026-10-27, maintainer's request): `hl config` is gone (`hl config <app>` is `hl show <app> --compose`; `config check` is what every command and `hl doctor` already do; `config defaults` is folded into `hl show`, the rest is the README's "Built-in values"; `config proxy` printed a generated file `hl show proxy` names), and so are `hl ps` (`hl status` and `docker ps`), `hl path` (`hl show` prints the folders) and `--wait-all` | Each was a second way to see something another command shows. Fewer commands, and `proxy` is the only reserved app name left. |
| Lists are changed with `hl set`/`hl unset`, not commands of their own: `key=a,b` replaces, `key+=x` adds, `key-=x` removes, `hl unset key` drops it; `hl set` also takes `scan_depth` and `name` (refused while any of the homelab's apps or its proxy runs) (2026-10-27, maintainer's request) | One command for every setting, so the user never needs to open `hl.yml` (`hl edit` stays as a fallback); `hl projects`/`hl ignore`/`hl strip-prefix` were three commands with three subcommands each for what is one key each. `name` renames every Docker project and network, so changing it under running containers would orphan them. |
| `hl add <folder> key=value...` takes `hl set <app>`'s settings plus `name`, not flags; no `--name` (`name=` does it) and no `--no-env` (`env.NAME=` with no value leaves one mapped variable out, `hl unset <app> env` drops them all) (2026-10-27, maintainer's request) | One way to write a setting, whether the app is being added or changed later; seven flags and their help text were a second vocabulary for the same keys. `--no-env` had no case `env.NAME=` and `hl unset` don't cover. `hl init`/`hl sync` keep their own flags. |
| `hl remove` of an app inside a projects folder adds its folder name to `ignore`; `hl add` of an ignored folder takes the exact entry out again (2026-10-27, maintainer's request) | Otherwise the next `hl sync` adds the removed app straight back. A name-based entry is what `ignore` already holds; one added by hl says how to undo it. |
| The proxy has only the keys a homelab needs: `bind port routing domain subnet image network project debug`. `proxy.config` (own Traefik file), `proxy.dynamic` (file provider), `proxy.dashboard`/`dashboard_public` and `proxy.access_log`/`proxy.log_level` were removed; `proxy.debug: true` gives Traefik's INFO log plus the access log. A removed key is a clear error naming `hl unset proxy <key>` (2026-10-27, maintainer's request: a minimal CLI; replaces "the dashboard is off by default ... `dashboard_public`" of 2026-10-23) | Each had no real use in the homelabs hl is for, and every one was more code, docs and validation (a user file hl had to check, a mounted folder hl had to police, an unauthenticated dashboard behind two switches). The two log keys only ever served debugging, so one switch does it. Someone who needs Traefik beyond this runs their own proxy in front (see HTTPS below). |
| Windows is not supported: hl says so in a friendly error and suggests WSL 2; `hl version` and `completion` still work (2026-10-23, maintainer's call via Claude, revisit if wrong) | hl drives Docker's Linux containers with Unix paths, the Docker socket and POSIX signals; WSL 2 gives Windows users a Linux to run hl in. Release builds stay Linux and macOS only. |
| HTTPS is out of scope for v0.1: the proxy serves plain HTTP; for TLS put hl behind your own TLS proxy (2026-10-23, maintainer's call via Claude, revisit if wrong) | Certificates (local CA or ACME) are a feature of their own (product plan, P2). See "HTTPS: your own TLS proxy in front" below and [the guide](guides/behind-https.md). |
| Errors say what failed and what to do (`error:` line, then `hint:`); exit codes are 0 ok, 1 failure, 2 usage (unknown command, argument or flag), 128+signal when interrupted (130 for Ctrl-C), and `hl shell` exits with the shell's own status (2026-10-23) | Scripts can tell a typo from a failure from a Ctrl-C; people get the next step without searching. Docker's own error output is read to explain the common failures (daemon down, no permission, port taken, compose too old, subnet overlap). |
| Ctrl-C while docker runs: hl waits for the command to stop by itself (a terminal Ctrl-C reaches it too, other signals are passed on), starts nothing more, and a second signal kills it (2026-10-23) | Never leaves compose half-way through a change because hl died first, and never hangs. |
| hl.yml is checked strictly on every command: unknown keys (with "did you mean"), wrong types, bad values and clashes between apps (same Docker project, URL prefix or host) are all reported at once, each with file:line:column (2026-10-23) | A typo in hl.yml used to be silently ignored or fail one key at a time. |
| hl.yml, the default-homelab note and every generated file are written atomically (temporary file, then rename; an existing file keeps its mode, a symlink stays a symlink) (2026-10-23) | A crash or Ctrl-C never leaves a half-written config. |
| Several homelabs on one Docker: a top-level `name:` gives a homelab its own project names (`hl-<name>-<app>`) and default proxy project and network (`hl-<name>-proxy`); without `name` the names stay as they were. hl never starts, stops or replaces a compose project whose containers were started from another homelab's folder (read from compose's `config_files` label); it refuses with the fix, and `hl down` leaves such projects alone (2026-10-23) | Two homelabs (or two users) on one machine share one Docker daemon, where project and network names are global. Existing single-homelab setups keep their names, so nothing is recreated on upgrade. |
| Every default and every name hl gives files, folders, labels and Docker objects is defined once, in `internal/defaults`; the README's "Built-in values" lists them (2026-10-23) | The no-hard-coding rule (product plan section 0). |
| Multi-app commands carry on past one broken app and end with "n of m apps failed: ..." (exit 1); `hl up`/`hl down` are safe to repeat (2026-10-23) | One app with a missing folder shouldn't keep the others down; repeating a command should never be an error. |
| `hl init` without a folder sets up the homelab hl already uses (`--home`, `$HL_HOME`, an `hl.yml` at or above the current folder, the recorded default), else `~/homelab`; never the current folder unless it is named (`hl init .`) (2026-10-27) | `cd ~ && hl init --yes` turned the home folder into a homelab. A fixed default is the simplest safe rule (no guessing whether a folder "looks like" a projects folder), and re-running init finds the same homelab. |
| Without a terminal, `hl init` and `hl sync` show their plan and write only with `--yes` (2026-10-27) | A piped answer (`echo n | hl init`) was ignored and the plan applied; one rule for both commands that change hl.yml from a plan. |
| `hl init` avoids clashes instead of reporting them: a busy port 80 moves the new proxy to the first free of a short list, and another homelab's `hl-proxy` project or network gives the new homelab a `name` from its folder; both are in the plan (2026-10-27) | A second homelab used to be written with the defaults and then refused by `hl up` and `hl doctor`; init already knows everything needed to pick free names. Existing homelabs are never renamed. |
| `hl init` strips a shared folder-name prefix by default: one ending in `-`, `_` or `__` (`defaults.PrefixEnds`) that at least two and at least half of the folders found share, never one that would give two apps the same name; `--strip-prefix none` turns it off (2026-10-27) | Folders like `project__blog` gave app names (and URLs) like `/project__blog/`. Requiring half of the folders keeps a pair like `blog-api`/`blog-web` among many others from being renamed by accident; the rule is generic, no prefix is built in. Only for a new homelab: apps already in `hl.yml` keep their names. |
| `hl status` points to `hl sync` only for changes sync would make (an app to add, an app whose folder is gone); folders sync skips are not mentioned (2026-10-27) | A hint that never goes away is noise: a non-web folder in the projects folders is skipped on every sync. `hl sync` itself lists skipped folders with the reason and the command to add one anyway. Costs one `docker compose config` per new folder on `hl status`. |
| Releases are built locally with GoReleaser; no GitHub Actions | The CI and release workflows were added and then removed on purpose ("Releases are cut locally with goreleaser"). Checks run locally. |
| Homebrew tap: prepared but off | Waits for a `homebrew-tap` repo; steps are in the comment at the end of `.goreleaser.yaml`. |
| `install.sh` verifies the archive against `checksums.txt` and uses sudo only when the target folder needs it | Safe `curl | sh` install. |
| MIT licence | |

## Not goals (as built today)

These are absent, not necessarily rejected; see [status.md](status.md) for open questions about them.

- TLS/HTTPS (the proxy has only an `:80` entrypoint) - see below for putting hl behind your own TLS proxy.
- Authentication in front of apps; the Traefik dashboard (removed rather than exposed without a password).
- Windows (use WSL 2).

## HTTPS: your own TLS proxy in front

hl serves plain HTTP. To reach the apps over HTTPS, run a TLS-terminating proxy you already
trust (Caddy, nginx, a NAS or router feature, a tunnel) in front of hl:

1. Keep hl on loopback and move it off port 80 if the TLS proxy needs it:
   `hl set proxy bind=127.0.0.1 port=8080`.
2. Point the TLS proxy at `http://127.0.0.1:8080`, passing the original `Host` header (and
   `X-Forwarded-Proto: https`) so host routing and the apps' own links keep working.
3. Give each app its public URL through `env` where it builds absolute links (for example
   `MY_APP_URL: https://apps.example.com${HL_PREFIX}`), since `${HL_URL}` is the plain-HTTP one.

The details and examples are in [guides/behind-https.md](guides/behind-https.md).
- Running apps on more than one machine.
- Managing app secrets: they stay in each app's own `.env`, which hl reads but never writes.
- Backups beyond keeping data in plain folders.
