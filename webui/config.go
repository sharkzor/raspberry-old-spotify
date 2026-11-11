package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"unicode"
)

type Config struct {
	Name          string `json:"name"`
	Bitrate       int    `json:"bitrate"`
	InitialVolume int    `json:"initial_volume"`
	VolumeCtrl    string `json:"volume_ctrl"`
	Normalisation bool   `json:"normalisation"`
	Autoplay      bool   `json:"autoplay"`
	DeviceType    string `json:"device_type"`

	OutputDevice string `json:"output_device"`
	OutputFormat string `json:"output_format"`

	EQ EQConfig `json:"eq"`

	PasswordHash string `json:"password_hash,omitempty"`
}

var (
	validBitrates    = []int{96, 160, 320}
	validVolumeCtrls = []string{"log", "cubic", "linear", "fixed"}
	validDeviceTypes = []string{"speaker", "avr", "audiodongle", "stb", "tv", "computer"}
	validFormats     = []string{"S32_LE", "S16_LE"}
	deviceRe         = regexp.MustCompile(`^[A-Za-z0-9_:=,.\-]{1,64}$`)
)

func defaultConfig() Config {
	return Config{
		Name:          "SpotifyPi",
		Bitrate:       320,
		InitialVolume: 100,
		VolumeCtrl:    "log",
		Autoplay:      true,
		DeviceType:    "speaker",
		OutputDevice:  "hw:CARD=sndrpihifiberry,DEV=0",
		OutputFormat:  "S32_LE",
		EQ:            EQConfig{Preset: "flat", AutoPreamp: true, Filters: []Filter{}},
	}
}

func contains[T comparable](list []T, v T) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (c *Config) Validate() error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len([]rune(c.Name)) > 64 {
		return errors.New("name must be 1-64 characters")
	}
	for _, r := range c.Name {
		if !unicode.IsPrint(r) {
			return errors.New("name contains invalid characters")
		}
	}
	if !contains(validBitrates, c.Bitrate) {
		return errors.New("bitrate must be 96, 160 or 320")
	}
	if c.InitialVolume < 0 || c.InitialVolume > 100 {
		return errors.New("initial volume must be 0-100")
	}
	if !contains(validVolumeCtrls, c.VolumeCtrl) {
		return errors.New("invalid volume control")
	}
	if !contains(validDeviceTypes, c.DeviceType) {
		return errors.New("invalid device type")
	}
	if !deviceRe.MatchString(c.OutputDevice) {
		return errors.New("invalid output device")
	}
	if !contains(validFormats, c.OutputFormat) {
		return errors.New("invalid output format")
	}
	return c.EQ.Validate()
}

var cfgMu sync.Mutex

// loadConfig never fails: a missing or broken file yields defaults so the
// device always comes up as a working Spotify endpoint.
func loadConfig() Config {
	cfg := defaultConfig()
	b, err := os.ReadFile(configPath())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("config: %v (using defaults)", err)
		}
		return cfg
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		log.Printf("config: %v (using defaults)", err)
		return defaultConfig()
	}
	if err := cfg.Validate(); err != nil {
		log.Printf("config: %v (using defaults)", err)
		def := defaultConfig()
		def.PasswordHash = cfg.PasswordHash
		return def
	}
	return cfg
}

// saveConfig persists the config with minimal, crash-safe SD card writes:
// /data is remounted rw only for the duration of an atomic write+rename.
func saveConfig(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	cfgMu.Lock()
	defer cfgMu.Unlock()

	if isMountpoint(dataDir) {
		if err := remount(dataDir, "rw"); err != nil {
			return fmt.Errorf("cannot make settings partition writable: %w", err)
		}
		defer func() {
			if err := remount(dataDir, "ro"); err != nil {
				log.Printf("remount ro: %v", err)
			}
		}()
	}
	return writeFileAtomic(configPath(), b, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

func isMountpoint(dir string) bool {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if fields := strings.Fields(s.Text()); len(fields) > 1 && fields[1] == dir {
			return true
		}
	}
	return false
}

func remount(dir, mode string) error {
	if mode == "ro" {
		syscall.Sync()
	}
	out, err := exec.Command("/bin/mount", "-o", "remount,"+mode, dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
