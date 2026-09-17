#!/usr/bin/env sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_dir=${1:-"$project_root/dist"}
build_root="$project_root/.buildtmp/wails"
mkdir -p "$output_dir" "$build_root/cache" "$build_root/tmp"

export TMPDIR="$build_root/tmp"
export GOTMPDIR="$build_root/tmp"
export GOCACHE="$build_root/cache"
# Match Wails' supported deployment floor instead of inheriting the build
# machine's current macOS version from clang (which would make the resulting
# binary refuse to start on older systems).
export MACOSX_DEPLOYMENT_TARGET=11.0
export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-mmacosx-version-min=11.0"
export CGO_CXXFLAGS="${CGO_CXXFLAGS:+$CGO_CXXFLAGS }-mmacosx-version-min=11.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-mmacosx-version-min=11.0"

if [ ! -d "$project_root/frontend/node_modules" ]; then
  npm --prefix "$project_root/frontend" ci
fi
npm --prefix "$project_root/frontend" run build

env GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 CC=clang \
  go build -mod=vendor -trimpath -buildvcs=false -tags production \
  -ldflags="-s -w -linkmode=external -extldflags=-Wl,-sectcreate,__TEXT,__info_plist,$project_root/build/macos/Info.plist,-framework,UniformTypeIdentifiers" \
  -o "$output_dir/dragfm-gui-wails-darwin-arm64" ./cmd/dragfm-wails

(cd "$output_dir" && shasum -a 256 dragfm-gui-wails-darwin-arm64 > SHA256SUMS.wails)
