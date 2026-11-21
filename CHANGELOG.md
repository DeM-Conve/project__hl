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

- `install.sh --source` builds hl from source instead of downloading a release: with Go if it
  is installed, otherwise inside a `golang` Docker container, so a machine with only Docker can
  install hl. The script does this on its own while the repository has no release.

- `hl sync` follows a moved or renamed app folder: when an app's folder is gone and exactly one
  new folder routes the same service and port (and has the app's data volumes; folders its
  `${HL_DATA}` env puts there, such as `BACKUP_LOCATION`'s `backups`, and empty ones don't
  count), the plan says
  `~ moved <app>: <old> -> <new>` and only the app's `path` changes, so it keeps its name,
  settings and data instead of being removed and added again.

- `hl set <app> name=<new>` renames an app: its settings (and hl.yml's comments) stay, its
  `data/<app>/` and `.hl/<app>/` move along. Refused while the app has containers (`hl down
  <app>` first), when `data/<new>/` exists, or for a name `hl add` would refuse.

- `hl logs <app>` (or `hl logs proxy`) on something that isn't running says so, instead of
  printing nothing.

- `hl remove <app>` of an app in a projects folder adds the folder to `ignore` (and says so), so
  `hl sync` doesn't add it straight back; `hl add` of that folder later takes it out again.

- `hl init` creates a homelab folder (a short `hl.yml`, `data/`, `.gitignore`) and makes it the
  default; without a folder it uses the homelab hl already uses, else `~/homelab`. It works out
  a plan - projects folders (`~/code`, `~/projects`, `~/src` when they exist), the proxy (a free
  port when 80 is busy, a `name` when another homelab runs one), every web app found with its
  service, port and env, and the folders it skips and why - shows it and asks once;
  `--projects` and `--strip-prefix`
  change the plan and `--yes` applies it without asking (required without a terminal, as for
  `hl sync`). It checks Docker and compose first, and on an existing homelab never overwrites
  `hl.yml` but adds the new apps. The `hl.yml` it writes
  holds only the projects folders, the proxy settings that differ from the defaults, and the
  apps; every other setting is documented in the README.
- `projects:` in `hl.yml`: folders whose subfolders with a compose file are apps; `ignore:`
  patterns or a `.hlignore` file control what is picked up. Each projects folder is looked
  into one level deep.
- `hl sync` keeps `hl.yml` in step with the projects folders: adds new apps, removes ones whose
  folder is gone (data kept), re-detects a pinned service or port that disappeared. It shows the
  plan first and asks (answer n to only see it; without a terminal it needs `--yes`). `hl status` says when
  `hl sync` has something to do.
- `hl add` adds any folder with a compose file as an app, finding the service that publishes a
  port; when several services or ports could be the one, it asks on a terminal and otherwise
  names the flag to pass. `hl remove` takes an app out again and keeps its data.
- One Traefik proxy in front of every app, with `path` (`http://localhost/<app>/`), `host`
  (`http://<app>.localhost`) or `none` routing, per homelab or per app.
- Every named volume kept as a plain folder under `data/<app>/<volume>`.
- Apps run unmodified: hl layers a generated compose override on top (`hl show <app> --compose`).
- Shared `env` for every app, per-app `env`, and `${HL_URL}`, `${HL_PREFIX}`,
  `${HL_HOST}`, `${HL_DATA}`, `${HL_APP}` for an app's settings.
- Commands: `status`, `up`, `down`, `restart`, `rebuild`, `logs`,
  `open`, `shell`, `show`, `doctor`,
  `version` and shell `completion` (which offers app and service names).
- `hl status` has a HEALTH column: `healthy`, `starting: <services>`, `unhealthy: <services>`,
  or `-` for apps without healthchecks.
- `--wait` on `hl up` and `hl rebuild`: wait (up to 2 minutes per app) until
  each app's service behind the proxy and the services it depends on (`depends_on`) are running
  and healthy, and for an app that isn't, name those services and show their last log lines. A
  sidecar nothing depends on (say, a backup job with a long healthcheck interval) no longer fails
  `--wait`; for an app with routing `none` it waits for every service.
- Apps get this machine's time zone as `TZ` (from `$TZ`, `/etc/localtime` or `/etc/timezone`)
  unless the shared env, the app's env or its `.env` set one; `hl show` shows it
  and where it came from. When the zone can't be told, no `TZ` is set.
- `hl logs` with no app shows every app's logs at once, each line prefixed by its app; `-n`/`--tail`
  sets how many lines (default 200).
- `hl rebuild --pull` runs `git pull --ff-only` in each app's folder before rebuilding it; a folder
  that isn't a git repo or has uncommitted changes is rebuilt from its own code without a pull, and the summary says which and why;
  `hl rebuild proxy --pull` pulls the proxy's image.
- `hl shell` opens the service behind the proxy by default, also when hl detected it rather than
  `hl.yml` pinning it, and exits with the shell's own status.
- `hl version` shows the Go version, platform and whether the build had local changes.
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
  from `hl.yml`; `hl show proxy` names it.
- `proxy.debug` (Traefik's INFO log and access log, in `hl logs proxy`); a
  healthcheck for the proxy.
- The proxy works like an app: `hl up proxy`, `hl down proxy`, `hl restart proxy`,
  `hl rebuild proxy` (recreates it), `hl logs proxy [-f]`, and `hl show proxy` for its state,
  health, listen address, every setting, and warnings (a running proxy behind `hl.yml`, an old
  image).
- The Traefik image is pinned to the tested version (`traefik:v3.6.25`); `hl doctor` warns when
  `proxy.image` is older.
- Reserved app name: `proxy` (its compose project would be the proxy's, and `hl up proxy` and
  the like mean the proxy). `hl add`, `hl.yml`, `hl init` and `hl sync` refuse it.
- `hl.yml` is versioned (`version: 1`). Older files without it keep working and get the line,
  with a message, the first time hl edits them; a newer file asks you to update hl.
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
  `BACKUP_LOCATION` `${HL_DATA}/backups`. They print what they mapped;
  `hl add <folder> env.NAME=` leaves one variable out, `hl unset <app> env` drops them all.
- `hl add <folder> key=value...` takes the settings `hl set <app>` takes, plus `name`:
  `hl add ~/code/wiki routing=host service=web port=8080 env.TZ=UTC`. `env.NAME=value` wins over
  the mapped values.
- `strip_prefix:` in `hl.yml` (and `hl init --strip-prefix`): folder-name prefixes left out of
  app names, so folder `project__blog` becomes app `blog` in `hl init`, `hl sync` and `hl add`.
- `hl set` changes the homelab's own settings too: `projects`, `ignore`,
  `strip_prefix` and `name`. A list is replaced with `=`, and `+=` / `-=` add and remove entries
  (`hl set projects+=~/src ignore-=old`, also for an app's `compose`);
  `hl unset ignore` drops one. `name` is refused while any of the homelab's apps or its proxy
  runs. Every setting is reachable from the command line.

### Changed

- **Breaking:** `--wait-timeout` is gone; `--wait` waits a fixed 2 minutes per app. Drop the
  flag from scripts (`hl up --wait`); a slow app's health shows in `hl status` once it is up.

- An app name that isn't in `hl.yml` exits 2 (usage) for every command that takes one (`up`,
  `down`, `restart`, `rebuild`, `logs`, `shell`, `open`, `show`, `remove`, `set`, `unset`), with
  "did you mean" or the list of apps and the same hint; most of them exited 1, and `hl set` /
  `hl unset` of a mistyped app gave a different message.

- `hl doctor` warns when the running proxy was started from older settings than hl.yml's (the
  check `hl show proxy` makes), with `hl up` to apply them; both used to say `hl up proxy`.

- A bare `hl up` applies the proxy's changed settings too: `hl set proxy debug=true` (or
  `port=`, `image=`, ...) then `hl up` used to leave the running proxy as it was, since `hl up`
  only started a proxy that wasn't running. It now runs the proxy's own `up` first (compose
  recreates it only when its files changed; a busy new port is still refused before that), then
  every app. `hl up <app>` still leaves a running proxy alone. `hl set` names `hl up` where that
  is enough (`domain`, `routing`, `port` and the shared env); `hl status` says "apply it: hl up".

