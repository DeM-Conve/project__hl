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
4. ~~Dashboard exposure~~ - decided 2026-10-23 (maintainer's call via Claude, revisit if wrong):
   off by default, `proxy.dashboard: true` turns it on, refused on a non-loopback bind unless
   `proxy.dashboard_public: true` ([requirements.md](requirements.md)).
5. ~~`hl restart` and the proxy~~ - decided 2026-10-23 (same): `restart` starts the proxy first,
   like `up`, `rebuild` and `update`.
6. ~~Windows~~ - decided 2026-10-23 (same): not supported; hl says so and suggests WSL 2.
7. ~~CLI tests~~ - done 2026-10-23: `internal/cli` is tested with a fake `docker` on PATH.
