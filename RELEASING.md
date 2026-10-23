# Releasing

Releases are cut by the maintainer, locally, with [GoReleaser](https://goreleaser.com). There is
no CI on purpose: every check runs on the release machine before the tag is pushed. Versions
follow [semantic versioning](https://semver.org/); tags look like `v0.1.0`.

## One-time setup

- Go (the version in `go.mod` or newer), Docker with compose v2.24+ (to try the build).
- [GoReleaser v2](https://goreleaser.com/install/) and [syft](https://github.com/anchore/syft#installation)
  (GoReleaser calls syft for the SBOMs) on `PATH`.
- [shellcheck](https://www.shellcheck.net) and the [GitHub CLI](https://cli.github.com), logged in
  as the repository owner (`gh auth status`).
- Git identity for this repo: `git config user.name DeM-Conve` and
  `git config user.email 154665057+DeM-Conve@users.noreply.github.com`.

## Steps

Replace `0.1.0` with the version you release.

1. **Start from a clean, up-to-date `main`.**

   ```sh
   git switch main && git pull --ff-only && git status --short   # prints nothing
   ```

2. **Run every check.**

   ```sh
   test -z "$(gofmt -l .)" && go vet ./... && go test ./...
   shellcheck install.sh
   goreleaser check
   goreleaser release --snapshot --clean        # builds dist/ without publishing
   v=$(basename dist/hl_*_"$(go env GOOS)"_"$(go env GOARCH)".tar.gz | cut -d_ -f2)
   HL_DOWNLOAD_URL="file://$PWD/dist" sh install.sh --version "$v" --dir /tmp/hl-try
   /tmp/hl-try/hl version
   ```

   That installs the snapshot (version `<next patch>-next`) through the real install script and
   checksum check. Try the snapshot binary for real
   (`hl doctor`, `hl up` on a test homelab, e.g. with `examples/`).

3. **Update CHANGELOG.md.** Rename `## [Unreleased]` to `## [0.1.0] - YYYY-MM-DD` (today), add a
   new empty `## [Unreleased]` above it, and update the links at the bottom:

   ```markdown
   [Unreleased]: https://github.com/DeM-Conve/project__hl/compare/v0.1.0...HEAD
   [0.1.0]: https://github.com/DeM-Conve/project__hl/releases/tag/v0.1.0
   ```

   Also update the "Current state" in `docs/status.md`. Commit:

   ```sh
   git commit -am "chore: release v0.1.0"
   ```

4. **Tag and push.**

   ```sh
   git tag -a v0.1.0 -m "hl v0.1.0"
   git push origin main v0.1.0
   ```

5. **Build and publish.** The release notes are the version's CHANGELOG section:

   ```sh
   awk '/^## \[0\.1\.0\]/{on=1; next} /^## \[/{on=0} on' CHANGELOG.md > dist-notes.md
   GITHUB_TOKEN=$(gh auth token) goreleaser release --clean --release-notes dist-notes.md
   rm dist-notes.md
   ```

   This runs the tests again, builds Linux and macOS archives for amd64 and arm64, writes
   `checksums.txt` and the SBOMs, and publishes the GitHub release with them. Versions with a
   pre-release suffix (`v0.2.0-rc.1`) are marked as pre-releases.

6. **Check the release.**

   ```sh
   gh release view v0.1.0
   curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh -s -- --dir /tmp/hl-check
   /tmp/hl-check/hl version                      # hl v0.1.0 (commit ..., built ...)
   go install github.com/DeM-Conve/project__hl/cmd/hl@v0.1.0
   ```

   The README's release badge and the install script's "latest" lookup pick it up within minutes.

If something goes wrong after the tag is pushed but before anyone could download it, delete the
release and the tag (`gh release delete v0.1.0 --cleanup-tag`), fix, and start again. Once a
release has been public, don't reuse its version: release a new patch version instead.

## Homebrew tap

Off until the tap repository exists. To turn it on (once):

1. Create the public repository `DeM-Conve/homebrew-tap` with a README (it must not be empty).
2. Create a fine-grained personal access token with **Contents: read and write** on that
   repository only.
3. Uncomment the `homebrew_casks` block at the end of `.goreleaser.yaml` and run
   `goreleaser check`.
4. From then on, release with the token in the environment:
   `HOMEBREW_TAP_TOKEN=<token> GITHUB_TOKEN=$(gh auth token) goreleaser release --clean --release-notes dist-notes.md`.
   GoReleaser pushes `Casks/hl.rb` to the tap.
5. Add `brew install DeM-Conve/tap/hl` to the README's Install section.

## Release notes and commits

Commits follow [Conventional Commits](https://www.conventionalcommits.org/) (CONTRIBUTING.md).
The CHANGELOG is written by hand for users; GoReleaser's own notes (grouped by `feat`/`fix`, with
`docs`, `test`, `ci`, `chore`, `style` and `refactor` left out) are only used if you leave out
`--release-notes`.
