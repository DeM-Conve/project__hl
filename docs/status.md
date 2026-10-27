# Status

Where hl stands and what is left. Read this before starting new work. Update it when something
here changes (a release, a pending item done, a question answered). Last reviewed 2026-10-23. The v1 task list is
[product-plan.md](product-plan.md).

## Current state

- Every command in the README is implemented. hl runs the maintainer's homelab (five apps, path
  routing, fixed proxy subnet, shared `TZ`), which is how it is tested end to end.
- **CLI first** (2026-10-27): the goal is that a user never has to open `hl.yml`. `hl set`/`hl unset`
  cover every app key, every `proxy.*` key (`hl set proxy port=8080`) and the shared env
  (`hl set env.TZ=UTC`), the homelab's own settings (`hl set projects+=~/src`, `scan_depth`,
  `name` - refused while anything runs); `hl show [app|proxy]` reads it back; `hl edit` was cut
  (round 3 of the review): a file broken by hand gets its problem lines and a hint to fix them
  in any editor. Error hints name
  the `hl set`/`hl unset` command instead of the `hl.yml` key. Left on purpose: the "edit
  hl.yml, not this file" header of generated files (changing it would change the static
  config's hash and recreate every running proxy once). `hl add`'s base-path note, `hl sync`
  with no `projects` and init's notes name `hl set` too.
- **Minimal surface** (2026-10-27): every command, flag and `hl.yml` key has to earn its
  place. Done so far: the proxy is a pseudo-app (`hl up|down|restart|rebuild|logs|show proxy`)
  instead of the `hl proxy` group; `hl config`, `hl ps`, `hl path` and `--wait-all` are gone
  (`hl show <app> --compose`, `hl show`, `hl status`); `hl update` is `hl rebuild --pull`;
  `hl projects`/`hl ignore`/`hl strip-prefix` are `hl set key+=`/`-=`, and `hl set` also takes
  `scan_depth` and `name`; `hl add` takes `key=value` settings like `hl set` instead of flags;
  `hl remove` ignores an app's folder so `hl sync` doesn't re-add it; the proxy keys are down to
  `bind port routing domain subnet image debug` (`network` and `project` follow `name`), the app keys to `path service port routing prefix host compose env`; `hl logs` of a stopped app says so; no command aliases
  (`start`, `stop`, `ls`) or `--version`/`version --short`; `hl init` takes only `--projects`,
  `--strip-prefix` and `--yes` (the proxy is `hl set proxy ...` afterwards); no `--no-env` on
  init or sync (`hl unset <app> env`); no `hl sync --dry-run` (answering n, or no terminal, only shows the plan).
- **Correctness fixes from the 2026-10-27 review**: `hl up` rebuilds images whose build args
  changed (`.hl/<app>/builds.json`); `hl up proxy` refuses a busy new port instead of taking
  the running proxy down; `compose+=` keeps the compose file hl found; `hl set <app> name=<new>`
  renames an app with its data; `hl set proxy port=` says to `hl up` the apps too; `hl add`
  warns about a non-web service, and init/sync's base-path note is one line like add's;
  `hl set`/`hl unset` mistakes exit 2 and name the right scope, bad paths and services are
  refused; `hl rebuild --pull` rebuilds a folder it can't pull;
  `hl logs` hints and service checks; `~/` paths and unpadded rows in `status`/`show`; init's
  plan says when the default homelab changes.
- **Config, sync and init fixes from review round 3** (2026-10-27): hl.yml keeps its blank
  lines on every edit; `hl set <app> path=` is relative to the current folder; `hl init` on an
  existing homelab writes only what its plan lists (no auto-detected `projects`, no `.gitignore`)
  and says `existing homelab <dir>`; a new homelab skips other homelabs' proxy ports even when
  stopped; `hl sync` follows a moved/renamed app folder (`~ moved`, rule in
  [requirements.md](requirements.md)) and has no `--dry-run`; `hl set` value errors name the key
  with the fix as hint and exit 2; `env.FOO` without a value, `scan_depth=0` and `hl add` of a
  file or a folder without compose file give specific hints.
- **Fixes from review round 4** (2026-10-27): a bare `hl up` applies the proxy's changed
  settings (then starts every app), so apply hints say `hl up` wherever that is enough; `hl doctor`
  warns when the running proxy is behind hl.yml; an unknown app name exits 2 for every command;
  `hl rebuild --pull` prints one `==> <app>` header per app.
- **No release yet.** There are no tags and no GitHub releases; everything in CHANGELOG.md is under
  `Unreleased`, ready to become 0.1.0. Until the first tag is pushed and published, `install.sh`,
  the README's release badge and the "download from Releases" route don't work; `go install
  github.com/DeM-Conve/project__hl/cmd/hl@latest` does (it reports a pseudo-version such as
  `v0.0.0-20261016...`).
- **Release tooling is ready**: `goreleaser check` and a snapshot build pass (linux/darwin x
  amd64/arm64, `checksums.txt`, an SPDX SBOM per archive via syft); `install.sh` was tested
  against the snapshot (checksum ok and mismatch, `--version`, `--dir`, `~` expansion) and is
  shellcheck clean. Steps: [RELEASING.md](../RELEASING.md).
