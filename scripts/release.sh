#!/usr/bin/env bash
# release.sh builds the riskelle release archives for one version.
#
#   scripts/release.sh vX.Y.Z <outdir>
#
# It is the single owner of the release targets, the archive layout and the
# checksums file. For every target it cross-compiles ./cmd/riskelle with the
# version stamped in, packs the binary and README.md into
# riskelle_<version>_<os>_<arch>.tar.gz (.zip on windows), and writes
# riskelle_<version>_checksums.txt (sha256sum format) beside them. It also
# copies scripts/install.sh beside them as the install.sh asset, which
# releases/latest/download/install.sh serves; the checksums file lists only
# the archives.
#
# The output depends only on the commit: the tree must be clean, the Go
# toolchain comes from go.mod, and every timestamp is the commit time, so a
# rebuild from the same tag gives byte-identical archives.
#
# Needs bash, git, go, GNU tar, gzip, zip and sha256sum (Linux, as on the
# GitHub runner).
set -euo pipefail

usage() {
	echo "usage: scripts/release.sh vX.Y.Z <outdir>" >&2
	exit 2
}

[[ $# -eq 2 ]] || usage
version=$1
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage

mkdir -p "$2"
out=$(cd "$2" && pwd)

cd "$(git rev-parse --show-toplevel)"
if [[ -n $(git status --porcelain) ]]; then
	echo "release.sh: the working tree is not clean; a release is built from a commit alone" >&2
	exit 1
fi

toolchain=$(awk '/^toolchain /{print $2}' go.mod)
[[ -n $toolchain ]] || { echo "release.sh: go.mod has no toolchain line" >&2; exit 1; }
export GOTOOLCHAIN=$toolchain CGO_ENABLED=0 TZ=UTC
SOURCE_DATE_EPOCH=$(git log -1 --format=%ct HEAD)
export SOURCE_DATE_EPOCH

targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
ldflags="-s -w -X github.com/bobmcallan/riskelle/internal/version.version=$version"

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

archives=()
for target in "${targets[@]}"; do
	os=${target%/*}
	arch=${target#*/}
	name="riskelle_${version}_${os}_${arch}"
	bin=riskelle
	[[ $os == windows ]] && bin=riskelle.exe

	mkdir "$stage/$name"
	GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$stage/$name/$bin" ./cmd/riskelle
	cp README.md "$stage/$name/README.md"
	chmod 0755 "$stage/$name" "$stage/$name/$bin"
	chmod 0644 "$stage/$name/README.md"
	find "$stage/$name" -exec touch -d "@$SOURCE_DATE_EPOCH" {} +

	if [[ $os == windows ]]; then
		archive="$name.zip"
		rm -f "$out/$archive"
		(cd "$stage" && find "$name" | LC_ALL=C sort | zip -X -q -@ "$out/$archive")
	else
		archive="$name.tar.gz"
		tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
			--format=gnu -C "$stage" -cf - "$name" | gzip -n -9 >"$out/$archive"
	fi
	archives+=("$archive")
done

(cd "$out" && LC_ALL=C sha256sum -- "${archives[@]}" >"riskelle_${version}_checksums.txt")
cp scripts/install.sh "$out/install.sh"
echo "release.sh: wrote ${#archives[@]} archives, riskelle_${version}_checksums.txt and install.sh to $out"
