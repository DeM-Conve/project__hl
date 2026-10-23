<!-- What does this change, and why? Link the issue if there is one (Fixes #123). -->

## Checklist

- [ ] The title follows [Conventional Commits](../CONTRIBUTING.md#commit-messages) (`feat: ...`, `fix(config): ...`)
- [ ] `test -z "$(gofmt -l .)"`, `go vet ./...` and `go test ./...` pass (and `shellcheck install.sh` if it changed)
- [ ] Tests cover the change
- [ ] README (commands, flags, hl.yml settings and defaults) and CHANGELOG.md (`Unreleased`) updated if users will notice it
- [ ] Nothing specific to one setup: no personal names, paths, app names, timezones or domains; no new hard-coded values (settings or named defaults instead)
- [ ] Tried it with Docker if it changes what hl runs (how: )
