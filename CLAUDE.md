# hl

`hl` is a generic Go CLI that runs self-hosted apps behind one Traefik proxy. Each app is a folder
with its own `docker compose` file; hl never edits it. Instead it layers a generated override on
top (route through the proxy, no host port, named volumes as folders under the homelab's `data/`).
A **homelab folder** holds `hl.yml` (the config), `data/` and `.hl/` (generated files). Public repo:
github.com/DeM-Conve/project__hl, MIT, module `github.com/DeM-Conve/project__hl`.

## Read next

| File | Read it when |
|---|---|
| [README.md](README.md) | You need the user-facing behaviour: install, commands and flags, `hl.yml` fields, routing, env order. It is the reference for those; keep it in sync with the code. |
| [docs/requirements.md](docs/requirements.md) | Before adding a feature or changing behaviour: hard rules and the decisions behind them. |
| [docs/architecture.md](docs/architecture.md) | Before changing code: what each package does, how a command runs, what the generated files contain, env layering, per-command details. |
| [docs/product-plan.md](docs/product-plan.md) | The v1 product task list (priorities, status) - pick work from here and tick it when done. |
| [docs/status.md](docs/status.md) | Before starting work: current state, pending items, open questions. |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Building, checks, commit style (Conventional Commits). |
| [RELEASING.md](RELEASING.md) | The release steps (GoReleaser, syft, Homebrew tap). |
| [CHANGELOG.md](CHANGELOG.md) | What a user would notice, per version; add to `Unreleased`. |
| [SECURITY.md](SECURITY.md) | Reporting policy and hl's security stance. |

## Rules

- **Generic**: nothing specific to one person's homelab or one app goes into code, defaults,
  tests' expectations or docs. Homelab-specific settings live in that homelab's own `hl.yml`
  (the maintainer's is a separate private repo; don't edit it from here).
- **Never touch the app's folder**; change behaviour through the generated override instead.
- **Don't parse compose files**; read `docker compose config --format json` (`internal/compose`).
- When a command, flag or `hl.yml` field changes, update README.md, CHANGELOG.md (`Unreleased`)
  and, if it changes how things work inside, docs/architecture.md. When a pending item or open
  question is settled, update docs/status.md (and docs/requirements.md for a new decision).
- Unclear requirement: add it to the open questions in docs/status.md, don't invent an answer.

## Code map

`cmd/hl` (entry) -> `internal/cli` (cobra commands; `project.go` builds and prepares compose
projects) -> `internal/gen` (override and proxy compose text) and `internal/compose` (runs
`docker compose`, reads the model) -> `internal/config` (hl.yml) ; `internal/version`.
Details in docs/architecture.md.

## Checks

```sh
gofmt -l .            # must print nothing
go vet ./...
go test ./...         # config, gen, compose, version; cli has no tests
shellcheck install.sh # if install.sh changed
go build -o hl ./cmd/hl
```

There is no CI; run these before every commit. Go 1.21 (`go.mod`). Trying it for real needs Docker
with compose v2.24+.

## Git

- Default branch `main`, tracking `origin` (GitHub). The maintainer commits to `main` directly;
  there is no PR requirement for the maintainer. For larger or parallel work use a `docs/*`,
  `feat/*` or `fix/*` branch (a git worktree keeps the main checkout untouched), then
  fast-forward `main` and push.
- Author and committer are always `DeM-Conve <154665057+DeM-Conve@users.noreply.github.com>`
  (this repo's local git config; pass `-c user.name=... -c user.email=...` if it differs). Never
  commit under any other name or email.
- No `Co-Authored-By:` trailer (no Claude co-author line) and no "Generated with Claude" line in
  commit messages or PR descriptions.
- Public repo: no real names, work emails or employers, absolute home paths or other personal data
  in code, docs or commit messages.
- Message style: Conventional Commits (`feat:`, `fix(config):`, `docs:`, ...), one change per
  commit; see CONTRIBUTING.md. Release notes come from CHANGELOG.md, not from commits.
- Releases: tag `vX.Y.Z` and run GoReleaser locally (RELEASING.md). Never tag or publish a
  release without the maintainer asking.