- **Breaking:** `hl sync --dry-run` is gone: answering n, or running without a terminal, already
  shows the plan and writes nothing.
- **Breaking:** `hl edit` is gone. Every key can be changed with `hl set`/`hl unset`, so a
  broken `hl.yml` can only come from editing it by hand; every command then lists the problem
  lines (`hl.yml:3:5: ...`) with a hint to fix them in any editor, and `hl doctor` checks the
  file again. `.hl/edit/` (its draft copy) is no longer used and can be deleted.
- **Breaking:** the command aliases `start` (`hl up`), `stop` (`hl down`) and `ls` (`hl status`),
  the `-v`/`--version` flag and `hl version --short` are gone; `hl version` prints the version.
- **Breaking:** `hl init` no longer takes `--bind`, `--port`, `--routing` or `--apps`. It sets up
  the proxy itself (path routing on `127.0.0.1`, port 80 or a free one); change it afterwards with
  `hl set proxy bind=0.0.0.0 port=8080 routing=host`. To leave out an app init added,
  `hl remove <app>` (its folder is then ignored by `hl sync`).
- **Breaking:** the proxy keys `network` and `project` are gone: the proxy's compose project and
  network are `hl-proxy`, or `hl-<name>-proxy` for a homelab with a `name` (`hl set name=<x>`
  gives a second homelab its own). An `hl.yml` that still has one is refused with a message
  naming `hl unset proxy network` / `hl unset proxy project`; `hl show proxy` prints both names.
