# Status

Where hl stands and what is left. Read this before starting new work. Update it when something
here changes (a release, a pending item done, a question answered). Last reviewed 2026-10-22.

## Current state

- Every command in the README is implemented. hl runs the maintainer's homelab (five apps, path
  routing, fixed proxy subnet, shared `TZ`), which is how it is tested end to end.
- **No release yet.** There are no tags and no GitHub releases; everything in CHANGELOG.md is under
  `Unreleased`. Until the first tag is pushed and published, `install.sh`, the README's release
  badge and the "download from Releases" route don't work; `go install
  github.com/DeM-Conve/project__hl/cmd/hl@latest` does (it reports a pseudo-version such as
  `v0.0.0-20261016...`).
- No CI, on purpose (see [requirements.md](requirements.md)); `gofmt`, `go vet`, `go test` and
  `shellcheck install.sh` are run by hand.
- Tests cover `config` (defaults, URLs, validation, comment-keeping add/remove, env layering),
  `gen` (override for path/host/none, proxy file), `compose` (service detection) and `version`.
  `internal/cli` and anything that calls Docker have no automated tests; they are checked by hand
  against a real homelab.

## Pending

- Cut the first release (`v0.1.0` per the README's example): move CHANGELOG `Unreleased` under the
  version, tag, `goreleaser release --clean` (steps in CONTRIBUTING.md).
- Homebrew tap: create `DeM-Conve/homebrew-tap`, then turn on the commented `homebrew_casks` block
  in `.goreleaser.yaml`.

## Open questions

Nothing below has been decided; ask the maintainer rather than picking an answer.

1. **Version of the first release**: `v0.1.0` (as the README and `install.sh` examples use) or
   something else?
2. **Commit message style**: CONTRIBUTING.md asks for plain messages (`Add hl version`,
   `config: keep comments ...`), and the history follows it; GoReleaser drops `docs:`, `test:` and
   `ci:` commits from release notes. Should Conventional Commits (`feat:`, `fix:`) become the rule?
3. **HTTPS**: is TLS (e.g. a `websecure` entrypoint, local CA or ACME) wanted, or does hl stay
   HTTP-only for `localhost`/LAN use?
4. **Dashboard exposure**: the Traefik dashboard is always on at `http://traefik.<domain>` with no
   auth. With `proxy.bind: 0.0.0.0` it is reachable from the network. Make it optional or protect
   it?
5. **`hl restart` and the proxy**: unlike `up`/`rebuild`/`update`, `restart` doesn't start the
   proxy first. Intended?
6. **Windows** support: out of scope, or just not done yet?
7. **CLI tests**: should `internal/cli` get tests (e.g. with a fake `docker` on PATH), or stay
   hand-tested?
