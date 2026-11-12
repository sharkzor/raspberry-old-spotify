#!/bin/sh
# Called by busybox ntpd; marks the clock as synced so librespot can start.
case "$1" in
step|periodic|stratum) mkdir -p /run/spotifypi && touch /run/spotifypi/clock-synced ;;
unsync) rm -f /run/spotifypi/clock-synced ;;
esac
