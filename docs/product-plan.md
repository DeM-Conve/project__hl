# Product plan: hl v1

Goal: a stable, easy tool that anyone can install and use for their own homelab - not many
features, but every feature works properly and the common path takes a few commands.
Written 2026-10-23. Status: `[ ]` not started · `[~]` in progress · `[x]` done · `[!]` needs the
maintainer.

Priorities: **P0** = needed before v0.1.0 · **P1** = v0.x, before calling it 1.0 · **P2** = later,
only if users ask.

The user journey this plan is built around:

```sh
hl init                      # asks a few questions, finds my apps, writes hl.yml
hl up                        # everything starts, behind one proxy
hl status                    # what runs where
# ...later: a new project folder appears, or one is deleted
hl sync                      # hl.yml follows my projects folder
```

## 0. Works for anyone, not one person's machine (P0, applies to every item below)

hl will have many users, each with their own folders, apps, OS, user name and timezone. Nothing
in hl may assume the maintainer's setup.

- [x] **No hard-coded personal values** anywhere - code, defaults, generated files, tests,
  examples, docs: no user names, home paths (`/home/<name>/...`), project-folder names or
  layouts, app names from one homelab, timezones, domains, emails. Examples use neutral names
  (`my-app`, `blog`, `~/code`, `~/homelab`, `Europe/Berlin`/`UTC`).
- [x] **No hard-coding at all, not only personal values.** Every value hl uses is either the
  user's setting or a named default defined in exactly one place (one `defaults` file in Go,
  listed in the README's hl.yml reference) - never a literal scattered in code or generated
  files. That covers: proxy port, bind address, routing mode, domain, Traefik image and version,
  entrypoint names, Docker network name and subnet, compose project prefix (`hl-`), generated
  folder and file names (`.hl/`, `data/`, `override.yml`, `traefik.yml`), label prefixes,
  timeouts and retry counts, healthcheck intervals, log tail length, backup folder and name
  pattern, ignore patterns, editor fallback, update-check URL and interval. Each default can be
  overridden in hl.yml (or a flag/env var where it makes sense), and `hl config defaults` prints
  them all. Tests assert that changing a setting changes every place it's used.
- [x] **Every path comes from the user or the OS**: the homelab folder from `hl init`/`--home`/
  `$HL_HOME`/nearest `hl.yml`; the projects folders from `projects:`; hl's own state from the OS
  config dir (`os.UserConfigDir`); `~` expanded for the current user only. Paths in hl.yml may be
  relative to the homelab, so a homelab folder can be moved or shared.
- [x] **Sensible, neutral defaults** that work on a fresh machine: no `TZ` unless the user sets
  it (the container's default), port 80 with an offer of another if busy, `localhost`, no fixed
  subnet unless asked.
- [x] **Fresh-user test**: an automated check that runs init -> add -> up (fake docker) as a
  different user name, in a home folder with spaces in its path, with no prior hl state, and on a
  second homelab next to the first - nothing leaks between them.
- [x] **Several homelabs per user** work side by side (different folders, project names, proxy
  ports/networks), and two users on one machine don't collide (proxy port/network names are
  per-homelab or clearly reported when they clash).
- [x] Audit found (2026-10-23): tests and code comments use one homelab's app name
  (`tierlist.fm`) and timezone (`Asia/Kolkata`) as examples - replace with neutral ones
  (`my-app.io`, `UTC`). No personal paths or names in code.

## 1. First run: `hl init` (P0)

- [x] **Guided init**: on a terminal, `hl init` asks (with defaults on Enter): where the homelab
  folder goes, where your projects live (e.g. `~/code`), `path` or `host` routing, listen on this
  machine only or the network. Then it scans the projects folder (step 2) and lets you pick which
  apps to add from a checklist. Writes a commented `hl.yml` and prints the next command.
- [x] **Non-interactive init** for scripts: every question has a flag (`--projects ~/code
  --routing path --bind 127.0.0.1 --yes`); no prompt when stdin isn't a terminal.
- [x] **Re-running init is safe**: on an existing homelab it never overwrites `hl.yml`; it offers
  to add newly found apps instead.
- [x] **Pre-flight**: init runs the `hl doctor` checks first (Docker running, compose v2.24+,
  port 80 free or offer another) and says exactly what to fix. (Naming the process holding the
  port is left to section 5.)

## 2. Syncing with your projects folder (P0)

- [x] **`projects:` in hl.yml**: one or more folders hl watches for apps (`projects: [~/code]`).
  An app is a direct subfolder with a compose file (`scan_depth: 2` also looks one level deeper).
- [x] **`hl sync`**: compares `projects` with `hl.yml` and shows a plan before changing anything:
  new folders with a compose file -> add (service and port detected like `hl add`); apps whose
  folder is gone -> remove from hl.yml (data kept, never deleted); apps whose compose changed
  (service/port no longer there) -> re-detect. `--yes` applies without asking; `--dry-run` only
  shows.
- [x] **Ignore list**: `ignore:` patterns in hl.yml, and a folder can opt out with a `.hlignore`
  file - so libraries/experiments aren't picked up.
