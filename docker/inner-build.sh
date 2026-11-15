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

# Each variant has its own Buildroot output tree: switching packages in one
# tree leaves stale target files behind.
VARIANT="${VARIANT:-librespot}"
case "${VARIANT}" in
	librespot) DEFCONFIG=spotifypi_rpi2_defconfig; OUT=/work/output; IMAGE_NAME=spotifypi-rpi2.img.xz ;;
	soloist) DEFCONFIG=spotifypi_soloist_rpi2_defconfig; OUT=/work/output-soloist; IMAGE_NAME=spotifypi-soloist-rpi2.img.xz ;;
	*) echo "Unknown VARIANT '${VARIANT}' (use librespot or soloist)" >&2; exit 1 ;;
esac

MAKE=(make -C "${BR_DIR}" O="${OUT}" BR2_EXTERNAL=/src/br2-external)
"${MAKE[@]}" "${DEFCONFIG}"

if [ "${1:-}" = "menuconfig" ]; then
	"${MAKE[@]}" menuconfig
	"${MAKE[@]}" savedefconfig BR2_DEFCONFIG="/out/${DEFCONFIG}"
	echo ">>> Saved to output/${DEFCONFIG}"
	exit 0
fi

# The web UI is a local package: re-sync it so source changes are picked up.
if [ -d "${OUT}/build/spotifypi-1.0" ]; then
	"${MAKE[@]}" spotifypi-dirclean
fi

# Soloist builds expire 90 days after they are made, and its download URL always
# serves the newest build: drop the cached archive so every build fetches a fresh one.
if [ "${VARIANT}" = "soloist" ]; then
	rm -f /work/dl/soloist/soloist_release_arm32.tar.gz
	if [ -d "${OUT}/build/soloist-latest" ]; then
		"${MAKE[@]}" soloist-dirclean
	fi
fi

"${MAKE[@]}" "$@"

cp "${OUT}/images/sdcard.img.xz" "/out/${IMAGE_NAME}"
echo ">>> Done: output/${IMAGE_NAME}"
