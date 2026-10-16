# hl

A generic Go CLI that runs self-hosted apps (each a folder with its own compose file) behind one
Traefik proxy. Nothing here may be specific to one person's homelab. See README.md.

- `internal/config`: hl.yml (load, defaults, validation, add/remove keeping comments)
- `internal/compose`: runs `docker compose` and reads the resolved model (`config --format json`)
- `internal/gen`: the generated override per app and the proxy's compose file
- `internal/cli`: the commands (cobra)

Check with `gofmt -l .`, `go vet ./...` and `go test ./...`.

## Commits

- Author and committer are always `DeM-Conve <154665057+DeM-Conve@users.noreply.github.com>`
  (this repo's local git config). Never commit under any other name or email.
- No `Co-Authored-By:` trailer (no Claude co-author line) and no "Generated with Claude" line in
  commit messages or PR descriptions.
