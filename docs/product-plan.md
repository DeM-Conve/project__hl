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

- [ ] **No hard-coded personal values** anywhere - code, defaults, generated files, tests,
  examples, docs: no user names, home paths (`/home/<name>/...`), project-folder names or
  layouts, app names from one homelab, timezones, domains, emails. Examples use neutral names
  (`my-app`, `blog`, `~/code`, `~/homelab`, `Europe/Berlin`/`UTC`).
- [ ] **Every path comes from the user or the OS**: the homelab folder from `hl init`/`--home`/
  `$HL_HOME`/nearest `hl.yml`; the projects folders from `projects:`; hl's own state from the OS
  config dir (`os.UserConfigDir`); `~` expanded for the current user only. Paths in hl.yml may be
  relative to the homelab, so a homelab folder can be moved or shared.
- [ ] **Sensible, neutral defaults** that work on a fresh machine: no `TZ` unless the user sets
  it (the container's default), port 80 with an offer of another if busy, `localhost`, no fixed
  subnet unless asked.
- [ ] **Fresh-user test**: an automated check that runs init -> add -> up (fake docker) as a
  different user name, in a home folder with spaces in its path, with no prior hl state, and on a
  second homelab next to the first - nothing leaks between them.
- [ ] **Several homelabs per user** work side by side (different folders, project names, proxy
  ports/networks), and two users on one machine don't collide (proxy port/network names are
  per-homelab or clearly reported when they clash).
- [ ] Audit found (2026-10-23): tests and code comments use one homelab's app name
  (`tierlist.fm`) and timezone (`Asia/Kolkata`) as examples - replace with neutral ones
  (`my-app.io`, `UTC`). No personal paths or names in code.

## 1. First run: `hl init` (P0)

- [ ] **Guided init**: on a terminal, `hl init` asks (with defaults on Enter): where the homelab
  folder goes, where your projects live (e.g. `~/code`), `path` or `host` routing, listen on this
  machine only or the network. Then it scans the projects folder (step 2) and lets you pick which
  apps to add from a checklist. Writes a commented `hl.yml` and prints the next command.
- [ ] **Non-interactive init** for scripts: every question has a flag (`--projects ~/code
  --routing path --bind 127.0.0.1 --yes`); no prompt when stdin isn't a terminal.
- [ ] **Re-running init is safe**: on an existing homelab it never overwrites `hl.yml`; it offers
  to add newly found apps instead.
- [ ] **Pre-flight**: init runs the `hl doctor` checks first (Docker running, compose v2.24+,
  port 80 free or offer another) and says exactly what to fix.

## 2. Syncing with your projects folder (P0)

- [ ] **`projects:` in hl.yml**: one or more folders hl watches for apps (`projects: [~/code]`).
  An app is a direct subfolder with a compose file.
- [ ] **`hl sync`**: compares `projects` with `hl.yml` and shows a plan before changing anything:
  new folders with a compose file -> add (service and port detected like `hl add`); apps whose
  folder is gone -> remove from hl.yml (data kept, never deleted); apps whose compose changed
  (service/port no longer there) -> re-detect. `--yes` applies without asking; `--dry-run` only
  shows.
- [ ] **Ignore list**: `ignore:` patterns in hl.yml, and a folder can opt out with a `.hlignore`
  file - so libraries/experiments aren't picked up.
