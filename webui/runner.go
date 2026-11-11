package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func librespotArgs(cfg Config) []string {
	args := []string{
		"--name", cfg.Name,
		"--bitrate", strconv.Itoa(cfg.Bitrate),
		"--backend", "alsa",
		"--device", loopbackPlayback,
		"--format", "S32",
		"--mixer", "softvol",
		"--initial-volume", strconv.Itoa(cfg.InitialVolume),
		"--volume-ctrl", cfg.VolumeCtrl,
		"--device-type", cfg.DeviceType,
		"--cache", runDir + "/cache", // tmpfs: never touches the SD card
		"--disable-audio-cache",
		"--onevent", eventHook,
	}
	if cfg.Autoplay {
		args = append(args, "--autoplay", "on")
	} else {
		args = append(args, "--autoplay", "off")
	}
	if cfg.Normalisation {
		args = append(args, "--enable-volume-normalisation")
	}
	return args
}

// waitFor polls cond until it is true or the timeout expires.
func waitFor(what string, timeout time.Duration, cond func() bool) {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			log.Printf("timeout waiting for %s, continuing", what)
			return
		}
		time.Sleep(time.Second)
	}
}

func hasIPv4() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			return true
		}
	}
	return false
}

func clockSane() bool { return time.Now().Year() >= 2025 }

func cardExists(id string) bool {
	_, err := os.Stat("/proc/asound/" + id)
	return err == nil
}

// cardID extracts the card name from "hw:CARD=xyz,DEV=0" / "hw:xyz,0".
func cardID(dev string) string {
	_, rest, ok := strings.Cut(dev, ":")
	if !ok {
		return ""
	}
	rest = strings.TrimPrefix(rest, "CARD=")
	id, _, _ := strings.Cut(rest, ",")
	return id
}

// execService replaces this process with the service (same PID), so init
// supervises the real binary and the PID file stays valid.
func execService(name, bin string, args []string) error {
	os.MkdirAll(runDir, 0o755)
	if err := os.WriteFile(pidPath(name), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	return syscall.Exec(bin, append([]string{bin}, args...), os.Environ())
}

func runLibrespot() error {
	time.Sleep(2 * time.Second) // throttle respawn loops
	waitFor("network", 60*time.Second, hasIPv4)
	waitFor("clock sync", 30*time.Second, clockSane)
	waitFor("loopback device", 20*time.Second, func() bool { return cardExists("Loopback") })
	cfg := loadConfig()
	os.MkdirAll(runDir+"/cache", 0o700)
	writeNowPlaying(map[string]any{"state": "idle"})
	return execService("librespot", librespotBin, librespotArgs(cfg))
}

func runCamilla() error {
	time.Sleep(time.Second)
	cfg := loadConfig()
	waitFor("loopback device", 30*time.Second, func() bool { return cardExists("Loopback") })
	if id := cardID(cfg.OutputDevice); id != "" {
		waitFor("sound card "+id, 30*time.Second, func() bool { return cardExists(id) })
	}
	b, err := renderCamilla(cfg, effectiveEQ(cfg))
	if err != nil {
		return err
	}
	if err := writeFileAtomic(camillaPath(), b, 0o644); err != nil {
		return err
	}
	// -g 0: main volume always 0 dB; no statefile and no websocket server.
	return execService("camilladsp", camilladspBin, []string{"-l", "warn", "-g", "0", camillaPath()})
}

// servicePID returns the PID from the pid file if that process is still the
// expected binary.
func servicePID(name string) int {
	b, err := os.ReadFile(pidPath(name))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return 0
	}
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil || strings.TrimSpace(string(comm)) != name {
		return 0
	}
	return pid
}

func signalService(name string, sig syscall.Signal) error {
	pid := servicePID(name)
	if pid == 0 {
		return nil // not running (yet); init will start it with the new config
	}
	return syscall.Kill(pid, sig)
}

// restartService stops the service; init respawns it with the new settings.
func restartService(name string) error { return signalService(name, syscall.SIGTERM) }

// reloadCamilla re-renders the DSP config and lets CamillaDSP reload it
// without interrupting playback.
func reloadCamilla(cfg Config) error {
	b, err := renderCamilla(cfg, effectiveEQ(cfg))
	if err != nil {
		return err
	}
	if err := writeFileAtomic(camillaPath(), b, 0o644); err != nil {
		return err
	}
	return signalService("camilladsp", syscall.SIGHUP)
}

// ---------------------------------------------------------------------------
// librespot --onevent hook: keeps a small "now playing" state file in tmpfs.

func readNowPlaying() map[string]any {
	st := map[string]any{}
	if b, err := os.ReadFile(nowPlayingPath()); err == nil {
		json.Unmarshal(b, &st)
	}
	return st
}

func writeNowPlaying(st map[string]any) {
	b, _ := json.Marshal(st)
	os.MkdirAll(runDir, 0o755)
	tmp := fmt.Sprintf("%s.%d", nowPlayingPath(), os.Getpid())
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, nowPlayingPath())
	}
}

func handleEvent() {
	ev := os.Getenv("PLAYER_EVENT")
	if ev == "" {
		return
	}
	os.MkdirAll(runDir, 0o755)
	lock, err := os.OpenFile(nowPlayingPath()+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err == nil {
		defer lock.Close()
		syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
	}
	st := readNowPlaying()
	env := os.Getenv
	switch ev {
	case "track_changed":
		st["name"] = env("NAME")
		st["artists"] = strings.Split(env("ARTISTS"), "\n")
		st["album"] = env("ALBUM")
		st["show"] = env("SHOW_NAME")
		st["uri"] = env("URI")
		if d, err := strconv.Atoi(env("DURATION_MS")); err == nil {
			st["duration_ms"] = d
		}
		if covers := strings.Split(env("COVERS"), "\n"); len(covers) > 0 && covers[0] != "" {
			st["cover"] = covers[0]
		} else {
			delete(st, "cover")
		}
	case "playing", "paused", "seeked", "position_correction":
		if ev != "seeked" && ev != "position_correction" {
			st["state"] = ev
		}
		if p, err := strconv.Atoi(env("POSITION_MS")); err == nil {
			st["position_ms"] = p
			st["position_at"] = time.Now().UnixMilli()
		}
	case "stopped", "end_of_track":
		if ev == "stopped" {
			st["state"] = "stopped"
		}
	case "session_connected":
		st["user"] = env("USER_NAME")
		st["client"] = env("CLIENT_NAME")
	case "session_disconnected":
		st = map[string]any{"state": "idle"}
	case "session_client_changed":
		st["client"] = env("CLIENT_NAME")
	case "volume_changed":
		if v, err := strconv.Atoi(env("VOLUME")); err == nil {
			st["volume"] = v * 100 / 65535
		}
	default:
		return
	}
	writeNowPlaying(st)
}
