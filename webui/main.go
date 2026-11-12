// spotifypi: web UI and process launcher for a read-only librespot endpoint.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var version = "dev"

const (
	librespotBin  = "/usr/bin/librespot"
	camilladspBin = "/usr/bin/camilladsp"
	eventHook     = "/usr/bin/spotifypi-event"
	// Playback side of the ALSA loopback (librespot) and its capture side (CamillaDSP).
	loopbackPlayback = "hw:Loopback,0,0"
	loopbackCapture  = "hw:Loopback,1,0"
)

// Paths can be overridden for development on a normal machine.
var (
	dataDir = envOr("SPOTIFYPI_DATA", "/data")
	runDir  = envOr("SPOTIFYPI_RUN", "/run/spotifypi")
	bootDir = envOr("SPOTIFYPI_BOOT", "/boot")
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func configPath() string     { return filepath.Join(dataDir, "spotifypi.json") }
func liveEQPath() string     { return filepath.Join(runDir, "eq-live.json") }
func camillaPath() string    { return filepath.Join(runDir, "camilladsp.yml") }
func nowPlayingPath() string { return filepath.Join(runDir, "nowplaying.json") }
func pidPath(name string) string {
	return filepath.Join(runDir, name+".pid")
}

func main() {
	if filepath.Base(os.Args[0]) == "spotifypi-event" {
		handleEvent()
		return
	}
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		addr := ":80"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		redirectLog("spotifypi")
		err = serve(addr)
	case "run-librespot":
		err = runLibrespot()
	case "run-camilladsp":
		err = runCamilla()
	case "event":
		handleEvent()
	case "render-camilladsp":
		var b []byte
		cfg := loadConfig()
		if b, err = renderCamilla(cfg, effectiveEQ(cfg)); err == nil {
			os.Stdout.Write(b)
		}
	case "librespot-args":
		fmt.Println(librespotArgs(loadConfig()))
	case "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("usage: spotifypi [serve [addr]|run-librespot|run-camilladsp|event|render-camilladsp|librespot-args|version]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "spotifypi:", err)
		os.Exit(1)
	}
}