- The guides and examples give `hl set`/`hl unset` commands instead of `hl.yml` snippets, and
  say where `hl add` already maps a base path by itself.
- **Breaking:** the JSON Schema `schema/hl.schema.json` is gone, and a new `hl.yml` no longer
  starts with a `# yaml-language-server: $schema=...` line. It only served editing the file by
  hand; hl checks `hl.yml` itself on every command. Remove that line from an existing `hl.yml`
  if you added it (it is a comment, so it does no harm, but the URL no longer resolves).
- **Breaking:** the homelab key `scan_depth` is gone: `hl init` and `hl sync` look one level
  into each projects folder; add a nested group folder to `projects` instead
  (`hl set projects+=~/code/group`). An `hl.yml` that still has it is refused with a message
  naming `hl unset scan_depth`.
- **Breaking:** the app key `prefix` is gone: path routing serves an app at `/<app>`, and
  `hl set <app> name=<new>` changes that (or `hl set <app> routing=host`). An `hl.yml` that still
  has it is refused with a message naming `hl unset <app> prefix`.
- **Breaking:** the app key `env_files` is gone. Keep an app's secrets in its own `.env` (hl
  reads it, as compose does) and anything else in its env (`hl set <app> env.NAME=value`). An
  `hl.yml` that still has it is refused with a message naming `hl unset <app> env_files`.
- **Breaking:** `hl init --no-env` and `hl sync --no-env` are gone, as on `hl add`: undo a
  mapped variable with `hl unset <app> env.NAME`, or all of them with `hl unset <app> env`.
- **Breaking:** the proxy keys `config`, `dynamic`, `dashboard`, `dashboard_public`, `access_log`
  and `log_level` are gone; `proxy.debug: true` (`hl set proxy debug=true`) turns on Traefik's
  INFO log and access log. An `hl.yml` that still has one is refused with a message naming the
  `hl unset proxy <key>` that removes it. The generated `traefik.yml` changed, so the next
  `hl up proxy` recreates the proxy container once.
