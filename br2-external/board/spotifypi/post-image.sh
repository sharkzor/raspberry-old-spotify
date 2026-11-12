#!/bin/sh
set -e
BOARD_DIR="$(dirname "$0")"
GENIMAGE_TMP="${BUILD_DIR}/genimage.tmp"
ROOTPATH_TMP="$(mktemp -d)"
trap 'rm -rf "${ROOTPATH_TMP}"' EXIT
rm -rf "${GENIMAGE_TMP}"
cp "${BOARD_DIR}/dac.txt" "${BINARIES_DIR}/dac.txt"

genimage \
	--rootpath "${ROOTPATH_TMP}" \
	--tmppath "${GENIMAGE_TMP}" \
	--inputpath "${BINARIES_DIR}" \
	--outputpath "${BINARIES_DIR}" \
	--config "${BOARD_DIR}/genimage.cfg"

xz -T0 -6 -k -f "${BINARIES_DIR}/sdcard.img"
echo "SD card image: ${BINARIES_DIR}/sdcard.img(.xz)"