- [x] **`hl status` hints** when sync would change something ("2 new apps in ~/code - run hl
  sync").
- [ ] P2: `hl sync --watch` (re-sync on folder changes).

## 3. Easy config (P0)

- [x] **`hl add` asks when it has to guess**: several services publishing ports, or several ports
  -> pick from a list (flags still win; non-terminal -> clear error naming the flag to pass).
- [x] **`hl set <app> key=value` / `hl unset`** (routing, prefix, host, port, service, env.X) -
  edits hl.yml keeping its comments, validates first.
- [x] **`hl config check`** (or `hl doctor`): validates hl.yml with line numbers and a fix for
  each problem; unknown keys are errors with a "did you mean".
- [x] **`hl edit`**: opens hl.yml in `$EDITOR`, validates on save, re-opens on error.
- [x] **Versioned config**: `version: 1` at the top of hl.yml; older files keep working and are
  upgraded with a message, never silently.
- [x] **JSON Schema** for hl.yml (published in the repo; `# yaml-language-server: $schema=...`
  line written by init) so editors autocomplete and flag mistakes.

## 4. The proxy: one Traefik file (P0)

- [x] **One readable Traefik config file**: `.hl/proxy/traefik.yml` (static config: entrypoints,
  docker provider, dashboard, logs) generated from hl.yml, instead of command-line flags hidden in
  the proxy's compose file. `hl config proxy` prints it.
- [x] **Bring your own Traefik file**: `proxy.config: traefik.yml` (relative to the homelab)
  replaces the generated static config entirely - hl only checks it has the entrypoint and docker
  provider hl needs and says what's missing. One file, your choice, default generated.
- [x] **Extra routes/middlewares**: `proxy.dynamic: dynamic.yml` (Traefik file provider) for
  things that aren't hl apps (another machine, a NAS, redirects).
- [x] **Dashboard off by default**; `proxy.dashboard: true` turns it on at `traefik.<domain>`,
  refused on a non-loopback bind unless `proxy.dashboard_public: true`.
- [x] **Pinned Traefik version** with a tested default; `hl doctor` warns when the configured
  image is older than the tested one.
- [x] Access log on/off (`proxy.access_log`), proxy healthcheck, `hl proxy status`.
- [ ] P2: HTTPS (local CA / ACME). v0.1 documents putting hl behind your own TLS proxy.

## 5. Running apps reliably (P0)

- [x] **Clear errors everywhere** (what failed, why, what to do), consistent exit codes.
- [x] **Port 80 in use** detected before starting the proxy, naming the process if possible.
- [x] **Subnet/network clash** detection with a fix.
- [x] **`hl up --wait`**: waits for healthchecks and reports which app is unhealthy and its last
  log lines.
- [x] **`hl status` shows health** (healthy/unhealthy/starting), not only up/down counts.
- [x] `hl restart` starts the proxy first like `up`.
- [x] `hl logs` with no app: every app's logs, prefixed by app name.
- [x] Ctrl-C during long docker commands stops cleanly.
- [x] `hl update` never pulls over uncommitted work; says which apps it skipped and why.

## 6. Data (P1)

- [ ] **`hl backup [app]`**: stops the app, archives `data/<app>` to `backups/<app>-<time>.tar.gz`,
  starts it again; `--all`. Apps with their own backup service (database dumps) are left to it -
  documented.
- [ ] **`hl restore <app> <file>`**: confirms, stops, restores, starts; keeps a safety copy.
- [ ] `hl data <app>` prints/opens the data folder; `hl status --size` shows data sizes.

## 7. Scripting and output (P1)

- [ ] `--json` for `status`, `ls`, `ps`, `config`, `doctor` (stable, documented fields).
- [ ] `--quiet` / `--no-color` / `NO_COLOR`; output that works in pipes.
- [x] Shell completion includes app names (and an app's services where a command takes one).
- [ ] Man page generated from cobra.

## 8. Quality (P0)

- [x] Automated tests for every command with a fake `docker` (`internal/cli/fake_test.go`).
- [ ] Optional end-to-end script (`make e2e` / `scripts/e2e.sh`) that runs a real tiny homelab
  with Docker: init -> add two sample apps -> up -> curl both URLs -> sync -> down.
- [x] Sample apps in `examples/` (a static site, an app needing a base path) used by docs and e2e.
- [x] gofmt, vet, staticcheck and golangci-lint clean. No CI, by the maintainer's choice - all checks
  run locally before every release (`scripts/check.sh`).
- [x] Linux and macOS, amd64 and arm64 builds; Windows gets a friendly "not supported". (Builds
  done in `.goreleaser.yaml`; the Windows message is in code.)

## 9. Release and distribution (P0)

- [x] GoReleaser config checked (`goreleaser check` + snapshot, SBOMs), install.sh with checksum
  verification, `--version`/`--dir`, shellcheck clean.
- [x] CHANGELOG (Keep a Changelog), RELEASING.md steps, Conventional Commits in CONTRIBUTING.
- [!] Tag and publish v0.1.0 - maintainer's go.
- [!] Homebrew tap repo `DeM-Conve/homebrew-tap` - maintainer creates it, then the commented block
  in `.goreleaser.yaml` is enabled.
- [ ] `hl version` tells you when a newer release exists (opt-out, no telemetry, one GitHub API
  call at most once a day).

## 10. Docs and community (P0)

- [x] README as a product page: pitch, comparison with similar tools, 60-second quickstart, full
  command and hl.yml reference, matching the merged code (no `<!-- slot ... -->` markers left;
  `hl backup`/`hl restore` and Homebrew are marked as planned).
- [x] Guides (`docs/guides/`): "add an app that needs a base path", "move an existing compose app into hl",
  "reach it from your phone (bind 0.0.0.0)", "back up the homelab", "put hl behind HTTPS".
- [x] Issue/PR templates, code of conduct (Contributor Covenant 2.1), security policy.

## Not doing (on purpose)

- A web UI (Dockge/Portainer already do that well).
- Multi-server / remote deploy (Komodo, Coolify).
- Building or editing apps' compose files; an app store.
- Kubernetes, Swarm, Windows.