- Docs: README is the user guide (CLI-first) and docs/reference.md the settings reference; guides in `docs/guides/`; example apps in
  `examples/`; CODE_OF_CONDUCT, SECURITY, issue and PR templates in place. Since 2026-10-27 the
  guides, examples, CONTRIBUTING and SECURITY give `hl set`/`hl unset` commands, never hl.yml
  snippets to paste (the only YAML left is an app's own compose file and the reference's "The
  hl.yml file" overview).
- No CI, on purpose (see [requirements.md](requirements.md)); `scripts/check.sh` runs gofmt,
  go vet, staticcheck, golangci-lint, `go test -race`, shellcheck (when installed) and the cross
  builds, all locally.
- Tests cover every package. `internal/cli` runs every command against a fake `docker` and `git`
  (the test binary itself, linked under those names on an otherwise empty PATH) in a fresh home
  folder with spaces in its path, including two homelabs side by side, error paths and Ctrl-C
  (about 82% of its statements; 83% overall). A real Docker is still only exercised by hand.

## Pending

Before tagging v0.1.0:

- Feature branches for 0.1.0 are merged; README, CONTRIBUTING and the CHANGELOG's `Unreleased`
  match the code (no `<!-- slot ... -->` markers left). Re-check them if anything else merges.
- Cut the release: [RELEASING.md](../RELEASING.md) (maintainer only; needs goreleaser v2 and
  syft installed).

After v0.1.0:

- Homebrew tap: create `DeM-Conve/homebrew-tap` and a token, then turn on the commented
  `homebrew_casks` block in `.goreleaser.yaml` (RELEASING.md, "Homebrew tap").
- Two homelabs on one Docker: handled (2026-10-23) - a clash is refused with the fix, and a
  top-level `name:` gives a homelab its own project and network names. Two users on one machine
  share one Docker daemon, so the same applies to them.

## Open questions

Nothing below has been decided; ask the maintainer rather than picking an answer.

1. **Version of the first release**: `v0.1.0` (as the README, CHANGELOG, RELEASING.md and
   `install.sh` examples use) or something else?
2. ~~Commit message style~~ - decided: Conventional Commits (CONTRIBUTING.md), matching the
   maintainer's other repos.
3. ~~HTTPS~~ - decided: out of scope for v0.1; documented as putting hl behind your own TLS proxy
   ([guide](guides/behind-https.md)). Built-in HTTPS is P2 in the product plan.
4. ~~Dashboard exposure~~ - decided 2026-10-23, then superseded 2026-10-27: the dashboard is
   gone altogether, with `proxy.config`, `proxy.dynamic` and the log keys (`proxy.debug`
   replaces those) ([requirements.md](requirements.md)).
5. ~~`hl restart` and the proxy~~ - decided 2026-10-23 (same): `restart` starts the proxy first,
   like `up` and `rebuild`.
6. ~~Windows~~ - decided 2026-10-23 (same): not supported; hl says so and suggests WSL 2.
7. ~~CLI tests~~ - done 2026-10-23: `internal/cli` is tested with a fake `docker` on PATH.
8. **Opting out of the host's `TZ`** (2026-10-27): apps now get the host's time zone unless
   something sets `TZ`. To keep an image's own default instead, a user can only set an explicit
   zone (`hl set env.TZ=UTC`). Is a way to say "no `TZ` at all" wanted (e.g. an empty
   `env.TZ`, which `hl set` refuses today), or is an explicit zone enough?
9. ~~`hl init` without a terminal and without `--yes`~~ - decided 2026-10-27: init requires
   `--yes` without a terminal, like `hl sync` (`echo n | hl init ...` used to write hl.yml).
10. **Auto-mapped env vs. the app's own `.env`**: a mapped value (say `DATA_LOCATION:
   ${HL_DATA}`) is in `apps.<app>.env`, which wins over the app's `.env`. Should the mapping
   leave out variables the app's `.env` already sets?
11. **Env for apps already in `hl.yml`**: add, init and sync map env only for apps they add;
    `hl sync` doesn't offer the mapping for apps added earlier. Should it (as an `~ update`)?
12. ~~`scan_depth` from the command line~~ - done 2026-10-27: `hl set scan_depth=2`; the lists
    are `hl set projects+=...` and the like.
13. **Picking apps in `hl init`**: `hl init` used to offer the apps found as a checklist, then
    took `--apps a,b`; since 2026-10-27 it adds every web app it finds and `hl remove <app>`
    (which also ignores the folder for `hl sync`) drops one afterwards. Is that enough, or is a
    way to pick before writing wanted back?
14. ~~`hl rebuild --pull` and a folder with uncommitted changes~~ - decided 2026-10-27 (review
    fixes): such an app, and one whose folder isn't a git repo, is rebuilt from its local code with
    only the pull skipped ([requirements.md](requirements.md)).
15. **Shared prefix and clash names on an existing homelab** (2026-10-27): `hl init` strips a
    shared folder-name prefix and picks a `name`/free port only for a new homelab. Re-running it
    on an existing `hl.yml` without `strip_prefix` (or one clashing with another homelab) changes
    nothing and leaves that to `hl set`/`hl doctor`. Should it offer them there too (renaming
    apps or projects that may already run)?
16. ~~Keep `schema/hl.schema.json`?~~ - decided 2026-10-27 (review round 3): deleted with its
    tests, together with `hl edit`; it only served editing hl.yml by hand
    ([requirements.md](requirements.md)).
17. **`--wait` and an app whose root is a plain 404** (2026-10-27): `--wait` tells "route not
    loaded yet" from Traefik's own body `404 page not found`. An app that answers its root URL
    with exactly that body (Go's default `http.NotFound`) waits until `--wait-timeout` and
    fails. Rare for a web app's root; if it bites, the alternative is a response header the
    override adds to each app's route (which recreates every app's containers once).
