# Status

Where hl stands and what is left. Read this before starting new work. Update it when something
here changes (a release, a pending item done, a question answered). Last reviewed 2026-10-23. The v1 task list is
[product-plan.md](product-plan.md).

## Current state

- Every command in the README is implemented. hl runs the maintainer's homelab (five apps, path
  routing, fixed proxy subnet, shared `TZ`), which is how it is tested end to end.
- **No release yet.** There are no tags and no GitHub releases; everything in CHANGELOG.md is under
  `Unreleased`, ready to become 0.1.0. Until the first tag is pushed and published, `install.sh`,
  the README's release badge and the "download from Releases" route don't work; `go install
  github.com/DeM-Conve/project__hl/cmd/hl@latest` does (it reports a pseudo-version such as
  `v0.0.0-20261016...`).
- **Release tooling is ready**: `goreleaser check` and a snapshot build pass (linux/darwin x
  amd64/arm64, `checksums.txt`, an SPDX SBOM per archive via syft); `install.sh` was tested
  against the snapshot (checksum ok and mismatch, `--version`, `--dir`, `~` expansion) and is
  shellcheck clean. Steps: [RELEASING.md](../RELEASING.md).
- Docs: README is the product page and full reference; guides in `docs/guides/`; example apps in
  `examples/`; CODE_OF_CONDUCT, SECURITY, issue and PR templates in place.
- No CI, on purpose (see [requirements.md](requirements.md)); `gofmt`, `go vet`, `go test` and
  `shellcheck install.sh` are run by hand.
- Tests cover `config` (defaults, URLs, validation, comment-keeping add/remove, env layering),
  `gen` (override for path/host/none, proxy file), `compose` (service detection) and `version`.
  `internal/cli` and anything that calls Docker have no automated tests; they are checked by hand
  against a real homelab.

## Pending

Before tagging v0.1.0:

- Merge the feature branches meant for 0.1.0 (feat/production-core and any others), and for each:
  add its CHANGELOG lines under `Unreleased`, and fill the README's `<!-- slot ... -->` markers
  (commands table, hl.yml reference, quickstart) with what was actually built.
- Check README claims that depend on feat/production-core once merged: dashboard off by default
  with `proxy.dashboard` / `proxy.dashboard_public`, `hl restart` starting the proxy first,
  Windows "not supported" message.
- Cut the release: [RELEASING.md](../RELEASING.md) (maintainer only; needs goreleaser v2 and
  syft installed).

After v0.1.0:

- Homebrew tap: create `DeM-Conve/homebrew-tap` and a token, then turn on the commented
  `homebrew_casks` block in `.goreleaser.yaml` (RELEASING.md, "Homebrew tap").
- Two homelabs on one machine share the proxy project name `hl-proxy` and network `hl-proxy`
  (CONTRIBUTING.md warns about it); product-plan section 0 tracks making them per-homelab.

## Open questions

Nothing below has been decided; ask the maintainer rather than picking an answer.

1. **Version of the first release**: `v0.1.0` (as the README, CHANGELOG, RELEASING.md and
   `install.sh` examples use) or something else?
2. ~~Commit message style~~ - decided: Conventional Commits (CONTRIBUTING.md), matching the
   maintainer's other repos.
3. ~~HTTPS~~ - decided: out of scope for v0.1; documented as putting hl behind your own TLS proxy
   ([guide](guides/behind-https.md)). Built-in HTTPS is P2 in the product plan.
4. **Dashboard exposure**: the Traefik dashboard is always on at `http://traefik.<domain>` with no
   auth. With `proxy.bind: 0.0.0.0` it is reachable from the network. Make it optional or protect
   it?
5. **`hl restart` and the proxy**: unlike `up`/`rebuild`/`update`, `restart` doesn't start the
   proxy first. Intended?
6. **Windows** support: out of scope, or just not done yet?
7. **CLI tests**: should `internal/cli` get tests (e.g. with a fake `docker` on PATH), or stay
   hand-tested?