- **Breaking:** `hl add` takes `key=value` settings instead of flags: `--name x` is `name=x`,
  `--routing`, `--prefix`, `--host`, `--service`, `--port` and `--compose` are `routing=`,
  `prefix=`, `host=`, `service=`, `port=` and `compose=`, `--env K=V` is `env.K=V`, and
  `--no-env` is gone (`env.K=` leaves one mapped variable out; `hl unset <app> env` drops them all).
- **Breaking:** `hl projects`, `hl ignore` and `hl strip-prefix` are gone: `hl set projects+=<folder>`,
  `hl set ignore-=<pattern>`, `hl set strip_prefix=<prefix>` and `hl unset <key>` do what their
  `add`/`remove` did, and `hl show` lists them.
- **Breaking:** `hl update` is `hl rebuild --pull`; a folder that isn't a git repo or has
  uncommitted changes is no longer skipped altogether but rebuilt without a pull.
- **Breaking:** `hl config`, `hl ps`, `hl path` and `--wait-all` are gone. `hl config <app>` is
  `hl show <app> --compose`; `hl config check` is what every command (and `hl doctor`) does
  already; what `hl config defaults` showed that matters is in `hl show` (the `TZ` apps get, the
  projects folders, `ignore`) and `hl show proxy`, the rest in the README's
  "Built-in values". Use `hl status` or `docker ps` for containers and `hl show` for folders.
  `check` and `defaults` are no longer reserved app names.
- **Breaking:** the `hl proxy` command group is gone; the proxy is a pseudo-app instead.
  `hl proxy up|down|logs` are `hl up|down|logs proxy`, `hl proxy status` is `hl show proxy`, and
  `hl proxy config` is gone (`hl show proxy` names the file).

- `hl.yml` is checked strictly on every command: unknown keys,
  wrong types, bad values and clashes between apps (same Docker project, URL prefix or host) stop
  the command, all listed at once with their lines (`hl doctor` lists them too).
- The Traefik dashboard is gone (it had no password); earlier builds always ran it.
- `hl init` and `hl sync` add only folders that look like web apps (a preferred service name or a
  common HTTP port); others, like a database on its own port, are listed with the `hl add`
  command to use (`routing=none` to run it without the proxy).
- `hl add` without a terminal no longer stops when several services publish ports and one has a
  usual web name (`frontend`, `web`, `app`, `nginx`, `proxy`, `ui`): it routes that one and says
  so. It still asks for `service=` when none has such a name.
- `hl restart` starts the proxy first, like `hl up` and `hl rebuild`.
- Error hints name the command that fixes a setting (`hl set proxy port=8000`, `hl unset proxy subnet`,
  `hl set my-app service=...`) instead of the `hl.yml` key to edit.

### Fixed

- `hl init` on an existing homelab with nothing to change says `nothing to change in hl.yml`
  and exits 0 without a terminal; when it isn't the default homelab, it used to fail with "not
  a terminal, so nothing was written" - now the default stays as it is and hl says how to
  change it (`hl init --yes <folder>`).
- `hl unset` removes a key `hl.yml` has that isn't a setting (a typo like `hl unset proxy prot`,
  or a key of another hl version) at any scope, and says it wasn't a known setting; it used to
  refuse it as unknown although the error for such a file suggests it. On a file with several
  problems it writes the result when it adds none, so they can be fixed one at a time. A key
  that is neither a setting nor in the file is still refused.
- `hl rebuild --pull` printed each app's `==> <app>` header twice (once with its folder before
  the pull, once before the rebuild); it is printed once, with the folder.
- `hl set`, `hl unset` and every other command that changes `hl.yml` (add, remove, sync, init)
  keep its blank lines and never add any; they used to remove all of them, so one setting
  rewrote the whole file's layout, and then added one below the header comment on every edit.
  A new `hl.yml` from `hl init` has one blank line between its sections.
