package main

import (
	"os"
	"path/filepath"
	"strings"
)

// variantFile is written at build time by the rootfs overlay: "librespot" or "soloist".
const variantFile = "/etc/spotifypi-variant"

// spotifyVariant is the Spotify Connect client built into this image. It is
// also the name of the supervised service process.
func spotifyVariant() string {
	if b, err := os.ReadFile(variantFile); err == nil && strings.TrimSpace(string(b)) == "soloist" {
		return "soloist"
	}
	return "librespot"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirNotEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

// Soloist's login session is saved on the settings partition (survives reboots)
// and runs from a tmpfs copy, so routine token refreshes never write the SD card.
func soloistKeyPath() string    { return filepath.Join(dataDir, "soloist-api-key.txt") }
func soloistPersistDir() string { return filepath.Join(dataDir, "soloist") }
func soloistRunDir() string     { return filepath.Join(runDir, "soloist") }
func soloistSessionDir() string { return filepath.Join(soloistRunDir(), "session") }
func soloistCacheDir() string   { return filepath.Join(soloistRunDir(), "cache") }
func soloistHomeDir() string    { return filepath.Join(soloistRunDir(), "home") }
func pairingFlagPath() string   { return filepath.Join(runDir, "soloist-pairing") }
func pulseRuntimeDir() string   { return filepath.Join(runDir, "pulse") }
func pulseStateDir() string     { return filepath.Join(runDir, "pulse-state") }
func pulseSocketPath() string   { return filepath.Join(pulseRuntimeDir(), "native") }
