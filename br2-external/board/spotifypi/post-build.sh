#!/bin/sh
# Strip the rootfs down to what a read-only Spotify endpoint needs.
set -eu

# Services we don't want (crond, telnet, httpd, seed file writer, ...)
for s in S01seedrng S20seedrng S50crond S50telnet S90httpd S41ifplugd; do
	rm -f "${TARGET_DIR}/etc/init.d/${s}"
done

# Clock fallback before NTP sync: never earlier than the build date,
# so TLS certificate validation works right after boot.
date -u +%s > "${TARGET_DIR}/etc/build-epoch"

mkdir -p "${TARGET_DIR}/data" "${TARGET_DIR}/boot"
