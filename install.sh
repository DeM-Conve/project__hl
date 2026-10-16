#!/bin/sh
# Install hl from its GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh
#
# HL_VERSION      a release tag, e.g. v0.1.0 (default: the latest release)
# HL_INSTALL_DIR  where to put hl (default: ~/.local/bin); sudo is used only
#                 when that folder isn't writable
# HL_DOWNLOAD_URL for testing: a base URL holding the release files, used
#                 instead of GitHub (e.g. file:///path/to/dist)
set -eu

repo=DeM-Conve/project__hl

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
has() { command -v "$1" >/dev/null 2>&1; }

fetch() { # url dest
	if has curl; then
		curl -fsSL -o "$2" "$1"
	elif has wget; then
		wget -q -O "$2" "$1"
	else
		die "need curl or wget"
	fi
}

case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported OS: $(uname -s) (hl runs on Linux and macOS)" ;;
esac
case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported CPU: $(uname -m) (hl is built for amd64 and arm64)" ;;
esac

if has sha256sum; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif has shasum; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	die "need sha256sum or shasum to verify the download"
fi

version=${HL_VERSION:-}
base=${HL_DOWNLOAD_URL:-}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' INT TERM

if [ -z "$base" ]; then
	if [ -z "$version" ]; then
		fetch "https://api.github.com/repos/$repo/releases/latest" "$tmp/latest.json" ||
			die "couldn't look up the latest release of $repo (set HL_VERSION to pick one)"
		version=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n 1)
		[ -n "$version" ] || die "couldn't find the latest release"
	fi
	base="https://github.com/$repo/releases/download/$version"
fi
[ -n "$version" ] || die "set HL_VERSION along with HL_DOWNLOAD_URL"

case $version in v*) ;; *) version=v$version ;; esac
archive="hl_${version#v}_${os}_${arch}.tar.gz"

say "downloading hl $version for $os/$arch"
fetch "$base/$archive" "$tmp/$archive" || die "couldn't download $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "couldn't download $base/checksums.txt"

want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$archive is not in checksums.txt"
got=$(sha256 "$tmp/$archive")
[ "$got" = "$want" ] || die "checksum mismatch for $archive (got $got, want $want)"

tar -xzf "$tmp/$archive" -C "$tmp" hl

dir=${HL_INSTALL_DIR:-$HOME/.local/bin}
sudo=
if ! mkdir -p "$dir" 2>/dev/null || [ ! -w "$dir" ]; then
	[ -n "${HL_INSTALL_DIR:-}" ] || die "can't write to $dir"
	has sudo || die "can't write to $dir, and sudo isn't available"
	say "$dir isn't writable, using sudo"
	sudo=sudo
	$sudo mkdir -p "$dir"
fi
$sudo install -m 0755 "$tmp/hl" "$dir/hl"
say "installed $dir/hl"

case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "note: $dir is not on your PATH; add it, e.g. export PATH=\"$dir:\$PATH\"" ;;
esac
"$dir/hl" version
