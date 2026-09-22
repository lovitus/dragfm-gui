#!/usr/bin/env sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
build_root=${DRAGFM_BUILD_TMP_ROOT:-"$project_root/build/.tmp/linux-hans-release"}
output_dir="$project_root/internal/assets/payload"
release_base=https://github.com/lovitus/hans/releases/download/v1.7.0

mkdir -p "$build_root" "$output_dir"

file_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

fetch_asset() {
  asset_name=$1
  embedded_arch=$2
  expected=$3
  binary="$build_root/$asset_name"

  curl --fail --location --retry 3 --output "$binary.partial" "$release_base/$asset_name"
  actual=$(file_sha256 "$binary.partial")
  if [ "$actual" != "$expected" ]; then
    printf 'Hans release checksum mismatch for %s: got %s, want %s\n' "$asset_name" "$actual" "$expected" >&2
    exit 1
  fi
  mv "$binary.partial" "$binary"
  gzip -n -9 -c "$binary" > "$output_dir/hans-linux-$embedded_arch.gz"
}

fetch_asset hans-linux-amd64-musl amd64 e486957d351852fc559b0f4be9f20c57c9eaa32ecd7395793a8858ce65e14958
fetch_asset hans-linux-arm64-musl arm64 342700230a9f9e4448ebc87e927e025f60676e7c20c980e5a2ea800cb79dce47
fetch_asset hans-linux-armv7-musl arm c43f758e2dfb6960f8ce7554abeb55c4b9f4e48d5cf2f949fee5e593452e49aa
fetch_asset hans-linux-i386-musl 386 c8eadadcb14f27cb6551d32beb9aeae1ddd965cd22702862e5ec70de9a4d67e0

cd "$project_root"
go test ./internal/assets -run TestEmbeddedHans -count=1
