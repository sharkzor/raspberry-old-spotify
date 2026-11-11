#!/bin/bash
# Runs inside the builder container. /src = this repo (read-only),
# /work = persistent named volume (Buildroot tree, downloads, output), /out = results.
set -euo pipefail
BR_VERSION="${BR_VERSION:-2026.08.1}"
BR_DIR="/work/buildroot-${BR_VERSION}"
export BR2_DL_DIR=/work/dl

if [ ! -d "${BR_DIR}" ]; then
	echo ">>> Downloading Buildroot ${BR_VERSION}"
	wget -q -O /tmp/br.tar.xz "https://buildroot.org/downloads/buildroot-${BR_VERSION}.tar.xz"
	tar -xf /tmp/br.tar.xz -C /work && rm /tmp/br.tar.xz
fi

MAKE=(make -C "${BR_DIR}" O=/work/output BR2_EXTERNAL=/src/br2-external)
"${MAKE[@]}" spotifypi_rpi2_defconfig

if [ "${1:-}" = "menuconfig" ]; then
	"${MAKE[@]}" menuconfig
	"${MAKE[@]}" savedefconfig BR2_DEFCONFIG=/out/spotifypi_rpi2_defconfig
	echo ">>> Saved to output/spotifypi_rpi2_defconfig"
	exit 0
fi

# The web UI is a local package: re-sync it so source changes are picked up.
if [ -d /work/output/build/spotifypi-1.0 ]; then
	"${MAKE[@]}" spotifypi-dirclean
fi
"${MAKE[@]}" "$@"

cp /work/output/images/sdcard.img.xz /out/spotifypi-rpi2.img.xz
echo ">>> Done: output/spotifypi-rpi2.img.xz"
