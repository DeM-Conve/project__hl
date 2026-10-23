#!/bin/sh
# The checks to run before every commit and release (there is no CI, on purpose).
#   scripts/check.sh          everything
#   scripts/check.sh --quick  skip the race detector, golangci-lint and cross builds
# Linters run with `go run`, pinned below; Go downloads the toolchain they need.
set -eu

STATICCHECK=honnef.co/go/tools/cmd/staticcheck@v0.8.2
GOLANGCI=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"

quick=0
[ "${1:-}" = "--quick" ] && quick=1
cd "$(dirname "$0")/.."

step() { printf '\n==> %s\n' "$*"; }

step gofmt
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "not gofmt'ed:"; echo "$unformatted"; exit 1
fi

step go vet
go vet ./...

step staticcheck
go run "$STATICCHECK" ./...

if [ "$quick" = 0 ]; then
	step golangci-lint
	go run "$GOLANGCI" run ./...
fi

step tests
if [ "$quick" = 0 ]; then
	go test -race -count=1 ./...
else
	go test -count=1 ./...
fi

if command -v shellcheck >/dev/null 2>&1; then
	step shellcheck
	shellcheck install.sh scripts/*.sh
else
	echo "(shellcheck not installed - skipped)"
fi

if [ "$quick" = 0 ]; then
	# Windows builds only to print hl's "not supported" message.
	step "builds: $PLATFORMS"
	for p in $PLATFORMS; do
		GOOS=${p%/*} GOARCH=${p#*/} go build -o /dev/null ./cmd/hl
	done
fi

step "all checks passed"
