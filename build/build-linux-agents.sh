#!/usr/bin/env sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
build_root=${DRAGFM_BUILD_TMP_ROOT:-"$project_root/build/.tmp/linux-agents"}
output_dir="$project_root/internal/assets/payload"

mkdir -p "$build_root" "$output_dir"

build_agent() {
  goarch=$1
  asset_arch=$2
  goarm=${3:-}
  binary="$build_root/dragfm-agent-linux-$asset_arch"
  archive="$output_dir/dragfm-agent-linux-$asset_arch.gz"

  if [ -n "$goarm" ]; then
    env GOOS=linux GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=0 \
      go build -mod=vendor -trimpath -buildvcs=false -ldflags="-s -w" \
      -o "$binary" ./cmd/dragfm-agent
  else
    env GOOS=linux GOARCH="$goarch" CGO_ENABLED=0 \
      go build -mod=vendor -trimpath -buildvcs=false -ldflags="-s -w" \
      -o "$binary" ./cmd/dragfm-agent
  fi
  gzip -n -9 -c "$binary" > "$archive"
}

cd "$project_root"
build_agent amd64 amd64
build_agent arm64 arm64
build_agent 386 386
build_agent arm arm 7

go test ./internal/assets -run TestEmbeddedLinuxAgents -count=1
