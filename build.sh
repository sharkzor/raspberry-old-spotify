#!/bin/sh
# Build the SpotifyPi SD card image with Docker (works on Linux and macOS).
#   ./build.sh               full build -> output/spotifypi-rpi2.img.xz
#   ./build.sh menuconfig    tweak the Buildroot config
#   ./build.sh clean         remove the cached build volume
set -eu
cd "$(dirname "$0")"
IMAGE=spotifypi-builder
VOLUME=spotifypi-build

if [ "${1:-}" = "clean" ]; then
	docker volume rm -f "$VOLUME"
	exit 0
fi

docker build --platform linux/amd64 -t "$IMAGE" docker/
mkdir -p output
# Build tree lives in a named volume: Buildroot needs a case-sensitive
# filesystem and bind mounts from macOS are slow.
TTY=""; [ -t 0 ] && TTY="-it"
docker run --rm $TTY --platform linux/amd64 \
	-v "$PWD":/src:ro \
	-v "$VOLUME":/work \
	-v "$PWD/output":/out \
	"$IMAGE" bash /src/docker/inner-build.sh "$@"
