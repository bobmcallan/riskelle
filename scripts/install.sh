#!/bin/sh
# riskelle installer: one command puts the riskelle CLI on the PATH.
#
#   curl -fsSL https://github.com/bobmcallan/riskelle/releases/latest/download/install.sh | sh
#
# Pin a release, or pick the directory, in front of sh:
#
#   curl -fsSL https://github.com/bobmcallan/riskelle/releases/latest/download/install.sh | RISKELLE_VERSION=vX.Y.Z sh
#   curl -fsSL https://github.com/bobmcallan/riskelle/releases/latest/download/install.sh | RISKELLE_INSTALL_DIR=/usr/local/bin sh
#
# It is published as the install.sh asset of every release (scripts/release.sh
# copies it). It picks the release archive for this machine (linux or darwin,
# amd64 or arm64), checks it against the release's riskelle_<tag>_checksums.txt
# and refuses to install on a mismatch or a missing line, checks the binary
# reports the tag, installs it, and prints the installed `riskelle version`.
#
# The environment it reads, a stable interface that pipelines call too:
#
#   RISKELLE_VERSION      the release tag to install, vX.Y.Z. Unset: the
#                         latest release, read from the /releases/latest
#                         redirect (no API call, no token).
#   RISKELLE_INSTALL_DIR  where riskelle is installed; default ~/.local/bin.
#   RISKELLE_REPO_URL     the repository releases are read from, for a mirror
#                         or a test; default https://github.com/bobmcallan/riskelle.
#                         Assets are read from <url>/releases/download/<tag>/.
#
# Needs sh, curl, tar, and sha256sum or shasum. Windows is not covered:
# download the .zip archive from the release.
set -eu

repo=${RISKELLE_REPO_URL:-https://github.com/bobmcallan/riskelle}
dir=${RISKELLE_INSTALL_DIR:-$HOME/.local/bin}

fail() {
	echo "riskelle install: $*" >&2
	exit 1
}

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) os= ;;
esac
case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) arch= ;;
esac
[ -n "$os" ] && [ -n "$arch" ] ||
	fail "unsupported platform '$(uname -sm)'; download a release from $repo/releases"

tag=${RISKELLE_VERSION:-}
if [ -z "$tag" ]; then
	# /releases/latest redirects to /releases/tag/<tag>; the tag is the last
	# path segment of where it lands.
	url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$repo/releases/latest")
	tag=${url##*/}
fi
printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
	fail "'$tag' is not a release tag vX.Y.Z"

name="riskelle_${tag}_${os}_${arch}"
archive="$name.tar.gz"
sums="riskelle_${tag}_checksums.txt"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "riskelle install: fetching $archive"
curl -fsSL -o "$tmp/$archive" "$repo/releases/download/$tag/$archive"
curl -fsSL -o "$tmp/$sums" "$repo/releases/download/$tag/$sums"

want=$(awk -v f="$archive" '$2 == f { print $1; exit }' "$tmp/$sums")
[ -n "$want" ] || fail "$sums has no line for $archive; nothing installed"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
else
	got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
fi
[ "$want" = "$got" ] ||
	fail "checksum mismatch for $archive (want $want, got $got); nothing installed"

tar -xzf "$tmp/$archive" -C "$tmp"
reported=$("$tmp/$name/riskelle" version) || fail "$archive holds no working riskelle; nothing installed"
case "$reported" in
	"riskelle $tag" | "riskelle $tag ("*) ;;
	*) fail "$archive's riskelle reports '$reported', not $tag; nothing installed" ;;
esac

# Copy beside the target, then rename: the rename is atomic, replaces a
# riskelle that is running, and never leaves a half-written binary.
mkdir -p "$dir"
cp "$tmp/$name/riskelle" "$dir/.riskelle.$$"
chmod 0755 "$dir/.riskelle.$$"
mv -f "$dir/.riskelle.$$" "$dir/riskelle"

echo "riskelle install: installed $dir/riskelle"
"$dir/riskelle" version

case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo "riskelle install: add $dir to your PATH (export PATH=\"$dir:\$PATH\")" ;;
esac
