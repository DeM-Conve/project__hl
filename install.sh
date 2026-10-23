#!/bin/sh
# Install hl from its GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh -s -- --version v0.1.0 --dir /usr/local/bin
#
# Options (or the environment variable after each):
#   --version TAG   a release tag, e.g. v0.1.0               HL_VERSION   (default: the latest release)
#   --dir DIR       where to put hl                          HL_INSTALL_DIR
#                   (default: ~/.local/bin, or /usr/local/bin when run as root);
#                   sudo is used only when DIR isn't writable
#   -h, --help      show this help
#
# HL_DOWNLOAD_URL   for testing: a base URL holding the release files, used
#                   instead of GitHub (e.g. file:///path/to/dist)
#
# The archive is checked against the release's checksums.txt (sha256) before
# anything is installed. Supported: Linux and macOS on amd64 and arm64.
set -eu

repo=DeM-Conve/project__hl

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
has() { command -v "$1" >/dev/null 2>&1; }
usage() { sed -n '2,18s/^# \{0,1\}//p' "$0" 2>/dev/null || say "see https://github.com/$repo#install"; }

version=${HL_VERSION:-}
dir=${HL_INSTALL_DIR:-}
while [ $# -gt 0 ]; do
	case $1 in
		--version) [ $# -ge 2 ] || die "--version needs a tag, e.g. v0.1.0"; version=$2; shift 2 ;;
		--version=*) version=${1#*=}; shift ;;
		--dir) [ $# -ge 2 ] || die "--dir needs a folder"; dir=$2; shift 2 ;;
		--dir=*) dir=${1#*=}; shift ;;
		-h | --help) usage; exit 0 ;;
		*) die "unknown option: $1 (try --help)" ;;
	esac
done

fetch() { # url dest
	if has curl; then
		curl -fsSL --proto '=https,file' --retry 3 -o "$2" "$1"
	elif has wget; then
		wget -q -O "$2" "$1"
	else
		die "need curl or wget"
	fi
}

case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported OS: $(uname -s) (hl runs on Linux and macOS; Windows isn't supported)" ;;
esac
case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported CPU: $(uname -m) (hl is built for amd64 and arm64)" ;;
esac
# A shell running under Rosetta reports x86_64 on Apple silicon; take the native build.
if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
	arch=arm64
fi

if has sha256sum; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif has shasum; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	die "need sha256sum or shasum to verify the download"
fi
has tar || die "need tar"

base=${HL_DOWNLOAD_URL:-}
tmp=$(mktemp -d 2>/dev/null || mktemp -d -t hl-install)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' INT TERM HUP

if [ -z "$base" ]; then
	if [ -z "$version" ]; then
		fetch "https://api.github.com/repos/$repo/releases/latest" "$tmp/latest.json" ||
			die "couldn't look up the latest release of $repo (pass --version to pick one)"
		version=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n 1)
		[ -n "$version" ] || die "couldn't find the latest release (pass --version to pick one)"
	fi
	case $version in v*) ;; *) version=v$version ;; esac
	base="https://github.com/$repo/releases/download/$version"
fi
[ -n "$version" ] || die "set HL_VERSION (or --version) along with HL_DOWNLOAD_URL"
case $version in v*) ;; *) version=v$version ;; esac
archive="hl_${version#v}_${os}_${arch}.tar.gz"

say "downloading hl $version for $os/$arch"
fetch "$base/$archive" "$tmp/$archive" || die "couldn't download $base/$archive (does release $version exist?)"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "couldn't download $base/checksums.txt"

want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$archive is not in checksums.txt"
got=$(sha256 "$tmp/$archive")
[ "$got" = "$want" ] || die "checksum mismatch for $archive (got $got, want $want)"
say "checksum ok"

tar -xzf "$tmp/$archive" -C "$tmp" hl || die "couldn't unpack $archive"
"$tmp/hl" version >/dev/null 2>&1 || die "the downloaded hl doesn't run on this machine"

if [ -z "$dir" ]; then
	if [ "$(id -u)" = 0 ]; then
		dir=/usr/local/bin
	else
		dir=$HOME/.local/bin
	fi
	explicit=
else
	explicit=1
fi
case $dir in \~ | \~/*) dir=$HOME${dir#\~} ;; esac

sudo=
if ! mkdir -p "$dir" 2>/dev/null || [ ! -w "$dir" ]; then
	[ -n "$explicit" ] || die "can't write to $dir"
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
say "next: hl doctor   (checks Docker and compose v2.24+)"