- `hl set <app> path=<folder>` takes a relative folder from the current folder, like `hl add`
  and `hl set projects+=`; it used to write it as given, so hl read it relative to the homelab.
- `hl init` on an existing homelab changes only what its plan lists. It used to add the
  auto-detected `projects:` (`~/code`, ...) to an `hl.yml` without one while the plan said
  "hl.yml kept", and to put back a deleted `.gitignore`. Now the plan names the folders and the
  `hl init --projects` command that saves one, a saved setting is a `+ ... (hl.yml updated)`
  line, and the first line says `existing homelab <folder>`.
- `hl init` of a second homelab no longer plans port 80 just because the first one's proxy is
  stopped: it skips the proxy port of the default homelab and of any homelab Docker has
  containers of, and the plan says whose port it is.
- A wrong value in `hl set`/`hl unset` is reported as the key and what's wrong, with the fix as
  the hint (`error: apps.my-app.routing: "hots" is not path, host or none` / `hint: did you
  mean "host"?`), and exits 2 like other usage mistakes. It used to point at a line of the
  `hl.yml` that was never written, and exit 1. A problem `hl.yml` already has is still shown
  with its file and line.
- `hl set env.FOO` (no `=`) asks for the value (`hl set env.FOO=<value>`) instead of calling env
  an app setting; `hl add <folder>` without a compose file hints `hl add <folder> compose=<file>`
  instead of an `hl set` for an app that isn't added yet; `hl add <folder>/compose.yml` says it
  takes the folder. docs/reference.md no longer calls `hl-proxy` a reserved app name: only
  `proxy` is.
- `hl status` shows the proxy where it really listens: after `hl set proxy port=8088` without
  `hl up proxy`, it used to print hl.yml's address and URLs on a port nothing served. It now
  shows the running address, the app URLs on that port, and a note: "hl.yml says ...:8088 -
  apply it: hl up".
- `hl up --wait` (and `rebuild --wait`) returns only once the app's URL answers through the
  proxy: a request right after it used to get Traefik's "404 page not found" for about a second,
  until the proxy had loaded the new route. Any answer of the app counts, its own 404 too; a
  route that doesn't show up within the timeout fails with a hint. Apps with routing `none`
  aren't asked.
- `hl show` lists the apps in aligned columns (name, URL, folder), and `hl show <app>` no
  longer ends its `project` and `url` rows with an empty source.
- No more silent no-ops: `hl restart <app>` of an app that isn't running says "`<app>` isn't
  running - start it with: `hl up <app>`" (exit 0) instead of printing an empty `==> <app>`
  section; `hl logs` with no app shows only the running apps' logs and says so when none runs;
  `hl shell` on a stopped service prints one error with the `hl up` hint, not docker's error
  line as well.
- `hl up` rebuilds an image whose build args changed: an app that bakes its base path or URL
  into the image (`build: args:` fed from `${HL_PREFIX}` or `${HL_URL}`) kept serving the old
  path after `hl set <app> prefix=...`, `routing=` or a proxy `domain`/`port` change, although hl
  said `hl up` applies it. hl now records each service's build settings after `hl up`
  (`.hl/<app>/builds.json`) and rebuilds the services whose settings changed since.
- `hl up proxy` after `hl set proxy port=` (or `bind=`) to an address something else uses took
  the proxy down: compose removed the running one and the new one couldn't bind. hl now checks
  the new address first and refuses, leaving the running proxy as it is, with a free port in the
  hint. `hl up` and `hl rebuild` print an error before the status table, not after it.
- `hl set <app> compose+=<file>` (and `hl add <folder> compose+=<file>`) on an app that names no
  compose file replaced the file compose picks with the new one; the list now starts from it.
- `hl set` and `hl unset` mistakes: an unknown key or app, or a malformed `key=value`, exits 2
  (usage) instead of 1; a key given without an app that belongs to the proxy or an app names that
  command (`hl set routing=host` -> `hl set proxy routing=host`), and `hl unset nosuchkey` says
  there is no app or homelab setting by that name. `hl set <app> path=` to a folder without the
  compose files, `compose=` to a missing file, and `service=` to a service the compose files
  don't have (when they can be read) are refused, listing the services. Unsetting the last proxy
  setting leaves no empty `proxy: {}` behind.
