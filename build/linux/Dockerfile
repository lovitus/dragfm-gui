ARG GO_IMAGE=golang:1.26-bookworm
FROM ${GO_IMAGE}

ARG APT_MIRROR=
RUN if [ -n "${APT_MIRROR}" ]; then \
      sed -i "s|http://deb.debian.org/debian-security|${APT_MIRROR}/debian-security|g; s|http://deb.debian.org/debian|${APT_MIRROR}/debian|g" /etc/apt/sources.list.d/debian.sources; \
    fi \
    && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    gcc libc6-dev libgl1-mesa-dev libwayland-dev libx11-dev libx11-xcb-dev \
    libxcursor-dev libxfixes-dev libxi-dev libxinerama-dev libxkbcommon-dev \
    libxrandr-dev libxrender-dev libxxf86vm-dev pkg-config wayland-protocols \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY . .
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} go build -tags x11 -mod=vendor -buildvcs=false \
    -trimpath -ldflags="-s -w" -o /out/dragfm-gui-linux-${TARGETARCH} ./cmd/dragfm-gui

FROM scratch AS export
COPY --from=0 /out/ /
