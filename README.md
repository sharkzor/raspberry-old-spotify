# SpotifyPi

A minimal, read-only Linux image that turns a **Raspberry Pi 2** (32-bit) with a
**HiFiBerry DAC** (or compatible PCM5102A board) into a dedicated
**Spotify Connect** speaker.

* [librespot](https://github.com/librespot-org/librespot) 0.8.0 – Spotify Connect
* [CamillaDSP](https://github.com/HEnquist/camilladsp) 4.1.3 – parametric EQ
* `spotifypi` – a tiny Go web UI and service launcher (stdlib only)
* Buildroot 2026.08.1, BusyBox init, no systemd, no package manager, optional key-only SSH

## Flashing

Write `output/spotifypi-rpi2.img.xz` with Raspberry Pi Imager ("Use custom")
or balenaEtcher, or run:

```sh
xz -dc output/spotifypi-rpi2.img.xz | sudo dd of=/dev/sdX bs=4M conv=fsync
```

Connect Ethernet (DHCP) and power on. The device shows up in Spotify as
**SpotifyPi**. The web UI listens on `http://<ip>/`. The IP address is printed
on the HDMI console and the serial console. Your router's DHCP list also shows
it as `spotifypi`.

## Web UI

* **Spotify**: the device name, bitrate (96/160/320), device type, volume curve,
  normalisation and autoplay. The initial volume defaults to **100%**.
* **EQ**: 15 presets, and up to 32 filters (peaking, low/high shelf, low/high
  pass, notch) with a live frequency-response plot and auto-preamp.
  * Changes preview live and are kept in RAM until you click **Save EQ**.
  * **Import PEQ** accepts AutoEQ / REW / Equalizer APO text
    (`Preamp: -6 dB`, `Filter 1: ON PK Fc 100 Hz Gain 3 dB Q 1.41`).
  * Export produces the same format.
* **System**: the output card and sample format, an optional web UI password
  (HTTP basic auth), reboot, and system info.

## Pull-the-plug safe

* The root filesystem is mounted **read-only**.
* `/tmp` and `/run` (logs, librespot cache, now-playing data, live EQ) are tmpfs.
* Settings live on a small separate partition (`/data`). It stays read-only and
  is remounted read-write only for the instant a setting is saved: an atomic
  write with fsync, then back to read-only.
* No swap, no audio cache, no persistent logs.

You can cut power at any time, except during the second it takes to click Save.

## SSH (optional, off by default)

To enable SSH, put your **public key** in a file named `authorized_keys` on the
SD card's `BOOT` (FAT) partition, e.g. on your PC after flashing:

```sh
cp ~/.ssh/id_ed25519.pub /Volumes/BOOT/authorized_keys
```

On the next boot Dropbear listens on port 22: root login with keys only, no
passwords and no port forwarding. Without that file, no SSH server runs. The
host key is generated once and stored on `/data`. Useful commands:

```sh
ssh root@<ip>
tail -f /run/spotifypi/librespot.log   # also camilladsp.log, spotifypi.log
```

Logs live in RAM (also in the web UI under System → Logs) and vanish on reboot.

## Attack surface

The only listening services are:

* the web UI on port 80;
* librespot's Spotify Connect (zeroconf/mDNS) ports.

SSH only runs if you provide `authorized_keys` (see above); there is no telnet,
cron, or httpd/inetd. The kernel is trimmed: no
Bluetooth, NFC, CAN, media/V4L, NFS or CIFS. POST requests must be same-origin
JSON. Set a web UI password if your LAN is not trusted.

A login console is available on HDMI (tty1) and serial (ttyAMA0). The user is
`root` and the password is `spotifypi`. Change `BR2_TARGET_GENERIC_ROOT_PASSWD`
in the defconfig before building if that matters to you.

## Audio path

```
librespot --(S32_LE 44.1k)--> ALSA Loopback --> CamillaDSP (preamp + biquads) --> hw:sndrpihifiberry
```

librespot uses software volume. When the EQ is disabled, CamillaDSP passes the
audio through unchanged. Settings changes restart librespot. EQ changes reload
CamillaDSP with SIGHUP and do not interrupt playback.

### Other DACs

Edit `br2-external/board/spotifypi/config.txt` (`dtoverlay=hifiberry-dac`)
before building. You can also edit `config.txt` on the FAT boot partition of
a flashed card, for example `dtoverlay=hifiberry-dacplus`. Then select the card
in the web UI under System → Output.

## Building

You need Docker (on macOS, Docker Desktop with Rosetta). The build takes about
1–2 hours the first time.

```sh
./build.sh              # -> output/spotifypi-rpi2.img.xz
./build.sh menuconfig   # tweak the Buildroot config (saved back to output/)
./build.sh clean        # drop the cached build volume
```

**Behind a TLS-inspecting corporate proxy**: put the proxy's root/issuing CA
certificates (PEM, `*.crt`) in `docker/extra-ca/`. They are trusted inside the
builder for wget, git and cargo. Files in that directory are git-ignored.

CamillaDSP is built without its websocket server, so no control port is open.
`package/camilladsp/` carries a pinned `Cargo.lock` (upstream ships none) and a
small patch that fixes a startup panic in builds without websocket support.

### Layout

| Path | Purpose |
|---|---|
| `br2-external/configs/spotifypi_rpi2_defconfig` | Buildroot configuration |
| `br2-external/package/` | librespot, camilladsp, spotifypi packages |
| `br2-external/board/spotifypi/` | boot config, kernel/busybox fragments, partition layout, rootfs overlay |
| `webui/` | Go web UI and launcher (`go test ./...`) |
| `docker/`, `build.sh` | reproducible build container |
