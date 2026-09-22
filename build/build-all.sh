#!/usr/bin/env sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_dir=${1:-"$project_root/dist"}
mkdir -p "$output_dir" "$project_root/.buildtmp/cache" "$project_root/.buildtmp/tmp"

export TMPDIR="$project_root/.buildtmp/tmp"
export GOTMPDIR="$project_root/.buildtmp/tmp"
export GOCACHE="$project_root/.buildtmp/cache"

build() {
    target_os=$1
    target_arch=$2
    output=$3
    shift 3
	ldflags="-s -w"
	if [ "$target_os" = darwin ]; then
		ldflags="$ldflags -linkmode=external -extldflags=-Wl,-sectcreate,__TEXT,__info_plist,$project_root/build/macos/Info.plist"
	fi
	if [ "$target_os" = windows ]; then
		ldflags="$ldflags -H=windowsgui -extldflags=-Wl,--subsystem,windows"
	fi
    env GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=1 "$@" \
		go build -mod=vendor -trimpath -buildvcs=false -ldflags="$ldflags" -o "$output_dir/$output" ./cmd/dragfm-gui
}

case "$(uname -s)" in
Darwin)
    build darwin arm64 dragfm-gui-darwin-arm64 CC=clang
	if [ "${DRAGFM_BUILD_ALL:-0}" = 1 ]; then
		build darwin amd64 dragfm-gui-darwin-amd64 CC="clang -arch x86_64"
		if command -v zig >/dev/null 2>&1; then
			build windows amd64 dragfm-gui-windows-amd64.exe CC="zig cc -target x86_64-windows-gnu" CXX="zig c++ -target x86_64-windows-gnu"
			build windows arm64 dragfm-gui-windows-arm64.exe CC="zig cc -target aarch64-windows-gnu" CXX="zig c++ -target aarch64-windows-gnu"
		fi
    fi
    ;;
Linux)
    build linux "$(go env GOARCH)" "dragfm-gui-linux-$(go env GOARCH)" CC=gcc
    ;;
esac

(cd "$output_dir" && sha256sum dragfm-gui-* > SHA256SUMS 2>/dev/null || shasum -a 256 dragfm-gui-* > SHA256SUMS)
