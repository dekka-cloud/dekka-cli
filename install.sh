#!/bin/sh
# Installs the latest dekka release for this machine.
#
#   curl -fsSL https://raw.githubusercontent.com/dekka-cloud/dekka-cli/main/install.sh | sh
#
# DEKKA_VERSION picks a release tag such as v0.1.0, and DEKKA_INSTALL_DIR
# picks where the binary goes. The archive is checked against the release's
# checksums.txt before anything is installed.
set -eu

repo="dekka-cloud/dekka-cli"

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "this script installs on macOS and Linux, so on Windows download the zip from https://github.com/$repo/releases" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "there is no release for $(uname -m)" ;;
esac

if [ -n "${DEKKA_VERSION:-}" ]; then
	base="https://github.com/$repo/releases/download/$DEKKA_VERSION"
else
	base="https://github.com/$repo/releases/latest/download"
fi

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
	fail "sha256sum or shasum is needed to check the download"
fi

archive="dekka_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $archive"
curl -fsSL --proto '=https' "$base/$archive" -o "$tmp/$archive" || fail "could not download $base/$archive"
curl -fsSL --proto '=https' "$base/checksums.txt" -o "$tmp/checksums.txt" || fail "could not download the checksums"

want="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "checksums.txt has no entry for $archive"
[ "$(sha256 "$tmp/$archive")" = "$want" ] || fail "the checksum of $archive does not match checksums.txt"

tar -xzf "$tmp/$archive" -C "$tmp" dekka

dir="${DEKKA_INSTALL_DIR:-/usr/local/bin}"
if [ -z "${DEKKA_INSTALL_DIR:-}" ] && [ ! -w "$dir" ]; then
	dir="$HOME/.local/bin"
fi
mkdir -p "$dir"
install -m 0755 "$tmp/dekka" "$dir/dekka"
echo "Installed $("$dir/dekka" --version) to $dir/dekka"

case ":$PATH:" in
*":$dir:"*) ;;
*) echo "Add $dir to your PATH to run dekka from any directory." ;;
esac
