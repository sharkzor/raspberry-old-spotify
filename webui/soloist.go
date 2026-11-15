package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// soloistArgs builds the long-running Soloist command line.
func soloistArgs(cfg Config, apiKey string) []string {
	return []string{
		"-n", cfg.Name,
		"-k", apiKey,
		"-i", strconv.Itoa(cfg.InitialVolume),
		"-D", soloistSessionDir(),
		"-C", soloistCacheDir(),
		"-z", "100",
	}
}

// soloistPairArgs is the same identity as soloistArgs, but for the one-time
// --pair flow, which exits once a controller has transferred playback.
func soloistPairArgs(cfg Config, apiKey string) []string {
	return append([]string{"--pair"}, soloistArgs(cfg, apiKey)...)
}

// soloistEnv keeps every write Soloist makes on the tmpfs; the rootfs is read-only.
func soloistEnv() []string {
	home := soloistHomeDir()
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"PULSE_SERVER=unix:" + pulseSocketPath(),
	}
}

func readSoloistKey() (string, error) {
	b, err := os.ReadFile(soloistKeyPath())
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", errors.New("api key file is empty")
	}
	return key, nil
}

// prepareSoloistDirs mounts a private tmpfs for Soloist's state so session
// refreshes and the audio cache never touch the SD card. Idempotent.
func prepareSoloistDirs() error {
	dir := soloistRunDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if !isMountpoint(dir) {
		if err := syscall.Mount("tmpfs", dir, "tmpfs", 0, "size=160m,mode=0700"); err != nil {
			return fmt.Errorf("mount soloist tmpfs: %w", err)
		}
	}
	home := soloistHomeDir()
	for _, d := range []string{
		soloistSessionDir(), soloistCacheDir(),
		filepath.Join(home, "config"), filepath.Join(home, "data"), filepath.Join(home, "cache"),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// restoreSoloistSession copies the saved login from the settings partition
// into the tmpfs session directory.
func restoreSoloistSession() error {
	if err := os.RemoveAll(soloistSessionDir()); err != nil {
		return err
	}
	if err := os.MkdirAll(soloistSessionDir(), 0o700); err != nil {
		return err
	}
	if !dirNotEmpty(soloistPersistDir()) {
		return nil
	}
	return copyTree(soloistPersistDir(), soloistSessionDir())
}

// copyTree copies regular files from src into dst, creating directories as needed.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return writeFileAtomic(target, b, 0o600)
	})
}

// runPulseaudio runs the system PulseAudio daemon that Soloist connects to.
// It drops to the pulse user itself; the sink is the ALSA loopback that CamillaDSP reads.
func runPulseaudio() error {
	redirectLog("pulseaudio")
	time.Sleep(time.Second)
	waitFor("loopback device", 30*time.Second, func() bool { return cardExists("Loopback") })
	u, err := user.Lookup("pulse")
	if err != nil {
		return fmt.Errorf("pulse user: %w", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	for _, d := range []string{pulseRuntimeDir(), pulseStateDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := os.Chown(d, uid, gid); err != nil {
			return err
		}
	}
	os.Setenv("PULSE_RUNTIME_PATH", pulseRuntimeDir())
	os.Setenv("PULSE_STATE_PATH", pulseStateDir())
	os.Setenv("HOME", pulseStateDir())
	return execService("pulseaudio", pulseBin, []string{
		"--system", "--daemonize=no", "--disallow-exit", "--exit-idle-time=-1",
		"-n", "-F", "/etc/pulse/spotifypi.pa",
	})
}

// runSoloist is the supervised Soloist service. It waits while a pairing run
// holds the session, then starts from the saved login.
func runSoloist() error {
	redirectLog("soloist")
	time.Sleep(2 * time.Second) // throttle respawn loops
	key, err := readSoloistKey()
	if err != nil {
		log.Printf("soloist: %v (set the API key in the web UI)", err)
		time.Sleep(30 * time.Second)
		return err
	}
	waitFor("pulseaudio", 60*time.Second, func() bool { return fileExists(pulseSocketPath()) })
	waitFor("network", 60*time.Second, hasIPv4)
	waitFor("clock sync", 30*time.Second, clockSane)
	waitFor("pairing to finish", 15*time.Minute, func() bool { return !fileExists(pairingFlagPath()) })

	cfg := loadConfig()
	if err := prepareSoloistDirs(); err != nil {
		return err
	}
	if err := restoreSoloistSession(); err != nil {
		return fmt.Errorf("restore soloist session: %w", err)
	}
	for _, kv := range soloistEnv() {
		k, v, _ := strings.Cut(kv, "=")
		os.Setenv(k, v)
	}
	return execService("soloist", soloistBin, soloistArgs(cfg, key))
}

// stopService asks a service to exit and waits until it is gone.
func stopService(name string) error {
	for i := 0; i < 50 && servicePID(name) != 0; i++ {
		restartService(name)
		time.Sleep(200 * time.Millisecond)
	}
	if servicePID(name) != 0 {
		return fmt.Errorf("%s did not stop", name)
	}
	return nil
}

// runPairing performs Soloist's one-time --pair flow and persists the resulting
// login to the settings partition. Callers must hold the pairing flag.
func runPairing(key string) error {
	cfg := loadConfig()
	if err := stopService("soloist"); err != nil {
		return err
	}
	if err := prepareSoloistDirs(); err != nil {
		return err
	}
	if err := os.RemoveAll(soloistSessionDir()); err != nil {
		return err
	}
	if err := os.MkdirAll(soloistSessionDir(), 0o700); err != nil {
		return err
	}

	logf, err := os.OpenFile(logPath("soloist"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	plog := log.New(logf, "", log.Ldate|log.Ltime)
	plog.Printf("soloist: starting pairing, open Spotify on another device and transfer playback to %q", cfg.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, soloistBin, soloistPairArgs(cfg, key)...)
	cmd.Env = append(os.Environ(), soloistEnv()...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("soloist --pair: %w", err)
	}
	if !dirNotEmpty(soloistSessionDir()) {
		return errors.New("pairing finished but produced no session")
	}
	return withDataRW(func() error {
		tmp := soloistPersistDir() + ".tmp"
		if err := os.RemoveAll(tmp); err != nil {
			return err
		}
		if err := copyTree(soloistSessionDir(), tmp); err != nil {
			return err
		}
		if err := os.RemoveAll(soloistPersistDir()); err != nil {
			return err
		}
		return os.Rename(tmp, soloistPersistDir())
	})
}

var (
	pairMu         sync.Mutex
	errPairingBusy = errors.New("pairing already in progress")
)

// startPairing creates the pairing flag and runs pairing in the background,
// because the HTTP write timeout is shorter than the pairing flow.
func startPairing(key string) error {
	pairMu.Lock()
	defer pairMu.Unlock()
	if fileExists(pairingFlagPath()) {
		return errPairingBusy
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(pairingFlagPath(), []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
		return err
	}
	go func() {
		defer os.Remove(pairingFlagPath())
		if err := runPairing(key); err != nil {
			log.Printf("soloist pairing failed: %v", err)
			return
		}
		log.Printf("soloist: pairing complete, session saved")
	}()
	return nil
}
