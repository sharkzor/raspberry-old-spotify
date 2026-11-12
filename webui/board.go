package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The Pi firmware picks the DAC device-tree overlay at boot from the FAT boot
// partition. config.txt contains "include dac.txt"; only that one-line file is
// ever rewritten, so a failed write cannot leave config.txt broken.
// All listed boards register the ALSA card "sndrpihifiberry".
type board struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Overlay string `json:"overlay"`
}

var boards = []board{
	{"pcm5102a", "PCM5102A DAC (HiFiBerry DAC and clones)", "hifiberry-dac"},
	{"pcm5122", "PCM5122 DAC+ (HiFiBerry DAC+ and clones)", "hifiberry-dacplus"},
	{"pcm5122-std", "PCM5122 DAC+ without onboard oscillators", "hifiberry-dacplus-std"},
}

var bootMu sync.Mutex

func dacFilePath() string { return filepath.Join(bootDir, "dac.txt") }

func boardByID(id string) (board, bool) {
	for _, b := range boards {
		if b.ID == id {
			return b, true
		}
	}
	return board{}, false
}

// currentBoard returns the id of the board configured in dac.txt ("" if unknown).
func currentBoard() string {
	b, err := os.ReadFile(dacFilePath())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "dtoverlay="); ok {
			v, _, _ = strings.Cut(v, ",")
			for _, bd := range boards {
				if bd.Overlay == v {
					return bd.ID
				}
			}
			return ""
		}
	}
	return ""
}

func setBoard(id string) error {
	bd, ok := boardByID(id)
	if !ok {
		return errors.New("unknown board")
	}
	bootMu.Lock()
	defer bootMu.Unlock()
	if isMountpoint(bootDir) {
		if err := remount(bootDir, "rw"); err != nil {
			return fmt.Errorf("cannot make boot partition writable: %w", err)
		}
		defer func() {
			if err := remount(bootDir, "ro"); err != nil {
				log.Printf("remount boot ro: %v", err)
			}
		}()
	}
	content := "# Written by the SpotifyPi web UI (System > DAC board).\ndtoverlay=" + bd.Overlay + "\n"
	return writeFileAtomic(dacFilePath(), []byte(content), 0o644)
}

func (s *server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"boards": boards, "current": currentBoard()})
}

func (s *server) handleSetBoard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := boardByID(req.ID); !ok {
		writeErr(w, http.StatusBadRequest, errors.New("unknown board"))
		return
	}
	if err := setBoard(req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"status": "saved, reboot to apply", "current": req.ID, "reboot_required": true})
}
