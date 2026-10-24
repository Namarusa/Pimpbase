#!/bin/sh
# Builds release archives of the pimpbase binary for Linux, macOS and
# Windows into dist/ (or $OUT), with a short README in each. The UI in
# web/builder/dist is embedded as committed; run `make ui` first after
# changing it. No cgo is needed, so any machine with Go builds them all.
#
#   scripts/release.sh                     every platform
#   scripts/release.sh windows/amd64       only the named ones
#   VERSION=1.2.0 scripts/release.sh       version printed by `pimpbase version`
set -eu
cd "$(dirname "$0")/.."

VERSION=${VERSION:-$(date -u +%Y.%m.%d)-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}
OUT=${OUT:-dist}
TARGETS=${*:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64}

rm -rf "$OUT/stage"
mkdir -p "$OUT"
for target in $TARGETS; do
	os=${target%/*}
	arch=${target#*/}
	name=pimpbase-$VERSION-$os-$arch
	stage=$OUT/stage/$name
	mkdir -p "$stage"
	exe=pimpbase
	[ "$os" = windows ] && exe=pimpbase.exe
	echo "building $name"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
		-ldflags "-s -w -X main.version=$VERSION" -o "$stage/$exe" ./cmd/pimpbase
	if [ "$os" = windows ]; then
		# Notepad and double-clicked .bat files want CRLF line endings.
		sed "s/{{VERSION}}/$VERSION/g; s/\$/\r/" scripts/release/README-windows.txt >"$stage/README.txt"
		sed 's/$/\r/' scripts/release/start-pimpbase.bat >"$stage/start-pimpbase.bat"
		(cd "$OUT/stage" && rm -f "../$name.zip" && zip -qr "../$name.zip" "$name")
	else
		sed "s/{{VERSION}}/$VERSION/g" scripts/release/README-unix.txt >"$stage/README.txt"
		tar -C "$OUT/stage" -czf "$OUT/$name.tar.gz" "$name"
	fi
done
rm -rf "$OUT/stage"
(cd "$OUT" && sha256sum pimpbase-"$VERSION"-*.zip pimpbase-"$VERSION"-*.tar.gz 2>/dev/null >"pimpbase-$VERSION-SHA256SUMS.txt")
ls -l "$OUT" | grep "$VERSION"
