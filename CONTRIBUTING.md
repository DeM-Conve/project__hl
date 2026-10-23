# Contributing

Thanks for helping. Bug reports, fixes, docs and small features are welcome. For anything bigger,
open an issue first so we can agree on the shape before you write the code. Everyone taking part
follows the [Code of Conduct](CODE_OF_CONDUCT.md).

Two rules shape every change:

- **Generic.** hl is used by many people with their own folders, apps, OS and timezone. Nothing in
  code, defaults, tests, examples or docs may be specific to one person's setup: no user names,
  home paths, project-folder names, one homelab's app names, timezones, domains or emails. Use
  neutral examples (`my-app`, `blog`, `~/code`, `~/homelab`, `UTC`).
- **No hard-coded values.** Every value hl uses is the user's setting or a named default defined
  in one place, and listed in the README's hl.yml reference.

The decisions behind hl are in [docs/requirements.md](docs/requirements.md) and the plan in
[docs/product-plan.md](docs/product-plan.md); how it works inside is in
[docs/architecture.md](docs/architecture.md).

## Build and test

You need Go (the version in `go.mod` or newer) and, to try it for real, Docker with the compose
plugin v2.24+.

```sh
go build -o hl ./cmd/hl
go test ./...
test -z "$(gofmt -l .)" && go vet ./...   # both must be clean
shellcheck install.sh                     # if you touch the installer
```

There is no CI, so run these before you open a pull request. To try a change end to end, use a
throwaway homelab and the apps in [examples/](examples/):

```sh
export HL_HOME=/tmp/hl-try
./hl init "$HL_HOME"
./hl add examples/static-site
./hl add examples/base-path-app
./hl up && ./hl status
./hl down
```

Two things to know: `hl init` also records the folder as your default homelab (run `hl init` on
your real one again afterwards), and every homelab's proxy currently runs as the compose project
`hl-proxy` on the same network name - stop a homelab you already run (`hl down`) before trying
another one on the same machine.

## Changes

- Add or update tests for behaviour you change.
- When a command, flag or `hl.yml` setting changes, update README.md (it is the user reference)
  and add a line under `Unreleased` in [CHANGELOG.md](CHANGELOG.md), written for users.
- Keep pull requests focused: one change, with its tests and docs.

## Commit messages

hl uses [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```
<type>[(scope)][!]: <what changes, lower case, no period>

[optional body: why, and anything a reviewer should know]
```

| Type | For |
| --- | --- |
| `feat` | a new command, flag, setting or behaviour users notice |
| `fix` | a bug fix |
| `docs` | README, guides, docs/, comments only |
| `test` | tests only |
| `refactor` | code changes that don't change behaviour |
| `build` | GoReleaser, install.sh, go.mod |
| `chore` | everything else (release commits, housekeeping) |

Scopes are optional and name the area: `feat(add): ...`, `fix(config): ...`. Mark breaking
changes with `!` (`feat(config)!: ...`) and a `BREAKING CHANGE:` line in the body. Examples:

```
feat(status): show container health
fix(config): keep comments when removing an app
docs: add a guide for backups
```

Commits before 0.1.0 used plain messages (`Add hl version`); from now on, use the format above.

## Releases

Maintainers: see [RELEASING.md](RELEASING.md).