- `hl status` and `hl show` print folders inside your home folder as `~/...`, and `hl show` no
  longer pads every row to its longest value (a long path pushed every source far right).
- `hl init` making a second homelab the default said so only after writing; the plan says it
  first, naming the homelab that was the default.
- `hl logs proxy` printing nothing (Traefik logs only errors by default) now says how to see more:
  `hl set proxy debug=true`. `hl logs <app> <service>` with a service the app doesn't have lists
  its services instead of compose's error.
- `hl set proxy port=...` said to apply it with `hl up proxy` only, but the port is in every
  app's `HL_URL`; it now says `hl up` (the proxy, then the apps), like `domain` and `routing`.

- `hl add`'s base-path note was three lines, shown even when it didn't apply; it is one `hint:`
  line, shown only with path routing when no base-path variable was mapped. `hl init` and
  `hl sync` print the same one line instead of their three-line note.
- `hl add` of a service that doesn't look like a web app (none of the usual names, not a common
  HTTP port - what makes `hl init` and `hl sync` skip a folder) routed it without a word; it now
  warns and names `hl set <app> routing=none`.

- `hl init` strips a folder-name prefix the projects folders share (`project__blog`,
  `project__wiki` -> apps `blog`, `wiki`): a prefix ending in `-`, `_` or `__` that at least two,
  and at least half, of the folders found have, unless stripping it would give two apps the same
  name. The plan says so; `--strip-prefix none` keeps whole names.
- `hl init` settles clashes with another homelab on the same machine itself, and shows them in
  its plan: when port 80 is busy it takes the first free of 8080, 8000, 8081, 8888, 8090 (it
  gave up when 8080 was busy too), and when another homelab runs the proxy project or network
  `hl-proxy` it gives the new homelab a `name` from its folder, so its projects, network and
  port don't collide (`hl doctor` used to report the clash only after the fact). When hl can't
  read that other homelab's proxy port (its `hl.yml` is gone, or only the `hl-proxy` network is
  left), port 80 counts as taken too and the plan says why.
- `hl init` warns when hl commands would keep using another homelab than the one it just made
  default (`$HL_HOME`, `--home`, or the current folder inside another homelab), and says how to fix it.
- Quieter `hl init` and `hl sync`: an app's env is shown once, in the plan (not again after
  "added"); a skipped folder's line names its path once (a projects folder given as a long
  absolute path is shortened to `<its name>/<app folder>`), and the hints name the commands:
  `hl add <folder> routing=none`, `hl set ignore+=<folder>`, `hl set <app> routing=host`,
  `hl set projects+=<folder>`.
- `hl status` said "1 new app ... run hl sync" forever when the only new folder was one sync
  always skips (not a web app); it now counts only the folders sync would add. Its header for a
  proxy run by another homelab reads `proxy: run by another homelab (<folder>) - see hl doctor`
  instead of mixing that homelab's folder with this one's address.
- `hl init` without a folder no longer sets up the current folder: run in your home folder it
  wrote `hl.yml`, `data/` and `.gitignore` there. It now uses the homelab hl already uses, else
  `~/homelab`; `hl init .` still sets up the current folder.
- Folders inside `data/<app>/` that the app's env points at (like an auto-mapped
  `BACKUP_LOCATION`) are created by hl before the app starts; Docker created them owned by root,
  so an app running as you couldn't write its backups.
- Colours are used only on a real terminal: output sent to `/dev/null` had them.

[Unreleased]: https://github.com/DeM-Conve/project__hl/commits/main
<!-- After v0.1.0 is tagged:
[Unreleased]: https://github.com/DeM-Conve/project__hl/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/DeM-Conve/project__hl/releases/tag/v0.1.0
-->
