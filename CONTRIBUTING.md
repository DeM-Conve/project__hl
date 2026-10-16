# Contributing

Bug reports, fixes and small features are welcome. For anything bigger, open an issue first so we
can agree on the shape before you write the code.

hl must stay generic: nothing in it may be specific to one person's homelab or one app.

## Build and test

You need Go (the version in `go.mod`) and, to try it for real, Docker with the compose plugin.

```sh
go build -o hl ./cmd/hl
go test ./...
gofmt -l . && go vet ./...      # CI fails on either
shellcheck install.sh           # if you touch the installer
```

CI runs the same checks, plus `go test -race` and `goreleaser check`.

## Commits and pull requests

- Small commits, each doing one thing, with a plain message that says what changes:
  `Add hl version`, `config: keep comments when removing an app`.
- Add or update tests for behaviour you change, and the README when a command or `hl.yml` changes.
- Add a line under `Unreleased` in CHANGELOG.md for anything a user would notice.

## Releases

Releases are built by [GoReleaser](https://goreleaser.com) from a version tag:

1. Move the `Unreleased` entries in CHANGELOG.md under the new version and date, and commit.
2. Tag and push: `git tag -a v0.2.0 -m v0.2.0 && git push origin v0.2.0`.
3. The release workflow builds the archives for Linux and macOS (amd64, arm64), writes
   `checksums.txt` and publishes a GitHub release with them.

To see what a release would contain without publishing anything:
`goreleaser release --snapshot --clean` (output in `dist/`).