- [ ] **`hl status` hints** when sync would change something ("2 new apps in ~/code - run hl
  sync").
- [ ] P2: `hl sync --watch` (re-sync on folder changes).

## 3. Easy config (P0)

- [ ] **`hl add` asks when it has to guess**: several services publishing ports, or several ports
  -> pick from a list (flags still win; non-terminal -> clear error naming the flag to pass).
- [ ] **`hl set <app> key=value` / `hl unset`** (routing, prefix, host, port, service, env.X) -
  edits hl.yml keeping its comments, validates first.
- [ ] **`hl config check`** (or `hl doctor`): validates hl.yml with line numbers and a fix for
  each problem; unknown keys are errors with a "did you mean".
- [ ] **`hl edit`**: opens hl.yml in `$EDITOR`, validates on save, re-opens on error.
- [ ] **Versioned config**: `version: 1` at the top of hl.yml; older files keep working and are
  upgraded with a message, never silently.
- [ ] **JSON Schema** for hl.yml (published in the repo; `# yaml-language-server: $schema=...`
  line written by init) so editors autocomplete and flag mistakes.

## 4. The proxy: one Traefik file (P0)

- [ ] **One readable Traefik config file**: `.hl/proxy/traefik.yml` (static config: entrypoints,
  docker provider, dashboard, logs) generated from hl.yml, instead of command-line flags hidden in
  the proxy's compose file. `hl config proxy` prints it.
- [ ] **Bring your own Traefik file**: `proxy.config: traefik.yml` (relative to the homelab)
  replaces the generated static config entirely - hl only checks it has the entrypoint and docker
  provider hl needs and says what's missing. One file, your choice, default generated.
- [ ] **Extra routes/middlewares**: `proxy.dynamic: dynamic.yml` (Traefik file provider) for
  things that aren't hl apps (another machine, a NAS, redirects).
- [ ] **Dashboard off by default**; `proxy.dashboard: true` turns it on at `traefik.<domain>`,
  refused on a non-loopback bind unless `proxy.dashboard_public: true`. (in progress, core agent)
- [ ] **Pinned Traefik version** with a tested default; `hl doctor` warns when the configured
  image is older than the tested one.
- [ ] Access log on/off (`proxy.access_log`), proxy healthcheck, `hl proxy status`.
- [ ] P2: HTTPS (local CA / ACME). v0.1 documents putting hl behind your own TLS proxy.

## 5. Running apps reliably (P0)

- [ ] **Clear errors everywhere** (what failed, why, what to do), consistent exit codes. (in
  progress, core agent)
- [ ] **Port 80 in use** detected before starting the proxy, naming the process if possible.
- [ ] **Subnet/network clash** detection with a fix.
- [ ] **`hl up --wait`**: waits for healthchecks and reports which app is unhealthy and its last
  log lines.
- [ ] **`hl status` shows health** (healthy/unhealthy/starting), not only up/down counts.
- [ ] `hl restart` starts the proxy first like `up`. (in progress, core agent)
- [ ] `hl logs` with no app: every app's logs, prefixed by app name.
- [ ] Ctrl-C during long docker commands stops cleanly. (in progress, core agent)
- [ ] `hl update` never pulls over uncommitted work; says which apps it skipped and why.

## 6. Data (P1)

- [ ] **`hl backup [app]`**: stops the app, archives `data/<app>` to `backups/<app>-<time>.tar.gz`,
  starts it again; `--all`. Apps with their own backup service (database dumps) are left to it -
  documented.
- [ ] **`hl restore <app> <file>`**: confirms, stops, restores, starts; keeps a safety copy.
- [ ] `hl data <app>` prints/opens the data folder; `hl status --size` shows data sizes.

## 7. Scripting and output (P1)

- [ ] `--json` for `status`, `ls`, `ps`, `config`, `doctor` (stable, documented fields).
- [ ] `--quiet` / `--no-color` / `NO_COLOR`; output that works in pipes.
- [ ] Shell completion includes app names. Man page generated from cobra.

## 8. Quality (P0)

- [~] Automated tests for every command with a fake `docker` (core agent).
- [ ] Optional end-to-end script (`make e2e` / `scripts/e2e.sh`) that runs a real tiny homelab
  with Docker: init -> add two sample apps -> up -> curl both URLs -> sync -> down.
- [ ] Sample apps in `examples/` (a static site, an app needing a base path) used by docs and e2e.
- [~] gofmt, vet, staticcheck clean (core agent). No CI, by the maintainer's choice - all checks
  run locally before every release (`scripts/check.sh`).
- [ ] Linux and macOS, amd64 and arm64 builds; Windows gets a friendly "not supported".

## 9. Release and distribution (P0)

- [~] GoReleaser config checked, install.sh with checksum verification (product agent).
- [~] CHANGELOG (Keep a Changelog), RELEASING steps, Conventional Commits (product agent).
- [!] Tag and publish v0.1.0 - maintainer's go.
- [!] Homebrew tap repo `DeM-Conve/homebrew-tap` - maintainer creates it, then the commented block
  in `.goreleaser.yaml` is enabled.
- [ ] `hl version` tells you when a newer release exists (opt-out, no telemetry, one GitHub API
  call at most once a day).

## 10. Docs and community (P0)

- [~] README as a product page: pitch, comparison with similar tools, 60-second quickstart, full
  command and hl.yml reference (product agent).
- [ ] Guides: "add an app that needs a base path", "move an existing compose app into hl",
  "reach it from your phone (bind 0.0.0.0)", "back up the homelab", "put hl behind HTTPS".
- [~] Issue/PR templates, code of conduct, security policy (product agent).

## Not doing (on purpose)

- A web UI (Dockge/Portainer already do that well).
- Multi-server / remote deploy (Komodo, Coolify).
- Building or editing apps' compose files; an app store.
- Kubernetes, Swarm, Windows.
